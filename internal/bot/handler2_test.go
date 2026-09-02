package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/gdrive"
)

func TestCmdList(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	drive.listFiles = []gdrive.File{
		{Name: "docs", MimeType: "folder"},
		{Name: "a.txt", MimeType: "text/plain"},
	}
	b.cmdList(context.Background(), msg(1, 1, "/list"), "")
	out := tg.lastText()
	if !strings.Contains(out, "📁 docs") || !strings.Contains(out, "📄 a.txt") {
		t.Errorf("list = %q", out)
	}
}

func TestCmdListEmpty(t *testing.T) {
	b, tg, _, _, _, _ := newTestBot()
	b.cmdList(context.Background(), msg(1, 1, "/list"), "")
	if !strings.Contains(tg.lastText(), "(空)") {
		t.Errorf("empty list = %q", tg.lastText())
	}
}

func TestCmdSearch(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	drive.searchFiles = []gdrive.File{{Name: "report.pdf", WebLink: "https://drive.google.com/r"}}
	b.cmdSearch(context.Background(), msg(1, 1, "/search report"), "report")
	out := tg.lastText()
	if !strings.Contains(out, "report.pdf") || !strings.Contains(out, "https://drive.google.com/r") {
		t.Errorf("search = %q", out)
	}
	// Empty keyword -> usage.
	b.cmdSearch(context.Background(), msg(1, 1, "/search"), "")
	if !strings.Contains(tg.lastText(), "用法") {
		t.Errorf("search empty = %q", tg.lastText())
	}
}

func TestCmdCopy(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	b.cmdCopy(context.Background(), msg(1, 1, "/copy srcID123 dstID456"), "srcID123 dstID456")
	if !strings.Contains(tg.lastText(), "已复制") {
		t.Errorf("copy = %q", tg.lastText())
	}
	if len(drive.copied) != 1 || drive.copied[0].src != "srcID123" || drive.copied[0].dst != "dstID456" {
		t.Errorf("copied = %+v", drive.copied)
	}
	// Too few args -> usage.
	b.cmdCopy(context.Background(), msg(1, 1, "/copy onlyone"), "onlyone")
	if !strings.Contains(tg.lastText(), "用法") {
		t.Errorf("copy usage = %q", tg.lastText())
	}
}

func TestCmdMove(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	b.cmdMove(context.Background(), msg(1, 1, "/move a b"), "a b")
	if !strings.Contains(tg.lastText(), "已移动") {
		t.Errorf("move = %q", tg.lastText())
	}
	if len(drive.moved) != 1 || drive.moved[0].src != "a" || drive.moved[0].dst != "b" {
		t.Errorf("moved = %+v", drive.moved)
	}
}

func TestCmdDelete(t *testing.T) {
	b, tg, _, _, drive, _ := newTestBot()
	b.cmdDelete(context.Background(), msg(1, 1, "/delete fileID789"), "fileID789")
	if !strings.Contains(tg.lastText(), "已删除") {
		t.Errorf("delete = %q", tg.lastText())
	}
	if len(drive.deleted) != 1 || drive.deleted[0] != "fileID789" {
		t.Errorf("deleted = %+v", drive.deleted)
	}
	// Empty -> usage.
	b.cmdDelete(context.Background(), msg(1, 1, "/delete"), "")
	if !strings.Contains(tg.lastText(), "用法") {
		t.Errorf("delete usage = %q", tg.lastText())
	}
}

func TestCmdAuth(t *testing.T) {
	b, tg, _, _, _, _ := newTestBot()
	// No client ID configured.
	b.cfg.GDriveClientID = ""
	b.cmdAuth(context.Background(), msg(1, 1, "/auth"))
	if !strings.Contains(tg.lastText(), "G_DRIVE_CLIENT_ID") {
		t.Errorf("auth no-client = %q", tg.lastText())
	}
	// With client ID: stores pending state and sends the auth link.
	b.cfg.GDriveClientID = "cid"
	b.cmdAuth(context.Background(), msg(1, 1, "/auth"))
	if !strings.Contains(tg.lastText(), "state=u1") {
		t.Errorf("auth = %q", tg.lastText())
	}
	b.pendingMu.Lock()
	state, ok := b.pendingAuth[1]
	b.pendingMu.Unlock()
	if !ok || state != "u1" {
		t.Errorf("pendingAuth[1] = %q (ok=%v), want u1", state, ok)
	}
}

func TestCmdStart(t *testing.T) {
	b, tg, _, _, _, _ := newTestBot()
	b.cmdStart(context.Background(), msg(1, 1, "/start"))
	out := tg.lastText()
	for _, want := range []string{"/download", "/ytdl", "/auth", "/list", "/emptytrash"} {
		if !strings.Contains(out, want) {
			t.Errorf("start help missing %q", want)
		}
	}
}
