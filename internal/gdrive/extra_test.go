package gdrive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFetchTokenHTTPError covers the non-200 branch of fetchToken.
func TestFetchTokenHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	err := c.ExchangeCode(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("err = %v, want HTTP 400", err)
	}
}

// TestFetchTokenDecodeError covers the json.Unmarshal error branch of fetchToken.
func TestFetchTokenDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	err := c.ExchangeCode(context.Background(), "code")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// TestServiceAccountTokenHTTPError covers the non-200 branch of serviceAccountToken.
func TestServiceAccountTokenHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"x"}`))
	}))
	defer srv.Close()
	key := testRSAKey(t)
	keyJSON, _ := json.Marshal(map[string]string{
		"client_email": "sa@proj.iam.gserviceaccount.com",
		"private_key":  pkcs1PEM(t, key),
	})
	c, err := NewServiceAccountClient(keyJSON)
	if err != nil {
		t.Fatalf("NewServiceAccountClient: %v", err)
	}
	c.http = &http.Client{Transport: &redirectingTransport{target: srv.Listener.Addr().String()}}
	_, err = c.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("err = %v, want HTTP 502", err)
	}
}

// TestServiceAccountTokenDecodeError covers the json decode error branch of serviceAccountToken.
func TestServiceAccountTokenDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	key := testRSAKey(t)
	keyJSON, _ := json.Marshal(map[string]string{
		"client_email": "sa@proj.iam.gserviceaccount.com",
		"private_key":  pkcs1PEM(t, key),
	})
	c, err := NewServiceAccountClient(keyJSON)
	if err != nil {
		t.Fatalf("NewServiceAccountClient: %v", err)
	}
	c.http = &http.Client{Transport: &redirectingTransport{target: srv.Listener.Addr().String()}}
	_, err = c.Token(context.Background())
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// TestQueryDecodeError covers the response decode error branch of query.
func TestQueryDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	if _, err := c.ListFiles(context.Background(), ""); err == nil {
		t.Fatal("expected decode error")
	}
}

// TestUploadDecodeError covers the response decode error branch of Upload.
func TestUploadDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Upload(context.Background(), p, "folder1", "f.txt"); err == nil {
		t.Fatal("expected decode error")
	}
}

// TestCopyDecodeError covers the response decode error branch of Copy.
func TestCopyDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	if _, err := c.Copy(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected decode error")
	}
}

// TestCopyHTTPError covers the non-200 branch of Copy.
func TestCopyHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	_, err := c.Copy(context.Background(), "a", "b")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err = %v, want HTTP 404", err)
	}
}

// TestMoveDecodeError covers the parents-fetch decode error branch of Move.
func TestMoveDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	if err := c.Move(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected decode error")
	}
}

// TestMoveHTTPError covers the non-200 branch of the PATCH request in Move.
func TestMoveHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"parents":["old"]}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"conflict"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	err := c.Move(context.Background(), "a", "b")
	if err == nil || !strings.Contains(err.Error(), "HTTP 409") {
		t.Errorf("err = %v, want HTTP 409", err)
	}
}

// TestDeleteHTTPError covers the non-200 branch of Delete.
func TestDeleteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	err := c.Delete(context.Background(), "a")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err = %v, want HTTP 404", err)
	}
}

// TestEmptyTrashHTTPError covers the non-200 branch of EmptyTrash.
func TestEmptyTrashHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	err := c.EmptyTrash(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500", err)
	}
}

// TestSearchFilesQuoteEscape covers the keyword single-quote escaping branch.
func TestSearchFilesQuoteEscape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if !strings.Contains(q, `O\'Brien`) {
			t.Errorf("query = %q, want escaped quote", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	if _, err := c.SearchFiles(context.Background(), "O'Brien"); err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
}
