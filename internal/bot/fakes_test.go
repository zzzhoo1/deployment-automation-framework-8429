package bot

import (
	"context"
	"errors"
	"sync"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/config"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/gdrive"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/store"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/task"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/tg"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/ytdlp"
)

// pollResult is a canned Poll response.
type pollResult struct {
	updates []tg.Update
	err     error
}

// fakeTG records sends/edits and returns canned values.
type fakeTG struct {
	mu       sync.Mutex
	sent     []tg.SendOptions
	edits    []editCall
	lastSent int64

	pollMu  sync.Mutex
	pollSeq []pollResult
	pollIdx int
}

type editCall struct {
	chatID, msgID int64
	text          string
}

func (f *fakeTG) Send(ctx context.Context, opts tg.SendOptions) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, opts)
	f.lastSent++
	return f.lastSent, nil
}

func (f *fakeTG) Edit(ctx context.Context, chatID, messageID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, editCall{chatID, messageID, text})
	return nil
}

func (f *fakeTG) AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool) error {
	return nil
}

func (f *fakeTG) Poll(ctx context.Context, timeoutSeconds int) ([]tg.Update, error) {
	f.pollMu.Lock()
	defer f.pollMu.Unlock()
	if f.pollIdx < len(f.pollSeq) {
		r := f.pollSeq[f.pollIdx]
		f.pollIdx++
		return r.updates, r.err
	}
	return nil, errPollDone
}

func (f *fakeTG) lastText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1].Text
}

// lastEditText returns the text of the most recent edit ("" if none).
func (f *fakeTG) lastEditText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.edits) == 0 {
		return ""
	}
	return f.edits[len(f.edits)-1].text
}

// fakeDrive records calls and returns canned results.
type fakeDrive struct {
	mu          sync.Mutex
	emptyErr    error
	listFiles   []gdrive.File
	searchFiles []gdrive.File
	copied      []copyCall
	moved       []moveCall
	deleted     []string
	exchanged   []string
	listErr     error
	searchErr   error
	copyErr     error
	moveErr     error
	deleteErr   error
	uploadErr   error
	exchangeErr error
}

type copyCall struct{ src, dst string }
type moveCall struct{ src, dst string }

func (f *fakeDrive) AuthURL(state string) string { return "https://auth?state=" + state }
func (f *fakeDrive) ExchangeCode(ctx context.Context, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exchanged = append(f.exchanged, code)
	return f.exchangeErr
}
func (f *fakeDrive) ListFiles(ctx context.Context, folderID string) ([]gdrive.File, error) {
	return f.listFiles, f.listErr
}
func (f *fakeDrive) SearchFiles(ctx context.Context, keyword string) ([]gdrive.File, error) {
	return f.searchFiles, f.searchErr
}
func (f *fakeDrive) Upload(ctx context.Context, path, folderID, name string) (*gdrive.File, error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	return &gdrive.File{ID: "up", WebLink: "https://drive.google.com/up"}, nil
}
func (f *fakeDrive) Copy(ctx context.Context, fileID, destFolder string) (*gdrive.File, error) {
	f.mu.Lock()
	f.copied = append(f.copied, copyCall{fileID, destFolder})
	f.mu.Unlock()
	if f.copyErr != nil {
		return nil, f.copyErr
	}
	return &gdrive.File{ID: "copy", WebLink: "https://drive.google.com/copy"}, nil
}
func (f *fakeDrive) Move(ctx context.Context, fileID, newFolder string) error {
	f.mu.Lock()
	f.moved = append(f.moved, moveCall{fileID, newFolder})
	f.mu.Unlock()
	return f.moveErr
}
func (f *fakeDrive) Delete(ctx context.Context, fileID string) error {
	f.mu.Lock()
	f.deleted = append(f.deleted, fileID)
	f.mu.Unlock()
	return f.deleteErr
}
func (f *fakeDrive) EmptyTrash(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.emptyErr
}

// fakeTasks records pause/resume/cancel and returns a canned task.
type fakeTasks struct {
	mu        sync.Mutex
	paused    []int64
	resumed   []int64
	canceled  []int64
	submitted []string
	submitErr error
}

