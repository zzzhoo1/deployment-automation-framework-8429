package bot

import (
	"context"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/gdrive"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/store"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/task"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/tg"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/ytdlp"
)

// The interfaces below capture the subset of each dependency the bot uses.
// The concrete implementations (*tg.Client, *gdrive.Client, *task.Manager,
// *store.Store, *ytdlp.Client) satisfy them, so New() is unchanged; they exist
// so the orchestration layer can be exercised in tests with fakes.

// tgClient is the Telegram client surface used by the bot.
type tgClient interface {
	Send(ctx context.Context, opts tg.SendOptions) (int64, error)
	Edit(ctx context.Context, chatID, messageID int64, text string) error
	AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool) error
	Poll(ctx context.Context, timeoutSeconds int) ([]tg.Update, error)
}

// driveClient is the Google Drive client surface used by the bot.
type driveClient interface {
	AuthURL(state string) string
	ExchangeCode(ctx context.Context, code string) error
	ListFiles(ctx context.Context, folderID string) ([]gdrive.File, error)
	SearchFiles(ctx context.Context, keyword string) ([]gdrive.File, error)
	Upload(ctx context.Context, path, folderID, name string) (*gdrive.File, error)
	Copy(ctx context.Context, fileID, destFolder string) (*gdrive.File, error)
	Move(ctx context.Context, fileID, newFolder string) error
	Delete(ctx context.Context, fileID string) error
	EmptyTrash(ctx context.Context) error
}

// taskManager is the mirror-task manager surface used by the bot.
type taskManager interface {
	Submit(ctx context.Context, userID, chatID int64, urlStr, filename string, onProgress task.ProgressFunc) (*task.Task, error)
	Pause(id int64) bool
	Resume(id int64) bool
	Cancel(id int64) bool
}

// storeIface is the persistence surface used by the bot.
type storeIface interface {
	SaveCredential(rec store.CredentialRecord) error
	GetCredential(userID int64) *store.CredentialRecord
	DeleteCredential(userID int64) error
	SaveTask(rec store.TaskRecord) error
	GetTask(id int64) *store.TaskRecord
}

// ytdlpClient is the yt-dlp wrapper surface used by the bot.
type ytdlpClient interface {
	Available() bool
	Info(ctx context.Context, url string) (*ytdlp.Info, error)
	Download(ctx context.Context, url, quality, outDir string) (string, error)
}
