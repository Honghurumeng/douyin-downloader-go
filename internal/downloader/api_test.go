package downloader

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type stubFetcher struct {
	raw   string
	err   error
	calls []string
}

func (s *stubFetcher) Detail(_ context.Context, awemeID string) ([]byte, error) {
	s.calls = append(s.calls, awemeID)
	if s.err != nil {
		return nil, s.err
	}
	return []byte(s.raw), nil
}

const sampleVideoDetailJSON = `{
  "status_code": 0,
  "aweme_detail": {
    "aweme_id": "7700822507755451647",
    "desc": "测试视频  #标签",
    "create_time": 1729000000,
    "author": {"nickname": "尧尧吖", "unique_id": "yaoyao", "short_id": "123"},
    "video": {
      "width": 576,
      "height": 1024,
      "duration": 5080,
      "play_addr": {"uri": "v0d00fg1", "url_list": ["https://example.com/playwm/video.mp4?logo_name=1"]},
      "download_addr": {"uri": "v0d00fg1", "url_list": ["https://example.com/aweme/video.mp4"]},
      "cover": {"url_list": ["https://example.com/cover.jpg"]},
      "bit_rate": [
        {"gear_name": "normal_540_0", "quality_type": 20, "is_h265": 0,
         "play_addr": {"uri": "v540", "url_list": ["https://example.com/540.mp4"], "width": 576, "height": 1024, "data_size": 448992}},
        {"gear_name": "720_1_1", "quality_type": 11, "is_h265": 1, "is_bytevc1": 1,
         "play_addr": {"uri": "v720hevc", "url_list": ["https://example.com/720-hevc.mp4"], "width": 720, "height": 1280, "data_size": 433679}},
        {"gear_name": "normal_720_0", "quality_type": 10, "is_h265": 0,
         "play_addr": {"uri": "v720", "url_list": ["https://example.com/720.mp4"], "width": 720, "height": 1280, "data_size": 421935}}
      ]
    },
    "statistics": {"digg_count": 1000, "comment_count": 20, "share_count": 3, "play_count": 50000, "collect_count": 8}
  }
}`

const sampleNoteDetailJSON = `{
  "status_code": 0,
  "aweme_detail": {
    "aweme_id": "7500000000000000001",
    "desc": "图集作品",
    "create_time": 1729000000,
    "author": {"nickname": "图集作者", "unique_id": "note_author", "short_id": "456"},
    "images": [
      {"uri": "img1", "url_list": ["https://example.com/a-1.jpg"], "width": 1080, "height": 1440}
    ]
  }
}`

func TestExtractWithMapsVideoDetail(t *testing.T) {
	fetcher := &stubFetcher{raw: sampleVideoDetailJSON}

	got, err := ExtractWith(context.Background(), "https://www.douyin.com/video/7700822507755451647", fetcher)
	if err != nil {
		t.Fatalf("ExtractWith: %v", err)
	}

	if len(fetcher.calls) != 1 || fetcher.calls[0] != "7700822507755451647" {
		t.Fatalf("unexpected fetcher calls: %v", fetcher.calls)
	}
	if got.VideoID != "7700822507755451647" {
		t.Errorf("VideoID = %q", got.VideoID)
	}
	if got.ContentType != "video" {
		t.Errorf("ContentType = %q, want video", got.ContentType)
	}
	if got.Author != "尧尧吖" {
		t.Errorf("Author = %q", got.Author)
	}
	if got.AuthorID != "yaoyao" {
		t.Errorf("AuthorID = %q", got.AuthorID)
	}
	// 720p 里 HEVC 那档体积更大但兼容性差，要跳过它选 H.264 的 720p；
	// download_addr（/aweme/video.mp4）是有片尾的版本，不该被选中
	if got.VideoDownloadURL != "https://example.com/720.mp4" {
		t.Errorf("VideoDownloadURL = %q", got.VideoDownloadURL)
	}
	if got.CoverURL != "https://example.com/cover.jpg" {
		t.Errorf("CoverURL = %q", got.CoverURL)
	}
	// 尺寸要跟着选中的档位走，而不是顶层 video.width
	if got.VideoWidth != 720 || got.VideoHeight != 1280 {
		t.Errorf("dimensions = %dx%d", got.VideoWidth, got.VideoHeight)
	}
	if got.Duration < 5.07 || got.Duration > 5.09 {
		t.Errorf("Duration = %v, want ~5.08", got.Duration)
	}
	if got.LikeCount != 1000 || got.PlayCount != 50000 {
		t.Errorf("statistics = %d/%d", got.LikeCount, got.PlayCount)
	}
	if got.ShareURL == "" || got.OriginalURL == "" {
		t.Errorf("share/original url missing: %q %q", got.ShareURL, got.OriginalURL)
	}
}

