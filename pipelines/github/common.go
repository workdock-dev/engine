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

// ResetInstallation cleans up a GitHub installation that is no longer
// available: it disconnects the specified repositories linked to it and deletes its
// stored credentials, so future sessions request a fresh GitHub connection.
func resetInstallation(
	ctx context.Context,
	repository GitHubConnectionRepository,
	secretManager ports.ForSecrets,
	installationId string,
	repos []string,
) error {
	if err := repository.ResetGitHubConnection(ctx, installationId, repos); err != nil {
		return err
	}

	if err := secretManager.Delete(ctx, GitHub_SecretPath, installationId); err != nil {
		return err
	}

	return nil
}

// batchGitHubConnections links each repository to the given installation
func batchGitHubConnections(
	ctx context.Context,
	repository GitHubConnectionRepository,
	installationId string,
	repos []string,
) ([]types.GitHubConnection, error) {
	// TODO: Refactor this to a batch upsert
	connections := make([]types.GitHubConnection, 0, len(repos))

	for i, repo := range repos {
		connection := &types.GitHubConnection{
			SessionEventIdentifier: nil,
			RepoFullName:           repo,
			Connected:              true,
			InstallationId:         &installationId,
		}

		if err := repository.UpsertGitHubConnection(ctx, connection); err != nil {
			return nil, err
		}

		connections[i] = *connection
	}

	return connections, nil
}

// getGitHubAccessToken retrieves the GitHub installation access token for a
// given installation from the secrets store.
//
// If the token is expired or expires within the next 5 minutes, it is renewed
// by requesting a fresh installation access token from GitHub and the renewed
// credentials are persisted back to the secrets store before returning.
func getGitHubAccessToken(
	ctx context.Context,
	secretManager ports.ForSecrets,
	client ClientInterface,
	installationId string,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	raw, err := secretManager.Get(ctx, GitHub_SecretPath, installationId)

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

	renewed, err := client.CreateInstallationAccessToken(id)

	if err != nil {
		return "", fmt.Errorf("failed to renew github access token: %w", err)
	}

	data, err := json.Marshal(renewed)

	if err != nil {
		return "", fmt.Errorf("failed to marshal github token: %w", err)
	}

	if err := secretManager.Set(ctx, GitHub_SecretPath, installationId, string(data)); err != nil {
		return "", fmt.Errorf("failed to store github token: %w", err)
	}

	slog.Debug("Renewed github access token", "installation_id", installationId)
	return renewed.Token, nil
}
