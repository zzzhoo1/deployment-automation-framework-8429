package config

import (
	"testing"
)

func TestParseIDs(t *testing.T) {
	if got := parseIDs(""); got != nil {
		t.Errorf("parseIDs(\"\") = %v, want nil", got)
	}
	if got := parseIDs("  "); got != nil {
		t.Errorf("parseIDs(whitespace) = %v, want nil", got)
	}
	got := parseIDs("1, 2 3,4")
	want := []int64{1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("parseIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseIDs = %v, want %v", got, want)
		}
	}
	// Non-numeric tokens are skipped.
	if got := parseIDs("1,abc,2"); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("parseIDs(1,abc,2) = %v", got)
	}
}

func TestIsSudo(t *testing.T) {
	empty := &Config{}
	if !empty.IsSudo(123) {
		t.Error("empty sudo list should allow everyone")
	}
	c := &Config{SudoUsers: []int64{10, 20}}
	if !c.IsSudo(10) || !c.IsSudo(20) {
		t.Error("listed users should be sudo")
	}
	if c.IsSudo(30) {
		t.Error("unlisted user should not be sudo")
	}
}

func TestLoadDefaultsAndEnv(t *testing.T) {
	t.Setenv("BOT_TOKEN", "tok-123")
	t.Setenv("SUDO_USERS", "5,6")
	t.Setenv("MAX_CONCURRENT_MIRRORS", "7")
	t.Setenv("POLL_TIMEOUT_SECONDS", "45")
	t.Setenv("DATA_DIR", "/tmp/dd")
	t.Setenv("DOWNLOAD_DIRECTORY", "/tmp/dl")
	t.Setenv("YTDLP_BIN", "/usr/bin/yt-dlp")
	t.Setenv("DEFAULT_AUTH_MODE", "service_account")

	c := Load()
	if c.BotToken != "tok-123" {
		t.Errorf("BotToken = %q", c.BotToken)
	}
	if len(c.SudoUsers) != 2 || c.SudoUsers[0] != 5 || c.SudoUsers[1] != 6 {
		t.Errorf("SudoUsers = %v", c.SudoUsers)
	}
	if c.MaxConcurrentMirrors != 7 {
		t.Errorf("MaxConcurrentMirrors = %d", c.MaxConcurrentMirrors)
	}
	if c.PollTimeoutSeconds != 45 {
		t.Errorf("PollTimeoutSeconds = %d", c.PollTimeoutSeconds)
	}
	if c.DataDir != "/tmp/dd" || c.DownloadDirectory != "/tmp/dl" {
		t.Errorf("DataDir/DownloadDirectory = %q/%q", c.DataDir, c.DownloadDirectory)
	}
	if c.YTDLPBin != "/usr/bin/yt-dlp" {
		t.Errorf("YTDLPBin = %q", c.YTDLPBin)
	}
	if c.DefaultAuthMode != "service_account" {
		t.Errorf("DefaultAuthMode = %q", c.DefaultAuthMode)
	}
}

func TestLoadDefaultsWhenUnset(t *testing.T) {
	for _, k := range []string{"DATA_DIR", "DOWNLOAD_DIRECTORY", "MAX_CONCURRENT_MIRRORS", "POLL_TIMEOUT_SECONDS", "DEFAULT_AUTH_MODE", "MAX_MIRROR_FILE_SIZE"} {
		t.Setenv(k, "")
	}
	c := Load()
	if c.DataDir != "./data/" {
		t.Errorf("default DataDir = %q", c.DataDir)
	}
	if c.DownloadDirectory != "./downloads/" {
		t.Errorf("default DownloadDirectory = %q", c.DownloadDirectory)
	}
	if c.MaxConcurrentMirrors != 2 {
		t.Errorf("default MaxConcurrentMirrors = %d", c.MaxConcurrentMirrors)
	}
	if c.PollTimeoutSeconds != 30 {
		t.Errorf("default PollTimeoutSeconds = %d", c.PollTimeoutSeconds)
	}
	if c.DefaultAuthMode != "oauth" {
		t.Errorf("default DefaultAuthMode = %q", c.DefaultAuthMode)
	}
}
