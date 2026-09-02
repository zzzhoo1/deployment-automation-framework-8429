package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStageAndSetStage(t *testing.T) {
	mgr := NewManager(t.TempDir(), 1, nil)
	tk, _ := mgr.Submit(context.Background(), 1, 2, "https://example.com/x", "x", nil)
	tk.SetStage("自定义阶段")
	if got := tk.Stage(); got != "自定义阶段" {
		t.Errorf("Stage() = %q, want 自定义阶段", got)
	}
}

func TestNewManagerClampsMaxConcurrent(t *testing.T) {
	mgr := NewManager(t.TempDir(), 0, nil)
	if mgr.maxConc != 1 {
		t.Errorf("maxConc = %d, want clamped to 1", mgr.maxConc)
	}
	if cap(mgr.sem) != 1 {
		t.Errorf("sem cap = %d, want 1", cap(mgr.sem))
	}
}

func TestRunNilUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	var lastStage string
	mgr := NewManager(t.TempDir(), 1, nil) // nil upload
	tk, _ := mgr.Submit(context.Background(), 1, 2, srv.URL, "x.txt", func(id int64, stage string, status Status) {
		lastStage = stage
	})
	waitStatus(t, tk, StatusFailed)
	if tk.Status() != StatusFailed {
		t.Fatalf("status = %s, want failed", tk.Status())
	}
	if lastStage != "失败: 未配置上传器" {
		t.Errorf("stage = %q, want 未配置上传器", lastStage)
	}
}

func TestRunUploadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	var lastStage string
	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, userID int64, path, filename string) (string, error) {
		return "", errors.New("upload boom")
	})
	tk, _ := mgr.Submit(context.Background(), 1, 2, srv.URL, "x.txt", func(id int64, stage string, status Status) {
		lastStage = stage
	})
	waitStatus(t, tk, StatusFailed)
	if tk.Status() != StatusFailed {
		t.Fatalf("status = %s, want failed", tk.Status())
	}
	if lastStage != "上传失败: upload boom" {
		t.Errorf("stage = %q, want 上传失败", lastStage)
	}
}

func TestRunDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	var lastStage string
	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, userID int64, path, filename string) (string, error) {
		return "link", nil
	})
	tk, _ := mgr.Submit(context.Background(), 1, 2, srv.URL, "x.txt", func(id int64, stage string, status Status) {
		lastStage = stage
	})
	waitStatus(t, tk, StatusFailed)
	if tk.Status() != StatusFailed {
		t.Fatalf("status = %s, want failed", tk.Status())
	}
	if lastStage != "失败: download: HTTP 404" {
		t.Errorf("stage = %q, want download HTTP 404", lastStage)
	}
}

func TestRunDownloadBadURL(t *testing.T) {
	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, userID int64, path, filename string) (string, error) {
		return "link", nil
	})
	// A URL that fails to parse -> http.NewRequestWithContext error.
	tk, _ := mgr.Submit(context.Background(), 1, 2, "://bad-url", "x.txt", nil)
	waitStatus(t, tk, StatusFailed)
	if tk.Status() != StatusFailed {
		t.Fatalf("status = %s, want failed", tk.Status())
	}
}

func TestRunCanceledDuringDownload(t *testing.T) {
	// The server advertises a large Content-Length, sends a little, then
	// holds the connection open. After Cancel, the client's read loop sees
	// the task is canceled and the body read errors (connection closed by
	// the server on release), so the task ends as canceled rather than done.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("partial"))
		if fl != nil {
			fl.Flush()
		}
		<-release
	}))
	defer srv.Close()
	defer close(release)

	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, userID int64, path, filename string) (string, error) {
		return "link", nil
	})
	tk, _ := mgr.Submit(context.Background(), 1, 2, srv.URL, "x.txt", nil)
	// Wait until it is running, then cancel.
	waitStatus(t, tk, StatusRunning)
	mgr.Cancel(tk.ID)
	// The task must end as canceled (not done/failed).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if tk.Status() == StatusCanceled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tk.Status() != StatusCanceled {
		t.Fatalf("status = %s, want canceled", tk.Status())
	}
}

func TestExt(t *testing.T) {
	cases := map[string]string{
		"a/b/c.mp4": ".mp4",
		"noext":     "",
		".hidden":   ".hidden",
	}
	for in, want := range cases {
		if got := Ext(in); got != want {
			t.Errorf("Ext(%q) = %q, want %q", in, got, want)
		}
	}
}

// waitStatus polls until the task reaches want or times out.
func waitStatus(t *testing.T, tk *Task, want Status) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if tk.Status() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
