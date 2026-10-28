package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"douyin/internal/signer"
)

// DetailFetcher 负责取回作品详情接口的原始 JSON。
//
// 真实实现是 signer.Client（Node sidecar），测试里可以注入桩。
type DetailFetcher interface {
	Detail(ctx context.Context, awemeID string) ([]byte, error)
}

var (
	fetcherMu      sync.Mutex
	defaultFetcher DetailFetcher
)

// DefaultFetcher 返回进程级的详情获取器（第一次调用时创建，之后复用）。
func DefaultFetcher() DetailFetcher {
	fetcherMu.Lock()
	defer fetcherMu.Unlock()
	if defaultFetcher == nil {
		defaultFetcher = signer.New("", 0)
	}
	return defaultFetcher
}

// SetDefaultFetcher 替换默认获取器，主要供测试注入桩实现。
func SetDefaultFetcher(fetcher DetailFetcher) {
	fetcherMu.Lock()
	defaultFetcher = fetcher
	fetcherMu.Unlock()
}

// CloseDefaultFetcher 释放进程级获取器。真实实现会顺手结束 sidecar 子进程，
// 不调用也不会出问题（sidecar 空闲后自行退出），只是收得更及时。
func CloseDefaultFetcher() {
	fetcherMu.Lock()
	fetcher := defaultFetcher
	defaultFetcher = nil
	fetcherMu.Unlock()

	if closer, ok := fetcher.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// Extract 解析分享文案，取回作品详情并整理成可下载的元数据。
//
// 数据来源是 /aweme/v1/web/aweme/detail/ 接口而不是分享页 HTML：
// 分享页在未通过风控时会返回验证码中间页，而接口只需一次签名即可稳定拿到结构化的
// aweme_detail，字段比 RENDER_DATA 更全（含 bit_rate 多档位、完整 statistics）。
func Extract(ctx context.Context, input string) (*ExtractedVideo, error) {
	return ExtractWith(ctx, input, DefaultFetcher())
}

// ExtractWith 用指定的获取器执行抓取，方便测试。
func ExtractWith(ctx context.Context, input string, fetcher DetailFetcher) (*ExtractedVideo, error) {
	if fetcher == nil {
		return nil, errors.New("nil detail fetcher")
	}

	shareURL, err := extractShareURL(input)
	if err != nil {
		return nil, err
	}

	awemeID, originalURL, err := resolveAwemeID(ctx, shareURL)
	if err != nil {
		return nil, err
	}

	raw, err := fetcher.Detail(ctx, awemeID)
	if err != nil {
		return nil, fmt.Errorf("fetch aweme detail: %w", err)
	}

	detail, err := decodeAwemeDetail(raw, awemeID)
	if err != nil {
		return nil, err
	}

	contentType := "video"
	if hasNoteImages(detail) {
		contentType = "note"
	}

	video := extractedContentFromDetail(detail, contentType)
	video.VideoID = firstNonEmpty(video.VideoID, awemeID)
	video.ShareURL = shareURL
	video.OriginalURL = originalURL
	video.ContentType = contentType

	switch contentType {
	case "video":
		if video.VideoDownloadURL == "" {
			return nil, errors.New("missing downloadable video URL")
		}
	case "note":
		if len(video.ImageURLs) == 0 {
			return nil, errors.New("missing downloadable image URLs")
		}
	default:
		return nil, fmt.Errorf("unsupported content type %q", contentType)
	}

	return video, nil
}

type awemeDetailResponse struct {
	StatusCode  int          `json:"status_code"`
	StatusMsg   string       `json:"status_msg"`
	AwemeDetail *awemeDetail `json:"aweme_detail"`
}

func decodeAwemeDetail(raw []byte, awemeID string) (*awemeDetail, error) {
	var payload awemeDetailResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode aweme detail: %w", err)
	}

	if payload.StatusCode != 0 {
		message := strings.TrimSpace(payload.StatusMsg)
		if message == "" {
			message = "unknown error"
		}
		return nil, fmt.Errorf("douyin api status %d: %s", payload.StatusCode, message)
	}

	detail := payload.AwemeDetail
	if !isUsefulDetail(detail) {
		return nil, fmt.Errorf("aweme detail missing in response for %s", awemeID)
	}
	if strings.TrimSpace(detail.AwemeID) == "" {
		detail.AwemeID = awemeID
	}

	return detail, nil
}

// resolveAwemeID 把分享链接解析成作品 ID。
//
// 形如 www.douyin.com/video/123 的链接本身带 ID，直接取；v.douyin.com 短链需要跟一次
// 302（Location 里就是 iesdouyin 分享页地址，带 video/{id}）。这一步不需要签名。
func resolveAwemeID(ctx context.Context, shareURL string) (string, string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		// 自己跟跳转，好在每一跳上找 ID，也能避免被重定向到无关页面
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	current := shareURL
	for hop := 0; hop < 5; hop++ {
		if id := extractContentIDFromURL(current); id != "" {
			return id, current, nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", "", fmt.Errorf("build share link request: %w", err)
		}
		applyBrowserHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			return "", "", fmt.Errorf("follow share link: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()

		location := strings.TrimSpace(resp.Header.Get("Location"))
		if location == "" {
			break
		}

		next, err := req.URL.Parse(location)
		if err != nil {
			return "", "", fmt.Errorf("resolve redirect target: %w", err)
		}
		current = next.String()
	}

	return "", "", fmt.Errorf("could not resolve aweme id from %s", shareURL)
}
