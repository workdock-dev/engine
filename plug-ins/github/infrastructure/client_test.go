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

package infrastructure

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
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
	"github.com/workdock-dev/engine/plug-ins/github/types"
	"github.com/workdock-dev/engine/shared"
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
		config:     types.Config{ClientId: "test-client-id", WebhookSecret: "test-secret"},
		privateKey: rsaKey,
		httpClient: &http.Client{},
	}
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
		config: types.Config{
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
		config: types.Config{
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
		config: types.Config{
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
		config: types.Config{
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
		config: types.Config{
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
		config: types.Config{
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
		config: types.Config{
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
	c, err := NewClient(types.Config{PrivateKeyPath: path, ClientId: "c1"})
	s.NoError(err)
	s.NotNil(c)
	s.Equal("c1", c.config.ClientId)
}

func (s *GitHubClientSuite) TestNew_Success_PKCS1() {
	path := s.writeRSAKeyPKCS1(s.T())
	c, err := NewClient(types.Config{PrivateKeyPath: path, ClientId: "c1"})
	s.NoError(err)
	s.NotNil(c)
}

func (s *GitHubClientSuite) TestNew_ErrFileNotFound() {
	_, err := NewClient(types.Config{PrivateKeyPath: "/nonexistent/path.pem"})
	s.Error(err)
	s.True(errors.Is(err, os.ErrNotExist))
}

func (s *GitHubClientSuite) TestNew_ErrInvalidPEM() {
	dir := s.T().TempDir()
	path := s.writeTempFile(dir, "key.pem", "this is not PEM content")
	_, err := NewClient(types.Config{PrivateKeyPath: path})
	s.ErrorIs(err, ErrInvalidPEM)
}

func (s *GitHubClientSuite) TestNew_ErrBothPKCS8AndPKCS1Fail() {
	dir := s.T().TempDir()
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage DER")}
	var buf bytes.Buffer
	pem.Encode(&buf, block)
	path := s.writeTempFile(dir, "key.pem", buf.String())
	_, err := NewClient(types.Config{PrivateKeyPath: path})
	s.Error(err)
	s.False(errors.Is(err, ErrInvalidPEM))
	s.False(errors.Is(err, ErrNotRSAKey))
}

func (s *GitHubClientSuite) TestNew_ErrNotRSAKey() {
	path := s.writeECKey(s.T())
	_, err := NewClient(types.Config{PrivateKeyPath: path})
	s.ErrorIs(err, ErrNotRSAKey)
}

// --- GenerateJWT() tests ---

func (s *GitHubClientSuite) TestGenerateJWT_Success() {
	c := s.newClientWithKey(s.T())
	token, err := c.generateJWT()
	s.NoError(err)
	parts := strings.Split(token, ".")
	s.Len(parts, 3, "JWT must have 3 dot-separated parts")
}

func (s *GitHubClientSuite) TestGenerateJWT_VerifyClaims() {
	c := s.newClientWithKey(s.T())
	token, err := c.generateJWT()
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
	_, err := c.generateJWT()
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
	s.True(errors.Is(err, shared.ErrGitHubInstallationUnavailable))
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

// --- baseURL() tests ---

func (s *GitHubClientSuite) TestBaseURL_Default() {
	c := &GitHubClient{config: types.Config{}}
	s.Equal(defaultBaseURL, c.baseURL())
}

func (s *GitHubClientSuite) TestBaseURL_Custom() {
	c := &GitHubClient{config: types.Config{BaseURL: "http://localhost:9999"}}
	s.Equal("http://localhost:9999", c.baseURL())
}

func (s *GitHubClientSuite) TestCreatePullRequestSuccess() {
	input := types.CreatePullRequestInput{Title: "Changes", Body: "Details", Head: "feature", Base: "main", Draft: true}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		s.Equal(http.MethodPost, r.Method)
		s.Equal("/repos/owner/repo/pulls", r.URL.Path)
		s.Equal("Bearer installation-token", r.Header.Get("Authorization"))
		s.Equal("application/vnd.github+json", r.Header.Get("Accept"))
		s.Equal("application/json", r.Header.Get("Content-Type"))
		s.Equal("2022-11-28", r.Header.Get("X-GitHub-Api-Version"))
		var body types.CreatePullRequestInput
		s.NoError(json.NewDecoder(r.Body).Decode(&body))
		s.Equal(input, body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"html_url":"https://github.com/owner/repo/pull/42","number":42,"head":{"ref":"feature","sha":"commit"}}`)
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	pr, err := client.CreatePullRequest(context.Background(), "owner/repo", "installation-token", input)
	s.Require().NoError(err)
	s.Require().NotNil(pr)
	s.Equal(1, requests)
	s.Equal("https://github.com/owner/repo/pull/42", pr.URL)
	s.Equal(42, pr.Number)
	s.Equal("feature", pr.Head.Ref)
	s.Equal("commit", pr.Head.SHA)
}

func (s *GitHubClientSuite) TestCreatePullRequestInvalidRepository() {
	client := &GitHubClient{}
	for _, repo := range []string{"", "owner", "/repo", "owner/", "owner/repo/extra"} {
		s.Run(repo, func() {
			pr, err := client.CreatePullRequest(context.Background(), repo, "token", types.CreatePullRequestInput{})
			s.ErrorContains(err, "invalid repository format: expected owner/repo")
			s.Nil(pr)
		})
	}
}

func (s *GitHubClientSuite) TestCreatePullRequestInvalidURL() {
	client := &GitHubClient{config: types.Config{BaseURL: "://invalid"}}
	pr, err := client.CreatePullRequest(context.Background(), "owner/repo", "token", types.CreatePullRequestInput{})
	s.Error(err)
	s.Nil(pr)
}

func (s *GitHubClientSuite) TestCreatePullRequestTransportError() {
	client := &GitHubClient{httpClient: &http.Client{Transport: &failTransport{err: errors.New("connection refused")}}}
	pr, err := client.CreatePullRequest(context.Background(), "owner/repo", "token", types.CreatePullRequestInput{})
	s.EqualError(err, "GitHub pull request request failed")
	s.Nil(pr)
}

func (s *GitHubClientSuite) TestCreatePullRequestResponseErrors() {
	for _, test := range []struct {
		name   string
		status int
		body   string
		error  string
	}{
		{name: "forbidden", status: http.StatusForbidden, body: `{"message":"Forbidden"}`, error: "GitHub pull request creation failed with status 403"},
		{name: "validation error", status: http.StatusUnprocessableEntity, body: `{"message":"Validation Failed"}`, error: "GitHub pull request creation failed with status 422"},
		{name: "server error", status: http.StatusInternalServerError, body: `{}`, error: "GitHub pull request creation failed with status 500"},
		{name: "invalid JSON", status: http.StatusCreated, body: "not json"},
		{name: "wrong JSON type", status: http.StatusCreated, body: `{"number":"invalid"}`},
	} {
		s.Run(test.name, func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
			pr, err := client.CreatePullRequest(context.Background(), "owner/repo", "token", types.CreatePullRequestInput{})
			s.Error(err)
			s.Nil(pr)
			if test.error != "" {
				s.EqualError(err, test.error)
			}
		})
	}
}

type PullRequestClientSuite struct {
	suite.Suite
}

func TestPullRequestClientSuite(t *testing.T) {
	suite.Run(t, new(PullRequestClientSuite))
}

func (s *PullRequestClientSuite) TestCommentsExcludeResolvedThreadsAndPaginateAllConnections() {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		s.Equal("/graphql", r.URL.Path)
		s.Equal(http.MethodPost, r.Method)
		s.Equal("Bearer private", r.Header.Get("Authorization"))
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		s.Require().NoError(json.NewDecoder(r.Body).Decode(&request))
		cursor := request.Variables["cursor"]

		switch {
		case strings.Contains(request.Query, "reviewThreads"):
			s.Equal("owner", request.Variables["owner"])
			s.Equal("repo", request.Variables["repo"])
			s.Equal(float64(42), request.Variables["number"])

			if cursor == nil {
				fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"resolved","isResolved":true},{"id":"thread","isResolved":false}],"pageInfo":{"hasNextPage":true,"endCursor":"threads-next"}}}}}}`)
				return
			}

			s.Equal("threads-next", cursor)
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"thread-two","isResolved":false}],"pageInfo":{"hasNextPage":false}}}}}}`)
		case strings.Contains(request.Query, "node(id:"):
			id := request.Variables["id"]
			s.NotEqual("resolved", id)

			if id == "thread-two" {
				fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"second-thread","body":"Other feedback","author":null}],"pageInfo":{"hasNextPage":false}}}}}`)
				return
			}

			s.Equal("thread", id)

			if cursor == nil {
				fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"review","url":"comment-url","body":"Fix this","author":{"login":"reviewer"},"createdAt":"today","path":"file.go","line":7,"originalLine":8,"diffHunk":"diff","outdated":true}],"pageInfo":{"hasNextPage":true,"endCursor":"replies-next"}}}}}`)
				return
			}

			s.Equal("replies-next", cursor)
			fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"reply","body":"Reply"}],"pageInfo":{"hasNextPage":false}}}}}`)
		default:

			if cursor == nil {
				fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"comments":{"nodes":[{"id":"conversation","body":"General feedback"}],"pageInfo":{"hasNextPage":true,"endCursor":"conversation-next"}}}}}}`)
				return
			}

			s.Equal("conversation-next", cursor)
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"comments":{"nodes":[{"id":"conversation-two","body":"More feedback"}],"pageInfo":{"hasNextPage":false}}}}}}`)
		}
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	comments, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", 42)
	s.Require().NoError(err)
	s.Require().Len(comments, 5)
	s.Equal(7, calls)
	s.Equal([]string{"review", "reply", "second-thread", "conversation", "conversation-two"}, []string{comments[0].ID, comments[1].ID, comments[2].ID, comments[3].ID, comments[4].ID})
	s.Equal("thread", comments[0].ThreadID)
	s.Equal("thread", comments[1].ThreadID)
	s.Equal("thread-two", comments[2].ThreadID)
	s.Empty(comments[3].ThreadID)
	s.Equal("reviewer", comments[0].Author.Login)
	s.Equal("file.go", comments[0].Path)
	s.Equal(7, *comments[0].Line)
	s.Equal(8, *comments[0].OriginalLine)
	s.Equal("diff", comments[0].DiffHunk)
	s.True(comments[0].IsOutdated)
}

