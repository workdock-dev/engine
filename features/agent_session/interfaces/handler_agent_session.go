package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/shared"
)

type PromptContext struct {
	Prompt  string
	Context *string // Optional context to provide
	Issue   shared.Issue
}

// AgentSessionHandler is the interfaces required to be implemented
// by work platforms that provides agent assignment to tickets
type AgentSessionHandler interface {
	// Ingest parses the agent session domain event into into domain session
	// and session event.
	Ingest(event shared.DomainEvent) (*shared.Session, *shared.SessionEvent, error)

	// GetCredentials returns the access token required to send agent session updates
	GetCredentials(ctx context.Context, orgId string) (string, error)

	// GetPromptContext returns the data required to build the user prompt
	GetPromptContext(sessionEvent *shared.SessionEvent) (*PromptContext, error)

	// SendThought sends the thinking state to the provider
	SendThought(ctx context.Context, sessionId, accessToken, text string) error

	// SendResponse sends text chunks/parts to the provider
	SendResponse(ctx context.Context, sessionId, accessToken, text string) error

	// SendACtion sends an action required to be executed by the user
	SendAction(ctx context.Context, sessionId, accessToken string, action shared.AgentAction) error

	// SendElicitation sends a collection of questions to be answer by the user
	SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation shared.AgentElicitation) error

	// SendGitConnectionRequest indicates the user to grant access to the git hosting provider
	SendGitConnectionRequest(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error

	// SendServerInternalError sends a geneeric server internal error
	SendServerInternalError(ctx context.Context, sessionId, accessToken string) error
}