func (f *fakeTasks) Submit(ctx context.Context, userID, chatID int64, urlStr, filename string, onProgress task.ProgressFunc) (*task.Task, error) {
	f.mu.Lock()
	f.submitted = append(f.submitted, urlStr)
	f.mu.Unlock()
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return &task.Task{}, nil
}
func (f *fakeTasks) Pause(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, id)
	return true
}
func (f *fakeTasks) Resume(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = append(f.resumed, id)
	return true
}
func (f *fakeTasks) Cancel(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceled = append(f.canceled, id)
	return true
}

// fakeStore is an in-memory credential store.
type fakeStore struct {
	mu        sync.Mutex
	creds     map[int64]*store.CredentialRecord
	saved     []store.CredentialRecord
	deleteErr error
}

func newFakeStore() *fakeStore { return &fakeStore{creds: map[int64]*store.CredentialRecord{}} }

func (f *fakeStore) SaveCredential(rec store.CredentialRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := rec
	f.creds[rec.UserID] = &r
	f.saved = append(f.saved, rec)
	return nil
}
func (f *fakeStore) GetCredential(userID int64) *store.CredentialRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creds[userID]
}
func (f *fakeStore) DeleteCredential(userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.creds, userID)
	return f.deleteErr
}
func (f *fakeStore) SaveTask(rec store.TaskRecord) error { return nil }
func (f *fakeStore) GetTask(id int64) *store.TaskRecord  { return nil }

// fakeYtdlp returns canned video info and download results.
type fakeYtdlp struct {
	mu          sync.Mutex
	available   bool
	info        *ytdlp.Info
	infoErr     error
	downloadErr error
	downloaded  []string
}

func (f *fakeYtdlp) Available() bool { return f.available }
func (f *fakeYtdlp) Info(ctx context.Context, url string) (*ytdlp.Info, error) {
	return f.info, f.infoErr
}
func (f *fakeYtdlp) Download(ctx context.Context, url, quality, outDir string) (string, error) {
	f.mu.Lock()
	f.downloaded = append(f.downloaded, quality)
	f.mu.Unlock()
	if f.downloadErr != nil {
		return "", f.downloadErr
	}
	return outDir + "/V.mp4", nil
}

// newTestBot wires a Bot with fakes for handler testing.
func newTestBot() (*Bot, *fakeTG, *fakeTasks, *fakeStore, *fakeDrive, *fakeYtdlp) {
	tg := &fakeTG{}
	tasks := &fakeTasks{}
	st := newFakeStore()
	drive := &fakeDrive{}
	ytd := &fakeYtdlp{available: true, info: &ytdlp.Info{
		Title:      "V",
		WebPageURL: "https://example.com/v",
		Formats:    []ytdlp.Quality{{FormatID: "1", Label: "360p", Height: 360}, {FormatID: "2", Label: "720p", Height: 720}},
	}}
	cfg := &config.Config{BotToken: "t", DefaultAuthMode: "oauth", SudoUsers: []int64{1}, DownloadDirectory: "/tmp/dl", DataDir: "/tmp/dd"}
	b := &Bot{
		cfg:           cfg,
		tg:            tg,
		drive:         drive,
		tasks:         tasks,
		store:         st,
		ytdlp:         ytd,
		pendingAuth:   map[int64]string{},
		defaultFolder: map[int64]string{},
		pendingVideo:  map[int64]*ytdlp.Info{},
	}
	return b, tg, tasks, st, drive, ytd
}

func msg(fromID, chatID int64, text string) *tg.Message {
	return &tg.Message{From: &tg.User{ID: fromID}, Chat: &tg.Chat{ID: chatID}, Text: text}
}

func tgCallback(data string) *tg.CallbackQuery {
	return &tg.CallbackQuery{ID: "cb", Data: data}
}

func mustCred(userID int64) store.CredentialRecord {
	return store.CredentialRecord{UserID: userID, Mode: "oauth", Payload: map[string]string{"refresh_token": "***"}}
}

func ytdlpInfo() *ytdlp.Info {
	return &ytdlp.Info{
		Title:      "V",
		WebPageURL: "https://example.com/v",
		Formats:    []ytdlp.Quality{{FormatID: "1", Label: "360p", Height: 360}, {FormatID: "2", Label: "720p", Height: 720}},
	}
}

// errBoom is a sentinel error for tests.
type errBoom struct{}

func (errBoom) Error() string { return "boom" }

// errPollDone signals the fake Poll sequence is exhausted so Run() can exit.
var errPollDone = errors.New("poll sequence exhausted")
