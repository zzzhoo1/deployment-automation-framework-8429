// Package bot wires the Telegram client, Google Drive client, and task
// manager together and implements the command handlers.
package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/config"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/gdrive"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/task"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/tg"
)

// Bot is the running application.
type Bot struct {
	cfg   *config.Config
	tg    *tg.Client
	drive *gdrive.Client
	tasks *task.Manager
}

// New constructs a Bot from configuration.
func New(cfg *config.Config) (*Bot, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("bot: BOT_TOKEN is required")
	}
	drive := gdrive.NewOAuthClient(cfg.GDriveClientID, cfg.GDriveClientSecret)
	upload := func(ctx context.Context, path, filename string) (string, error) {
		file, err := drive.Upload(ctx, path, "", filename)
		if err != nil {
			return "", err
		}
		return file.WebLink, nil
	}
	return &Bot{
		cfg:   cfg,
		tg:    tg.NewClient(cfg.BotToken),
		drive: drive,
		tasks: task.NewManager(cfg.DownloadDirectory, cfg.MaxConcurrentMirrors, upload),
	}, nil
}

// Run starts the long-polling loop and dispatches updates until ctx ends.
func (b *Bot) Run(ctx context.Context) error {
	for {
		updates, err := b.tg.Poll(ctx, b.cfg.PollTimeoutSeconds)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, u := range updates {
			b.handle(ctx, &u)
		}
	}
}

func (b *Bot) handle(ctx context.Context, u *tg.Update) {
	switch {
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	case u.EditedMessage != nil:
		b.handleMessage(ctx, u.EditedMessage)
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tg.Message) {
	if msg.From == nil || msg.Chat == nil || msg.Text == "" {
		return
	}
	cmd, args := splitCommand(msg.Text)
	switch cmd {
	case "start", "help":
		b.cmdStart(ctx, msg)
	case "auth":
		b.cmdAuth(ctx, msg)
	case "download", "dl":
		b.cmdDownload(ctx, msg, args)
	case "list", "ls":
		b.cmdList(ctx, msg, args)
	case "search", "sd":
		b.cmdSearch(ctx, msg, args)
	case "copy", "cp":
		b.cmdCopy(ctx, msg, args)
	case "move", "mv":
		b.cmdMove(ctx, msg, args)
	case "delete", "del":
		b.cmdDelete(ctx, msg, args)
	default:
		b.cmdUnknown(ctx, msg, cmd)
	}
}

func (b *Bot) handleCallback(ctx context.Context, q *tg.CallbackQuery) {
	// mirror:<taskID>:<pause|resume|cancel>
	re := regexp.MustCompile(`^mirror:(\d+):(pause|resume|cancel)$`)
	if m := re.FindStringSubmatch(q.Data); m != nil {
		id := parseID(m[1])
		switch m[2] {
		case "pause":
			b.tasks.Pause(id)
		case "resume":
			b.tasks.Resume(id)
		case "cancel":
			b.tasks.Cancel(id)
		}
		_ = b.tg.AnswerCallback(ctx, q.ID, "ok", false)
		return
	}
	_ = b.tg.AnswerCallback(ctx, q.ID, "", false)
}

func (b *Bot) cmdStart(ctx context.Context, msg *tg.Message) {
	const text = "👋 欢迎使用 Google Drive 上传器\n\n" +
		"📥 /download <url>  下载直链并上传到 Drive\n" +
		"🔐 /auth  授权 Google Drive\n" +
		"📂 /list [folder]  浏览目录\n" +
		"🔍 /search <keyword>  搜索文件\n" +
		"📋 /copy <src> <dst>  复制文件\n" +
		"➡️ /move <src> <dst>  移动文件\n" +
		"🗑 /delete <file>  删除文件"
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: text})
}

func (b *Bot) cmdAuth(ctx context.Context, msg *tg.Message) {
	if b.cfg.GDriveClientID == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 未配置 G_DRIVE_CLIENT_ID"})
		return
	}
	state := fmt.Sprintf("u%d", msg.From.ID)
	link := b.drive.AuthURL(state)
	_, _ = b.tg.Send(ctx, tg.SendOptions{
		ChatID: msg.Chat.ID,
		Text:   "🔐 点击授权，然后把返回页面里的 code 发给我：\n" + link,
	})
}

