package ports

import (
	"context"

	"github.com/workdock-dev/engine/shared"
)

type GitAccess struct {
	EnvVarName string
	Secret     string
	Hosts      []string
	Granted    bool
}

// GitHandler is the interface to interact with the git hosting provider
// for the intial setup of the sandbox
type GitHandler interface {
	// GetInstallationUrl returns the installation URL where user can
	// grant access
	GetInstallationUrl() string

	// GetConfigurationCommands may return a list of commands the git handler
	// provider requires to be install in the sandbox
	GetConfigurationCommands() []string

	// GetCommands may return a list of commands the git handler provider
	// requires to be run on every sandbox execution
	GetCommands() []string

	// GetLatestChangesComand returns the command to verify if a pull request or commit with push
	// was created
	GetLatestChangesComand() string

	// ParseLatestChangesResult receives the changes procude by the latest changes command
	// and parse it to a concrete domain type
	ParseLatestChangesResult(changes string) *shared.PullRequest

	// VerifyRepoAccess verifies whether the platform connection associated
	// with the session has access to the specified repository.
	VerifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repo *string) (*GitAccess, error)

	// RequestConnection requests access to the specified repository.
	RequestConnection(ctx context.Context, sessionEventIdentifier, repo string) error
}
