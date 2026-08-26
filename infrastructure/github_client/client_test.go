// Copyright 2026 Jaziel Guerrero
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package github_client

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/domain/types"
)

type GitHubClientSuite struct {
	suite.Suite
}

func TestGitHubClientSuite(t *testing.T) {
	suite.Run(t, new(GitHubClientSuite))
}

func (s *GitHubClientSuite) writeTempFile(dir, name, content string) string {
	path := filepath.Join(dir, name)
	os.WriteFile(path, []byte(content), 0o600)
	return path
}

func (s *GitHubClientSuite) writeRSAKeyPKCS8(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	var buf bytes.Buffer
	pem.Encode(&buf, block)
	return s.writeTempFile(dir, "key.pem", buf.String())
}

func (s *GitHubClientSuite) writeRSAKeyPKCS1(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	var buf bytes.Buffer
	pem.Encode(&buf, block)
	return s.writeTempFile(dir, "key.pem", buf.String())
}

func (s *GitHubClientSuite) writeECKey(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	var buf bytes.Buffer
	pem.Encode(&buf, block)
	return s.writeTempFile(dir, "key.pem", buf.String())
}

func (s *GitHubClientSuite) newClientWithKey(t *testing.T) *GitHubClient {
	t.Helper()
	path := s.writeRSAKeyPKCS8(t)
	keyData, _ := os.ReadFile(path)
	block, _ := pem.Decode(keyData)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	rsaKey := parsed.(*rsa.PrivateKey)
	return &GitHubClient{
		config:     GitHubClientConfig{ClientId: "test-client-id", WebhookSecret: "test-secret"},
		privateKey: rsaKey,
		httpClient: &http.Client{},
	}
}

func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s *GitHubClientSuite) newClientWithServer(t *testing.T, handler http.HandlerFunc) (*GitHubClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	path := s.writeRSAKeyPKCS8(t)
	keyData, _ := os.ReadFile(path)
	block, _ := pem.Decode(keyData)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	rsaKey := parsed.(*rsa.PrivateKey)
	return &GitHubClient{
		config: GitHubClientConfig{
			ClientId:       "test-client-id",
			WebhookSecret:  "test-secret",
			PrivateKeyPath: path,
			BaseURL:        server.URL,
		},
		privateKey: rsaKey,
		httpClient: server.Client(),
	}, server
}

func (s *GitHubClientSuite) TestIsRepoPublic_DecodeError() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not json {{{")
	})
	defer server.Close()

	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.Error(err)
	s.False(public)
}

func (s *GitHubClientSuite) TestIsRepoPublic_InvalidBaseURL() {
	c := &GitHubClient{
		config: GitHubClientConfig{
			ClientId:      "c1",
			WebhookSecret: "s",
			BaseURL:       "://bad",
		},
		privateKey: &rsa.PrivateKey{},
		httpClient: &http.Client{},
	}
	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.Error(err)
	s.False(public)
}

func (s *GitHubClientSuite) TestIsRepoPublic_DoError() {
	c := &GitHubClient{
		config: GitHubClientConfig{
			ClientId:      "c1",
			WebhookSecret: "s",
			BaseURL:       "http://dummy",
		},
		privateKey: &rsa.PrivateKey{},
		httpClient: &http.Client{Transport: &failTransport{err: fmt.Errorf("connection refused")}},
	}
	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.Error(err)
	s.False(public)
}

func (s *GitHubClientSuite) TestCreateToken_ReadBodyError() {
	path := s.writeRSAKeyPKCS8(s.T())
	keyData, _ := os.ReadFile(path)
	block, _ := pem.Decode(keyData)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	rsaKey := parsed.(*rsa.PrivateKey)
	c := &GitHubClient{
		config: GitHubClientConfig{
			ClientId:      "c1",
			WebhookSecret: "s",
			BaseURL:       "http://dummy",
		},
		privateKey: rsaKey,
		httpClient: &http.Client{Transport: &errorBodyTransport{statusCode: http.StatusCreated}},
	}
	_, err := c.CreateInstallationAccessToken(1)
	s.Error(err)
}

func (s *GitHubClientSuite) TestCreateToken_NewRequestError() {
	path := s.writeRSAKeyPKCS8(s.T())
	keyData, _ := os.ReadFile(path)
	block, _ := pem.Decode(keyData)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	rsaKey := parsed.(*rsa.PrivateKey)
	c := &GitHubClient{
		config: GitHubClientConfig{
			ClientId:      "c1",
			WebhookSecret: "s",
			BaseURL:       "://bad",
		},
		privateKey: rsaKey,
		httpClient: &http.Client{},
	}
	_, err := c.CreateInstallationAccessToken(1)
	s.Error(err)
}

