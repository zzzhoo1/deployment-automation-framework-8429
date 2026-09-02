package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/config"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/tg"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/ytdlp"
)

func TestNew(t *testing.T) {
	if _, err := New(&config.Config{}); err == nil {
		t.Error("New without BOT_TOKEN should error")
	}
	cfg := &config.Config{BotToken: "tok", DataDir: t.TempDir()}
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.tg == nil || b.drive == nil || b.tasks == nil || b.store == nil || b.ytdlp == nil {
		t.Error("New left a dependency nil")
	}
}

func TestRunDispatchesUpdates(t *testing.T) {
	b, ftk, tasks, _, _, _ := newTestBot()
	ftk.pollSeq = []pollResult{
		{updates: []tg.Update{{Message: msg(1, 1, "/start")}}},
		{updates: []tg.Update{{CallbackQuery: tgCallback("mirror:7:pause")}}},
		{updates: []tg.Update{{EditedMessage: msg(1, 1, "/help")}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := b.Run(ctx)
	if err != errPollDone {
		t.Fatalf("Run: %v, want errPollDone", err)
	}
	if len(ftk.sent) < 2 {
		t.Errorf("expected at least 2 sends, got %d", len(ftk.sent))
	}
	if !strings.Contains(ftk.sent[0].Text, "/download") {
		t.Errorf("first send should be the /start welcome, got %q", ftk.sent[0].Text)
	}
	if len(tasks.paused) != 1 || tasks.paused[0] != 7 {
		t.Errorf("callback should pause task 7, got %v", tasks.paused)
	}
}

func TestRunPollError(t *testing.T) {
	b, ftk, _, _, _, _ := newTestBot()
	ftk.pollSeq = []pollResult{{err: errBoom{}}}
	if err := b.Run(context.Background()); err == nil {
		t.Error("Run should return the poll error when ctx is not done")
	}
}

func TestHandleMessageValidation(t *testing.T) {
	b, ftk, _, _, _, _ := newTestBot()
	ctx := context.Background()
	b.handleMessage(ctx, &tg.Message{Text: "/start"})
	b.handleMessage(ctx, &tg.Message{From: &tg.User{ID: 1}, Text: "/start"})
	b.handleMessage(ctx, &tg.Message{From: &tg.User{ID: 1}, Chat: &tg.Chat{ID: 1}})
	if len(ftk.sent) != 0 {
		t.Errorf("invalid messages should not send, got %d sends", len(ftk.sent))
	}
	b.handleMessage(ctx, msg(1, 1, "hello world"))
	// Plain text (no leading /) splits to cmd="" -> cmdUnknown is silent.
	if len(ftk.sent) != 0 {
		t.Errorf("plain text should not send, got %d sends", len(ftk.sent))
	}
	// Unknown /command -> help hint.
	b.handleMessage(ctx, msg(1, 1, "/nope"))
	if !strings.Contains(ftk.lastText(), "未知命令") {
		t.Errorf("unknown command = %q, want unknown-command message", ftk.lastText())
	}
}

func TestCmdUnknown(t *testing.T) {
	b, ftk, _, _, _, _ := newTestBot()
	b.cmdUnknown(context.Background(), msg(1, 1, "x"), "")
	n := len(ftk.sent)
	b.cmdUnknown(context.Background(), msg(1, 1, "/nope"), "nope")
	if len(ftk.sent) != n+1 || !strings.Contains(ftk.lastText(), "/nope") {
		t.Errorf("cmdUnknown = %q", ftk.lastText())
	}
}

func TestTryCaptureAuthCode(t *testing.T) {
	ctx := context.Background()

	// No pending auth -> not consumed.
	b1, _, _, _, _, _ := newTestBot()
	if b1.tryCaptureAuthCode(ctx, msg(1, 1, "abc.def-ghi_jklmno12345678")) {
		t.Error("consumed a code with no pending auth")
	}

	// Pending auth, code too short -> not consumed.
	b2, _, _, _, _, _ := newTestBot()
	b2.pendingAuth[1] = "u1"
	if b2.tryCaptureAuthCode(ctx, msg(1, 1, "short")) {
		t.Error("consumed a too-short code")
	}

	// Pending auth, valid code, exchange succeeds -> consumed + credential saved.
	b3, ftk3, _, st3, _, _ := newTestBot()
	b3.pendingAuth[1] = "u1"
	if !b3.tryCaptureAuthCode(ctx, msg(1, 1, "abc.def-ghi_jklmno12345678")) {
		t.Error("did not consume a valid code")
	}
	if st3.GetCredential(1) == nil {
		t.Error("credential should be saved after successful exchange")
	}
	if !strings.Contains(ftk3.lastText(), "授权成功") {
		t.Errorf("auth success = %q", ftk3.lastText())
	}

	// Pending auth, valid code, exchange fails -> consumed + error shown.
	b4, ftk4, _, _, drive4, _ := newTestBot()
	b4.pendingAuth[1] = "u1"
	drive4.exchangeErr = errBoom{}
	if !b4.tryCaptureAuthCode(ctx, msg(1, 1, "abc.def-ghi_jklmno12345678")) {
		t.Error("did not consume a code that failed exchange")
	}
	if !strings.Contains(ftk4.lastText(), "授权码无效") {
		t.Errorf("auth fail = %q", ftk4.lastText())
	}
}

func TestCmdYtDl(t *testing.T) {
	ctx := context.Background()

	// Non-sudo.
	b1, ftk1, _, _, _, _ := newTestBot()
	b1.cmdYtDl(ctx, msg(2, 1, "/ytdl https://youtu.be/x"), "https://youtu.be/x")
	if !strings.Contains(ftk1.lastText(), "管理员") {
		t.Errorf("ytdl non-sudo = %q", ftk1.lastText())
	}

	// yt-dlp unavailable.
	b2, ftk2, _, _, _, ytd2 := newTestBot()
	ytd2.available = false
	b2.cmdYtDl(ctx, msg(1, 1, "/ytdl https://youtu.be/x"), "https://youtu.be/x")
	if !strings.Contains(ftk2.lastText(), "yt-dlp") {
		t.Errorf("ytdl unavailable = %q", ftk2.lastText())
	}

	// Bad URL.
	b3, ftk3, _, _, _, _ := newTestBot()
	b3.cmdYtDl(ctx, msg(1, 1, "/ytdl ftp://x"), "ftp://x")
	if !strings.Contains(ftk3.lastText(), "用法") {
		t.Errorf("ytdl bad url = %q", ftk3.lastText())
	}

	// Info error.
	b4, ftk4, _, _, _, ytd4 := newTestBot()
	ytd4.infoErr = errBoom{}
	b4.cmdYtDl(ctx, msg(1, 1, "/ytdl https://youtu.be/x"), "https://youtu.be/x")
	if !strings.Contains(ftk4.lastText(), "获取视频信息失败") {
		t.Errorf("ytdl info err = %q", ftk4.lastText())
	}

	// No qualities -> direct best download (async; wait for the completion edit).
	b5, ftk5, _, _, _, ytd5 := newTestBot()
	ytd5.info = &ytdlp.Info{Title: "V", WebPageURL: "https://example.com/v"}
	b5.cmdYtDl(ctx, msg(1, 1, "/ytdl https://youtu.be/x"), "https://youtu.be/x")
	waitFor(t, ftk5, "完成", 2*time.Second)
	if !strings.Contains(ftk5.lastEditText(), "完成") {
		t.Errorf("ytdl best download = %q", ftk5.lastEditText())
	}

	// With qualities -> numbered menu + pending state.
	b6, ftk6, _, _, _, _ := newTestBot()
	b6.cmdYtDl(ctx, msg(1, 1, "/ytdl https://youtu.be/x"), "https://youtu.be/x")
	if !strings.Contains(ftk6.lastText(), "请选择画质") {
		t.Errorf("ytdl menu = %q", ftk6.lastText())
	}
	b6.pendingVideoMu.Lock()
	_, pending := b6.pendingVideo[1]
	b6.pendingVideoMu.Unlock()
	if !pending {
		t.Error("pending video should be set after showing the menu")
	}
}

func TestStartYtDlDownloadError(t *testing.T) {
	b, ftk, _, _, _, ytd := newTestBot()
	ytd.downloadErr = errBoom{}
	b.startYtDlDownload(context.Background(), msg(1, 1, "/ytdl"), ytdlpInfo(), "720p")
	waitFor(t, ftk, "下载失败", 2*time.Second)
	if !strings.Contains(ftk.lastEditText(), "下载失败") {
		t.Errorf("ytdl download error = %q", ftk.lastEditText())
	}
}

func TestStartYtDlUploadError(t *testing.T) {
	b, ftk, _, _, drive, _ := newTestBot()
	drive.uploadErr = errBoom{}
	b.startYtDlDownload(context.Background(), msg(1, 1, "/ytdl"), ytdlpInfo(), "720p")
	waitFor(t, ftk, "上传失败", 2*time.Second)
	if !strings.Contains(ftk.lastEditText(), "上传失败") {
		t.Errorf("ytdl upload error = %q", ftk.lastEditText())
	}
}

func TestUploadUsesDefaultFolder(t *testing.T) {
	b, _, _, _, drive, _ := newTestBot()
	b.cmdSetFolder(context.Background(), msg(1, 1, "/setfolder myFolder123"), "myFolder123")
	link, err := b.upload(context.Background(), 1, "/tmp/f.txt", "f.txt")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if link != "https://drive.google.com/up" {
		t.Errorf("upload link = %q", link)
	}
	drive.uploadErr = errBoom{}
	if _, err := b.upload(context.Background(), 1, "/tmp/f.txt", "f.txt"); err == nil {
		t.Error("upload should propagate the drive error")
	}
}

func TestCmdDownloadSubmit(t *testing.T) {
	b, ftk, tasks, _, _, _ := newTestBot()
	b.cmdDownload(context.Background(), msg(1, 1, "/download https://x/f.txt"), "https://x/f.txt")
	if len(tasks.submitted) != 1 || tasks.submitted[0] != "https://x/f.txt" {
		t.Errorf("submitted = %v", tasks.submitted)
	}
	if !strings.Contains(ftk.lastText(), "任务已创建") {
		t.Errorf("download submit = %q", ftk.lastText())
	}
	tasks.submitErr = errBoom{}
	b.cmdDownload(context.Background(), msg(1, 1, "/download https://x/f.txt"), "https://x/f.txt")
	if !strings.Contains(ftk.lastText(), "提交失败") {
		t.Errorf("download submit error = %q", ftk.lastText())
	}
}

func TestCmdRevokeError(t *testing.T) {
	b, ftk, _, st, _, _ := newTestBot()
	st.deleteErr = errBoom{}
	b.cmdRevoke(context.Background(), msg(1, 1, "/revoke"))
	if !strings.Contains(ftk.lastText(), "boom") {
		t.Errorf("revoke error = %q", ftk.lastText())
	}
}

func TestDriveErrorPaths(t *testing.T) {
	ctx := context.Background()

	b1, ftk1, _, _, drive1, _ := newTestBot()
	drive1.listErr = errBoom{}
	b1.cmdList(ctx, msg(1, 1, "/list"), "")
	if !strings.Contains(ftk1.lastText(), "boom") {
		t.Errorf("list error = %q", ftk1.lastText())
	}

	b2, ftk2, _, _, drive2, _ := newTestBot()
	drive2.searchErr = errBoom{}
	b2.cmdSearch(ctx, msg(1, 1, "/search x"), "x")
	if !strings.Contains(ftk2.lastText(), "boom") {
		t.Errorf("search error = %q", ftk2.lastText())
	}

	b3, ftk3, _, _, drive3, _ := newTestBot()
	drive3.copyErr = errBoom{}
	b3.cmdCopy(ctx, msg(1, 1, "/copy a b"), "a b")
	if !strings.Contains(ftk3.lastText(), "boom") {
		t.Errorf("copy error = %q", ftk3.lastText())
	}

	b4, ftk4, _, _, drive4, _ := newTestBot()
	drive4.moveErr = errBoom{}
	b4.cmdMove(ctx, msg(1, 1, "/move a b"), "a b")
	if !strings.Contains(ftk4.lastText(), "boom") {
		t.Errorf("move error = %q", ftk4.lastText())
	}

	b5, ftk5, _, _, drive5, _ := newTestBot()
	drive5.deleteErr = errBoom{}
	b5.cmdDelete(ctx, msg(1, 1, "/delete x"), "x")
	if !strings.Contains(ftk5.lastText(), "boom") {
		t.Errorf("delete error = %q", ftk5.lastText())
	}
}

// waitFor polls the fakeTG until an edit containing want appears or timeout.
func waitFor(t *testing.T, f *fakeTG, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(f.lastEditText(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
