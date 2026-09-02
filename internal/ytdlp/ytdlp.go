// Package ytdlp wraps the yt-dlp command-line tool to fetch video metadata
// and download videos. It shells out to the yt-dlp binary (the standard
// approach for Go, mirroring the original Python project's use of the
// yt_dlp library) and parses its JSON output.
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Quality is a selectable video quality.
type Quality struct {
	FormatID string
	Label    string // e.g. "1080p" or "1080p 60fps"
	Height   int
	FPS      int
}

// Info is video metadata extracted by yt-dlp.
type Info struct {
	Title       string
	WebPageURL  string
	DurationSec int
	Formats     []Quality
}

// Client runs yt-dlp commands.
type Client struct {
	bin string
}

// NewClient creates a client using the yt-dlp binary at path (or "yt-dlp"
// to resolve from PATH).
func NewClient(bin string) *Client {
	if bin == "" {
		bin = "yt-dlp"
	}
	return &Client{bin: bin}
}

// Available reports whether the yt-dlp binary can be found.
func (c *Client) Available() bool {
	_, err := exec.LookPath(c.bin)
	return err == nil
}

// Info fetches video metadata and the list of selectable qualities.
func (c *Client) Info(ctx context.Context, url string) (*Info, error) {
	var raw struct {
		Title      string  `json:"title"`
		WebPageURL string  `json:"web_page_url"`
		Duration   float64 `json:"duration"`
		Formats    []struct {
			FormatID string   `json:"format_id"`
			Height   *int     `json:"height"`
			FPS      *float64 `json:"fps"`
			VCodec   string   `json:"vcodec"`
		} `json:"formats"`
	}
	out, err := c.run(ctx, "-J", "--no-playlist", url)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ytdlp: parse info: %w", err)
	}

	info := &Info{
		Title:       raw.Title,
		WebPageURL:  raw.WebPageURL,
		DurationSec: int(raw.Duration),
	}
	seen := map[string]bool{}
	for _, f := range raw.Formats {
		if f.VCodec == "none" || f.Height == nil {
			continue
		}
		label := fmt.Sprintf("%dp", *f.Height)
		if f.FPS != nil && *f.FPS >= 30 {
			label += fmt.Sprintf(" %dfps", int(*f.FPS))
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		fps := 0
		if f.FPS != nil {
			fps = int(*f.FPS)
		}
		info.Formats = append(info.Formats, Quality{
			FormatID: f.FormatID,
			Label:    label,
			Height:   *f.Height,
			FPS:      fps,
		})
	}
	sort.Slice(info.Formats, func(i, j int) bool {
		return info.Formats[i].Height < info.Formats[j].Height
	})
	return info, nil
}

// Download fetches a video with the given quality ("best" or a label such
// as "1080p") into outDir and returns the output file path.
func (c *Client) Download(ctx context.Context, url, quality, outDir string) (string, error) {
	format := "bestvideo*+bestaudio/best"
	if q := strings.TrimSpace(quality); q != "" && !strings.EqualFold(q, "best") {
		// Match the highest format at or below the requested height.
		height := parseHeight(q)
		if height > 0 {
			format = fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", height, height)
		}
	}
	outTpl := outDir + "/%(title)s.%(ext)s"
	args := []string{"-f", format, "--merge-output-format", "mp4",
		"-o", outTpl, "--no-playlist", url}
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	// yt-dlp prints the final path as "[download] Destination: <path>" and
	// ends with the merged file path on its own line.
	path := lastNonEmptyLine(string(out))
	if path == "" {
		return "", fmt.Errorf("ytdlp: could not determine output path")
	}
	return path, nil
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ytdlp: %w (stderr: %s)", err, tail(stderr.String(), 300))
	}
	return stdout.Bytes(), nil
}

// parseHeight extracts a pixel height from a quality label like "1080p" or
// "1080p 60fps".
func parseHeight(label string) int {
	label = strings.ToLower(strings.TrimSpace(label))
	var b strings.Builder
	for _, r := range label {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			break
		}
	}
	n, err := strconv.Atoi(b.String())
	if err != nil {
		return 0
	}
	return n
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