func (s *GitHubClientSuite) TestCreateToken_HTTPError() {
	path := s.writeRSAKeyPKCS8(s.T())
	keyData, _ := os.ReadFile(path)
	block, _ := pem.Decode(keyData)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	rsaKey := parsed.(*rsa.PrivateKey)
	c := &GitHubClient{
		config: GitHubClientConfig{
			ClientId:      "c1",
			WebhookSecret: "s",
			BaseURL:       "http://dummy",
		},
		privateKey: rsaKey,
		httpClient: &http.Client{Transport: &failTransport{err: fmt.Errorf("connection refused")}},
	}
	_, err := c.CreateInstallationAccessToken(1)
	s.Error(err)
}

func (s *GitHubClientSuite) TestCreateToken_JWTError() {
	c := &GitHubClient{
		config: GitHubClientConfig{
			ClientId:      "c1",
			WebhookSecret: "s",
			BaseURL:       "http://dummy",
		},
		privateKey: &rsa.PrivateKey{
			PublicKey: rsa.PublicKey{
				N: big.NewInt(1),
				E: 1,
			},
			D: new(big.Int),
		},
		httpClient: &http.Client{},
	}
	_, err := c.CreateInstallationAccessToken(1)
	s.Error(err)
}

// --- errorReader helper ---

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

// --- RoundTripper that fails on Do ---

type failTransport struct {
	err error
}

func (t *failTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, t.err
}

// --- RoundTripper that returns a response with error body ---

type errorBodyTransport struct {
	statusCode int
}

func (t *errorBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: t.statusCode,
		Body:       io.NopCloser(&errorReader{}),
		Header:     make(http.Header),
	}, nil
}

// --- New() tests ---

func (s *GitHubClientSuite) TestNew_Success_PKCS8() {
	path := s.writeRSAKeyPKCS8(s.T())
	c, err := New(GitHubClientConfig{PrivateKeyPath: path, ClientId: "c1"})
	s.NoError(err)
	s.NotNil(c)
	s.Equal("c1", c.config.ClientId)
}

func (s *GitHubClientSuite) TestNew_Success_PKCS1() {
	path := s.writeRSAKeyPKCS1(s.T())
	c, err := New(GitHubClientConfig{PrivateKeyPath: path, ClientId: "c1"})
	s.NoError(err)
	s.NotNil(c)
}

func (s *GitHubClientSuite) TestNew_ErrFileNotFound() {
	_, err := New(GitHubClientConfig{PrivateKeyPath: "/nonexistent/path.pem"})
	s.Error(err)
	s.True(errors.Is(err, os.ErrNotExist))
}

func (s *GitHubClientSuite) TestNew_ErrInvalidPEM() {
	dir := s.T().TempDir()
	path := s.writeTempFile(dir, "key.pem", "this is not PEM content")
	_, err := New(GitHubClientConfig{PrivateKeyPath: path})
	s.ErrorIs(err, ErrInvalidPEM)
}

func (s *GitHubClientSuite) TestNew_ErrBothPKCS8AndPKCS1Fail() {
	dir := s.T().TempDir()
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage DER")}
	var buf bytes.Buffer
	pem.Encode(&buf, block)
	path := s.writeTempFile(dir, "key.pem", buf.String())
	_, err := New(GitHubClientConfig{PrivateKeyPath: path})
	s.Error(err)
	s.False(errors.Is(err, ErrInvalidPEM))
	s.False(errors.Is(err, ErrNotRSAKey))
}

func (s *GitHubClientSuite) TestNew_ErrNotRSAKey() {
	path := s.writeECKey(s.T())
	_, err := New(GitHubClientConfig{PrivateKeyPath: path})
	s.ErrorIs(err, ErrNotRSAKey)
}

// --- GenerateJWT() tests ---

func (s *GitHubClientSuite) TestGenerateJWT_Success() {
	c := s.newClientWithKey(s.T())
	token, err := c.GenerateJWT()
	s.NoError(err)
	parts := strings.Split(token, ".")
	s.Len(parts, 3, "JWT must have 3 dot-separated parts")
}

