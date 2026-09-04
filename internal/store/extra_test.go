package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewMkdirAllError covers the MkdirAll error branch of New.
func TestNewMkdirAllError(t *testing.T) {
	dir := t.TempDir()
	// A regular file where a directory is expected makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(dir, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(dir, "blocker", "sub", "bot.json")); err == nil {
		t.Fatal("expected MkdirAll error")
	}
}

// TestLoadCorruptJSON covers the load() fallback when the file is not valid JSON.
func TestLoadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(CredentialRecord{UserID: 1, Mode: "oauth"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.GetCredential(1); got != nil {
		t.Errorf("corrupt file should yield nil, got %+v", got)
	}
}

// TestLoadNullMaps covers the post-unmarshal nil-map reinitialization branches.
func TestLoadNullMaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"credentials":null,"tasks":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.GetCredential(1); got != nil {
		t.Errorf("null maps should yield nil, got %+v", got)
	}
	// Saving still works after loading null maps.
	if err := s.SaveTask(TaskRecord{ID: 1, URL: "u"}); err != nil {
		t.Fatal(err)
	}
	if got := s.GetTask(1); got == nil {
		t.Error("task should be saved after null-map load")
	}
}

// TestSaveWriteFileError covers the WriteFile error branch of save.
func TestSaveWriteFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	// Make the .tmp path a directory so WriteFile fails (EISDIR).
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(CredentialRecord{UserID: 1}); err == nil {
		t.Fatal("expected WriteFile error")
	}
}

// TestSaveRenameError covers the Rename error branch of save.
func TestSaveRenameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	// Make the target a non-empty directory so Rename fails (ENOTEMPTY).
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(CredentialRecord{UserID: 1}); err == nil {
		t.Fatal("expected Rename error")
	}
}

// TestGetTaskMissing covers the not-found branch of GetTask.
func TestGetTaskMissing(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "bot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.GetTask(99); got != nil {
		t.Errorf("missing task = %+v, want nil", got)
	}
}
