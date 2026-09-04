package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// errEditor fails every edit.
type errEditor struct{}

func (errEditor) Edit(ctx context.Context, chatID, messageID int64, text string) error {
	return errors.New("edit failed")
}

// TestFormatProgressUnknownStatus covers the fallback icon branch.
func TestFormatProgressUnknownStatus(t *testing.T) {
	got := FormatProgress(10, 100, "weird-status", "", "")
	if !strings.Contains(got, "⚙️") {
		t.Errorf("unknown status should fall back to processing icon, got:\n%s", got)
	}
}

// TestFormatProgressOverflow covers the filled>barLen clamp branch.
func TestFormatProgressOverflow(t *testing.T) {
	got := FormatProgress(200, 100, "uploading", "", "")
	if strings.Count(got, "█") != 10 {
		t.Errorf("overflow should clamp to 10 filled, got:\n%s", got)
	}
}

// TestFormatListDefaultIcon covers the empty-icon default bullet branch.
func TestFormatListDefaultIcon(t *testing.T) {
	got := FormatList([]string{"a"}, "", "")
	if !strings.HasPrefix(got, "• a") {
		t.Errorf("default icon should be bullet, got %q", got)
	}
}

// TestAnimatorDots covers the Dots animation loop.
func TestAnimatorDots(t *testing.T) {
	f := &fakeEditor{}
	a := NewAnimator(f)
	a.Dots(context.Background(), 1, 2, "处理中", 1)
	if len(f.edits) == 0 {
		t.Fatal("Dots produced no edits")
	}
	for _, e := range f.edits {
		if !strings.Contains(e, "处理中") {
			t.Errorf("edit %q missing base text", e)
		}
	}
}

// TestAnimatorDotsCanceled covers the ctx-canceled branch of Dots.
func TestAnimatorDotsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled
	f := &fakeEditor{}
	a := NewAnimator(f)
	a.Dots(ctx, 1, 2, "x", 5)
	if len(f.edits) != 0 {
		t.Errorf("canceled Dots should not edit, got %d edits", len(f.edits))
	}
}

// TestAnimatorDotsEditorError covers the edit-error branch of Dots.
func TestAnimatorDotsEditorError(t *testing.T) {
	a := NewAnimator(errEditor{})
	a.Dots(context.Background(), 1, 2, "x", 5) // should return on first error, no panic
}

// TestAnimatorLoadingCanceled covers the ctx-canceled branch of runFrames.
func TestAnimatorLoadingCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeEditor{}
	a := NewAnimator(f)
	a.Loading(ctx, 1, 2, "x", time.Second)
	if len(f.edits) != 0 {
		t.Errorf("canceled Loading should not edit, got %d edits", len(f.edits))
	}
}

// TestAnimatorLoadingEditorError covers the edit-error branch of runFrames.
func TestAnimatorLoadingEditorError(t *testing.T) {
	a := NewAnimator(errEditor{})
	a.Loading(context.Background(), 1, 2, "x", time.Second) // returns on first error
}

// TestAnimatorSuccessRevealEditorError covers the final Edit error being ignored.
func TestAnimatorSuccessRevealEditorError(t *testing.T) {
	a := NewAnimator(errEditor{})
	a.SuccessReveal(context.Background(), 1, 2, "done") // must not panic
}
