package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeYtDlp creates a shell script that emulates the yt-dlp CLI for the
// -J (info) and download invocations used by this package.
func writeFakeYtDlp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "yt-dlp")
	content := `#!/bin/sh
for a in "$@"; do
  case "$a" in
    -J)
      cat <<'EOF'
{"title":"Test Video","web_page_url":"https://example.com/v","duration":120,
 "formats":[
   {"format_id":"18","height":360,"fps":30,"vcodec":"avc1"},
   {"format_id":"22","height":720,"fps":30,"vcodec":"avc1"},
   {"format_id":"137","height":1080,"fps":60,"vcodec":"avc1"},
   {"format_id":"140","height":null,"fps":null,"vcodec":"none"}
 ]}
EOF
      exit 0
      ;;
  esac
done
# Download mode: emit a final output path line.
echo "https://example.com/v"
exit 0
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return script
}

func TestInfoWithFakeBinary(t *testing.T) {
	bin := writeFakeYtDlp(t)
	c := NewClient(bin)
	if !c.Available() {
		t.Fatal("fake binary should be available")
	}
	info, err := c.Info(context.Background(), "https://example.com/v")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Title != "Test Video" || info.WebPageURL != "https://example.com/v" || info.DurationSec != 120 {
		t.Errorf("info = %+v", info)
	}
	// Audio-only (vcodec none) and null-height formats should be filtered out;
	// the three video formats should be present, sorted ascending by height.
	if len(info.Formats) != 3 {
		t.Fatalf("formats = %d, want 3: %+v", len(info.Formats), info.Formats)
	}
	if info.Formats[0].Height != 360 || info.Formats[2].Height != 1080 {
		t.Errorf("format order wrong: %+v", info.Formats)
	}
	// 1080p at 60fps should carry the fps suffix in its label.
	if info.Formats[2].Label != "1080p 60fps" {
		t.Errorf("1080p label = %q, want %q", info.Formats[2].Label, "1080p 60fps")
	}
}

func TestDownloadWithFakeBinary(t *testing.T) {
	bin := writeFakeYtDlp(t)
	c := NewClient(bin)
	out, err := c.Download(context.Background(), "https://example.com/v", "720p", t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if out == "" {
		t.Error("Download returned empty path")
	}
}

func TestDownloadBestQuality(t *testing.T) {
	bin := writeFakeYtDlp(t)
	c := NewClient(bin)
	if _, err := c.Download(context.Background(), "https://example.com/v", "best", t.TempDir()); err != nil {
		t.Fatalf("Download(best): %v", err)
	}
}

func TestAvailableMissingBinary(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "does-not-exist"))
	if c.Available() {
		t.Error("Available() = true for missing binary")
	}
}
