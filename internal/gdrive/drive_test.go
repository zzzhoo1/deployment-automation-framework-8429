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

// redirectingTransport rewrites requests to the test server's host.
type redirectingTransport struct {
	target string
}

func (r *redirectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = r.target
	return http.DefaultTransport.RoundTrip(req)
}

// newTestClient returns a gdrive client whose HTTP calls go to the test server.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := NewOAuthClient("cid", "csecret")
	c.http = &http.Client{Transport: &redirectingTransport{target: srv.Listener.Addr().String()}}
	c.refreshToken = "refresh-123"
	return c
}

func TestListFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/files") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer access-abc" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"id": "f1", "name": "a.txt", "mimeType": "text/plain", "webLink": "https://drive.google.com/f1"},
				{"id": "f2", "name": "b.txt", "mimeType": "text/plain", "webLink": "https://drive.google.com/f2"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)
	files, err := c.ListFiles(context.Background(), "")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 || files[0].ID != "f1" || files[1].Name != "b.txt" {
		t.Errorf("files = %+v", files)
	}
}

func TestSearchFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if !strings.Contains(q, "report") {
			t.Errorf("query %q does not contain keyword", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{"id": "x", "name": "report.pdf"}}})
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)
	files, err := c.SearchFiles(context.Background(), "report")
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "report.pdf" {
		t.Errorf("files = %+v", files)
	}
}

func TestCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/files/src1/copy") {
			t.Errorf("copy request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "copy1", "name": "a.txt", "webLink": "https://drive.google.com/copy1"})
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)
	f, err := c.Copy(context.Background(), "src1", "destFolder")
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if f.ID != "copy1" {
		t.Errorf("copy id = %q", f.ID)
	}
}

func TestMove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/files/mv1"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"parents": []string{"oldParent"}})
		case r.Method == http.MethodPatch:
			q := r.URL.Query()
			if q.Get("addParents") != "newParent" || q.Get("removeParents") != "oldParent" {
				t.Errorf("move params add=%q remove=%q", q.Get("addParents"), q.Get("removeParents"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)
	if err := c.Move(context.Background(), "mv1", "newParent"); err != nil {
		t.Fatalf("Move: %v", err)
	}
}

func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/files/del1/trash") {
			t.Errorf("delete request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)
	if err := c.Delete(context.Background(), "del1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestEmptyTrash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || !strings.Contains(r.URL.Path, "/trash") {
			t.Errorf("emptytrash request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)
	if err := c.EmptyTrash(context.Background()); err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
}

func TestUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/files") {
			t.Errorf("upload request = %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("upload content-type = %q", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "up1", "name": "up.txt", "webLink": "https://drive.google.com/up1"})
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "access-abc"
	c.expiresAt = nowPlus(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "up.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	f, err := c.Upload(context.Background(), path, "folder1", "up.txt")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if f.ID != "up1" || f.WebLink != "https://drive.google.com/up1" {
		t.Errorf("upload result = %+v", f)
	}
}

func TestRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("refresh method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 3600})
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := c.RefreshToken(context.Background(), "refresh-123"); err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	c.mu.Lock()
	got := c.accessToken
	c.mu.Unlock()
	if got != "new-access" {
		t.Errorf("accessToken = %q, want new-access", got)
	}
}
