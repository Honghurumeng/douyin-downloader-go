package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxHTMLBytes = 12 << 20

var (
	shareLinkPatterns = []*regexp.Regexp{
		regexp.MustCompile(`https://v\.douyin\.com/[A-Za-z0-9_-]+/`),
		regexp.MustCompile(`https://www\.douyin\.com/video/\d+`),
		regexp.MustCompile(`https://www\.douyin\.com/note/\d+`),
		regexp.MustCompile(`https://www\.iesdouyin\.com/share/video/\d+/`),
		regexp.MustCompile(`https://www\.iesdouyin\.com/share/note/\d+/`),
	}
	contentIDPatterns = []*regexp.Regexp{
		regexp.MustCompile(`/share/video/(\d+)/`),
		regexp.MustCompile(`/video/(\d+)`),
		regexp.MustCompile(`/share/note/(\d+)/`),
		regexp.MustCompile(`/note/(\d+)`),
		regexp.MustCompile(`aweme_id=(\d+)`),
		regexp.MustCompile(`item_ids=(\d+)`),
	}
	renderDataPattern = regexp.MustCompile(`(?s)<script id="RENDER_DATA" type="application/json">([^<]+)</script>`)
)

type ExtractedVideo struct {
	VideoID           string  `json:"video_id"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	Author            string  `json:"author"`
	AuthorID          string  `json:"author_id"`
	VideoURI          string  `json:"video_uri"`
	VideoURL          string  `json:"video_url"`
	VideoDownloadURL  string  `json:"video_download_url"`
	WatermarkVideoURL string  `json:"watermark_video_url"`
	CoverURL          string  `json:"cover_url"`
	VideoWidth        int64   `json:"video_width"`
	VideoHeight       int64   `json:"video_height"`
	Duration          float64 `json:"duration"`
	CreateTime        int64   `json:"create_time"`
	LikeCount         int64   `json:"like_count"`
	CommentCount      int64   `json:"comment_count"`
	ShareCount        int64   `json:"share_count"`
	PlayCount         int64   `json:"play_count"`
	CollectCount      int64   `json:"collect_count"`
	ShareURL          string  `json:"share_url"`
	OriginalURL       string  `json:"original_url"`
	ContentType       string  `json:"content_type"`
}

type routerDataEnvelope struct {
	LoaderData map[string]json.RawMessage `json:"loaderData"`
	State      *struct {
		VideoDetail *awemeDetail `json:"videoDetail"`
	} `json:"state"`
	AwemeDetail *awemeDetail `json:"awemeDetail"`
}

type loaderEntry struct {
	VideoInfoRes *videoInfoResponse `json:"videoInfoRes"`
	AwemeDetail  *awemeDetail       `json:"awemeDetail"`
	Detail       *awemeDetail       `json:"detail"`
}

type videoInfoResponse struct {
	ItemList []awemeDetail `json:"item_list"`
}

type awemeDetail struct {
	AwemeID    string         `json:"aweme_id"`
	Desc       string         `json:"desc"`
	CreateTime int64          `json:"create_time"`
	Author     authorInfo     `json:"author"`
	Video      videoInfo      `json:"video"`
	Statistics statisticsInfo `json:"statistics"`
}

type authorInfo struct {
	Nickname string `json:"nickname"`
	UniqueID string `json:"unique_id"`
	ShortID  string `json:"short_id"`
}

type videoInfo struct {
	PlayAddr      addressInfo `json:"play_addr"`
	DownloadAddr  addressInfo `json:"download_addr"`
	PlayAddrH264  addressInfo `json:"play_addr_h264"`
	PlayAddrLowbr addressInfo `json:"play_addr_lowbr"`
	Cover         addressInfo `json:"cover"`
	Width         int64       `json:"width"`
	Height        int64       `json:"height"`
	Duration      int64       `json:"duration"`
}

type addressInfo struct {
	URI     string   `json:"uri"`
	URLList []string `json:"url_list"`
}

type statisticsInfo struct {
	DiggCount    int64 `json:"digg_count"`
	CommentCount int64 `json:"comment_count"`
	ShareCount   int64 `json:"share_count"`
	PlayCount    int64 `json:"play_count"`
	CollectCount int64 `json:"collect_count"`
}

func Extract(ctx context.Context, input string) (*ExtractedVideo, error) {
	shareURL, err := extractShareURL(input)
	if err != nil {
		return nil, err
	}

	finalURL, htmlContent, err := fetchSharePage(ctx, shareURL)
	if err != nil {
		return nil, err
	}

	contentType := identifyContentType(finalURL)
	if contentType != "video" {
		return nil, fmt.Errorf("unsupported content type %q", contentType)
	}

	contentID := extractContentIDFromURL(finalURL)
	if contentID == "" {
		return nil, fmt.Errorf("could not extract content id from %s", finalURL)
	}

	detail, err := parseVideoDetail(htmlContent)
	if err != nil {
		return nil, err
	}

	video := extractedVideoFromDetail(detail)
	video.VideoID = firstNonEmpty(video.VideoID, contentID)
	video.ShareURL = shareURL
	video.OriginalURL = finalURL
	video.ContentType = contentType

	if video.VideoDownloadURL == "" {
		return nil, errors.New("missing downloadable video URL")
	}

	return video, nil
}

func DownloadToFile(ctx context.Context, sourceURL, destinationPath string) (int64, error) {
	if sourceURL == "" {
		return 0, errors.New("empty source url")
	}

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return 0, fmt.Errorf("create video directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build download request: %w", err)
	}

	applyBrowserHeaders(req)
	req.Header.Set("Referer", "https://www.douyin.com/")

	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download video: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected download status: %s", resp.Status)
	}

	tempPath := destinationPath + ".part"
	file, err := os.Create(tempPath)
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}

	written, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return written, fmt.Errorf("write video file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return written, fmt.Errorf("close temp file: %w", closeErr)
	}

	if err := os.Rename(tempPath, destinationPath); err != nil {
		_ = os.Remove(tempPath)
		return written, fmt.Errorf("move temp file: %w", err)
	}

	return written, nil
}

func fetchSharePage(ctx context.Context, shareURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shareURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build share page request: %w", err)
	}

	applyBrowserHeaders(req)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch share page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected share page status: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes))
	if err != nil {
		return "", "", fmt.Errorf("read share page body: %w", err)
	}

	return resp.Request.URL.String(), string(body), nil
}

func parseVideoDetail(htmlContent string) (*awemeDetail, error) {
	jsonPayload, err := extractRouterJSON(htmlContent)
	if err != nil {
		return nil, err
	}

	var payload routerDataEnvelope
	if err := json.Unmarshal(jsonPayload, &payload); err != nil {
		return nil, fmt.Errorf("decode page data: %w", err)
	}

	if detail := findDetail(payload); detail != nil {
		return detail, nil
	}

	return nil, errors.New("could not locate video detail in page data")
}

func findDetail(payload routerDataEnvelope) *awemeDetail {
	if payload.AwemeDetail != nil {
		return payload.AwemeDetail
	}

	if payload.State != nil && payload.State.VideoDetail != nil {
		return payload.State.VideoDetail
	}

	for _, raw := range payload.LoaderData {
		var entry loaderEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}

		switch {
		case entry.AwemeDetail != nil:
			return entry.AwemeDetail
		case entry.Detail != nil:
			return entry.Detail
		case entry.VideoInfoRes != nil && len(entry.VideoInfoRes.ItemList) > 0:
			return &entry.VideoInfoRes.ItemList[0]
		}
	}

	return nil
}

func extractRouterJSON(htmlContent string) ([]byte, error) {
	markers := []string{
		"window._ROUTER_DATA",
		"window.__INITIAL_STATE__",
		"window.__SSR_DATA__",
		"window.__NUXT__",
	}

	for _, marker := range markers {
		if jsonText, err := extractAssignedJSONObject(htmlContent, marker); err == nil {
			return []byte(jsonText), nil
		}
	}

	if matches := renderDataPattern.FindStringSubmatch(htmlContent); len(matches) == 2 {
		decoded, err := url.QueryUnescape(matches[1])
		if err == nil {
			return []byte(decoded), nil
		}
	}

	return nil, errors.New("could not extract structured page data")
}

func extractAssignedJSONObject(content, marker string) (string, error) {
	idx := strings.Index(content, marker)
	if idx < 0 {
		return "", fmt.Errorf("marker %s not found", marker)
	}

	assignment := content[idx:]
	start := strings.IndexByte(assignment, '{')
	if start < 0 {
		return "", fmt.Errorf("marker %s missing JSON start", marker)
	}

	start += idx
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(content); i++ {
		ch := content[i]

		if inString {
			if escaped {
				escaped = false
				continue
			}

			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : i+1], nil
			}
		}
	}

	return "", fmt.Errorf("marker %s JSON was not terminated", marker)
}

func extractedVideoFromDetail(detail *awemeDetail) *ExtractedVideo {
	videoData := detail.Video
	candidateURL := firstNonEmptyURL(
		videoData.DownloadAddr.URLList,
		videoData.PlayAddrH264.URLList,
		videoData.PlayAddrLowbr.URLList,
		videoData.PlayAddr.URLList,
	)

	watermarkURL := firstURL(videoData.PlayAddr.URLList)
	downloadURL := normalizeVideoURL(candidateURL)
	if downloadURL == "" {
		downloadURL = normalizeVideoURL(watermarkURL)
	}

	durationSeconds := float64(videoData.Duration)
	if durationSeconds > 1000 {
		durationSeconds = durationSeconds / 1000
	}

	return &ExtractedVideo{
		VideoID:           detail.AwemeID,
		Title:             cleanText(detail.Desc),
		Description:       cleanText(detail.Desc),
		Author:            cleanText(detail.Author.Nickname),
		AuthorID:          resolveAuthorID(detail.Author),
		VideoURI:          firstNonEmpty(videoData.DownloadAddr.URI, videoData.PlayAddr.URI, videoData.PlayAddrH264.URI, videoData.PlayAddrLowbr.URI),
		VideoURL:          downloadURL,
		VideoDownloadURL:  downloadURL,
		WatermarkVideoURL: watermarkURL,
		CoverURL:          firstURL(videoData.Cover.URLList),
		VideoWidth:        videoData.Width,
		VideoHeight:       videoData.Height,
		Duration:          durationSeconds,
		CreateTime:        detail.CreateTime,
		LikeCount:         detail.Statistics.DiggCount,
		CommentCount:      detail.Statistics.CommentCount,
		ShareCount:        detail.Statistics.ShareCount,
		PlayCount:         detail.Statistics.PlayCount,
		CollectCount:      detail.Statistics.CollectCount,
		ContentType:       "video",
	}
}

func resolveAuthorID(author authorInfo) string {
	return cleanText(firstNonEmpty(author.UniqueID, author.ShortID))
}

func extractShareURL(input string) (string, error) {
	for _, pattern := range shareLinkPatterns {
		if match := pattern.FindString(input); match != "" {
			return match, nil
		}
	}

	if isValidShareURL(input) {
		return strings.TrimSpace(input), nil
	}

	return "", errors.New("no valid douyin share link found")
}

func isValidShareURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	switch parsed.Hostname() {
	case "v.douyin.com", "www.douyin.com", "www.iesdouyin.com":
		return parsed.Path != ""
	default:
		return false
	}
}

func identifyContentType(raw string) string {
	switch {
	case strings.Contains(raw, "/share/note/"), strings.Contains(raw, "/note/"):
		return "note"
	default:
		return "video"
	}
}

func extractContentIDFromURL(raw string) string {
	for _, pattern := range contentIDPatterns {
		if matches := pattern.FindStringSubmatch(raw); len(matches) == 2 {
			return matches[1]
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	for _, key := range []string{"aweme_id", "item_ids"} {
		if value := parsed.Query().Get(key); value != "" {
			return value
		}
	}

	return ""
}

func normalizeVideoURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}

	if strings.Contains(parsed.Path, "/playwm/") {
		parsed.Path = strings.Replace(parsed.Path, "/playwm/", "/play/", 1)
		query := parsed.Query()
		query.Del("logo_name")
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}

func applyBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

func cleanText(input string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func firstURL(values []string) string {
	if len(values) == 0 {
		return ""
	}

	return firstNonEmpty(values...)
}

func firstNonEmptyURL(candidates ...[]string) string {
	for _, urls := range candidates {
		if value := firstURL(urls); value != "" {
			return value
		}
	}

	return ""
}
