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
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/workdock-dev/engine/plug-ings/github/types"
	"github.com/workdock-dev/engine/shared"
)

var (
	ErrInvalidPEM = errors.New("failed to decode PEM block from private key")
	ErrNotRSAKey  = errors.New("private key is not an RSA key")
)

const defaultBaseURL = "https://api.github.com"

type GitHubClient struct {
	config     types.Config
	privateKey *rsa.PrivateKey
	httpClient *http.Client
}

// NewClient initializes the GitHub client with the credentials required
// to authenticate as a GitHub App.
//
//   - Loads and validates the GitHub App private key from disk.
//   - Supports both PKCS#8 and PKCS#1 RSA private key formats for compatibility.
//   - Prepares the service for generating JWTs and installation access tokens
//     used to authenticate GitHub API requests.
//
// Service initialization fails if the private key cannot be loaded, is invalid,
// or is not an RSA key.
func NewClient(config types.Config) (*GitHubClient, error) {
	keyData, err := os.ReadFile(config.PrivateKeyPath)

	if err != nil {
		slog.Error("[github-client] failed to read private key file", "path", config.PrivateKeyPath, "err", err)
		return nil, err
	}

	block, _ := pem.Decode(keyData)

	if block == nil {
		err := ErrInvalidPEM
		slog.Error("[github-client] failed to decode PEM block from private key", "path", config.PrivateKeyPath, "err", err)
		return nil, err
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)

	if err != nil {
		rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes)

		if rsaErr != nil {
			slog.Error("[github-client] failed to parse private key", "path", config.PrivateKeyPath, "err", err, "err2", rsaErr)
			return nil, err
		}

		privateKey = rsaKey
	}

	rsaKey, ok := privateKey.(*rsa.PrivateKey)

	if !ok {
		err := ErrNotRSAKey
		slog.Error("[github-client] private key is not an RSA key", "path", config.PrivateKeyPath, "error", err)
		return nil, err
	}

	slog.Debug("[github-client] created")
	return &GitHubClient{
		config:     config,
		privateKey: rsaKey,
		httpClient: &http.Client{},
	}, nil
}

func (s *GitHubClient) baseURL() string {
	if s.config.BaseURL != "" {
		return s.config.BaseURL
	}
	return defaultBaseURL
}

// generateJWT creates a short-lived JWT that identifies this application as a
// GitHub App when calling the GitHub API.
//
//   - Generates a signed JWT using the configured GitHub App private key.
//   - Produces a token suitable for exchanging for installation access tokens and
//     performing other GitHub App authentication flows.
//
// The generated token has a limited lifetime in accordance with GitHub's
// authentication requirements.
func (s *GitHubClient) generateJWT() (string, error) {
	now := time.Now()

	claims := jwt.MapClaims{
		"iat": now.Unix() - 60,  // 60s behind
		"exp": now.Unix() + 600, // 10m into the future
		"iss": s.config.ClientId,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(s.privateKey)

	if err != nil {
		slog.Error("[github-client] failed to sign JWT", "error", err)
		return "", err
	}

	return signed, nil
}

// IsRepositoryPublic determines whether a GitHub repository is publicly
// accessible without authentication.
//
//   - Queries GitHub's public repository API using the provided repository name.
//   - Returns whether the repository is publicly accessible based on GitHub's
//     response.
//
// A result of false may indicate that the repository is private, does not
// exist, or is otherwise inaccessible without authentication.
func (s *GitHubClient) IsRepositoryPublic(ctx context.Context, repo string) (bool, error) {
	owner, name, ok := strings.Cut(repo, "/")

	if !ok || owner == "" || name == "" {
		err := fmt.Errorf("invalid repository format: expected owner/repo")
		slog.Error("[github-client] failed to verify if repository is public", "err", err)
		return false, err
	}

	url := fmt.Sprintf("%s/repos/%s/%s", s.baseURL(), owner, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	if err != nil {
		slog.Error("[github-client] failed to create request for verifying if repo is public", "err", err)
		return false, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "my-app")

	resp, err := s.httpClient.Do(req)

	if err != nil {
		slog.Error("[github-client] failed request when verifying if repo is public", "err", err)
		return false, err
	}

	defer resp.Body.Close()

	type repository struct {
		Private bool `json:"private"`
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var repo repository

		if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
			slog.Error("[github-client] failed unmarshalling repo is public response", "err", err)
			return false, err
		}

		return !repo.Private, nil

	case http.StatusNotFound:
		// Repository doesn't exist or is private and you're not authenticated.
		return false, nil

	default:
		err := fmt.Errorf("github api returned %s", resp.Status)
		slog.Error("[github-client] failed sending request got github api", "err", err)
		return false, err
	}
}

// CreateInstallationAccessToken exchanges the GitHub App identity for an
// installation-scoped access token.
//
//   - Authenticates as the GitHub App using a signed JWT.
//   - Requests an installation access token from GitHub for the specified
//     installation.
//   - Returns a short-lived token that can be used to perform GitHub API
//     operations on repositories the installation has been granted access to.
//
// The returned token is scoped to a single installation and expires
// automatically, requiring a new token to be generated when it is no longer
// valid.
func (s *GitHubClient) CreateInstallationAccessToken(installationId int) (*types.InstallationAccessToken, error) {
	jwt, err := s.generateJWT()

	if err != nil {
		slog.Error("[github-client] failed to generate JWT for installation access token", "installation_id", installationId, "err", err)
		return nil, err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", s.baseURL(), installationId)
	req, err := http.NewRequest("POST", url, bytes.NewReader(nil))

	if err != nil {
		slog.Error("[github-client] failed to create request for installation access token", "installation_id", installationId, "err", err)
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")

	resp, err := s.httpClient.Do(req)

	if err != nil {
		slog.Error("[github-client] failed to request installation access token", "installation_id", installationId, "err", err)
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		slog.Error("[github-client] failed to read installation access token response", "installation_id", installationId, "err", err)
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		slog.Error("[github-client] unexpected status code for installation access token", "installation_id", installationId, "status", resp.StatusCode, "body", string(body))

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: unexpected status %d: %s", shared.ErrGitHubInstallationUnavailable, resp.StatusCode, string(body))
		}

		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var token types.InstallationAccessToken

	if err := json.Unmarshal(body, &token); err != nil {
		slog.Error("[github-client] failed to unmarshal installation access token", "installation_id", installationId, "err", err)
		return nil, err
	}

	slog.Debug("[github-client] installation access token created", "installation_id", installationId, "expires_at", token.ExpiresAt)
	return &token, nil
}
