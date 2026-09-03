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

	now := time.Now()
	decision := shared.ShouldRenewToken(token.ExpiresAt, true, now, shared.DefaultTokenRefreshWindow)

	if decision == shared.TokenLifecycleKeep {
		slog.Debug("Got github access token", "installation_id", installationId)
		return token.Token, nil
	}

	if decision == shared.TokenLifecycleExpired {
		slog.Debug("GitHub access token expired and cannot be renewed", "installation_id", installationId, "expires_at", token.ExpiresAt)
		return "", fmt.Errorf("github access token expired and cannot be renewed")
	}

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
