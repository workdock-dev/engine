package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/shared"
)

type RepositoryGit interface {
	GetGitHubConnection(ctx context.Context, repoFullName string) (*shared.GitHubConnection, error)
	UpsertGitHubConnection(ctx context.Context, connection *shared.GitHubConnection) error
	ResetGitHubConnection(ctx context.Context, installationId string, repos []string) error
}