func (s *GitHubClientSuite) TestGenerateJWT_VerifyClaims() {
	c := s.newClientWithKey(s.T())
	token, err := c.GenerateJWT()
	s.NoError(err)

	parsed, _, err := new(jwt.Parser).ParseUnverified(token, jwt.MapClaims{})
	s.NoError(err)

	claims, ok := parsed.Claims.(jwt.MapClaims)
	s.True(ok)
	s.Equal("test-client-id", claims["iss"])

	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	s.Greater(exp, iat)
	s.Equal(int64(660), exp-iat, "token lifetime should be ~11 minutes (60s before + 600s after)")
}

func (s *GitHubClientSuite) TestGenerateJWT_SigningError() {
	c := s.newClientWithKey(s.T())
	c.privateKey = &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{
			N: big.NewInt(1),
			E: 1,
		},
		D: new(big.Int),
	}
	_, err := c.GenerateJWT()
	s.Error(err)
}

// --- IsRepositoryPublic() tests ---

func (s *GitHubClientSuite) TestIsRepoPublic_Public() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		s.Equal("/repos/owner/repo", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"private": false})
	})
	defer server.Close()

	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.NoError(err)
	s.True(public)
}

func (s *GitHubClientSuite) TestIsRepoPublic_Private() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"private": true})
	})
	defer server.Close()

	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.NoError(err)
	s.False(public)
}

func (s *GitHubClientSuite) TestIsRepoPublic_NotFound() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.NoError(err)
	s.False(public)
}

func (s *GitHubClientSuite) TestIsRepoPublic_ServerError() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal Server Error")
	})
	defer server.Close()

	public, err := c.IsRepositoryPublic(context.Background(), "owner/repo")
	s.Error(err)
	s.False(public)
	s.Contains(err.Error(), "500")
}

func (s *GitHubClientSuite) TestIsRepoPublic_InvalidFormat() {
	c := s.newClientWithKey(s.T())
	public, err := c.IsRepositoryPublic(context.Background(), "nobar")
	s.Error(err)
	s.False(public)
	s.Contains(err.Error(), "invalid repository format")
}

func (s *GitHubClientSuite) TestIsRepoPublic_EmptyOwner() {
	c := s.newClientWithKey(s.T())
	public, err := c.IsRepositoryPublic(context.Background(), "/repo")
	s.Error(err)
	s.False(public)
}

func (s *GitHubClientSuite) TestIsRepoPublic_EmptyName() {
	c := s.newClientWithKey(s.T())
	public, err := c.IsRepositoryPublic(context.Background(), "owner/")
	s.Error(err)
	s.False(public)
}

// --- CreateInstallationAccessToken() tests ---

func (s *GitHubClientSuite) TestCreateToken_Success() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		s.Equal("/app/installations/123/access_tokens", r.URL.Path)
		s.Equal(http.MethodPost, r.Method)
		s.Contains(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"token":                "ghs_test123",
			"expires_at":           time.Now().Add(time.Hour).Format(time.RFC3339),
			"repository_selection": "all",
		})
	})
	defer server.Close()

	token, err := c.CreateInstallationAccessToken(123)
	s.NoError(err)
	s.Equal("ghs_test123", token.Token)
	s.False(token.ExpiresAt.IsZero())
}

func (s *GitHubClientSuite) TestCreateToken_NotFound() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	})
	defer server.Close()

	_, err := c.CreateInstallationAccessToken(999)
	s.Error(err)
	s.True(errors.Is(err, types.ErrGitHubInstallationUnavailable))
}

func (s *GitHubClientSuite) TestCreateToken_ServerError() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "server error")
	})
	defer server.Close()

	_, err := c.CreateInstallationAccessToken(1)
	s.Error(err)
	s.Contains(err.Error(), "unexpected status 500")
}

func (s *GitHubClientSuite) TestCreateToken_InvalidJSON() {
	c, server := s.newClientWithServer(s.T(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "not json {{{")
	})
	defer server.Close()

	_, err := c.CreateInstallationAccessToken(1)
	s.Error(err)
}

// --- Webhook() tests ---

func (s *GitHubClientSuite) TestWebhook_Success() {
	c := s.newClientWithKey(s.T())
	payload := `{"action":"opened","installation":{"id":1},"pull_request":{"head":{"ref":"feat","repo":{"full_name":"o/r"}}}}`
	sig := computeHMAC([]byte(payload), "test-secret")

	req := types.WebhookRequest{
		Headers: map[string][]string{
			"X-Hub-Signature-256": {sig},
			"X-Github-Event":      {"pull_request"},
			"X-Github-Delivery":   {"abc-123"},
		},
		Body: strings.NewReader(payload),
	}

	event, err := c.Webhook(context.Background(), req)
	s.NoError(err)
	s.Equal("pull_request", event.EventType)
	s.Equal("abc-123", event.DeliveryID)
	s.Equal("opened", event.Action)
	s.Equal(1, event.Installation.ID)
}

