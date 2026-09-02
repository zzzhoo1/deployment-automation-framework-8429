package ui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeEditor records edits.
type fakeEditor struct {
	edits []string
}

func (f *fakeEditor) Edit(ctx context.Context, chatID, messageID int64, text string) error {
	f.edits = append(f.edits, text)
	return nil
}

func TestFormatProgress(t *testing.T) {
	got := FormatProgress(50*1024*1024, 100*1024*1024, "uploading", "a.mp4", "2.5 MB/s")
	for _, want := range []string{"⬆️", "**Uploading**", "a.mp4", "50.0%", "50.0 MB / 100.0 MB", "2.5 MB/s"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatProgress missing %q in:\n%s", want, got)
		}
	}
	// Half progress => 5 filled blocks.
	if strings.Count(got, "█") != 5 || strings.Count(got, "░") != 5 {
		t.Errorf("expected 5 filled and 5 empty blocks, got:\n%s", got)
	}
}

func TestFormatProgressFullAndZero(t *testing.T) {
	full := FormatProgress(100, 100, "completed", "", "")
	if strings.Count(full, "█") != 10 {
		t.Errorf("full bar should be 10 filled, got:\n%s", full)
	}
	zero := FormatProgress(0, 100, "queued", "", "")
	if strings.Count(zero, "░") != 10 {
		t.Errorf("zero bar should be 10 empty, got:\n%s", zero)
	}
}

func TestFormatSuccessAndError(t *testing.T) {
	s := FormatSuccess("完成", "https://drive.google.com/x")
	if !strings.Contains(s, "✅") || !strings.Contains(s, "完成") || !strings.Contains(s, "https://drive.google.com/x") {
		t.Errorf("FormatSuccess = %q", s)
	}
	e := FormatError("认证失败", "无法连接到 Google Drive", "重新授权")
	if !strings.Contains(e, "❌") || !strings.Contains(e, "认证失败") || !strings.Contains(e, "重新授权") {
		t.Errorf("FormatError = %q", e)
	}
}

func TestFormatList(t *testing.T) {
	got := FormatList([]string{"a", "b"}, "目录", "📄")
	if !strings.Contains(got, "**目录**") || !strings.Contains(got, "📄 a") || !strings.Contains(got, "📄 b") {
		t.Errorf("FormatList = %q", got)
	}
}

func TestAnimatorLoading(t *testing.T) {
	f := &fakeEditor{}
	a := NewAnimator(f)
	a.Loading(context.Background(), 1, 2, "处理中", 350*time.Millisecond)
	if len(f.edits) == 0 {
		t.Fatal("Loading produced no edits")
	}
	for _, e := range f.edits {
		if !strings.Contains(e, "处理中") {
			t.Errorf("edit %q missing base text", e)
		}
	}
}

func TestAnimatorSuccessReveal(t *testing.T) {
	f := &fakeEditor{}
	a := NewAnimator(f)
	a.SuccessReveal(context.Background(), 1, 2, "✅ 完成")
	if len(f.edits) == 0 {
		t.Fatal("SuccessReveal produced no edits")
	}
	if last := f.edits[len(f.edits)-1]; last != "✅ 完成" {
		t.Errorf("final edit = %q, want final text", last)
	}
}

func TestTitleCase(t *testing.T) {
	if titleCase("uploading") != "Uploading" {
		t.Errorf("titleCase(uploading) = %q", titleCase("uploading"))
	}
	if titleCase("") != "" {
		t.Errorf("titleCase(\"\") should be empty")
	}
}
