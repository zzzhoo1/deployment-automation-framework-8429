// Package bot wires the Telegram client, Google Drive client, and task
// manager together and implements the command handlers.
package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/config"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/gdrive"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/store"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/task"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/tg"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/ytdlp"
)

// Bot is the running application.
type Bot struct {
	cfg   *config.Config
	tg    *tg.Client
	drive *gdrive.Client
	tasks *task.Manager
	store *store.Store
	ytdlp *ytdlp.Client

	pendingMu       sync.Mutex
	pendingAuth     map[int64]string // userID -> OAuth state
	defaultFolderMu sync.Mutex
	defaultFolder   map[int64]string // userID -> default upload folder ID
	pendingVideoMu  sync.Mutex
	pendingVideo    map[int64]*ytdlp.Info // userID -> video awaiting quality choice
}

// New constructs a Bot from configuration.
func New(cfg *config.Config) (*Bot, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("bot: BOT_TOKEN is required")
	}
	s, err := store.New(cfg.DataDir + "/bot.json")
	if err != nil {
		return nil, fmt.Errorf("bot: open store: %w", err)
	}
	drive := gdrive.NewOAuthClient(cfg.GDriveClientID, cfg.GDriveClientSecret)
	b := &Bot{
		cfg:           cfg,
		tg:            tg.NewClient(cfg.BotToken),
		drive:         drive,
		store:         s,
		ytdlp:         ytdlp.NewClient(cfg.YTDLPBin),
		pendingAuth:   map[int64]string{},
		defaultFolder: map[int64]string{},
		pendingVideo:  map[int64]*ytdlp.Info{},
	}
	b.tasks = task.NewManager(cfg.DownloadDirectory, cfg.MaxConcurrentMirrors, b.upload)
	return b, nil
}

// upload performs the Drive upload step for a mirror task, honoring the
// user's configured default folder.
func (b *Bot) upload(ctx context.Context, userID int64, path, filename string) (string, error) {
	file, err := b.drive.Upload(ctx, path, b.defaultUploadFolder(userID), filename)
	if err != nil {
		return "", err
	}
	return file.WebLink, nil
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
	case "authmode":
		b.cmdAuthMode(ctx, msg, args)
	case "revoke":
		b.cmdRevoke(ctx, msg)
	case "setfolder", "setfl":
		b.cmdSetFolder(ctx, msg, args)
	case "emptytrash", "emptyTrash":
		b.cmdEmptyTrash(ctx, msg)
	case "download", "dl":
		b.cmdDownload(ctx, msg, args)
	case "ytdl":
		b.cmdYtDl(ctx, msg, args)
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
		// A plain message that matches a pending quality choice is treated as
		// the /ytdl quality selection step.
		if b.tryCaptureQuality(ctx, msg) {
			return
		}
		// A plain message that looks like a Google OAuth code is treated as
		// the auth-code capture step (the user pastes the code after /auth).
		if b.tryCaptureAuthCode(ctx, msg) {
			return
		}
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
		"📹 /ytdl <视频链接>  下载 YouTube 等视频并上传\n" +
		"🔐 /auth  授权 Google Drive\n" +
		"🔑 /revoke  撤销授权\n" +
		"⚙️ /authmode oauth|service_account  切换授权模式\n" +
		"📁 /setfolder <id>  设置默认上传文件夹\n" +
		"📂 /list [folder]  浏览目录\n" +
		"🔍 /search <keyword>  搜索文件\n" +
		"📋 /copy <src> <dst>  复制文件\n" +
		"➡️ /move <src> <dst>  移动文件\n" +
		"🗑 /delete <file>  删除文件\n" +
		"🧹 /emptytrash  清空回收站"
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: text})
}

func (b *Bot) cmdAuth(ctx context.Context, msg *tg.Message) {
	if b.cfg.GDriveClientID == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 未配置 G_DRIVE_CLIENT_ID"})
		return
	}
	state := fmt.Sprintf("u%d", msg.From.ID)
	link := b.drive.AuthURL(state)
	b.pendingMu.Lock()
	b.pendingAuth[msg.From.ID] = state
	b.pendingMu.Unlock()
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

func (b *Bot) cmdYtDl(ctx context.Context, msg *tg.Message, args string) {
	if !b.cfg.IsSudo(msg.From.ID) {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 需要管理员权限"})
		return
	}
	if !b.ytdlp.Available() {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 未找到 yt-dlp 可执行文件（设置 YTDLP_BIN）"})
		return
	}
	urlStr := strings.TrimSpace(args)
	if !regexp.MustCompile(`^https?://`).MatchString(urlStr) {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /ytdl <视频链接>"})
		return
	}
	info, err := b.ytdlp.Info(ctx, urlStr)
	if err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ 获取视频信息失败: " + err.Error()})
		return
	}
	if len(info.Formats) == 0 {
		// No selectable qualities: download best directly.
		b.startYtDlDownload(ctx, msg, info, "best")
		return
	}
	b.pendingVideoMu.Lock()
	b.pendingVideo[msg.From.ID] = info
	b.pendingVideoMu.Unlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎬 %s\n", info.Title))
	sb.WriteString("请选择画质（回复编号或 “best”）:\n")
	for i, q := range info.Formats {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, q.Label)
	}
	sb.WriteString("0. best (最高画质)\n")
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: sb.String()})
}

