# deployment-automation-framework-8429
Complete deployment automation framework with multi-stage orchestration, performance validation, and production rollout procedures

## Google Drive Uploader Telegram Bot (Go)

A Go rewrite of [zzzhoo1/google-drive-telegram-bot](https://github.com/zzzhoo1/google-drive-telegram-bot).
It is a Telegram bot that mirrors files from direct URLs into Google Drive, and
provides Drive browsing, search, copy, move, and delete.

The original is a ~15k-line Python project built on Pyrogram + google-api-python-client.
This rewrite uses only the Go standard library (no third-party dependencies) and
implements the core functionality:

- **Telegram Bot API client** (`internal/tg`) — long polling, send/edit messages,
  inline-button callbacks.
- **Google Drive client** (`internal/gdrive`) — OAuth authorization-code flow,
  service-account (JWT) auth, multipart upload, list, search, copy, move, delete.
- **Mirror task pipeline** (`internal/task`) — download a URL then upload to Drive,
  with pause / resume / cancel and progress callbacks, bounded concurrency.
- **Persistence** (`internal/store`) — file-backed credential and task store
  (replaces the original's SQLite layer, keeping the project dependency-free).
- **yt-dlp integration** (`internal/ytdlp`) — video metadata/quality listing and
  download via the yt-dlp CLI, wired into the download→upload pipeline.
- **Command handlers** (`internal/bot`) — `/start`, `/help`, `/auth`, `/authmode`,
  `/revoke`, `/setfolder`, `/emptytrash`, `/download`, `/ytdl`, `/list`, `/search`,
  `/copy`, `/move`, `/delete`.

### Layout

```
cmd/gdrive-bot/        entrypoint
internal/config/       environment-based configuration
internal/tg/           Telegram Bot API client
internal/gdrive/       Google Drive API client (OAuth + service account)
internal/task/         mirror task manager (download -> upload, pause/resume/cancel)
internal/store/        file-backed credential/task persistence
internal/ytdlp/        yt-dlp CLI wrapper (video metadata + download)
internal/bot/          command handlers wiring the above together
```

### Build & test

```sh
go build ./...
go test ./...
go run ./cmd/gdrive-bot
```

### Configuration

All configuration is read from environment variables (see `.env.example`):

| Variable | Description |
| --- | --- |
| `BOT_TOKEN` | Telegram bot token (required) |
| `G_DRIVE_CLIENT_ID` | Google OAuth client ID |
| `G_DRIVE_CLIENT_SECRET` | Google OAuth client secret |
| `G_DRIVE_CLIENT_SECRET_SA` | Service-account JSON key (optional, alternative to OAuth) |
| `DOWNLOAD_DIRECTORY` | Local temp dir for downloads (default `./downloads/`) |
| `DATA_DIR` | Persistence dir for credentials/tasks (default `./data/`) |
| `MAX_MIRROR_FILE_SIZE` | Max file size in bytes (default 10 GiB) |
| `MAX_CONCURRENT_MIRRORS` | Concurrent mirror tasks (default 2) |
| `SUDO_USERS` | Space/comma separated Telegram user IDs (empty = allow all) |
| `DEFAULT_AUTH_MODE` | `oauth` or `service_account` (default `oauth`) |
| `POLL_TIMEOUT_SECONDS` | Long-poll timeout (default 30) |
| `YTDLP_BIN` | Path to the yt-dlp binary (default: `yt-dlp` from PATH) |

### Notes

- The rewrite focuses on the core mirror + Drive-management surface. The original's
  UI animation layer is intentionally left out of this pass; the task manager's
  `UploadFunc` hook is the seam where it would be wired in. Persistence is provided
  by `internal/store` (JSON file) in place of the original's SQLite/SQLAlchemy layer.
- yt-dlp integration shells out to the `yt-dlp` CLI (set `YTDLP_BIN` to point at a
  specific binary). The interactive quality selector from the original is reproduced
  as a numbered text menu.
