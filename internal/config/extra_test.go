package config

import (
	"testing"
)

func TestEnvInt64OrInvalid(t *testing.T) {
	t.Setenv("MAX_MIRROR_FILE_SIZE", "not-a-number")
	c := Load()
	if c.MaxMirrorFileSize != 10*1024*1024*1024 {
		t.Errorf("invalid MAX_MIRROR_FILE_SIZE should fall back to default, got %d", c.MaxMirrorFileSize)
	}
}

func TestEnvIntOrInvalid(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_MIRRORS", "abc")
	c := Load()
	if c.MaxConcurrentMirrors != 2 {
		t.Errorf("invalid MAX_CONCURRENT_MIRRORS should fall back to default, got %d", c.MaxConcurrentMirrors)
	}
	t.Setenv("POLL_TIMEOUT_SECONDS", "xyz")
	c = Load()
	if c.PollTimeoutSeconds != 30 {
		t.Errorf("invalid POLL_TIMEOUT_SECONDS should fall back to default, got %d", c.PollTimeoutSeconds)
	}
}
