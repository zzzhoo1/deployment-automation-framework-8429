package task

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPauseResumeState(t *testing.T) {
	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, userID int64, path, filename string) (string, error) {
		return "link", nil
	})
	tk, err := mgr.Submit(context.Background(), 1, 2, "https://example.com/f.txt", "f.txt", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Wait until the task is running.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && tk.Status() != StatusRunning {
		time.Sleep(10 * time.Millisecond)
	}
	if tk.Status() != StatusRunning {
		t.Fatalf("status = %s, want running", tk.Status())
	}
	if !mgr.Pause(tk.ID) {
		t.Fatal("Pause returned false")
	}
	if tk.Status() != StatusPaused {
		t.Errorf("after pause status = %s, want paused", tk.Status())
	}
	if !mgr.Resume(tk.ID) {
		t.Fatal("Resume returned false")
	}
	if tk.Status() != StatusRunning {
		t.Errorf("after resume status = %s, want running", tk.Status())
	}
}

func TestPauseResumeUnknownTask(t *testing.T) {
	mgr := NewManager(t.TempDir(), 1, nil)
	if mgr.Pause(999) {
		t.Error("Pause(unknown) = true, want false")
	}
	if mgr.Resume(999) {
		t.Error("Resume(unknown) = true, want false")
	}
	if mgr.Cancel(999) {
		t.Error("Cancel(unknown) = true, want false")
	}
}

func TestCancelDoneTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()
	mgr := NewManager(t.TempDir(), 1, func(ctx context.Context, userID int64, path, filename string) (string, error) {
		return "link", nil
	})
	tk, _ := mgr.Submit(context.Background(), 1, 2, srv.URL, "f.txt", nil)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && tk.Status() != StatusDone {
		time.Sleep(10 * time.Millisecond)
	}
	if tk.Status() != StatusDone {
		t.Fatalf("status = %s, want done", tk.Status())
	}
	// Cancelling a done task should return true but keep it done.
	mgr.Cancel(tk.ID)
	if tk.Status() != StatusDone {
		t.Errorf("after cancel status = %s, want done", tk.Status())
	}
}
