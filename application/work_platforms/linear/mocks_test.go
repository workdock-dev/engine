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

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockLinearClient struct {
	mock.Mock
}

func (m *mockLinearClient) OauthAuthorize(ctx context.Context) string {
	args := m.Called(ctx)
	return args.String(0)
}

func (m *mockLinearClient) OauthCallback(ctx context.Context, code, errorP string) (*OAuthCallbackEvent, error) {
	args := m.Called(ctx, code, errorP)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*OAuthCallbackEvent), args.Error(1)
}

func (m *mockLinearClient) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	args := m.Called(ctx, refreshToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Token), args.Error(1)
}

func (m *mockLinearClient) CreateAgentActivity(ctx context.Context, accessToken string, input CreateAgentActivityInput) error {
	args := m.Called(ctx, accessToken, input)
	return args.Error(0)
}

func (m *mockLinearClient) GetIssueLabels(ctx context.Context, accessToken string, issueId string) ([]IssueLabel, error) {
	args := m.Called(ctx, accessToken, issueId)
	return args.Get(0).([]IssueLabel), args.Error(1)
}

func (m *mockLinearClient) GetIssue(ctx context.Context, accessToken string, issueId string) (*IssueStateResult, error) {
	args := m.Called(ctx, accessToken, issueId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*IssueStateResult), args.Error(1)
}

func (m *mockLinearClient) SetExternalURLs(ctx context.Context, accessToken string, input SetExternalURLsInput) (*AgentSessionUpdatePayload, error) {
	args := m.Called(ctx, accessToken, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*AgentSessionUpdatePayload), args.Error(1)
}

func (m *mockLinearClient) Webhook(ctx context.Context, req types.WebhookRequest) (any, types.WebhookEventType, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Get(1).(types.WebhookEventType), args.Error(2)
	}
	return args.Get(0), args.Get(1).(types.WebhookEventType), args.Error(2)
}

type mockSecrets struct {
	mock.Mock
}

func (m *mockSecrets) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	args := m.Called(ctx, secretPath, secretName)
	return args.String(0), args.Error(1)
}

func (m *mockSecrets) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	args := m.Called(ctx, secretPath, secretName, secretValue)
	return args.Error(0)
}

func (m *mockSecrets) Delete(ctx context.Context, secretPath, secretName string) error {
	args := m.Called(ctx, secretPath, secretName)
	return args.Error(0)
}

type mockSessions struct {
	mock.Mock
}

func (m *mockSessions) GetAgentSession(ctx context.Context, identifier string) (*types.Session, error) {
	args := m.Called(ctx, identifier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Session), args.Error(1)
}

func (m *mockSessions) GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*types.Session, error) {
	args := m.Called(ctx, issueId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*types.Session), args.Error(1)
}

func (m *mockSessions) GetAgentSessionEvent(ctx context.Context, identifier string) (*types.SessionEvent, error) {
	args := m.Called(ctx, identifier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.SessionEvent), args.Error(1)
}

func (m *mockSessions) GetAgentSessionEventByGitRef(ctx context.Context, identifier string, repoFullName string) (*types.SessionEvent, error) {
	args := m.Called(ctx, identifier, repoFullName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.SessionEvent), args.Error(1)
}

func (m *mockSessions) CreateSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockSessions) UpsertAgentSession(ctx context.Context, session *types.Session) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *mockSessions) UpdateSessionEventResult(ctx context.Context, event *types.SessionEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockSessions) CancelSession(ctx context.Context, queuedBy, reason string) (int, error) {
	args := m.Called(ctx, queuedBy, reason)
	return args.Int(0), args.Error(1)
}

func (m *mockSessions) GetGitHubConnection(ctx context.Context, repoFullName string) (*types.GitHubConnection, error) {
	args := m.Called(ctx, repoFullName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.GitHubConnection), args.Error(1)
}

func (m *mockSessions) UpsertGitHubConnection(ctx context.Context, connection *types.GitHubConnection) error {
	args := m.Called(ctx, connection)
	return args.Error(0)
}

func (m *mockSessions) ResetGitHubConnection(ctx context.Context, installationId string, repos []string) error {
	args := m.Called(ctx, installationId, repos)
	return args.Error(0)
}

type mockOrganizations struct {
	mock.Mock
}

func (m *mockOrganizations) GetOrganization(ctx context.Context, identifier string) (*types.Organization, error) {
	args := m.Called(ctx, identifier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Organization), args.Error(1)
}

func (m *mockOrganizations) UpsertOrganization(ctx context.Context, org *types.Organization) error {
	args := m.Called(ctx, org)
	return args.Error(0)
}

type mockHarness struct {
	mock.Mock
}

func (m *mockHarness) Run(ctx context.Context) (*types.SessionEventResult, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.SessionEventResult), args.Error(1)
}

func (m *mockHarness) Dispose(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockHarness) Archive(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type mockGitHosting struct {
	mock.Mock
}

func (m *mockGitHosting) Ingest(ctx context.Context, event any) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockGitHosting) VerifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repo *string) (bool, string, error) {
	args := m.Called(ctx, sessionEventIdentifier, repo)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *mockGitHosting) RequestConnection(ctx context.Context, sessionEventIdentifier, repo string) error {
	args := m.Called(ctx, sessionEventIdentifier, repo)
	return args.Error(0)
}

func (m *mockGitHosting) Webhook(ctx context.Context, req types.WebhookRequest) (any, types.WebhookEventType, error) {
	args := m.Called(ctx, req)
	return args.Get(0), args.Get(1).(types.WebhookEventType), args.Error(2)
}

type mockEventBus struct {
	mock.Mock
}

func (m *mockEventBus) Publish(ctx context.Context, event ports.DomainEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockEventBus) Subscribe(eventType string, handler ports.EventHandler) {
	m.Called(eventType, handler)
}

// Compile-time interface checks.
var _ LinearClientInterface = (*mockLinearClient)(nil)
var _ ports.ForSecrets = (*mockSecrets)(nil)
var _ ports.ForHarnessPlatform = (*mockHarness)(nil)
var _ ports.ForGitHostingPlatform = (*mockGitHosting)(nil)
var _ ports.ForEventBus = (*mockEventBus)(nil)

// tracerNoop returns a no-op tracer for tests.
func tracerNoop() trace.Tracer {
	return otel.Tracer("test.noop")
}

var _ ports.ForGitHostingPlatform = (*mockGitHosting)(nil)
