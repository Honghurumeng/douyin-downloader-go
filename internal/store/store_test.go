package store

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"
)

func TestStoreTagsLifecycleAndFilters(t *testing.T) {
	root := t.TempDir()

	videoStore, err := New(filepath.Join(root, "videos.db"), filepath.Join(root, "videos.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() {
		_ = videoStore.Close()
	})

	for _, record := range []VideoRecord{
		testVideoRecord("video-1", 5),
		testVideoRecord("video-2", 0),
		testVideoRecord("video-3", 4),
	} {
		if _, err := videoStore.Upsert(record); err != nil {
			t.Fatalf("upsert %s: %v", record.VideoID, err)
		}
	}

	danceTag, err := videoStore.CreateTag("舞蹈")
	if err != nil {
		t.Fatalf("create dance tag: %v", err)
	}
	favoriteTag, err := videoStore.CreateTag("收藏")
	if err != nil {
		t.Fatalf("create favorite tag: %v", err)
	}
	materialTag, err := videoStore.CreateTag("素材")
	if err != nil {
		t.Fatalf("create material tag: %v", err)
	}

	if _, err := videoStore.SetVideoTags("video-1", []int64{danceTag.ID, favoriteTag.ID}); err != nil {
		t.Fatalf("set tags for video-1: %v", err)
	}
	if _, err := videoStore.SetVideoTags("video-2", []int64{favoriteTag.ID}); err != nil {
		t.Fatalf("set tags for video-2: %v", err)
	}
	if _, err := videoStore.SetVideoTags("video-3", []int64{danceTag.ID, materialTag.ID}); err != nil {
		t.Fatalf("set tags for video-3: %v", err)
	}

	if _, err := videoStore.SetVideoTags("video-1", []int64{danceTag.ID, 999999}); !errors.Is(err, ErrUnknownTag) {
		t.Fatalf("expected ErrUnknownTag, got %v", err)
	}

	tags, err := videoStore.ListTags()
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}

	tagCounts := make(map[string]int, len(tags))
	for _, tag := range tags {
		tagCounts[tag.Name] = tag.VideoCount
	}

	if tagCounts["舞蹈"] != 2 || tagCounts["收藏"] != 2 || tagCounts["素材"] != 1 {
		t.Fatalf("unexpected tag counts: %#v", tagCounts)
	}

	withDance, err := videoStore.List(ListFilter{TagIDs: []int64{danceTag.ID}})
	if err != nil {
		t.Fatalf("list videos with dance tag: %v", err)
	}
	assertVideoIDs(t, withDance, []string{"video-1", "video-3"})

	withDanceAndFavorite, err := videoStore.List(ListFilter{TagIDs: []int64{danceTag.ID, favoriteTag.ID}})
	if err != nil {
		t.Fatalf("list videos with dance and favorite tags: %v", err)
	}
	assertVideoIDs(t, withDanceAndFavorite, []string{"video-1"})
	if len(withDanceAndFavorite[0].Tags) != 2 {
		t.Fatalf("expected video-1 to keep both tags, got %#v", withDanceAndFavorite[0].Tags)
	}

	ratedFiveWithFavorite, err := videoStore.List(ListFilter{
		Mode:   "exact",
		Rating: 5,
		TagIDs: []int64{favoriteTag.ID},
	})
	if err != nil {
		t.Fatalf("list 5-star videos with favorite tag: %v", err)
	}
	assertVideoIDs(t, ratedFiveWithFavorite, []string{"video-1"})

	if err := videoStore.DeleteTag(favoriteTag.ID); err != nil {
		t.Fatalf("delete favorite tag: %v", err)
	}

	videoOne, err := videoStore.Get("video-1")
	if err != nil {
		t.Fatalf("get video-1 after tag delete: %v", err)
	}
	assertTagNames(t, videoOne.Tags, []string{"舞蹈"})

	videoTwo, err := videoStore.Get("video-2")
	if err != nil {
		t.Fatalf("get video-2 after tag delete: %v", err)
	}
	assertTagNames(t, videoTwo.Tags, nil)

	remainingTags, err := videoStore.ListTags()
	if err != nil {
		t.Fatalf("list tags after delete: %v", err)
	}
	assertTagNames(t, remainingTags, []string{"素材", "舞蹈"})
}

func testVideoRecord(id string, rating int) VideoRecord {
	return VideoRecord{
		ID:              id,
		VideoID:         id,
		Title:           "title-" + id,
		Description:     "description-" + id,
		Author:          "author-" + id,
		AuthorID:        "author-id-" + id,
		ShareURL:        "https://example.com/share/" + id,
		OriginalURL:     "https://example.com/original/" + id,
		ContentType:     "video/mp4",
		CoverURL:        "https://example.com/covers/" + id + ".jpg",
		VideoURI:        "video-uri-" + id,
		DownloadURL:     "https://example.com/download/" + id + ".mp4",
		WatermarkURL:    "https://example.com/watermark/" + id + ".mp4",
		Duration:        12,
		Rating:          rating,
		LocalFile:       "data/videos/" + id + ".mp4",
		LocalURL:        "/media/" + id + ".mp4",
		LastSourceInput: "source-" + id,
	}
}

func assertVideoIDs(t *testing.T, records []VideoRecord, want []string) {
	t.Helper()

	got := make([]string, 0, len(records))
	for _, record := range records {
		got = append(got, record.VideoID)
	}

	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("unexpected video count: got %v want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected video ids: got %v want %v", got, want)
		}
	}
}

func assertTagNames(t *testing.T, tags []Tag, want []string) {
	t.Helper()

	got := make([]string, 0, len(tags))
	for _, tag := range tags {
		got = append(got, tag.Name)
	}

	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("unexpected tag count: got %v want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected tag names: got %v want %v", got, want)
		}
	}
}
