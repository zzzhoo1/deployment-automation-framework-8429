package tg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := NewClient("test-token")
	c.api = srv.URL
	return c
}

func TestSend(t *testing.T) {
	var gotChat, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotChat = r.PostForm.Get("chat_id")
		gotText = r.PostForm.Get("text")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42,"chat":{"id":7}}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	id, err := c.Send(context.Background(), SendOptions{ChatID: 7, Text: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != 42 {
		t.Errorf("message id = %d, want 42", id)
	}
	if gotChat != "7" || gotText != "hello" {
		t.Errorf("server saw chat=%q text=%q", gotChat, gotText)
	}
}

func TestEdit(t *testing.T) {
	var gotMsgID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editMessageText" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = r.ParseForm()
		gotMsgID = r.PostForm.Get("message_id")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := c.Edit(context.Background(), 7, 99, "updated"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if gotMsgID != "99" {
		t.Errorf("message_id = %q, want 99", gotMsgID)
	}
}

func TestAnswerCallback(t *testing.T) {
	var gotCallback string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/answerCallbackQuery" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = r.ParseForm()
		gotCallback = r.PostForm.Get("callback_query_id")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := c.AnswerCallback(context.Background(), "cb-1", "done", false); err != nil {
		t.Fatalf("AnswerCallback: %v", err)
	}
	if gotCallback != "cb-1" {
		t.Errorf("callback_query_id = %q, want cb-1", gotCallback)
	}
}

func TestPollTracksOffset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getUpdates" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":100,"message":{"message_id":1,"chat":{"id":1},"text":"a"}},{"update_id":101,"message":{"message_id":2,"chat":{"id":1},"text":"b"}}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	ups, err := c.Poll(context.Background(), 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(ups) != 2 {
		t.Fatalf("got %d updates, want 2", len(ups))
	}
	c.mu.Lock()
	offset := c.offset
	lastSeen := c.lastSeen
	c.mu.Unlock()
	if offset != 102 {
		t.Errorf("offset = %d, want 102", offset)
	}
	if lastSeen != 101 {
		t.Errorf("lastSeen = %d, want 101", lastSeen)
	}
}

func TestCallErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := c.Edit(context.Background(), 1, 2, "x"); err == nil {
		t.Fatal("expected error for ok:false envelope")
	} else if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("error = %q, want to contain description", err)
	}
}
