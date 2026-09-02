// Package task implements the mirror task pipeline: download a file from a
// direct URL, then upload it to Google Drive. Tasks support pause, resume,
// and cancel, and report progress through a callback.
package task

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Status is a task lifecycle state.
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusPaused   Status = "paused"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

// Task is a single mirror job.
type Task struct {
	ID       int64
	UserID   int64
	ChatID   int64
	URL      string
	Filename string

	mu     sync.Mutex
	status Status
	stage  string
	cancel context.CancelFunc
	pause  chan struct{}
	resume chan struct{}
}

// Status returns the current status.
func (t *Task) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// Stage returns the current human-readable stage.
func (t *Task) Stage() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stage
}

// SetStage updates the stage label.
func (t *Task) SetStage(stage string) {
	t.mu.Lock()
	t.stage = stage
	t.mu.Unlock()
}

// ProgressFunc is called with progress updates for a task.
type ProgressFunc func(taskID int64, stage string, status Status)

// UploadFunc uploads a local file to Drive and returns a human-readable
// result (e.g. the web link).
type UploadFunc func(ctx context.Context, path, filename string) (string, error)

// Manager owns and schedules mirror tasks.
type Manager struct {
	downloadDir string
	maxConc     int
	sem         chan struct{}
	upload      UploadFunc

	mu    sync.Mutex
	next  int64
	tasks map[int64]*Task
}

// NewManager creates a task manager. upload performs the Drive upload step.
func NewManager(downloadDir string, maxConcurrent int, upload UploadFunc) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Manager{
		downloadDir: downloadDir,
		maxConc:     maxConcurrent,
		sem:         make(chan struct{}, maxConcurrent),
		upload:      upload,
		tasks:       map[int64]*Task{},
	}
}

// Submit creates and schedules a new mirror task.
func (m *Manager) Submit(ctx context.Context, userID, chatID int64, urlStr, filename string, onProgress ProgressFunc) (*Task, error) {
	m.mu.Lock()
	m.next++
	id := m.next
	t := &Task{
		ID:       id,
		UserID:   userID,
		ChatID:   chatID,
		URL:      urlStr,
		Filename: filename,
		status:   StatusPending,
		stage:    "排队中",
		pause:    make(chan struct{}),
		resume:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	m.tasks[id] = t
	m.mu.Unlock()

	go m.run(ctx, t, onProgress)
	return t, nil
}

// Pause pauses a running task.
func (m *Manager) Pause(id int64) bool {
	t := m.get(id)
	if t == nil {
		return false
	}
	t.mu.Lock()
	if t.status == StatusRunning {
		t.status = StatusPaused
		close(t.pause)
	}
	t.mu.Unlock()
	return true
}

// Resume resumes a paused task.
func (m *Manager) Resume(id int64) bool {
	t := m.get(id)
	if t == nil {
		return false
	}
	t.mu.Lock()
	if t.status == StatusPaused {
		t.status = StatusRunning
		close(t.resume)
		t.pause = make(chan struct{})
		t.resume = make(chan struct{})
	}
	t.mu.Unlock()
	return true
}

// Cancel cancels a task.
func (m *Manager) Cancel(id int64) bool {
	t := m.get(id)
	if t == nil {
		return false
	}
	t.mu.Lock()
	if t.status != StatusDone && t.status != StatusCanceled {
		t.status = StatusCanceled
	}
	t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	return true
}

func (m *Manager) get(id int64) *Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id]
}

func (m *Manager) run(ctx context.Context, t *Task, onProgress ProgressFunc) {
	// Acquire a concurrency slot.
	m.sem <- struct{}{}
	defer func() { <-m.sem }()

	t.mu.Lock()
	t.status = StatusRunning
	t.mu.Unlock()
	t.SetStage("下载中")
	if onProgress != nil {
		onProgress(t.ID, "下载中", StatusRunning)
	}

	tmp, err := m.download(t)
	if err != nil {
		t.mu.Lock()
		if t.status == StatusCanceled {
			t.status = StatusCanceled
		} else {
			t.status = StatusFailed
		}
		t.mu.Unlock()
		if onProgress != nil {
			onProgress(t.ID, "失败: "+err.Error(), t.Status())
		}
		return
	}
	defer os.Remove(tmp)

	t.SetStage("上传中")
	if onProgress != nil {
		onProgress(t.ID, "上传中", StatusRunning)
	}
	if m.upload == nil {
		t.mu.Lock()
		t.status = StatusFailed
		t.mu.Unlock()
		if onProgress != nil {
			onProgress(t.ID, "失败: 未配置上传器", StatusFailed)
		}
		return
	}
	result, uerr := m.upload(ctx, tmp, t.Filename)
	if uerr != nil {
		t.mu.Lock()
		if t.status == StatusCanceled {
			t.status = StatusCanceled
		} else {
			t.status = StatusFailed
		}
		t.mu.Unlock()
		if onProgress != nil {
			onProgress(t.ID, "上传失败: "+uerr.Error(), t.Status())
		}
		return
	}
	t.mu.Lock()
	t.status = StatusDone
	t.mu.Unlock()
	if onProgress != nil {
		onProgress(t.ID, "完成: "+result, StatusDone)
	}
}

// download fetches the URL into a temp file under the download dir,
// honoring pause/cancel.
func (m *Manager) download(t *Task) (string, error) {
	if err := os.MkdirAll(m.downloadDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(m.downloadDir, "mirror-*")
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, t.URL, nil)
	if err != nil {
		tmp.Close()
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// Honor pause/cancel while copying.
	buf := make([]byte, 256*1024)
	for {
		t.mu.Lock()
		st := t.status
		pauseCh := t.pause
		t.mu.Unlock()

		if st == StatusPaused {
			select {
			case <-pauseCh:
				continue
			}
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				tmp.Close()
				return "", werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return "", rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

// FilenameFromURL extracts a filename from a URL, falling back to a default.
func FilenameFromURL(rawURL, fallback string) string {
	if rawURL == "" {
		return fallback
	}
	re := regexp.MustCompile(`[?#].*$`)
	clean := re.ReplaceAllString(rawURL, "")
	if i := strings.LastIndex(clean, "/"); i >= 0 {
		clean = clean[i+1:]
	}
	if clean == "" || clean == "." {
		return fallback
	}
	return clean
}

// Sanitize ensures a filename is safe for the filesystem.
func Sanitize(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, name)
	if name == "" {
		name = "downloaded_file"
	}
	return name
}

// Ext returns the file extension of a path.
func Ext(path string) string { return filepath.Ext(path) }
