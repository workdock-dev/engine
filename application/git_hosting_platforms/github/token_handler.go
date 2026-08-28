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
	"github.com/workdock-dev/engine/domain/types"
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

// getGitHubAccessToken retrieves the GitHub installation access token for a
// given installation from the secrets store.
//
// If the token is expired or expires within the next 5 minutes, it is renewed
// by requesting a fresh installation access token from GitHub and the renewed
// credentials are persisted back to the secrets store before returning.
func (h *tokenHandler) getGitHubAccessToken(ctx context.Context, installationId string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	raw, err := h.config.ForSecrets.Get(ctx, GitHub_SecretPath, installationId)

	if err != nil {
		return "", fmt.Errorf("failed to get github token: %w", err)
	}

	var token InstallationAccessToken

	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return "", fmt.Errorf("failed to unmarshal github token: %w", err)
	}

	now := time.Now()
	decision := types.ShouldRenewToken(token.ExpiresAt, true, now, types.DefaultTokenRefreshWindow)

	if decision == types.TokenLifecycleKeep {
		slog.Debug("Got github access token", "installation_id", installationId)
		return token.Token, nil
	}

	if decision == types.TokenLifecycleExpired {
		slog.Debug("GitHub access token expired and cannot be renewed", "installation_id", installationId, "expires_at", token.ExpiresAt)
		return "", fmt.Errorf("github access token expired and cannot be renewed")
	}

	slog.Debug("GitHub access token is expired or expiring soon, renewing", "installation_id", installationId, "expires_at", token.ExpiresAt)

	id, err := strconv.Atoi(installationId)

	if err != nil {
		return "", fmt.Errorf("failed to parse installation id: %w", err)
	}

	renewed, err := h.config.Client.CreateInstallationAccessToken(id)

	if err != nil {
		return "", fmt.Errorf("failed to renew github access token: %w", err)
	}

	data, err := json.Marshal(renewed)

	if err != nil {
		return "", fmt.Errorf("failed to marshal github token: %w", err)
	}

	if err := h.config.ForSecrets.Set(ctx, GitHub_SecretPath, installationId, string(data)); err != nil {
		return "", fmt.Errorf("failed to store github token: %w", err)
	}

	slog.Debug("Renewed github access token", "installation_id", installationId)
	return renewed.Token, nil
}
