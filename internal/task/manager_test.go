package task

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFilenameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a/b/video.mp4?x=1": "video.mp4",
		"https://example.com/":                  "downloaded_file",
		"":                                      "downloaded_file",
	}
	for in, want := range cases {
		if got := FilenameFromURL(in, "downloaded_file"); got != want {
			t.Errorf("FilenameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize("a/b:c.mp4"); got != "a_b_c.mp4" {
		t.Errorf("Sanitize = %q", got)
	}
	if got := Sanitize(""); got != "downloaded_file" {
		t.Errorf("Sanitize empty = %q", got)
	}
}

func TestSubmitDownloadUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello drive"))
	}))
	defer srv.Close()

	var uploaded string
	mgr := NewManager(t.TempDir(), 2, func(ctx context.Context, path, filename string) (string, error) {
		uploaded = filename
		return "https://drive.google.com/file/d/abc", nil
	})

	var lastStage string
	tk, err := mgr.Submit(context.Background(), 1, 2, srv.URL, "test.txt", func(id int64, stage string, status Status) {
		lastStage = stage
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if tk.Status() == StatusDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if tk.Status() != StatusDone {
		t.Fatalf("status = %s, want done (stage %q)", tk.Status(), lastStage)
	}
	if uploaded != "test.txt" {
		t.Errorf("uploaded filename = %q", uploaded)
	}
}

func TestCancel(t *testing.T) {
	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, path, filename string) (string, error) {
		return "link", nil
	})
	t1, _ := mgr.Submit(context.Background(), 1, 2, "https://example.com/x", "x", nil)
	if !mgr.Cancel(t1.ID) {
		t.Error("Cancel returned false")
	}
}
