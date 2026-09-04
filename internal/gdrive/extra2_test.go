package gdrive

import (
	"context"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// httptestServer starts an httptest.Server and registers cleanup on t.
func httptestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// writeFile writes data to path.
func writeFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o644)
}

// failTransport makes every RoundTrip fail, exercising the http.Do error
// branches of the client methods.
type failTransport struct{ err error }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

// errTransportFail is a distinctive sentinel for transport-error assertions.
var errTransportFail = errors.New("transport-fail")

func newFailClient(t *testing.T) *Client {
	t.Helper()
	c := NewOAuthClient("cid", "csecret")
	c.http = &http.Client{Transport: failTransport{err: errTransportFail}}
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	return c
}

func TestFetchTokenTransportError(t *testing.T) {
	c := newFailClient(t)
	c.accessToken = "" // force a token fetch
	err := c.ExchangeCode(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "transport-fail") {
		t.Errorf("err = %v, want transport error", err)
	}
}

func TestServiceAccountTokenTransportError(t *testing.T) {
	key := testRSAKey(t)
	keyJSON := []byte(`{"client_email":"sa@test.iam.gserviceaccount.com","private_key":` +
		jsonString(t, pkcs1PEM(t, key)) + `}`)
	c, err := NewServiceAccountClient(keyJSON)
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: failTransport{err: errTransportFail}}
	_, err = c.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "transport-fail") {
		t.Errorf("err = %v, want transport error", err)
	}
}

func TestTokenServiceAccountErrorBranch(t *testing.T) {
	// Token() via service account where serviceAccountToken fails.
	key := testRSAKey(t)
	keyJSON := []byte(`{"client_email":"sa@test.iam.gserviceaccount.com","private_key":` +
		jsonString(t, pkcs1PEM(t, key)) + `}`)
	c, err := NewServiceAccountClient(keyJSON)
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: failTransport{err: errTransportFail}}
	_, err = c.Token(context.Background())
	if err == nil {
		t.Fatal("expected service-account token error via Token")
	}
}

func TestQueryTransportError(t *testing.T) {
	c := newFailClient(t)
	if _, err := c.ListFiles(context.Background(), ""); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestUploadTransportError(t *testing.T) {
	c := newFailClient(t)
	p := t.TempDir() + "/f.txt"
	if err := writeFile(p, "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Upload(context.Background(), p, "folder1", "f.txt"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestCopyTransportError(t *testing.T) {
	c := newFailClient(t)
	if _, err := c.Copy(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestMoveTransportError(t *testing.T) {
	c := newFailClient(t)
	if err := c.Move(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestDeleteTransportError(t *testing.T) {
	c := newFailClient(t)
	if err := c.Delete(context.Background(), "a"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestEmptyTrashTransportError(t *testing.T) {
	c := newFailClient(t)
	if err := c.EmptyTrash(context.Background()); err == nil {
		t.Fatal("expected transport error")
	}
}

// TestListFilesFolderID covers the non-empty folderID branch of ListFiles.
func TestListFilesFolderID(t *testing.T) {
	srv := httptestServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if !strings.Contains(q, "'myFolder123' in parents") {
			t.Errorf("query = %q, want folder parent", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[]}`))
	})
	c := newTestClient(t, srv)
	c.accessToken = "at"
	c.expiresAt = nowPlus(t)
	if _, err := c.ListFiles(context.Background(), "myFolder123"); err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
}

// TestParseRSAPrivateKeyPKCS8Error covers the ParsePKCS8PrivateKey error
// branch (valid PEM block, bytes that are neither PKCS1 nor PKCS8).
func TestParseRSAPrivateKeyPKCS8Error(t *testing.T) {
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("garbage-bytes")}))
	_, err := parseRSAPrivateKey(pemStr)
	if err == nil {
		t.Fatal("expected parse error for garbage PKCS8 bytes")
	}
}

// TestBuildServiceAccountJWTSignError covers the rsa.SignPKCS1v15 error
// branch using an RSA key whose modulus is too small for a SHA256 digest
// (k-11 < 32 bytes). Go 1.24's GenerateKey rejects keys <512 bits, so the
// key is assembled manually from small primes.
func TestBuildServiceAccountJWTSignError(t *testing.T) {
	one := big.NewInt(1)
	p, _ := new(big.Int).SetString("1000000007", 10)
	q, _ := new(big.Int).SetString("1000000009", 10)
	n := new(big.Int).Mul(p, q) // ~128-bit modulus (16 bytes)
	phi := new(big.Int).Sub(p, one)
	phi.Mul(phi, new(big.Int).Sub(q, one))
	e := big.NewInt(65537)
	d := new(big.Int).ModInverse(e, phi)
	if d == nil {
		t.Fatal("mod inverse failed")
	}
	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: n, E: 65537},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	_, err := buildServiceAccountJWT("sa@example.com", key, []string{"scope"}, nowPlus(t))
	if err == nil {
		t.Fatal("expected sign error for undersized RSA key")
	}
}
