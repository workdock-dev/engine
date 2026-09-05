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

	"github.com/workdock-dev/engine/plug-ings/github/interfaces"
	"github.com/workdock-dev/engine/plug-ings/github/types"
	"github.com/workdock-dev/engine/shared"
)

// githubTokenRefreshWindow is how far ahead of the token expiry a renewal is
// triggered, so requests never race against an about-to-expire token.
const githubTokenRefreshWindow = 5 * time.Minute

// getGitHubAccessToken retrieves the GitHub installation access token for a
// given installation from the secrets store.
//
// If the token is expired or expires within the next 5 minutes, it is renewed
// by requesting a fresh installation access token from GitHub and the renewed
// credentials are persisted back to the secrets store before returning.
func getGitHubAccessToken(
	ctx context.Context,
	secretManager shared.ForSecrets,
	client interfaces.Client,
	installationId string,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	raw, err := secretManager.Get(ctx, types.GitHub_SecretPath, installationId)

	if err != nil {
		return "", fmt.Errorf("failed to get github token: %w", err)
	}

	var token types.InstallationAccessToken

	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return "", fmt.Errorf("failed to unmarshal github token: %w", err)
	}

	// The token cannot be refreshed, only replaced; request a fresh one
	// when the stored token expires within the refresh window.
	if time.Now().Add(githubTokenRefreshWindow).After(token.ExpiresAt) {
		slog.Debug("GitHub access token is expired or expiring soon, renewing", "installation_id", installationId, "expires_at", token.ExpiresAt)

		id, err := strconv.Atoi(installationId)

		if err != nil {
			return "", fmt.Errorf("failed to parse installation id: %w", err)
		}

		renewed, err := client.CreateInstallationAccessToken(id)

		if err != nil {
			return "", fmt.Errorf("failed to renew github access token: %w", err)
		}

		data, err := json.Marshal(renewed)

		if err != nil {
			return "", fmt.Errorf("failed to marshal github token: %w", err)
		}

		if err := secretManager.Set(ctx, types.GitHub_SecretPath, installationId, string(data)); err != nil {
			return "", fmt.Errorf("failed to store github token: %w", err)
		}

		slog.Debug("Renewed github access token", "installation_id", installationId)
		return renewed.Token, nil
	}

	slog.Debug("Got github access token", "installation_id", installationId)
	return token.Token, nil
}
