package downloader

import "testing"

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
