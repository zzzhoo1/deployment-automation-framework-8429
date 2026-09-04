package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/gdrive"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/task"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/tg"
)

// TestHandleMessageDispatch covers the dispatch branches of handleMessage:
// start, auth, authmode, revoke, setfolder, emptytrash, download, ytdl, list,
// search, copy, move, delete, and the default (quality/auth-code/unknown).
func TestHandleMessageDispatch(t *testing.T) {
	ctx := context.Background()
	b, tg, tasks, _, _, _ := newTestBot()

	// Each command should be routed without panicking and produce a send.
	cases := []string{
		"/start",
		"/help",
		"/auth",
		"/authmode oauth",
		"/revoke",
		"/setfolder abc123",
		"/emptytrash",
		"/download https://x/f.txt",
		"/ytdl https://youtu.be/x",
		"/list",
		"/search report",
		"/copy a b",
		"/move a b",
		"/delete x",
	}
	for _, c := range cases {
		before := len(tg.sent)
		b.handleMessage(ctx, msg(1, 1, c))
		if len(tg.sent) == before {
			t.Errorf("handleMessage(%q) produced no send", c)
		}
	}
	if len(tasks.submitted) == 0 {
		t.Error("expected a download task to be submitted")
	}
}

// TestHandleCallbackUnknownData covers the non-mirror callback branch.
func TestHandleCallbackUnknownData(t *testing.T) {
	b, _, _, _, _, _ := newTestBot()
	ctx := context.Background()
	// Unknown callback data -> just answers with empty text, no panic.
	b.handleCallback(ctx, tgCallback("something:else"))
}

// TestCmdDownloadFilenamePipe covers the "url|filename" parsing branch.
func TestCmdDownloadFilenamePipe(t *testing.T) {
	b, tg, tasks, _, _, _ := newTestBot()
	b.cmdDownload(context.Background(), msg(1, 1, "/download https://x/f.txt|my name.txt"), "https://x/f.txt|my name.txt")
	if len(tasks.submitted) != 1 {
		t.Fatalf("submitted = %d, want 1", len(tasks.submitted))
	}
	if !strings.Contains(tg.lastText(), "任务已创建") {
		t.Errorf("download = %q", tg.lastText())
	}
}

// TestCmdDownloadBadURL covers the non-http URL branch.
func TestCmdDownloadBadURL(t *testing.T) {
	b, tg, _, _, _, _ := newTestBot()
	b.cmdDownload(context.Background(), msg(1, 1, "/download ftp://x"), "ftp://x")
	if !strings.Contains(tg.lastText(), "http") {
		t.Errorf("bad url download = %q", tg.lastText())
	}
}

// TestTryCaptureQualityInvalid covers the invalid-selection branch (returns false).
func TestTryCaptureQualityInvalid(t *testing.T) {
	b, _, _, _, _, _ := newTestBot()
	ctx := context.Background()
	b.pendingVideoMu.Lock()
	b.pendingVideo[1] = ytdlpInfo()
	b.pendingVideoMu.Unlock()
	// Out-of-range and non-numeric selections are not consumed.
	if b.tryCaptureQuality(ctx, msg(1, 1, "99")) {
		t.Error("out-of-range selection should not be consumed")
	}
	if b.tryCaptureQuality(ctx, msg(1, 1, "abc")) {
		t.Error("non-numeric selection should not be consumed")
	}
	// "best" and "0" both map to best.
	if !b.tryCaptureQuality(ctx, msg(1, 1, "best")) {
		t.Error("best should be consumed")
	}
}

// TestCmdListTruncation covers the >20-file truncation branch.
func TestCmdListTruncation(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	for i := 0; i < 25; i++ {
		drive.listFiles = append(drive.listFiles, gdriveFile("f", i))
	}
	b.cmdList(context.Background(), msg(1, 1, "/list"), "")
	if !strings.Contains(tg.lastText(), "…") {
		t.Errorf("long list should be truncated, got %q", tg.lastText())
	}
}

// TestCmdSearchTruncation covers the >10-result truncation branch.
func TestCmdSearchTruncation(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	for i := 0; i < 15; i++ {
		drive.searchFiles = append(drive.searchFiles, gdriveFile("f", i))
	}
	b.cmdSearch(context.Background(), msg(1, 1, "/search x"), "x")
	if len(tg.sent) == 0 {
		t.Fatal("no send")
	}
	// 15 results -> only 10 printed (no truncation marker, just stops).
	if strings.Count(tg.lastText(), "•") > 10 {
		t.Errorf("search should cap at 10 results, got %q", tg.lastText())
	}
}

// TestTryCaptureAuthCodeInvalid covers the invalid-code branch (returns false).
func TestTryCaptureAuthCodeInvalid(t *testing.T) {
	b, _, _, _, _, _ := newTestBot()
	ctx := context.Background()
	b.pendingAuth[1] = "u1"
	// A message that is not a valid code shape is not consumed.
	if b.tryCaptureAuthCode(ctx, msg(1, 1, "hello world")) {
		t.Error("non-code message should not be consumed")
	}
}

// TestHandleEditedMessage covers the EditedMessage dispatch path.
func TestHandleEditedMessage(t *testing.T) {
	b, ftk, _, _, _, _ := newTestBot()
	ctx := context.Background()
	u := &tg.Update{EditedMessage: msg(1, 1, "/start")}
	b.handle(ctx, u)
	if len(ftk.sent) == 0 {
		t.Error("edited /start should dispatch to cmdStart")
	}
}

// TestOnProgressBranches exercises the cmdDownload onProgress callback branches
// (failed / done / progress) by invoking the submitted callback directly.
func TestOnProgressBranches(t *testing.T) {
	b, tg, _, _, _, _ := newTestBot()
	ctx := context.Background()

	// Capture the callback passed to Submit.
	var captured task.ProgressFunc
	b.tasks = &captureTasks{submitFn: func(cb task.ProgressFunc) {
		captured = cb
	}}
	b.cmdDownload(ctx, msg(1, 1, "/download https://x/f.txt"), "https://x/f.txt")
	if captured == nil {
		t.Fatal("no progress callback captured")
	}

	// Set a sent message ID so the callback edits it.
	tg.lastSent = 1

	// Failed stage.
	captured(1, "失败: something", task.StatusFailed)
	if !strings.Contains(tg.lastEditText(), "任务失败") {
		t.Errorf("failed progress = %q", tg.lastEditText())
	}
	// Done status.
	captured(1, "完成: ok", task.StatusDone)
	if !strings.Contains(tg.lastEditText(), "任务完成") {
		t.Errorf("done progress = %q", tg.lastEditText())
	}
	// Default progress.
	captured(1, "下载中", task.StatusRunning)
	if !strings.Contains(tg.lastEditText(), "Downloading") {
		t.Errorf("progress = %q", tg.lastEditText())
	}
}

// gdriveFile builds a minimal gdrive.File for list/search tests.
func gdriveFile(name string, i int) gdrive.File {
	return gdrive.File{Name: name, WebLink: "https://drive.google.com/" + name}
}

// captureTasks is a minimal taskManager that captures the progress callback.
type captureTasks struct {
	submitFn func(task.ProgressFunc)
}

func (c *captureTasks) Submit(_ context.Context, _, _ int64, _, _ string, onProgress task.ProgressFunc) (*task.Task, error) {
	c.submitFn(onProgress)
	return &task.Task{}, nil
}
func (c *captureTasks) Pause(id int64) bool  { return true }
func (c *captureTasks) Resume(id int64) bool { return true }
func (c *captureTasks) Cancel(id int64) bool { return true }
