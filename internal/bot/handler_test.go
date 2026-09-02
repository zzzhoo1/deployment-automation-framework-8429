package bot

import (
	"context"
	"strings"
	"testing"
)

func TestCmdAuthMode(t *testing.T) {
	b, tg, _, _, _ := newTestBot()
	ctx := context.Background()

	// No arg: reports current mode.
	b.cmdAuthMode(ctx, msg(1, 1, "/authmode"), "")
	if !strings.Contains(tg.lastText(), "oauth") {
		t.Errorf("no-arg authmode = %q, want current mode", tg.lastText())
	}
	// Valid switch.
	b.cmdAuthMode(ctx, msg(1, 1, "/authmode service_account"), "service_account")
	if !strings.Contains(tg.lastText(), "service_account") {
		t.Errorf("switch authmode = %q", tg.lastText())
	}
	if b.cfg.DefaultAuthMode != "service_account" {
		t.Errorf("DefaultAuthMode = %q, want service_account", b.cfg.DefaultAuthMode)
	}
	// Invalid mode.
	b.cmdAuthMode(ctx, msg(1, 1, "/authmode bogus"), "bogus")
	if !strings.Contains(tg.lastText(), "oauth 或 service_account") {
		t.Errorf("invalid authmode = %q", tg.lastText())
	}
}

func TestCmdRevoke(t *testing.T) {
	b, tg, _, st, _ := newTestBot()
	ctx := context.Background()
	// Seed a credential then revoke it.
	_ = st.SaveCredential(mustCred(1))
	b.cmdRevoke(ctx, msg(1, 1, "/revoke"))
	if !strings.Contains(tg.lastText(), "已撤销") {
		t.Errorf("revoke = %q", tg.lastText())
	}
	if st.GetCredential(1) != nil {
		t.Error("credential should be deleted after revoke")
	}
}

func TestCmdSetFolder(t *testing.T) {
	b, tg, _, _, _ := newTestBot()
	ctx := context.Background()
	b.cmdSetFolder(ctx, msg(1, 1, "/setfolder https://drive.google.com/drive/folders/abc123"), "https://drive.google.com/drive/folders/abc123")
	if !strings.Contains(tg.lastText(), "abc123") {
		t.Errorf("setfolder = %q", tg.lastText())
	}
	if got := b.defaultUploadFolder(1); got != "abc123" {
		t.Errorf("defaultUploadFolder = %q, want abc123", got)
	}
	// Empty args -> usage.
	b.cmdSetFolder(ctx, msg(1, 1, "/setfolder"), "")
	if !strings.Contains(tg.lastText(), "用法") {
		t.Errorf("setfolder empty = %q", tg.lastText())
	}
}

func TestCmdEmptyTrash(t *testing.T) {
	b, tg, _, _, drive := newTestBot()
	ctx := context.Background()
	b.cmdEmptyTrash(ctx, msg(1, 1, "/emptytrash"))
	if !strings.Contains(tg.lastText(), "回收站已清空") {
		t.Errorf("emptytrash = %q", tg.lastText())
	}
	// Error path.
	drive.emptyErr = errBoom{}
	b.cmdEmptyTrash(ctx, msg(1, 1, "/emptytrash"))
	if !strings.Contains(tg.lastText(), errBoom{}.Error()) {
		t.Errorf("emptytrash error = %q", tg.lastText())
	}
}

func TestHandleCallbackMirror(t *testing.T) {
	b, _, tasks, _, _ := newTestBot()
	ctx := context.Background()
	b.handleCallback(ctx, tgCallback("mirror:42:pause"))
	b.handleCallback(ctx, tgCallback("mirror:42:resume"))
	b.handleCallback(ctx, tgCallback("mirror:42:cancel"))
	if len(tasks.paused) != 1 || tasks.paused[0] != 42 {
		t.Errorf("paused = %v", tasks.paused)
	}
	if len(tasks.resumed) != 1 || tasks.resumed[0] != 42 {
		t.Errorf("resumed = %v", tasks.resumed)
	}
	if len(tasks.canceled) != 1 || tasks.canceled[0] != 42 {
		t.Errorf("canceled = %v", tasks.canceled)
	}
}

func TestCmdDownloadValidation(t *testing.T) {
	b, tg, _, _, _ := newTestBot()
	ctx := context.Background()
	// Non-sudo user (cfg.SudoUsers=[1], user 2).
	b.cmdDownload(ctx, msg(2, 1, "/download https://x/f.txt"), "https://x/f.txt")
	if !strings.Contains(tg.lastText(), "管理员") {
		t.Errorf("non-sudo download = %q", tg.lastText())
	}
	// Sudo user, empty args.
	b.cmdDownload(ctx, msg(1, 1, "/download"), "")
	if !strings.Contains(tg.lastText(), "用法") {
		t.Errorf("empty download = %q", tg.lastText())
	}
	// Sudo user, non-http url.
	b.cmdDownload(ctx, msg(1, 1, "/download ftp://x"), "ftp://x")
	if !strings.Contains(tg.lastText(), "http") {
		t.Errorf("non-http download = %q", tg.lastText())
	}
}

func TestTryCaptureQuality(t *testing.T) {
	b, tg, _, _, _ := newTestBot()
	ctx := context.Background()
	// Seed a pending video for user 1.
	b.pendingVideoMu.Lock()
	b.pendingVideo[1] = ytdlpInfo()
	b.pendingVideoMu.Unlock()

	// No pending video for user 2 -> not consumed.
	if b.tryCaptureQuality(ctx, msg(2, 1, "1")) {
		t.Error("tryCaptureQuality consumed a message with no pending video")
	}
	// Valid selection for user 1 -> consumed and download started.
	if !b.tryCaptureQuality(ctx, msg(1, 1, "2")) {
		t.Error("tryCaptureQuality did not consume a valid selection")
	}
	b.pendingVideoMu.Lock()
	_, stillPending := b.pendingVideo[1]
	b.pendingVideoMu.Unlock()
	if stillPending {
		t.Error("pending video should be cleared after selection")
	}
	if !strings.Contains(tg.lastText(), "正在下载") {
		t.Errorf("after selection = %q", tg.lastText())
	}
}