func (b *Bot) cmdDownload(ctx context.Context, msg *tg.Message, args string) {
	if !b.cfg.IsSudo(msg.From.ID) {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 需要管理员权限"})
		return
	}
	args = strings.TrimSpace(args)
	if args == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /download <url> [文件名]"})
		return
	}
	urlStr, filename := args, ""
	if i := strings.Index(args, "|"); i > 0 {
		urlStr = strings.TrimSpace(args[:i])
		filename = strings.TrimSpace(args[i+1:])
	}
	if !regexp.MustCompile(`^https?://`).MatchString(urlStr) {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 需要 http(s) 链接"})
		return
	}
	if filename == "" {
		filename = task.FilenameFromURL(urlStr, "downloaded_file")
	}
	filename = task.Sanitize(filename)

	sentID := int64(0)
	onProgress := func(taskID int64, stage string, status task.Status) {
		text := fmt.Sprintf("📥 任务 %d\n文件: %s\n状态: %s", taskID, filename, stage)
		if id := sentID; id > 0 {
			_ = b.tg.Edit(context.Background(), msg.Chat.ID, id, text)
		}
	}
	t, err := b.tasks.Submit(ctx, msg.From.ID, msg.Chat.ID, urlStr, filename, onProgress)
	if err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ 提交失败: " + err.Error()})
		return
	}
	sentID, _ = b.tg.Send(ctx, tg.SendOptions{
		ChatID: msg.Chat.ID,
		Text:   fmt.Sprintf("📥 任务已创建\nID: %d\n文件: %s\n状态: 排队中", t.ID, filename),
	})
}

func (b *Bot) cmdList(ctx context.Context, msg *tg.Message, args string) {
	files, err := b.drive.ListFiles(ctx, strings.TrimSpace(args))
	if err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	var sb strings.Builder
	sb.WriteString("📂 目录内容:\n")
	if len(files) == 0 {
		sb.WriteString("(空)")
	}
	for i, f := range files {
		if i >= 20 {
			sb.WriteString("…\n")
			break
		}
		icon := "📄"
		if f.MimeType == "folder" {
			icon = "📁"
		}
		fmt.Fprintf(&sb, "%s %s\n", icon, f.Name)
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: sb.String()})
}

func (b *Bot) cmdSearch(ctx context.Context, msg *tg.Message, args string) {
	keyword := strings.TrimSpace(args)
	if keyword == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /search <keyword>"})
		return
	}
	files, err := b.drive.SearchFiles(ctx, keyword)
	if err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 搜索 “%s”:\n", keyword))
	if len(files) == 0 {
		sb.WriteString("(无结果)")
	}
	for i, f := range files {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&sb, "• %s\n  %s\n", f.Name, f.WebLink)
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: sb.String()})
}

func (b *Bot) cmdCopy(ctx context.Context, msg *tg.Message, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /copy <源ID或链接> <目标文件夹ID>"})
		return
	}
	src := extractID(parts[0])
	dst := extractID(parts[1])
	file, err := b.drive.Copy(ctx, src, dst)
	if err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "✅ 已复制: " + file.WebLink})
}

func (b *Bot) cmdMove(ctx context.Context, msg *tg.Message, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /move <源ID或链接> <目标文件夹ID>"})
		return
	}
	src := extractID(parts[0])
	dst := extractID(parts[1])
	if err := b.drive.Move(ctx, src, dst); err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "✅ 已移动"})
}

func (b *Bot) cmdDelete(ctx context.Context, msg *tg.Message, args string) {
	id := extractID(strings.TrimSpace(args))
	if id == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /delete <文件ID或链接>"})
		return
	}
	if err := b.drive.Delete(ctx, id); err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "🗑 已删除（移入回收站）"})
}

func (b *Bot) cmdUnknown(ctx context.Context, msg *tg.Message, cmd string) {
	if cmd == "" {
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❓ 未知命令 /" + cmd + "，发送 /help 查看帮助"})
}

// splitCommand splits "/cmd args" into the command (without slash) and args.
func splitCommand(text string) (string, string) {
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	rest := text[1:]
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		return strings.ToLower(rest[:i]), rest[i+1:]
	}
	return strings.ToLower(rest), ""
}

// extractID pulls a Drive file/folder ID out of a raw ID or a web link.
func extractID(raw string) string {
	raw = strings.TrimSpace(raw)
	re := regexp.MustCompile(`/[?&]id=([a-zA-Z0-9_-]+)`)
	if m := re.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	re2 := regexp.MustCompile(`/folders/([a-zA-Z0-9_-]+)`)
	if m := re2.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	if !strings.Contains(raw, "/") && len(raw) > 8 {
		return raw
	}
	return raw
}

func parseID(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
