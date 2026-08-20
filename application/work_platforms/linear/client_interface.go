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

package linear

import (
	"context"

	"github.com/jazielguerrero/workdock/domain/types"
)

type LinearClientInterface interface {
	OauthAuthorize(ctx context.Context) string
	OauthCallback(ctx context.Context, code, errorP string) (*OAuthCallbackEvent, error)
	RefreshToken(ctx context.Context, refreshToken string) (*Token, error)
	CreateAgentActivity(ctx context.Context, accessToken string, input CreateAgentActivityInput) error
	GetIssueLabels(ctx context.Context, accessToken string, issueId string) ([]IssueLabel, error)
	SetExternalURLs(ctx context.Context, accessToken string, input SetExternalURLsInput) (*AgentSessionUpdatePayload, error)
	Webhook(ctx context.Context, req types.WebhookRequest) (*AgentSessionEventData, error)
	ParseIssueStatusChange(ctx context.Context, req types.WebhookRequest) (*IssueStatusChangePayload, error)
}
