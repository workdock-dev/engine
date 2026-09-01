package github

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/workdock-dev/engine/pipelines/runners"
)

const (
	GITHUB_ACCESS_TOKEN_ENV_VAR = "GH_TOKEN"
)

var (
	//go:embed gh_cli_install.sh
	GH_CLI_INSTALL string

	//go:embed gh_git_setup.sh
	GH_GIT_SETUP string

	//go:emebed get_changes.sh
	GET_CHANGES string
)

type GitHandler struct {
	client          ClientInterface
	repository      GitHubConnectionRepository
	secretManager   ports.ForSecrets
	installationUrl string
}

func NewGitHandler(installationUrl string, repository GitHubConnectionRepository, client ClientInterface, secretManager ports.ForSecrets) runners.GitHandler {
	return &GitHandler{
		installationUrl: installationUrl,
		repository:      repository,
		client:          client,
		secretManager:   secretManager,
	}
}

func (h *GitHandler) GetInstallationUrl() string {
	return h.installationUrl
}

func (h *GitHandler) GetConfigurationCommands() []string {
	return []string{
		GH_CLI_INSTALL,
	}
}

func (h *GitHandler) GetCommands() []string {
	return []string{
		GH_GIT_SETUP,
	}
}

func (h *GitHandler) GetLatestChangesComand() string {
	return GET_CHANGES
}

func (h *GitHandler) ParseLatestChangesResult(changes string) *types.PullRequest {
	if changes == "" {
		return nil
	}

	var pr types.PullRequest

	if err := json.Unmarshal([]byte(changes), &pr); err != nil {
		slog.Error("failed to unmarshal pull request metadata", "err", err)
		return nil
	}

	return &pr
}

func (h *GitHandler) VerifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repo *string) (*runners.GitAccess, error) {
	if repo == nil {
		return nil, nil
	}

	connection, err := h.repository.GetGitHubConnection(ctx, *repo)

	if err != nil {
		return nil, err
	}

	// Requires connection
	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		return &runners.GitAccess{
			Granted: false,
		}, nil
	}

	token, err := getGitHubAccessToken(ctx, h.secretManager, h.client, *connection.InstallationId)

	if err != nil {
		if errors.Is(err, types.ErrGitHubInstallationUnavailable) {
			slog.Debug(
				"GitHub installation unavailable, resetting connection",
				"installation_id", *connection.InstallationId,
				"event_identifier", sessionEventIdentifier,
			)

			// TODO: Safe to ignore returned error?
			resetInstallation(ctx, h.repository, h.secretManager, *connection.InstallationId, []string{*repo})
			return &runners.GitAccess{
				Granted: false,
			}, nil
		}

		return nil, err
	}

	slog.Debug("Verified repo access", "has_access", true)
	return &runners.GitAccess{
		EnvVarName: GITHUB_ACCESS_TOKEN_ENV_VAR,
		Secret:     token,
		Hosts:      []string{"api.github.com", "github.com"},
	}, nil
}

func (h *GitHandler) RequestConnection(ctx context.Context, sessionEventIdentifier, repo string) error {
	sessionEventId := sessionEventIdentifier
	return h.repository.UpsertGitHubConnection(
		ctx,
		&types.GitHubConnection{
			SessionEventIdentifier: &sessionEventId,
			RepoFullName:           repo,
			Connected:              false,
			InstallationId:         nil,
		},
	)
}
