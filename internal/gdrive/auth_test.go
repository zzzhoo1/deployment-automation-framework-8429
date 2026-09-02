package gdrive

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type = %q", ct)
		}
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") != "auth-code-1" {
			t.Errorf("form = %v", r.PostForm)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.ExchangeCode(context.Background(), "auth-code-1"); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	c.mu.Lock()
	at, rt := c.accessToken, c.refreshToken
	c.mu.Unlock()
	if at != "at-1" || rt != "rt-1" {
		t.Errorf("tokens = %q / %q", at, rt)
	}
}

func TestExchangeCodeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"server_error"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.ExchangeCode(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("err = %v, want HTTP 502", err)
	}
}

func TestSetRefreshTokenAndToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "refreshing" || r.PostForm.Get("refresh_token") != "stored-rt" {
			t.Errorf("form = %v", r.PostForm)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-2","expires_in":3600}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.SetRefreshToken("stored-rt")
	tok, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "at-2" {
		t.Errorf("token = %q", tok)
	}
	// Second call should be served from the cache (no new HTTP round trip).
	tok2, err := c.Token(context.Background())
	if err != nil || tok2 != "at-2" {
		t.Errorf("cached token = %q (err %v)", tok2, err)
	}
}

func TestTokenNoCredentials(t *testing.T) {
	c := NewOAuthClient("cid", "csecret")
	c.refreshToken = ""
	c.accessToken = ""
	_, err := c.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Errorf("err = %v, want no-credentials", err)
	}
}

func TestTokenServiceAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
		}
		assertion := r.PostForm.Get("assertion")
		parts := strings.Split(assertion, ".")
		if len(parts) != 3 {
			t.Fatalf("assertion = %q, want 3 JWT segments", assertion)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"sa-at","expires_in":3600}`))
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
	tok, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("Token (service account): %v", err)
	}
	if tok != "sa-at" {
		t.Errorf("token = %q", tok)
	}
}

func TestTokenServiceAccountHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
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
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("err = %v, want HTTP 401", err)
	}
}

func TestNewServiceAccountClientErrors(t *testing.T) {
	// Bad JSON.
	if _, err := NewServiceAccountClient([]byte(`{not json`)); err == nil {
		t.Error("bad JSON should error")
	}
	// Bad private key (PEM but not RSA).
	nonRSA, _ := json.Marshal(map[string]string{
		"client_email": "x@y",
		"private_key":  pkcs1PEM(t, testRSAKey(t))[:10] + "\n",
	})
	if _, err := NewServiceAccountClient(nonRSA); err == nil {
		t.Error("bad private key should error")
	}
	// PKCS8 RSA key -> covers the ParsePKCS8 branch.
	keyJSON, _ := json.Marshal(map[string]string{
		"client_email": "sa@proj.iam.gserviceaccount.com",
		"private_key":  pkcs8PEM(t, testRSAKey(t)),
	})
	c, err := NewServiceAccountClient(keyJSON)
	if err != nil {
		t.Fatalf("PKCS8 RSA key should parse: %v", err)
	}
	if !c.ServiceAccountAvailable() {
		t.Error("ServiceAccountAvailable should be true")
	}
}

func TestParseRSAPrivateKeyNoPEM(t *testing.T) {
	_, err := parseRSAPrivateKey("not a pem block")
	if err == nil || !strings.Contains(err.Error(), "no PEM block") {
		t.Errorf("err = %v, want no PEM block", err)
	}
}

func TestParseRSAPrivateKeyNotRSA(t *testing.T) {
	// A PKCS#8-wrapped EC key parses fine but is not RSA -> "not an RSA key".
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	b, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatalf("MarshalPKCS8: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
	_, err = parseRSAPrivateKey(pemStr)
	if err == nil || !strings.Contains(err.Error(), "not an RSA key") {
		t.Errorf("err = %v, want not-an-RSA-key", err)
	}
}

func TestUploadTokenError(t *testing.T) {
	c := NewOAuthClient("cid", "csecret")
	// No refresh token, no service account -> Token() fails.
	_, err := c.Upload(context.Background(), "/tmp/whatever.txt", "", "f.txt")
	if err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Errorf("err = %v, want no-credentials", err)
	}
}

func TestUploadMissingFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	_, err := c.Upload(context.Background(), filepath.Join(t.TempDir(), "missing.bin"), "", "f.bin")
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Errorf("err = %v, want open error", err)
	}
}

func TestUploadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := c.Upload(context.Background(), p, "folder1", "f.txt")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500", err)
	}
}

func TestCopyTokenError(t *testing.T) {
	c := NewOAuthClient("cid", "csecret")
	_, err := c.Copy(context.Background(), "a", "b")
	if err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Errorf("err = %v, want no-credentials", err)
	}
}

func TestMoveTokenError(t *testing.T) {
	c := NewOAuthClient("cid", "csecret")
	if err := c.Move(context.Background(), "a", "b"); err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Errorf("err = %v, want no-credentials", err)
	}
}

func TestDeleteTokenError(t *testing.T) {
	c := NewOAuthClient("cid", "csecret")
	if err := c.Delete(context.Background(), "a"); err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Errorf("err = %v, want no-credentials", err)
	}
}

func TestEmptyTrashTokenError(t *testing.T) {
	c := NewOAuthClient("cid", "csecret")
	if err := c.EmptyTrash(context.Background()); err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Errorf("err = %v, want no-credentials", err)
	}
}

func TestListFilesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"error":"teapot"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	_, err := c.ListFiles(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 418") {
		t.Errorf("err = %v, want HTTP 418", err)
	}
}

func TestSearchFilesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"error":"teapot"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	_, err := c.SearchFiles(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "HTTP 418") {
		t.Errorf("err = %v, want HTTP 418", err)
	}
}

// testRSAKey and pkcs1PEM/pkcs8PEM helpers live in client_test.go.
