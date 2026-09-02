package ytdlp

import (
	"testing"
)

func TestParseHeight(t *testing.T) {
	cases := map[string]int{
		"1080p":        1080,
		"1080p 60fps":  1080,
		"720p":         720,
		"best":         0,
		"480p 30fps":   480,
		"not-a-height": 0,
	}
	for in, want := range cases {
		if got := parseHeight(in); got != want {
			t.Errorf("parseHeight(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestLastNonEmptyLine(t *testing.T) {
	if got := lastNonEmptyLine("a\nb\n\n"); got != "b" {
		t.Errorf("lastNonEmptyLine = %q", got)
	}
	if got := lastNonEmptyLine(""); got != "" {
		t.Errorf("lastNonEmptyLine empty = %q", got)
	}
}

func TestNewClientDefault(t *testing.T) {
	c := NewClient("")
	if c.bin != "yt-dlp" {
		t.Errorf("default bin = %q", c.bin)
	}
}