func (s *GitHubClientSuite) TestWebhook_InvalidSignature() {
	c := s.newClientWithKey(s.T())
	payload := `{"action":"opened"}`

	req := types.WebhookRequest{
		Headers: map[string][]string{
			"X-Hub-Signature-256": {"sha256=0000000000000000000000000000000000000000000000000000000000000000"},
			"X-Github-Event":      {"push"},
		},
		Body: strings.NewReader(payload),
	}

	_, err := c.Webhook(context.Background(), req)
	s.ErrorIs(err, types.ErrUnAuthorized)
}

func (s *GitHubClientSuite) TestWebhook_MissingSignature() {
	c := s.newClientWithKey(s.T())
	payload := `{"action":"opened"}`

	req := types.WebhookRequest{
		Headers: map[string][]string{
			"X-Github-Event": {"push"},
		},
		Body: strings.NewReader(payload),
	}

	_, err := c.Webhook(context.Background(), req)
	s.ErrorIs(err, types.ErrUnAuthorized)
}

func (s *GitHubClientSuite) TestWebhook_MissingEventType() {
	c := s.newClientWithKey(s.T())
	payload := `{"action":"opened"}`
	sig := computeHMAC([]byte(payload), "test-secret")

	req := types.WebhookRequest{
		Headers: map[string][]string{
			"X-Hub-Signature-256": {sig},
		},
		Body: strings.NewReader(payload),
	}

	_, err := c.Webhook(context.Background(), req)
	s.ErrorIs(err, types.ErrBadRequest)
}

func (s *GitHubClientSuite) TestWebhook_InvalidPayload() {
	c := s.newClientWithKey(s.T())
	payload := `{not json`
	sig := computeHMAC([]byte(payload), "test-secret")

	req := types.WebhookRequest{
		Headers: map[string][]string{
			"X-Hub-Signature-256": {sig},
			"X-Github-Event":      {"push"},
		},
		Body: strings.NewReader(payload),
	}

	_, err := c.Webhook(context.Background(), req)
	s.ErrorIs(err, types.ErrBadRequest)
}

func (s *GitHubClientSuite) TestWebhook_BodyReadError() {
	c := s.newClientWithKey(s.T())
	req := types.WebhookRequest{
		Headers: map[string][]string{},
		Body:    &errorReader{},
	}

	_, err := c.Webhook(context.Background(), req)
	s.ErrorIs(err, types.ErrBadRequest)
}

// --- verifyWebhookSignature() tests ---

func (s *GitHubClientSuite) TestVerifySig_EmptyHeader() {
	c := s.newClientWithKey(s.T())
	s.False(c.verifyWebhookSignature("", []byte("body")))
}

func (s *GitHubClientSuite) TestVerifySig_MissingPrefix() {
	c := s.newClientWithKey(s.T())
	s.False(c.verifyWebhookSignature("abc123", []byte("body")))
}

func (s *GitHubClientSuite) TestVerifySig_InvalidHex() {
	c := s.newClientWithKey(s.T())
	s.False(c.verifyWebhookSignature("sha256=zzzz", []byte("body")))
}

func (s *GitHubClientSuite) TestVerifySig_Correct() {
	c := s.newClientWithKey(s.T())
	body := []byte("test payload")
	sig := computeHMAC(body, "test-secret")
	s.True(c.verifyWebhookSignature(sig, body))
}

func (s *GitHubClientSuite) TestVerifySig_WrongSecret() {
	c := s.newClientWithKey(s.T())
	mac := hmac.New(sha256.New, []byte("wrong-secret"))
	mac.Write([]byte("body"))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	s.False(c.verifyWebhookSignature(sig, []byte("body")))
}

// --- baseURL() tests ---

func (s *GitHubClientSuite) TestBaseURL_Default() {
	c := &GitHubClient{config: GitHubClientConfig{}}
	s.Equal(defaultBaseURL, c.baseURL())
}

func (s *GitHubClientSuite) TestBaseURL_Custom() {
	c := &GitHubClient{config: GitHubClientConfig{BaseURL: "http://localhost:9999"}}
	s.Equal("http://localhost:9999", c.baseURL())
}
