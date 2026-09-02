package store

import (
	"testing"
)

func TestCredentialRoundTrip(t *testing.T) {
	s, err := New(t.TempDir() + "/bot.json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := CredentialRecord{UserID: 42, Mode: "oauth", Payload: map[string]string{"refresh_token": "rt-123"}}
	if err := s.SaveCredential(rec); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	got := s.GetCredential(42)
	if got == nil {
		t.Fatal("GetCredential returned nil")
	}
	if got.Payload["refresh_token"] != "rt-123" || got.Mode != "oauth" {
		t.Errorf("got %+v", got)
	}
	if err := s.DeleteCredential(42); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if s.GetCredential(42) != nil {
		t.Error("credential still present after delete")
	}
}

func TestTaskRoundTrip(t *testing.T) {
	s, _ := New(t.TempDir() + "/bot.json")
	rec := TaskRecord{ID: 7, UserID: 1, ChatID: 2, URL: "https://x/y", Filename: "y", Status: "done", Stage: "完成"}
	if err := s.SaveTask(rec); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	got := s.GetTask(7)
	if got == nil || got.Filename != "y" || got.Status != "done" {
		t.Errorf("got %+v", got)
	}
}
