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

// ContextFile is provider context too large to inline in the prompt. The
// engine uploads it to the sandbox and points the agent at it, so the agent
// reads only the parts it needs instead of paying for the whole document on
// every turn.
type ContextFile struct {
	// Content is the provider's complete context document
	Content string

	// Summary describes what Content holds so the agent can decide whether
	// and where reading it is worth the cost
	Summary string
}

type PromptContext struct {
	Context *string // Optional context to provide
	Issue   types.Issue

	// ContextFile is optional provider context delivered as a file rather
	// than inlined into the prompt
	ContextFile *ContextFile
}

// IssueState describes the workflow state of a ticket on the work platform.
type IssueState struct {
	Name string
	Type string
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

	// SendError sends the message of the given error to the user
	SendError(ctx context.Context, sessionId, accessToken string, err error) error

	// GetIssueState returns the workflow state metadata of the ticket
	GetIssueState(ctx context.Context, issueId, accessToken string) (*IssueState, error)

	// TransitionIssueToStarted moves the ticket to the provider's first started
	// workflow state when the agent session begins executing, regardless of
	// the ticket's current state
	TransitionIssueToStarted(ctx context.Context, issueId, accessToken string) error

	// TransitionIssueToInReview moves the ticket to the provider's workflow
	// state representing completed work awaiting review, regardless of the
	// ticket's current state
	TransitionIssueToInReview(ctx context.Context, issueId, accessToken string) error
}