func (s *PullRequestClientSuite) TestChecksFilterFailureAndPaginateChecksAndAnnotations() {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		s.Equal(http.MethodGet, r.Method)
		s.Equal("Bearer private", r.Header.Get("Authorization"))

		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42":
			fmt.Fprint(w, `{"head":{"sha":"head-commit"}}`)
		case "/repos/owner/repo/commits/head-commit/check-runs":
			s.Equal("latest", r.URL.Query().Get("filter"))
			s.Equal("100", r.URL.Query().Get("per_page"))

			if r.URL.Query().Get("page") == "1" {
				checks := make([]map[string]any, 100)

				for i := range checks {
					checks[i] = map[string]any{"id": i + 1, "conclusion": "success"}
				}

				checks[0] = map[string]any{"id": 1, "name": "build", "head_sha": "head-commit", "conclusion": "failure", "html_url": "check-url", "details_url": "details-url", "output": map[string]any{"title": "Error", "summary": "Compilation failed", "text": "Compiler output", "annotations_count": 101}}
				checks[1]["conclusion"] = "cancelled"
				checks[2]["conclusion"] = "timed_out"
				checks[3]["conclusion"] = nil
				s.NoError(json.NewEncoder(w).Encode(map[string]any{"check_runs": checks}))
				return
			}

			s.Equal("2", r.URL.Query().Get("page"))
			fmt.Fprint(w, `{"check_runs":[{"id":101,"name":"test","conclusion":"failure","output":{"summary":"Tests failed"}}]}`)
		case "/repos/owner/repo/check-runs/1/annotations":
			s.Equal("100", r.URL.Query().Get("per_page"))

			if r.URL.Query().Get("page") == "1" {
				annotations := make([]types.PullRequestCheckAnnotation, 100)

				for i := range annotations {
					annotations[i] = types.PullRequestCheckAnnotation{Path: "file.go", StartLine: i + 1, EndLine: i + 1, AnnotationLevel: "failure", Message: "Syntax error", RawDetails: "Compiler details"}
				}

				s.NoError(json.NewEncoder(w).Encode(annotations))
				return
			}

			s.Equal("2", r.URL.Query().Get("page"))
			fmt.Fprint(w, `[{"path":"other.go","start_line":1,"end_line":2,"message":"Last error","raw_details":"details"}]`)
		default:
			s.Fail("unexpected endpoint", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	checks, err := client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", 42)
	s.Require().NoError(err)
	s.Require().Len(checks, 2)
	s.Equal(5, calls)
	s.Equal("build", checks[0].Name)
	s.Equal("head-commit", checks[0].HeadSHA)
	s.Equal("check-url", checks[0].URL)
	s.Equal("details-url", checks[0].DetailsURL)
	s.Equal("Error", checks[0].Output.Title)
	s.Equal("Compilation failed", checks[0].Output.Summary)
	s.Equal("Compiler output", checks[0].Output.Text)
	s.Require().Len(checks[0].Annotations, 101)
	s.Equal("Compiler details", checks[0].Annotations[0].RawDetails)
	s.Equal("Last error", checks[0].Annotations[100].Message)
	s.Equal("Tests failed", checks[1].Output.Summary)
	s.NotNil(checks[1].Annotations)
}

func (s *PullRequestClientSuite) TestReadErrorsReturnNoPartialResults() {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "forbidden", status: http.StatusForbidden, body: `{}`},
		{name: "invalid JSON", status: http.StatusOK, body: `invalid`},
		{name: "GraphQL errors", status: http.StatusOK, body: `{"errors":[{"message":"denied"}]}`},
		{name: "missing PR", status: http.StatusOK, body: `{"data":{"repository":{"pullRequest":null}}}`},
	} {
		s.Run(test.name, func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
			comments, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", 42)
			s.Error(err)
			s.Nil(comments)
			checks, err := client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", 42)
			s.Error(err)
			s.Nil(checks)
		})
	}
}

func (s *PullRequestClientSuite) TestEmptyResultsAndInputValidation() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"comments":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
			return
		}

		if strings.Contains(r.URL.Path, "/pulls/") {
			fmt.Fprint(w, `{"head":{"sha":"head"}}`)
			return
		}

		fmt.Fprint(w, `{"check_runs":[]}`)
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	comments, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", 42)
	s.NoError(err)
	s.NotNil(comments)
	s.Empty(comments)
	checks, err := client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", 42)
	s.NoError(err)
	s.NotNil(checks)
	s.Empty(checks)

	for _, repo := range []string{"", "owner", "/repo", "owner/", "owner/repo/extra"} {
		_, err := client.GetUnresolvedPullRequestComments(context.Background(), repo, "private", 42)
		s.Error(err)
		_, err = client.GetFailedPullRequestChecks(context.Background(), repo, "private", 42)
		s.Error(err)
	}

	for _, number := range []int{0, -1} {
		_, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", number)
		s.Error(err)
		_, err = client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", number)
		s.Error(err)
	}
}
