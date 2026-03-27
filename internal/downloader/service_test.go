package downloader

import "testing"

func TestSupportedService(t *testing.T) {
	tests := []struct {
		url      string
		want     string
		expected bool
	}{
		{"https://www.youtube.com/watch?v=1", "youtube", true},
		{"https://youtu.be/1", "youtube", true},
		{"https://www.instagram.com/reel/abc", "instagram", true},
		{"https://vm.tiktok.com/abc", "tiktok", true},
		{"https://pin.it/abc", "pinterest", true},
		{"https://example.com/file.mp4", "", false},
	}

	for _, tt := range tests {
		got, ok := SupportedService(tt.url)
		if ok != tt.expected || got != tt.want {
			t.Fatalf("SupportedService(%q) = (%q, %v), want (%q, %v)", tt.url, got, ok, tt.want, tt.expected)
		}
	}
}
