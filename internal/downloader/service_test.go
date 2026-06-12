package downloader

import "testing"

func TestIsYouTube(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://www.youtube.com/watch?v=1", true},
		{"https://youtu.be/1", true},
		{"https://music.youtube.com/watch?v=1", true},
		{"https://example.com/file.mp4", false},
		{"not-a-url", false},
	}

	for _, tt := range tests {
		if got := IsYouTube(tt.url); got != tt.want {
			t.Fatalf("IsYouTube(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}
