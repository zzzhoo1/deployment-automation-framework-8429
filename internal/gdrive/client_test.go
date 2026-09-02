package gdrive

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func pkcs1PEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func pkcs8PEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
}

func TestParseRSAPrivateKey(t *testing.T) {
	key := testRSAKey(t)
	for name, pemStr := range map[string]string{
		"pkcs1": pkcs1PEM(t, key),
		"pkcs8": pkcs8PEM(t, key),
	} {
		got, err := parseRSAPrivateKey(pemStr)
		if err != nil {
			t.Errorf("%s: parse: %v", name, err)
			continue
		}
		if got.N.Cmp(key.N) != 0 {
			t.Errorf("%s: parsed key differs from original", name)
		}
	}
	if _, err := parseRSAPrivateKey("not a pem"); err == nil {
		t.Error("expected error for non-PEM input")
	}
}

func TestBuildServiceAccountJWT(t *testing.T) {
	key := testRSAKey(t)
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	jwt, err := buildServiceAccountJWT("sa@example.iam.gserviceaccount.com", key, []string{"scope.a", "scope.b"}, now)
	if err != nil {
		t.Fatalf("build jwt: %v", err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d segments, want 3", len(parts))
	}
	// Header.
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var h map[string]string
	if err := json.Unmarshal(header, &h); err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if h["alg"] != "RS256" || h["typ"] != "JWT" {
		t.Errorf("header = %v", h)
	}
	// Claims.
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var c struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Exp   int64  `json:"exp"`
		Iat   int64  `json:"iat"`
	}
	if err := json.Unmarshal(claims, &c); err != nil {
		t.Fatalf("parse claims: %v", err)
	}
	if c.Iss != "sa@example.iam.gserviceaccount.com" || c.Scope != "scope.a scope.b" || c.Aud != "https://oauth2.googleapis.com/token" {
		t.Errorf("claims = %+v", c)
	}
	if c.Exp != now.Add(time.Hour).Unix() || c.Iat != now.Unix() {
		t.Errorf("exp/iat = %d/%d", c.Exp, c.Iat)
	}
	// Signature: verify RS256 over header.claims.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("signature verify: %v", err)
	}
}

func TestNewServiceAccountClient(t *testing.T) {
	key := testRSAKey(t)
	keyJSON := []byte(`{"client_email":"sa@test.iam.gserviceaccount.com","private_key":` +
		jsonString(t, pkcs1PEM(t, key)) + `}`)
	c, err := NewServiceAccountClient(keyJSON)
	if err != nil {
		t.Fatalf("NewServiceAccountClient: %v", err)
	}
	if !c.ServiceAccountAvailable() {
		t.Error("ServiceAccountAvailable = false, want true")
	}
	if c.saEmail != "sa@test.iam.gserviceaccount.com" {
		t.Errorf("saEmail = %q", c.saEmail)
	}
	if _, err := NewServiceAccountClient([]byte(`{not json`)); err == nil {
		t.Error("expected error for bad JSON")
	}
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal string: %v", err)
	}
	return string(b)
}

// nowPlus returns a time in the future for token-expiry test setup.
func nowPlus(t *testing.T) time.Time {
	t.Helper()
	return time.Now().Add(time.Hour)
}

func TestAuthURL(t *testing.T) {
	c := NewOAuthClient("client-id-123", "secret-456")
	u := c.AuthURL("state-abc")
	for _, want := range []string{"client_id=client-id-123", "response_type=code", "state=state-abc", "access_type=offline", "accounts.google.com"} {
		if !strings.Contains(u, want) {
			t.Errorf("AuthURL missing %q in %s", want, u)
		}
	}
}
