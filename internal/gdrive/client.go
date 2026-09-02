// Package gdrive implements a Google Drive API client covering the
// operations the bot needs: OAuth authorization-code flow, service-account
// auth, resumable uploads, and file management (list/search/copy/move/
// delete). It uses only the standard library.
package gdrive

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	authURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenURL    = "https://oauth2.googleapis.com/token"
	driveAPI    = "https://www.googleapis.com/drive/v3"
	scopes      = "https://www.googleapis.com/auth/drive"
	jwtAudience = "https://oauth2.googleapis.com/token"
)

// Client is a Google Drive API client.
type Client struct {
	http *http.Client
	// OAuth (user) credentials
	clientID     string
	clientSecret string
	// Service account
	saEmail  string
	saKey    *rsa.PrivateKey
	saScopes []string

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	expiresAt    time.Time
}

// NewOAuthClient creates a client for the OAuth authorization-code flow.
func NewOAuthClient(clientID, clientSecret string) *Client {
	return &Client{
		http:         &http.Client{Timeout: 120 * time.Second},
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// NewServiceAccountClient creates a client from a service-account JSON key
// (the standard Google "service_account" key file).
func NewServiceAccountClient(keyJSON []byte) (*Client, error) {
	var info struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.Unmarshal(keyJSON, &info); err != nil {
		return nil, fmt.Errorf("gdrive: parse service account key: %w", err)
	}
	key, err := parseRSAPrivateKey(info.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("gdrive: parse private key: %w", err)
	}
	return &Client{
		http:     &http.Client{Timeout: 120 * time.Second},
		saEmail:  info.ClientEmail,
		saKey:    key,
		saScopes: []string{scopes},
	}, nil
}

// parseRSAPrivateKey parses a PEM PKCS#1 or PKCS#8 RSA private key.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA key")
	}
	return rsaKey, nil
}

// AuthURL builds the Google authorization URL for the OAuth flow.
func (c *Client) AuthURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.clientID)
	q.Set("response_type", "code")
	q.Set("scope", scopes)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	if state != "" {
		q.Set("state", state)
	}
	return authURL + "?" + q.Encode()
}

// ExchangeCode trades an authorization code for tokens and stores the
// refresh token for future use.
func (c *Client) ExchangeCode(ctx context.Context, code string) error {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("scope", scopes)
	return c.fetchToken(ctx, form)
}

// RefreshToken exchanges the stored refresh token for a new access token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) error {
	form := url.Values{}
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("grant_type", "refreshing")
	return c.fetchToken(ctx, form)
}

func (c *Client) fetchToken(ctx context.Context, form url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gdrive: token: HTTP %d: %s", resp.StatusCode, string(data))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &tok); err != nil {
		return err
	}
	c.mu.Lock()
	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	c.expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn-30) * time.Second)
	c.mu.Unlock()
	return nil
}

// SetRefreshToken stores a refresh token (e.g. loaded from the DB) so the
// client can mint access tokens on demand.
func (c *Client) SetRefreshToken(rt string) {
	c.mu.Lock()
	c.refreshToken = rt
	c.mu.Unlock()
}

// ServiceAccountAvailable reports whether service-account auth is configured.
func (c *Client) ServiceAccountAvailable() bool {
	return c.saKey != nil
}

// Token returns a valid access token, minting one via service account or
// refresh token as needed.
func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.accessToken != "" && time.Now().Before(c.expiresAt) {
		tok := c.accessToken
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	if c.ServiceAccountAvailable() {
		tok, err := c.serviceAccountToken(ctx)
		if err != nil {
			return "", err
		}
		c.mu.Lock()
		c.accessToken = tok
		c.expiresAt = time.Now().Add(55 * time.Minute)
		c.mu.Unlock()
		return tok, nil
	}

	c.mu.Lock()
	rt := c.refreshToken
	c.mu.Unlock()
	if rt == "" {
		return "", fmt.Errorf("gdrive: no credentials (no refresh token or service account)")
	}
	if err := c.RefreshToken(ctx, rt); err != nil {
		return "", err
	}
	c.mu.Lock()
	tok := c.accessToken
	c.mu.Unlock()
	return tok, nil
}

