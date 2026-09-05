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

package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
)

type PromptContext struct {
	Prompt  string
	Context *string // Optional context to provide
	Issue   types.Issue
}

// HandlerAgentSession is the interfaces required to be implemented
// by work platforms that provides agent assignment to tickets
type HandlerAgentSession interface {
	// Ingest transform the work platform agent session payload into the domain session
	Ingest(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error)

	// GetLabels returns the list of labels assigned to the ticket
	GetLabels(ctx context.Context, issueId, accessToken string) ([]string, error)

	// GetCredentials returns the access token required to send agent session updates
	GetCredentials(ctx context.Context, orgId string) (string, error)

	// GetPromptContext returns the data required to build the user prompt
	GetPromptContext(sessionEvent *types.SessionEvent) (*PromptContext, error)

	// SendThought sends the thinking state to the provider
	SendThought(ctx context.Context, sessionId, accessToken, text string) error

	// SendResponse sends text chunks/parts to the provider
	SendResponse(ctx context.Context, sessionId, accessToken, text string) error

	// SendACtion sends an action required to be executed by the user
	SendAction(ctx context.Context, sessionId, accessToken string, action types.AgentAction) error

	// SendElicitation sends a collection of questions to be answer by the user
	SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation types.AgentElicitation) error

	// SendGitConnectionRequest indicates the user to grant access to the git hosting provider
	SendGitConnectionRequest(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error

	// SendServerInternalError sends a geneeric server internal error
	SendServerInternalError(ctx context.Context, sessionId, accessToken string) error
}
