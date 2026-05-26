package downloader

import "testing"

func TestParseContentDetailExtractsNoteImages(t *testing.T) {
	html := `<html><script>window._ROUTER_DATA = {"loaderData":{"note_(id)/page":{"noteInfoRes":{"item_list":[{"aweme_id":"7400123456789","desc":"图集标题","create_time":1710000000,"author":{"nickname":"作者","unique_id":"author_id"},"images":[{"url_list":["https://example.com/image-1.webp"],"width":1080,"height":1440},{"url_list":["https://example.com/image-2.webp"]}],"statistics":{"digg_count":12,"comment_count":3,"share_count":4,"collect_count":5}}]}}}};</script></html>`

	detail, err := parseContentDetail(html)
	if err != nil {
		t.Fatalf("parseContentDetail() error = %v", err)
	}

	extracted := extractedContentFromDetail(detail, "note")
	if extracted.VideoID != "7400123456789" {
		t.Fatalf("VideoID = %q, want %q", extracted.VideoID, "7400123456789")
	}
	if extracted.ContentType != "note" {
		t.Fatalf("ContentType = %q, want note", extracted.ContentType)
	}
	if extracted.Title != "图集标题" || extracted.Author != "作者" {
		t.Fatalf("unexpected title/author: %#v", extracted)
	}
	if extracted.ImageCount != 2 || len(extracted.ImageURLs) != 2 {
		t.Fatalf("unexpected image urls: %#v", extracted.ImageURLs)
	}
	if extracted.CoverURL != "https://example.com/image-1.webp" {
		t.Fatalf("CoverURL = %q", extracted.CoverURL)
	}
	if extracted.VideoWidth != 1080 || extracted.VideoHeight != 1440 {
		t.Fatalf("unexpected dimensions: %dx%d", extracted.VideoWidth, extracted.VideoHeight)
	}
	if extracted.LikeCount != 12 || extracted.CollectCount != 5 {
		t.Fatalf("unexpected stats: %#v", extracted)
	}
}

func TestResolveAuthorID(t *testing.T) {
	testCases := []struct {
		name   string
		author authorInfo
		want   string
	}{
		{
			name:   "prefer unique id",
			author: authorInfo{UniqueID: "douyin_user", ShortID: "123456"},
			want:   "douyin_user",
		},
		{
			name:   "fallback to short id",
			author: authorInfo{ShortID: "123456"},
			want:   "123456",
		},
		{
			name:   "trim whitespace",
			author: authorInfo{UniqueID: "  douyin_user  "},
			want:   "douyin_user",
		},
		{
			name:   "empty when missing",
			author: authorInfo{},
			want:   "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := resolveAuthorID(testCase.author); got != testCase.want {
				t.Fatalf("resolveAuthorID() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestImageURLPrefersNonWatermarkedCandidates(t *testing.T) {
	image := imageInfo{
		DownloadURLList: []string{
			"https://example.com/tplv-dy-lqen-new-water:1440:2104:q80.webp",
		},
		URLList: []string{
			"https://example.com/tplv-dy-lqen-new-water:1440:2104:q80.webp",
			"https://example.com/clean-from-url-list.webp",
		},
		OriginImage: addressInfo{
			URLList: []string{"https://example.com/origin.webp"},
		},
		OwnerWatermarkImage: addressInfo{
			URLList: []string{"https://example.com/owner_watermark.webp"},
		},
	}

	if got := imageURL(image); got != "https://example.com/origin.webp" {
		t.Fatalf("imageURL() = %q, want origin image", got)
	}

	image.OriginImage = addressInfo{}
	if got := imageURL(image); got != "https://example.com/clean-from-url-list.webp" {
		t.Fatalf("imageURL() = %q, want clean url_list fallback", got)
	}
}
