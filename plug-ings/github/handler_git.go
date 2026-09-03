package github

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/plug-ings/github/interfaces"
	"github.com/workdock-dev/engine/plug-ings/github/types"
	"github.com/workdock-dev/engine/shared"
)

const (
	GITHUB_ACCESS_TOKEN_ENV_VAR = "GH_TOKEN"
)

var (
	//go:embed scripts/gh_cli_install.sh
	GH_CLI_INSTALL string

	//go:embed scripts/gh_git_setup.sh
	GH_GIT_SETUP string

	//go:emebed scripts/get_changes.sh
	GET_CHANGES string
)

type GitHandler struct {
	client interfaces.Client
	// repository      interfaces.Repository
	secretManager   shared.ForSecrets
	installationUrl string
}

func NewGitHandler(
	config types.Config,
	// repository interfaces.Repository,
	client interfaces.Client,
	secretManager shared.ForSecrets,
) agent_session_interfaces.GitHandler {
	return &GitHandler{
		installationUrl: config.AppInstallURL,
		// repository:      repository,
		client:        client,
		secretManager: secretManager,
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

func (h *GitHandler) GetGitAccess(ctx context.Context, connection *shared.GitHubConnection) (*agent_session_interfaces.GitAccess, error) {
	token, err := getGitHubAccessToken(ctx, h.secretManager, h.client, *connection.InstallationId)

	if err != nil {
		return nil, err
	}

	return &agent_session_interfaces.GitAccess{
		EnvVarName: GITHUB_ACCESS_TOKEN_ENV_VAR,
		Secret:     token,
		Hosts:      []string{"api.github.com", "github.com"},
	}, nil
}

func (h *GitHandler) ParseLatestChangesResult(changes string) *shared.PullRequest {
	if changes == "" {
		return nil
	}

	var pr shared.PullRequest

	if err := json.Unmarshal([]byte(changes), &pr); err != nil {
		slog.Error("failed to unmarshal pull request metadata", "err", err)
		return nil
	}

	return &pr
}
