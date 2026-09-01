package github

import (
	"context"

	"github.com/workdock-dev/engine/domain/types"
)

// GitHubConnectionRepository persists the connections between repositories
// and the GitHub installations authorized to access them.
type GitHubConnectionRepository interface {
	GetGitHubConnection(ctx context.Context, repoFullName string) (*types.GitHubConnection, error)
	UpsertGitHubConnection(ctx context.Context, connection *types.GitHubConnection) error
	ResetGitHubConnection(ctx context.Context, installationId string, repos []string) error
}