func TestSelectBestVideoAddrFallsBackWhenNoBitrate(t *testing.T) {
	video := videoInfo{
		PlayAddr: addressInfo{URLList: []string{"https://example.com/fallback.mp4"}},
		Width:    640,
		Height:   360,
		BitRate:  []bitRateInfo{{GearName: "broken", PlayAddr: addressInfo{URLList: []string{"https://example.com/x.mp4"}}}},
	}

	if best := selectBestVideoAddr(video); len(addressURLs(best)) != 0 {
		t.Errorf("expected empty selection when no bitrate has dimensions, got %+v", best)
	}

	// 走回退路径时尺寸应保留顶层值
	got := extractedVideoFromDetail(&awemeDetail{AwemeID: "1", Video: video})
	if got.VideoDownloadURL != "https://example.com/fallback.mp4" {
		t.Errorf("VideoDownloadURL = %q", got.VideoDownloadURL)
	}
	if got.VideoWidth != 640 || got.VideoHeight != 360 {
		t.Errorf("dimensions = %dx%d", got.VideoWidth, got.VideoHeight)
	}
}

func TestExtractWithMapsNoteDetail(t *testing.T) {
	fetcher := &stubFetcher{raw: sampleNoteDetailJSON}

	got, err := ExtractWith(context.Background(), "https://www.douyin.com/note/7500000000000000001", fetcher)
	if err != nil {
		t.Fatalf("ExtractWith: %v", err)
	}

	if got.ContentType != "note" {
		t.Errorf("ContentType = %q, want note", got.ContentType)
	}
	if got.ImageCount != 1 || len(got.ImageURLs) != 1 {
		t.Fatalf("ImageCount = %d, urls = %v", got.ImageCount, got.ImageURLs)
	}
	if got.ImageURLs[0] != "https://example.com/a-1.jpg" {
		t.Errorf("ImageURLs[0] = %q", got.ImageURLs[0])
	}
	if got.CoverURL != "https://example.com/a-1.jpg" {
		t.Errorf("CoverURL = %q", got.CoverURL)
	}
}

func TestExtractWithRejectsAPIError(t *testing.T) {
	fetcher := &stubFetcher{raw: `{"status_code": 2096, "status_msg": "invalid signature", "aweme_detail": null}`}

	_, err := ExtractWith(context.Background(), "https://www.douyin.com/video/1", fetcher)
	if err == nil {
		t.Fatal("expected error for non-zero status_code")
	}
	if !strings.Contains(err.Error(), "invalid signature") {
		t.Errorf("error should surface status_msg, got %v", err)
	}
}

func TestExtractWithRejectsMissingDetail(t *testing.T) {
	fetcher := &stubFetcher{raw: `{"status_code": 0, "aweme_detail": null}`}

	if _, err := ExtractWith(context.Background(), "https://www.douyin.com/video/1", fetcher); err == nil {
		t.Fatal("expected error for missing aweme_detail")
	}
}

func TestExtractWithPropagatesFetcherError(t *testing.T) {
	fetcher := &stubFetcher{err: errors.New("sidecar down")}

	_, err := ExtractWith(context.Background(), "https://www.douyin.com/video/1", fetcher)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "sidecar down") {
		t.Errorf("error should wrap fetcher failure, got %v", err)
	}
}

func TestExtractWithRejectsInvalidInput(t *testing.T) {
	if _, err := ExtractWith(context.Background(), "随便一段没有链接的文案", &stubFetcher{}); err == nil {
		t.Fatal("expected error for input without share link")
	}
}

// TestExtractLive 会真的拉起 sidecar 打抖音接口，默认跳过。
//
//	DOUYIN_LIVE=1 go test ./internal/downloader -run TestExtractLive -v
func TestExtractLive(t *testing.T) {
	if os.Getenv("DOUYIN_LIVE") != "1" {
		t.Skip("set DOUYIN_LIVE=1 to hit the real API")
	}

	shareText := os.Getenv("DOUYIN_LIVE_INPUT")
	if shareText == "" {
		shareText = "https://v.douyin.com/VrCY7XYxoGw/"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	started := time.Now()
	got, err := Extract(ctx, shareText)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	t.Logf("耗时 %s | id=%s type=%s author=%s size=%dx%d duration=%.2fs",
		time.Since(started).Round(time.Millisecond), got.VideoID, got.ContentType,
		got.Author, got.VideoWidth, got.VideoHeight, got.Duration)
	t.Logf("播放地址: %s", got.VideoDownloadURL)

	if got.VideoID == "" {
		t.Error("empty VideoID")
	}
	if got.ContentType == "video" && got.VideoDownloadURL == "" {
		t.Error("empty VideoDownloadURL")
	}
}