// serviceAccountToken mints an OAuth2 token from a signed JWT (JWT grant).
func (c *Client) serviceAccountToken(ctx context.Context) (string, error) {
	now := time.Now()
	claims := struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Exp   int64  `json:"exp"`
		Iat   int64  `json:"iat"`
	}{
		Iss:   c.saEmail,
		Scope: strings.Join(c.saScopes, " "),
		Aud:   jwtAudience,
		Exp:   now.Add(time.Hour).Unix(),
		Iat:   now.Unix(),
	}
	claimJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	headerJSON := []byte(`{"alg":"RS256","typ":"JWT"}`)
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimJSON)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.saKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	jwt := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gdrive: service account token: HTTP %d: %s", resp.StatusCode, string(data))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// File is a Drive file metadata object.
type File struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	MimeType string   `json:"mime_type"`
	Size     string   `json:"size"`
	Created  string   `json:"created_time"`
	Modified string   `json:"modified_time"`
	Parents  []string `json:"parents"`
	WebLink  string   `json:"web_link"`
}

// ListFiles lists files in a folder ("" = My Drive root).
func (c *Client) ListFiles(ctx context.Context, folderID string) ([]File, error) {
	parent := "root"
	if folderID != "" {
		parent = folderID
	}
	q := fmt.Sprintf("'%s' in parents and trashed = false", parent)
	return c.query(ctx, q, "name")
}

// SearchFiles searches files by name.
func (c *Client) SearchFiles(ctx context.Context, keyword string) ([]File, error) {
	q := fmt.Sprintf("name contains '%s' and trashed = false", strings.ReplaceAll(keyword, "'", `\'`))
	return c.query(ctx, q, "name")
}

func (c *Client) query(ctx context.Context, q, orderBy string) ([]File, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/files?q=%s&orderBy=%s&fields=files(id,name,mimeType,size,createdTime,modifiedTime,parents,webLink)&pageSize=100",
		driveAPI, url.QueryEscape(q), url.QueryEscape(orderBy))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gdrive: query: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Files []File `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// Upload uploads a local file to a Drive folder ("" = root) using a
// multipart upload. It returns the created file.
func (c *Client) Upload(ctx context.Context, path, folderID, name string) (*File, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mime := mime.TypeByExtension(filepath.Ext(path))
	if mime == "" {
		mime = "application/octet-stream"
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	metaPart, err := w.CreatePart(textHeaders("application/json; charset=utf-8"))
	if err != nil {
		return nil, err
	}
	meta := map[string]any{"name": name}
	if folderID != "" {
		meta["parents"] = []string{folderID}
	}
	if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
		return nil, err
	}
	filePart, err := w.CreatePart(fileHeaders(mime, name))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(filePart, f); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("%s/files?uploadType=multipart&fields=id,name,webLink", driveAPI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gdrive: upload: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var file File
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, err
	}
	return &file, nil
}

// Copy copies a file or folder into a destination folder.
func (c *Client) Copy(ctx context.Context, fileID, destFolder string) (*File, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"parents": []string{destFolder}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/files/%s/copy?fields=id,name,webLink", driveAPI, fileID), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gdrive: copy: HTTP %d: %s", resp.StatusCode, string(b))
	}
	var file File
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, err
	}
	return &file, nil
}

// Move moves a file into a different folder (API-level move).
func (c *Client) Move(ctx context.Context, fileID, newFolder string) error {
	tok, err := c.Token(ctx)
	if err != nil {
		return err
	}
	// Fetch current parents.
	u := fmt.Sprintf("%s/files/%s?fields=parents", driveAPI, fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var meta struct {
		Parents []string `json:"parents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return err
	}
	add, remove := newFolder, ""
	for _, p := range meta.Parents {
		if p != newFolder {
			remove = p
		}
	}
	q := url.Values{}
	if add != "" {
		q.Set("addParents", add)
	}
	if remove != "" {
		q.Set("removeParents", remove)
	}
	req2, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		fmt.Sprintf("%s/files/%s?%s", driveAPI, fileID, q.Encode()), nil)
	if err != nil {
		return err
	}
	req2.Header.Set("Authorization", "Bearer "+tok)
	resp2, err := c.http.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("gdrive: move: HTTP %d: %s", resp2.StatusCode, string(b))
	}
	return nil
}

// Delete moves a file to the trash.
func (c *Client) Delete(ctx context.Context, fileID string) error {
	tok, err := c.Token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/files/%s/trash", driveAPI, fileID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gdrive: delete: HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func textHeaders(v string) map[string][]string {
	return map[string][]string{"Content-Type": {v}}
}

func fileHeaders(mime, name string) map[string][]string {
	return map[string][]string{
		"Content-Type":        {mime},
		"Content-Disposition": {fmt.Sprintf(`form-data; name="media"; filename="%s"`, name)},
	}
}
