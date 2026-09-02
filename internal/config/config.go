// Package config loads bot configuration from environment variables.
//
// It mirrors the Python bot's bot/config.py `config` class, but sources all
// values from the environment (12-factor) instead of hard-coded defaults.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration for the bot.
type Config struct {
	// Telegram
	BotToken string
	// Google Drive OAuth
	GDriveClientID     string
	GDriveClientSecret string
	// Google Drive service account (JSON key)
	GDriveServiceAccountJSON string
	// Storage
	DownloadDirectory string
	// Limits
	MaxMirrorFileSize    int64
	MaxConcurrentMirrors int
	// Access control
	SudoUsers []int64
	// Default auth mode: "oauth" or "service_account"
	DefaultAuthMode string
	// Polling
	PollTimeoutSeconds int
}

// Load reads configuration from the environment, applying sane defaults.
func Load() *Config {
	return &Config{
		BotToken:                 os.Getenv("BOT_TOKEN"),
		GDriveClientID:           os.Getenv("G_DRIVE_CLIENT_ID"),
		GDriveClientSecret:       os.Getenv("G_DRIVE_CLIENT_SECRET"),
		GDriveServiceAccountJSON: os.Getenv("G_DRIVE_CLIENT_SECRET_SA"),
		DownloadDirectory:        envOr("DOWNLOAD_DIRECTORY", "./downloads/"),
		MaxMirrorFileSize:        envInt64Or("MAX_MIRROR_FILE_SIZE", 10*1024*1024*1024),
		MaxConcurrentMirrors:     envIntOr("MAX_CONCURRENT_MIRRORS", 2),
		SudoUsers:                parseIDs(os.Getenv("SUDO_USERS")),
		DefaultAuthMode:          envOr("DEFAULT_AUTH_MODE", "oauth"),
		PollTimeoutSeconds:       envIntOr("POLL_TIMEOUT_SECONDS", 30),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt64Or(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// parseIDs parses a space/comma separated list of Telegram user IDs.
func parseIDs(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []int64
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ','
	}) {
		if n, err := strconv.ParseInt(part, 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// IsSudo reports whether the given user ID is in the sudo list.
// An empty sudo list means everyone is allowed.
func (c *Config) IsSudo(id int64) bool {
	if len(c.SudoUsers) == 0 {
		return true
	}
	for _, s := range c.SudoUsers {
		if s == id {
			return true
		}
	}
	return false
}
