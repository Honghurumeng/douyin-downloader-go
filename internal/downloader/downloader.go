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
	VideoID           string   `json:"video_id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Author            string   `json:"author"`
	AuthorID          string   `json:"author_id"`
	VideoURI          string   `json:"video_uri"`
	VideoURL          string   `json:"video_url"`
	VideoDownloadURL  string   `json:"video_download_url"`
	WatermarkVideoURL string   `json:"watermark_video_url"`
	CoverURL          string   `json:"cover_url"`
	VideoWidth        int64    `json:"video_width"`
	VideoHeight       int64    `json:"video_height"`
	Duration          float64  `json:"duration"`
	CreateTime        int64    `json:"create_time"`
	LikeCount         int64    `json:"like_count"`
	CommentCount      int64    `json:"comment_count"`
	ShareCount        int64    `json:"share_count"`
	PlayCount         int64    `json:"play_count"`
	CollectCount      int64    `json:"collect_count"`
	ShareURL          string   `json:"share_url"`
	OriginalURL       string   `json:"original_url"`
	ContentType       string   `json:"content_type"`
	ImageURLs         []string `json:"image_urls"`
	ImageCount        int      `json:"image_count"`
}

type routerDataEnvelope struct {
	LoaderData map[string]json.RawMessage `json:"loaderData"`
	State      *struct {
		VideoDetail *awemeDetail `json:"videoDetail"`
		NoteDetail  *awemeDetail `json:"noteDetail"`
	} `json:"state"`
	AwemeDetail *awemeDetail `json:"awemeDetail"`
	NoteDetail  *awemeDetail `json:"noteDetail"`
}

type loaderEntry struct {
	VideoInfoRes *videoInfoResponse `json:"videoInfoRes"`
	NoteInfoRes  *videoInfoResponse `json:"noteInfoRes"`
	AwemeDetail  *awemeDetail       `json:"awemeDetail"`
	NoteDetail   *awemeDetail       `json:"noteDetail"`
	Detail       *awemeDetail       `json:"detail"`
}

type videoInfoResponse struct {
	ItemList []awemeDetail `json:"item_list"`
}

type awemeDetail struct {
	AwemeID    string         `json:"aweme_id"`
	Title      string         `json:"title"`
	Desc       string         `json:"desc"`
	CreateTime int64          `json:"create_time"`
	Author     authorInfo     `json:"author"`
	Video      videoInfo      `json:"video"`
	Images     []imageInfo    `json:"images"`
	Image      addressInfo    `json:"image"`
	Cover      addressInfo    `json:"cover"`
	Thumb      addressInfo    `json:"thumb"`
	ImagePost  imagePostInfo  `json:"image_post_info"`
	Statistics statisticsInfo `json:"statistics"`
}

type authorInfo struct {
	Nickname string `json:"nickname"`
	UniqueID string `json:"unique_id"`
	ShortID  string `json:"short_id"`
}

type videoInfo struct {
	PlayAddr      addressInfo   `json:"play_addr"`
	DownloadAddr  addressInfo   `json:"download_addr"`
	PlayAddrH264  addressInfo   `json:"play_addr_h264"`
	PlayAddrLowbr addressInfo   `json:"play_addr_lowbr"`
	Cover         addressInfo   `json:"cover"`
	BitRate       []bitRateInfo `json:"bit_rate"`
	Width         int64         `json:"width"`
	Height        int64         `json:"height"`
	Duration      int64         `json:"duration"`
}

// bitRateInfo 是接口返回的多档位清单，同一作品会有 540p/720p 若干条。
// 顶层 play_addr 给哪一档并不稳定（实测同一作品时而 720p 时而 540p），
// 所以按这里的尺寸自己挑。
type bitRateInfo struct {
	GearName    string      `json:"gear_name"`
	QualityType int         `json:"quality_type"`
	IsH265      int         `json:"is_h265"`
	IsByteVC1   int         `json:"is_bytevc1"`
	HDRType     string      `json:"HDR_type"`
	PlayAddr    addressInfo `json:"play_addr"`
}

type addressInfo struct {
	URI      string   `json:"uri"`
	URL      string   `json:"url"`
	URLList  []string `json:"url_list"`
	Width    int64    `json:"width"`
	Height   int64    `json:"height"`
	DataSize int64    `json:"data_size"`
}

type imagePostInfo struct {
	Images []imageInfo `json:"images"`
}

type imageInfo struct {
	URI                 string      `json:"uri"`
	URL                 string      `json:"url"`
	URLList             []string    `json:"url_list"`
	DownloadURLList     []string    `json:"download_url_list"`
	DisplayImage        addressInfo `json:"display_image"`
	OriginImage         addressInfo `json:"origin_image"`
	Image               addressInfo `json:"image"`
	Cover               addressInfo `json:"cover"`
	Thumbnail           addressInfo `json:"thumbnail"`
	Thumb               addressInfo `json:"thumb"`
	OwnerWatermarkImage addressInfo `json:"owner_watermark_image"`
	UserWatermarkImage  addressInfo `json:"user_watermark_image"`
	Width               int64       `json:"width"`
	Height              int64       `json:"height"`
}

type statisticsInfo struct {
	DiggCount    int64 `json:"digg_count"`
	CommentCount int64 `json:"comment_count"`
	ShareCount   int64 `json:"share_count"`
	PlayCount    int64 `json:"play_count"`
	CollectCount int64 `json:"collect_count"`
}

// 注意：抓取入口在 api.go（走 /aweme/v1/web/aweme/detail/ 接口）。
// 下面这些 HTML 解析函数是分享页时代留下的实现，现在只作为解析能力保留，
// 供测试与排查用，不再出现在主链路上。

// DownloadToFile 把远端的视频/图片/封面下载到本地，先写 .part 再改名，避免半截文件。
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

func parseContentDetail(htmlContent string) (*awemeDetail, error) {
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

	if detail := findDetailInRawJSON(jsonPayload); detail != nil {
		return detail, nil
	}

	return nil, errors.New("could not locate content detail in page data")
}

func findDetail(payload routerDataEnvelope) *awemeDetail {
	if isUsefulDetail(payload.AwemeDetail) {
		return payload.AwemeDetail
	}
	if isUsefulDetail(payload.NoteDetail) {
		return payload.NoteDetail
	}

	if payload.State != nil {
		if isUsefulDetail(payload.State.VideoDetail) {
			return payload.State.VideoDetail
		}
		if isUsefulDetail(payload.State.NoteDetail) {
			return payload.State.NoteDetail
		}
	}

	for _, raw := range payload.LoaderData {
		var entry loaderEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}

		switch {
		case isUsefulDetail(entry.AwemeDetail):
			return entry.AwemeDetail
		case isUsefulDetail(entry.NoteDetail):
			return entry.NoteDetail
		case isUsefulDetail(entry.Detail):
			return entry.Detail
		case entry.VideoInfoRes != nil && len(entry.VideoInfoRes.ItemList) > 0:
			return &entry.VideoInfoRes.ItemList[0]
		case entry.NoteInfoRes != nil && len(entry.NoteInfoRes.ItemList) > 0:
			return &entry.NoteInfoRes.ItemList[0]
		}
	}

	return nil
}

func findDetailInRawJSON(raw []byte) *awemeDetail {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}

	return findDetailInValue(value, 0)
}

func findDetailInValue(value any, depth int) *awemeDetail {
	if value == nil || depth > 12 {
		return nil
	}

	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"awemeDetail", "noteDetail", "videoDetail", "detail"} {
			if detail := detailFromValue(typed[key]); detail != nil {
				return detail
			}
		}

		for _, key := range []string{"item_list", "items"} {
			if detail := findDetailInValue(typed[key], depth+1); detail != nil {
				return detail
			}
		}

		if detail := detailFromValue(typed); detail != nil {
			return detail
		}

		for _, child := range typed {
			if detail := findDetailInValue(child, depth+1); detail != nil {
				return detail
			}
		}
	case []any:
		for _, child := range typed {
			if detail := findDetailInValue(child, depth+1); detail != nil {
				return detail
			}
		}
	}

	return nil
}

func detailFromValue(value any) *awemeDetail {
	if value == nil {
		return nil
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var detail awemeDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil
	}

	if isUsefulDetail(&detail) {
		return &detail
	}

	return nil
}

func isUsefulDetail(detail *awemeDetail) bool {
	if detail == nil {
		return false
	}

	if strings.TrimSpace(detail.AwemeID) != "" {
		return true
	}
	if len(detail.Images) > 0 || len(detail.ImagePost.Images) > 0 {
		return true
	}
	if firstNonEmptyURL(addressURLs(detail.Video.PlayAddr), addressURLs(detail.Video.DownloadAddr), addressURLs(detail.Image), addressURLs(detail.Cover), addressURLs(detail.Thumb)) != "" {
		return true
	}

	return false
}

func hasNoteImages(detail *awemeDetail) bool {
	return detail != nil && (len(detail.Images) > 0 || len(detail.ImagePost.Images) > 0)
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

func extractedContentFromDetail(detail *awemeDetail, contentType string) *ExtractedVideo {
	if contentType == "note" {
		return extractedNoteFromDetail(detail)
	}

	return extractedVideoFromDetail(detail)
}

func extractedVideoFromDetail(detail *awemeDetail) *ExtractedVideo {
	videoData := detail.Video
	best := selectBestVideoAddr(videoData)
	// 优先 play_addr：download_addr 是「保存到相册」版本，末尾会拼接三秒左右的水印片尾，
	// 实测 5.08 秒的作品从 download_addr 下下来是 8.10 秒，和正片对不上。
	candidateURL := firstNonEmptyURL(
		addressURLs(best),
		addressURLs(videoData.PlayAddrH264),
		addressURLs(videoData.PlayAddr),
		addressURLs(videoData.DownloadAddr),
	)

	watermarkURL := firstURL(addressURLs(videoData.PlayAddr))
	downloadURL := normalizeVideoURL(candidateURL)
	if downloadURL == "" {
		downloadURL = normalizeVideoURL(watermarkURL)
	}

	durationSeconds := float64(videoData.Duration)
	if durationSeconds > 1000 {
		durationSeconds = durationSeconds / 1000
	}

	// 尺寸以实际选中的档位为准，避免「元数据写 720p、文件其实是 540p」
	width, height := videoData.Width, videoData.Height
	if best.Width > 0 && best.Height > 0 {
		width, height = best.Width, best.Height
	}

	return &ExtractedVideo{
		VideoID:           detail.AwemeID,
		Title:             cleanText(firstNonEmpty(detail.Desc, detail.Title)),
		Description:       cleanText(firstNonEmpty(detail.Desc, detail.Title)),
		Author:            cleanText(detail.Author.Nickname),
		AuthorID:          resolveAuthorID(detail.Author),
		VideoURI:          firstNonEmpty(best.URI, videoData.DownloadAddr.URI, videoData.PlayAddr.URI, videoData.PlayAddrH264.URI, videoData.PlayAddrLowbr.URI),
		VideoURL:          downloadURL,
		VideoDownloadURL:  downloadURL,
		WatermarkVideoURL: watermarkURL,
		CoverURL:          firstNonEmptyURL(addressURLs(videoData.Cover), addressURLs(detail.Cover), addressURLs(detail.Image), addressURLs(detail.Thumb)),
		VideoWidth:        width,
		VideoHeight:       height,
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

// selectBestVideoAddr 从多档位里挑分辨率最高的一档（同分辨率取体积大的）。
//
// 会跳过 H.265/HEVC 和 HDR 档位：接口即使在请求里带了 support_h265=0 也照样返回
// 这些档位，而它们的浏览器兼容性差（Chrome 对 HEVC 支持受限）。实测同一作品里
// 720_1_1 这一档是 HEVC、normal_720_0 才是 H.264，前者体积更大但没法播。
//
// 没有任何可用档位时返回空值，交给调用方回退到顶层的 play_addr。
func selectBestVideoAddr(videoData videoInfo) addressInfo {
	var best addressInfo
	var bestPixels int64

	for _, rate := range videoData.BitRate {
		if len(addressURLs(rate.PlayAddr)) == 0 {
			continue
		}
		if rate.IsH265 != 0 || rate.IsByteVC1 != 0 || strings.TrimSpace(rate.HDRType) != "" {
			continue
		}

		pixels := rate.PlayAddr.Width * rate.PlayAddr.Height
		if pixels == 0 {
			continue
		}
		if pixels > bestPixels || (pixels == bestPixels && rate.PlayAddr.DataSize > best.DataSize) {
			best = rate.PlayAddr
			bestPixels = pixels
		}
	}

	return best
}

func extractedNoteFromDetail(detail *awemeDetail) *ExtractedVideo {
	imageURLs := imageURLsFromDetail(detail)
	width, height := firstImageDimensions(detail)

	return &ExtractedVideo{
		VideoID:      detail.AwemeID,
		Title:        cleanText(firstNonEmpty(detail.Desc, detail.Title)),
		Description:  cleanText(firstNonEmpty(detail.Desc, detail.Title)),
		Author:       cleanText(detail.Author.Nickname),
		AuthorID:     resolveAuthorID(detail.Author),
		CoverURL:     firstNonEmpty(firstURL(imageURLs), firstNonEmptyURL(addressURLs(detail.Cover), addressURLs(detail.Image), addressURLs(detail.Thumb))),
		VideoWidth:   width,
		VideoHeight:  height,
		Duration:     0,
		CreateTime:   detail.CreateTime,
		LikeCount:    detail.Statistics.DiggCount,
		CommentCount: detail.Statistics.CommentCount,
		ShareCount:   detail.Statistics.ShareCount,
		PlayCount:    detail.Statistics.PlayCount,
		CollectCount: detail.Statistics.CollectCount,
		ContentType:  "note",
		ImageURLs:    imageURLs,
		ImageCount:   len(imageURLs),
	}
}

func imageURLsFromDetail(detail *awemeDetail) []string {
	if detail == nil {
		return nil
	}

	imageURLs := make([]string, 0, len(detail.Images)+len(detail.ImagePost.Images))
	seen := make(map[string]bool)

	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		imageURLs = append(imageURLs, raw)
	}

	addImages := func(images []imageInfo) {
		for _, image := range images {
			add(imageURL(image))
		}
	}

	addImages(detail.Images)
	addImages(detail.ImagePost.Images)

	if len(imageURLs) == 0 {
		for _, address := range []addressInfo{detail.Image, detail.Cover, detail.Thumb, detail.Video.Cover} {
			add(firstURL(addressURLs(address)))
		}
	}

	return imageURLs
}

func imageURL(image imageInfo) string {
	groups := [][]string{
		addressURLs(image.OriginImage),
		image.DownloadURLList,
		addressURLs(image.DisplayImage),
		addressURLs(image.Image),
		[]string{image.URL},
		image.URLList,
		addressURLs(image.Cover),
		addressURLs(image.Thumbnail),
		addressURLs(image.Thumb),
		addressURLs(image.OwnerWatermarkImage),
		addressURLs(image.UserWatermarkImage),
	}

	fallback := ""
	for _, group := range groups {
		for _, raw := range group {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if fallback == "" {
				fallback = raw
			}
			if !looksWatermarkedImageURL(raw) {
				return raw
			}
		}
	}

	return fallback
}

func looksWatermarkedImageURL(raw string) bool {
	raw = strings.ToLower(raw)
	watermarkMarkers := []string{
		"new-water",
		"watermark",
		"water:",
		"owner_watermark",
		"user_watermark",
	}

	for _, marker := range watermarkMarkers {
		if strings.Contains(raw, marker) {
			return true
		}
	}

	return false
}

func firstImageDimensions(detail *awemeDetail) (int64, int64) {
	if detail == nil {
		return 0, 0
	}

	for _, images := range [][]imageInfo{detail.Images, detail.ImagePost.Images} {
		for _, image := range images {
			if image.Width > 0 || image.Height > 0 {
				return image.Width, image.Height
			}
		}
	}

	return 0, 0
}

func addressURLs(address addressInfo) []string {
	urls := make([]string, 0, len(address.URLList)+1)
	if strings.TrimSpace(address.URL) != "" {
		urls = append(urls, address.URL)
	}
	urls = append(urls, address.URLList...)
	return urls
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
