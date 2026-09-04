package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeYtDlpScript writes a custom fake yt-dlp script.
func writeFakeYtDlpScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return script
}

// TestTail covers both branches of tail (short and long input).
func TestTail(t *testing.T) {
	if got := tail("short", 300); got != "short" {
		t.Errorf("tail(short) = %q, want unchanged", got)
	}
	long := strings.Repeat("x", 400)
	got := tail(long, 300)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, strings.Repeat("x", 300)) {
		t.Errorf("tail(long) = %q, want ellipsis + last 300 chars", got)
	}
}

// TestRunError covers the run() error branch, including the stderr tail.
func TestRunError(t *testing.T) {
	bin := writeFakeYtDlpScript(t, `#!/bin/sh
echo "warning line" >&2
echo "another stderr line" >&2
exit 3
`)
	c := NewClient(bin)
	_, err := c.run(context.Background(), "-J", "https://example.com/v")
	if err == nil {
		t.Fatal("expected run error")
	}
	if !strings.Contains(err.Error(), "stderr") || !strings.Contains(err.Error(), "another stderr line") {
		t.Errorf("err = %q, want stderr tail", err)
	}
}

// TestInfoParseError covers the Info json.Unmarshal error branch.
func TestInfoParseError(t *testing.T) {
	bin := writeFakeYtDlpScript(t, `#!/bin/sh
echo 'not json at all'
exit 0
`)
	c := NewClient(bin)
	_, err := c.Info(context.Background(), "https://example.com/v")
	if err == nil || !strings.Contains(err.Error(), "parse info") {
		t.Errorf("err = %v, want parse info", err)
	}
}

// TestDownloadEmptyPath covers the "could not determine output path" branch.
func TestDownloadEmptyPath(t *testing.T) {
	bin := writeFakeYtDlpScript(t, `#!/bin/sh
exit 0
`)
	c := NewClient(bin)
	_, err := c.Download(context.Background(), "https://example.com/v", "best", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "could not determine output path") {
		t.Errorf("err = %v, want could-not-determine-path", err)
	}
}

// TestInfoDuplicateLabels covers the seen[label] dedup branch.
func TestInfoDuplicateLabels(t *testing.T) {
	bin := writeFakeYtDlpScript(t, `#!/bin/sh
cat <<'EOF'
{"title":"V","web_page_url":"https://example.com/v","duration":10,
 "formats":[
   {"format_id":"a","height":720,"fps":30,"vcodec":"avc1"},
   {"format_id":"b","height":720,"fps":30,"vcodec":"avc1"},
   {"format_id":"c","height":1080,"fps":60,"vcodec":"avc1"}
 ]}
EOF
exit 0
`)
	c := NewClient(bin)
	info, err := c.Info(context.Background(), "https://example.com/v")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Formats) != 2 {
		t.Fatalf("formats = %d, want 2 (deduped): %+v", len(info.Formats), info.Formats)
	}
}

// TestDownloadFormatString verifies the format expression built for a
// specific height and for "best".
func TestDownloadFormatString(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("YTDLP_ARGS_FILE", argsFile)
	bin := writeFakeYtDlpScript(t, `#!/bin/sh
printf '%s\n' "$@" > "$YTDLP_ARGS_FILE"
echo "/out/video.mp4"
exit 0
`)
	c := NewClient(bin)

	// Specific height -> constrained format expression.
	if _, err := c.Download(context.Background(), "https://example.com/v", "720p", t.TempDir()); err != nil {
		t.Fatalf("Download(720p): %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if !strings.Contains(string(args), "bestvideo[height<=720]+bestaudio/best[height<=720]/best") {
		t.Errorf("720p format = %q", string(args))
	}

	// "best" -> unconstrained format expression.
	if _, err := c.Download(context.Background(), "https://example.com/v", "best", t.TempDir()); err != nil {
		t.Fatalf("Download(best): %v", err)
	}
	args, err = os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if !strings.Contains(string(args), "bestvideo*+bestaudio/best") {
		t.Errorf("best format = %q", string(args))
	}
}
