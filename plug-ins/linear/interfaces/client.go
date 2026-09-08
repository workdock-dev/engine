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

	"github.com/workdock-dev/engine/plug-ins/linear/types"
)

type Client interface {
	GetIssue(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error)
	GetIssueLabels(ctx context.Context, issueId, accessToken string) ([]string, error)
	GetTeamWorkflowStates(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error)
	UpdateIssueState(ctx context.Context, accessToken, issueId, stateId string) error
	ExchangeCode(ctx context.Context, code string) (*types.TokenExchanged, error)
	GetWorkspaceInfo(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error)
	RefreshToken(ctx context.Context, refreshToken string) (*types.Token, error)
	CreateAgentActivity(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error

	// SendInitialThought immediately acknowledges a newly created agent
	// session with a thought activity, resolving the access token for the
	// given organization first. It lets the webhook ingestion path respond
	// to the created event before any other processing happens.
	SendInitialThought(ctx context.Context, sessionId, organizationId string) error

	// GetCredentials resolves the Linear API access token for the given
	// organization, refreshing and persisting it when expired.
	GetCredentials(ctx context.Context, organizationId string) (string, error)
}
