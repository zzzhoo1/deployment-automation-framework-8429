package tg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSendReplyToAndNotify covers the ReplyTo and DisableNotify branches of Send.
func TestSendReplyToAndNotify(t *testing.T) {
	var gotReply, gotNotify string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotReply = r.PostForm.Get("reply_to_message_id")
		gotNotify = r.PostForm.Get("disable_notification")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5,"chat":{"id":1}}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Send(context.Background(), SendOptions{ChatID: 1, Text: "x", ReplyTo: 9, DisableNotify: true})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotReply != "9" {
		t.Errorf("reply_to_message_id = %q, want 9", gotReply)
	}
	if gotNotify != "true" {
		t.Errorf("disable_notification = %q, want true", gotNotify)
	}
}

// TestSendNoReplyTo ensures ReplyTo=0 omits the field.
func TestSendNoReplyTo(t *testing.T) {
	var gotReply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotReply = r.PostForm.Get("reply_to_message_id")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1}}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Send(context.Background(), SendOptions{ChatID: 1, Text: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotReply != "" {
		t.Errorf("reply_to_message_id = %q, want empty", gotReply)
	}
}

// TestAnswerCallbackAlert covers the show_alert branch.
func TestAnswerCallbackAlert(t *testing.T) {
	var gotAlert string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotAlert = r.PostForm.Get("show_alert")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := c.AnswerCallback(context.Background(), "cb", "text", true); err != nil {
		t.Fatalf("AnswerCallback: %v", err)
	}
	if gotAlert != "true" {
		t.Errorf("show_alert = %q, want true", gotAlert)
	}
}

// TestCallHTTPError covers the non-200 response branch of call().
func TestCallHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"description":"flood"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	err := c.Edit(context.Background(), 1, 2, "x")
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Errorf("err = %v, want HTTP 429", err)
	}
}

// TestCallEnvelopeDecodeError covers the bad-envelope branch of call().
func TestCallEnvelopeDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	err := c.Edit(context.Background(), 1, 2, "x")
	if err == nil || !strings.Contains(err.Error(), "decode envelope") {
		t.Errorf("err = %v, want decode envelope", err)
	}
}

// TestCallResultDecodeError covers the out!=nil decode branch of call().
func TestCallResultDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":`)) // truncated -> decode error
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Send(context.Background(), SendOptions{ChatID: 1, Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want decode", err)
	}
}

// TestCallTransportError covers the http.Do error branch of call().
func TestCallTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close so the client gets a connection error
	c := newTestClient(t, srv)
	err := c.Edit(context.Background(), 1, 2, "x")
	if err == nil {
		t.Fatal("expected a transport error")
	}
}

// TestPollHTTPError covers the error path of Poll.
func TestPollHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Poll(context.Background(), 0); err == nil {
		t.Fatal("expected Poll error")
	}
}

// TestPollEmptyResult covers an empty update batch (no offset advance).
func TestPollEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	ups, err := c.Poll(context.Background(), 0)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(ups) != 0 {
		t.Errorf("got %d updates, want 0", len(ups))
	}
	c.mu.Lock()
	offset := c.offset
	c.mu.Unlock()
	if offset != 0 {
		t.Errorf("offset = %d, want 0", offset)
	}
}

// TestSendTransportError covers Send returning 0 on transport error.
func TestSendTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	c := newTestClient(t, srv)
	id, err := c.Send(context.Background(), SendOptions{ChatID: 1, Text: "x"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if id != 0 {
		t.Errorf("id = %d, want 0", id)
	}
}

var _ = errors.New // keep errors import if unused
