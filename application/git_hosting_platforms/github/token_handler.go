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

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/workdock-dev/engine/domain/ports"
)

type tokenHandlerConfig struct {
	ForSecrets ports.ForSecrets
	Client     ClientInterface
}

type tokenHandler struct {
	config tokenHandlerConfig
}

func newTokenHandler(config tokenHandlerConfig) *tokenHandler {
	return &tokenHandler{
		config: config,
	}
}

type tokenResult struct {
	Token   string
	Expired bool
	Error   error
}

// getGitHubAccessToken retrieves a GitHub installation access token for the given installation ID.
// It first checks if a cached token exists and is still valid (not expiring within 5 minutes).
// If the token is expired or expiring soon, it renews the token via the GitHub API.
// Returns the token, whether it was renewed (expired), and any error encountered.
func (h *tokenHandler) getGitHubAccessToken(ctx context.Context, installationId string) (string, bool, error) {
	// Check if the context has been cancelled
	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}

	// Try to get the cached token from secrets storage
	raw, err := h.config.ForSecrets.Get(ctx, GitHub_SecretPath, installationId)

	if err != nil {
		return "", false, fmt.Errorf("failed to get github token: %w", err)
	}

	var token InstallationAccessToken

	// Parse the cached token
	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return "", false, fmt.Errorf("failed to unmarshal github token: %w", err)
	}

	// If token is still valid (not expiring within 5 minutes), return it
	if time.Until(token.ExpiresAt) > 5*time.Minute {
		slog.Debug("Got github access token", "installation_id", installationId)
		return token.Token, false, nil
	}

	// Token is expired or expiring soon, need to renew
	slog.Debug("GitHub access token is expired or expiring soon, renewing", "installation_id", installationId, "expires_at", token.ExpiresAt)

	// Parse installation ID to integer for API call
	id, err := strconv.Atoi(installationId)

	if err != nil {
		return "", false, fmt.Errorf("failed to parse installation id: %w", err)
	}

	// Request a new token from GitHub API
	renewed, err := h.config.Client.CreateInstallationAccessToken(id)

	if err != nil {
		return "", false, fmt.Errorf("failed to renew github access token: %w", err)
	}

	// Marshal the renewed token for storage
	data, err := json.Marshal(renewed)

	if err != nil {
		return "", false, fmt.Errorf("failed to marshal github token: %w", err)
	}

	// Store the renewed token
	if err := h.config.ForSecrets.Set(ctx, GitHub_SecretPath, installationId, string(data)); err != nil {
		return "", false, fmt.Errorf("failed to store github token: %w", err)
	}

	slog.Debug("Renewed github access token", "installation_id", installationId)
	return renewed.Token, true, nil
}