// startYtDlDownload downloads the chosen quality and uploads it to Drive.
func (b *Bot) startYtDlDownload(ctx context.Context, msg *tg.Message, info *ytdlp.Info, quality string) {
	statusID, _ := b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: fmt.Sprintf("⏳ 正在下载 %s（%s）…", info.Title, quality)})
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		path, err := b.ytdlp.Download(cctx, info.WebPageURL, quality, b.cfg.DownloadDirectory)
		if err != nil {
			_ = b.tg.Edit(cctx, msg.Chat.ID, statusID, "❌ yt-dlp 下载失败: "+err.Error())
			return
		}
		defer os.Remove(path)
		_ = b.tg.Edit(cctx, msg.Chat.ID, statusID, "⏳ 正在上传到 Google Drive…")
		link, err := b.upload(cctx, msg.From.ID, path, filepath.Base(path))
		if err != nil {
			_ = b.tg.Edit(cctx, msg.Chat.ID, statusID, "❌ 上传失败: "+err.Error())
			return
		}
		_ = b.tg.Edit(cctx, msg.Chat.ID, statusID, "✅ 完成: "+link)
	}()
}

// tryCaptureQuality handles a plain message that is a quality choice for a
// pending /ytdl selection. Returns true if the message was consumed.
func (b *Bot) tryCaptureQuality(ctx context.Context, msg *tg.Message) bool {
	b.pendingVideoMu.Lock()
	info, ok := b.pendingVideo[msg.From.ID]
	b.pendingVideoMu.Unlock()
	if !ok {
		return false
	}
	sel := strings.TrimSpace(strings.ToLower(msg.Text))
	var quality string
	if sel == "best" || sel == "0" {
		quality = "best"
	} else if n, err := strconv.Atoi(sel); err == nil && n >= 1 && n <= len(info.Formats) {
		quality = info.Formats[n-1].Label
	} else {
		return false
	}
	b.pendingVideoMu.Lock()
	delete(b.pendingVideo, msg.From.ID)
	b.pendingVideoMu.Unlock()
	b.startYtDlDownload(ctx, msg, info, quality)
	return true
}

func (b *Bot) cmdUnknown(ctx context.Context, msg *tg.Message, cmd string) {
	if cmd == "" {
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❓ 未知命令 /" + cmd + "，发送 /help 查看帮助"})
}

// tryCaptureAuthCode handles a plain message that is a Google OAuth code
// following /auth. Returns true if the message was consumed.
func (b *Bot) tryCaptureAuthCode(ctx context.Context, msg *tg.Message) bool {
	b.pendingMu.Lock()
	_, ok := b.pendingAuth[msg.From.ID]
	b.pendingMu.Unlock()
	if !ok {
		return false
	}
	code := strings.TrimSpace(msg.Text)
	if !regexp.MustCompile(`^[A-Za-z0-9._~-]{20,}$`).MatchString(code) {
		return false
	}
	if err := b.drive.ExchangeCode(ctx, code); err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ 授权码无效: " + err.Error()})
		return true
	}
	rec := store.CredentialRecord{UserID: msg.From.ID, Mode: "oauth"}
	_ = b.store.SaveCredential(rec)
	b.pendingMu.Lock()
	delete(b.pendingAuth, msg.From.ID)
	b.pendingMu.Unlock()
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "✅ Google Drive 授权成功"})
	return true
}

func (b *Bot) cmdAuthMode(ctx context.Context, msg *tg.Message, args string) {
	mode := strings.ToLower(strings.TrimSpace(args))
	if mode == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "当前模式: " + b.cfg.DefaultAuthMode + "\n用法: /authmode oauth|service_account"})
		return
	}
	if mode != "oauth" && mode != "service_account" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "⚠️ 模式需为 oauth 或 service_account"})
		return
	}
	b.cfg.DefaultAuthMode = mode
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "✅ 已切换授权模式: " + mode})
}

func (b *Bot) cmdRevoke(ctx context.Context, msg *tg.Message) {
	if err := b.store.DeleteCredential(msg.From.ID); err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "🔑 已撤销当前账户的 Drive 授权"})
}

func (b *Bot) cmdSetFolder(ctx context.Context, msg *tg.Message, args string) {
	id := extractID(strings.TrimSpace(args))
	if id == "" {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "用法: /setfolder <文件夹ID或链接>"})
		return
	}
	b.defaultFolderMu.Lock()
	b.defaultFolder[msg.From.ID] = id
	b.defaultFolderMu.Unlock()
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "📁 默认上传文件夹已设为: " + id})
}

func (b *Bot) cmdEmptyTrash(ctx context.Context, msg *tg.Message) {
	if err := b.drive.EmptyTrash(ctx); err != nil {
		_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "❌ " + err.Error()})
		return
	}
	_, _ = b.tg.Send(ctx, tg.SendOptions{ChatID: msg.Chat.ID, Text: "🧹 回收站已清空"})
}

// defaultUploadFolder returns the user's configured default folder ("" = root).
func (b *Bot) defaultUploadFolder(userID int64) string {
	b.defaultFolderMu.Lock()
	defer b.defaultFolderMu.Unlock()
	return b.defaultFolder[userID]
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
