// Package ui provides the presentation layer: animation frame sets and
// Apple-style message formatting, mirroring the original Python project's
// bot/ui_animations.py and bot/ui_apple_style.py. Formatting helpers are pure
// and unit-testable; the Animator drives frame-by-frame edits of an existing
// Telegram message.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MessageEditor edits an existing message. *tg.Client satisfies this.
type MessageEditor interface {
	Edit(ctx context.Context, chatID, messageID int64, text string) error
}

// Frames are predefined animation frame sets.
var Frames = struct {
	Loading    []string
	Dots       []string
	Success    []string
	Download   []string
	Upload     []string
	Search     []string
	Processing []string
}{
	Loading:    []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	Dots:       []string{".", "..", "...", "....", "....."},
	Success:    []string{"○", "◔", "◑", "◕", "●", "✓", "✅"},
	Download:   []string{"⬇️ ", "⬇️ ▁", "⬇️ ▂", "⬇️ ▃", "⬇️ ▄", "⬇️ ▅", "⬇️ ▆", "⬇️ ▇", "⬇️ █"},
	Upload:     []string{"⬆️ ", "⬆️ ▁", "⬆️ ▂", "⬆️ ▃", "⬆️ ▄", "⬆️ ▅", "⬆️ ▆", "⬆️ ▇", "⬆️ █"},
	Search:     []string{"🔍 .", "🔍 ..", "🔍 ...", "🔎 ...", "🔎 ..", "🔎 ."},
	Processing: []string{"⚙️ ", "⚙️  ○", "⚙️  ◔", "⚙️  ◑", "⚙️  ◕", "⚙️  ●"},
}

// statusIcons maps progress status labels to display icons.
var statusIcons = map[string]string{
	"downloading": "⬇️",
	"uploading":   "⬆️",
	"processing":  "⚙️",
	"completed":   "✅",
	"mirroring":   "🪞",
	"queued":      "⏳",
	"paused":      "⏸️",
	"failed":      "❌",
}

// FormatProgress renders an Apple-style progress block.
func FormatProgress(current, total int64, status, filename, speed string) string {
	pct := 0.0
	if total > 0 {
		pct = float64(current) / float64(total) * 100
	}
	const barLen = 10
	filled := int(pct / 100 * barLen)
	if filled > barLen {
		filled = barLen
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)

	icon, ok := statusIcons[status]
	if !ok {
		icon = statusIcons["processing"]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s **%s**\n", icon, titleCase(status))
	if filename != "" {
		fmt.Fprintf(&sb, "`%s`\n", filename)
	}
	fmt.Fprintf(&sb, "%s %.1f%%\n", bar, pct)
	fmt.Fprintf(&sb, "%.1f MB / %.1f MB", float64(current)/(1024*1024), float64(total)/(1024*1024))
	if speed != "" {
		fmt.Fprintf(&sb, " • %s", speed)
	}
	return sb.String()
}

// FormatSuccess renders a standardized success message.
func FormatSuccess(title, detail string) string {
	var sb strings.Builder
	sb.WriteString("✅ ")
	sb.WriteString(title)
	if detail != "" {
		sb.WriteString("\n\n")
		sb.WriteString(detail)
	}
	return sb.String()
}

// FormatError renders a standardized error message.
func FormatError(title, detail, action string) string {
	var sb strings.Builder
	sb.WriteString("❌ ")
	sb.WriteString(title)
	if detail != "" {
		sb.WriteString("\n\n")
		sb.WriteString(detail)
	}
	if action != "" {
		sb.WriteString("\n\n")
		sb.WriteString(action)
	}
	return sb.String()
}

// FormatList renders a bulleted list with an optional title.
func FormatList(items []string, title, icon string) string {
	if icon == "" {
		icon = "•"
	}
	var sb strings.Builder
	if title != "" {
		sb.WriteString("**")
		sb.WriteString(title)
		sb.WriteString("**\n")
	}
	for _, it := range items {
		sb.WriteString(icon)
		sb.WriteString(" ")
		sb.WriteString(it)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Animator drives frame-by-frame message edits.
type Animator struct {
	editor MessageEditor
}

// NewAnimator creates an Animator that edits messages via editor.
func NewAnimator(editor MessageEditor) *Animator {
	return &Animator{editor: editor}
}

// Loading animates a spinner on the message for the given duration.
func (a *Animator) Loading(ctx context.Context, chatID, messageID int64, baseText string, duration time.Duration) {
	a.runFrames(ctx, chatID, messageID, Frames.Loading, func(f string) string { return f + " " + baseText }, 300*time.Millisecond, duration)
}

// Dots animates a trailing-dots indicator on the message.
func (a *Animator) Dots(ctx context.Context, chatID, messageID int64, baseText string, cycles int) {
	for c := 0; c < cycles; c++ {
		for _, d := range Frames.Dots {
			if ctx.Err() != nil {
				return
			}
			if err := a.editor.Edit(ctx, chatID, messageID, baseText+d); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
}

// SuccessReveal animates the success frames, then shows the final text.
func (a *Animator) SuccessReveal(ctx context.Context, chatID, messageID int64, finalText string) {
	a.runFrames(ctx, chatID, messageID, Frames.Success, func(f string) string { return f + " 处理中..." }, 150*time.Millisecond, time.Duration(len(Frames.Success))*150*time.Millisecond)
	_ = a.editor.Edit(ctx, chatID, messageID, finalText)
}

// runFrames cycles through frames, editing the message, until duration elapses
// or ctx ends.
func (a *Animator) runFrames(ctx context.Context, chatID, messageID int64, frames []string, render func(string) string, interval, duration time.Duration) {
	deadline := time.Now().Add(duration)
	i := 0
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return
		}
		if err := a.editor.Edit(ctx, chatID, messageID, render(frames[i%len(frames)])); err != nil {
			return
		}
		i++
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
