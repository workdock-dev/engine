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

package github

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockClient struct {
	mock.Mock
}

func (m *mockClient) GenerateJWT() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *mockClient) IsRepositoryPublic(ctx context.Context, repo string) (bool, error) {
	args := m.Called(ctx, repo)
	return args.Bool(0), args.Error(1)
}

func (m *mockClient) CreateInstallationAccessToken(installationId int) (*InstallationAccessToken, error) {
	args := m.Called(installationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*InstallationAccessToken), args.Error(1)
}

func (m *mockClient) Webhook(ctx context.Context, req types.WebhookRequest) (*WebhookEvent, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*WebhookEvent), args.Error(1)
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

type mockGitHubConnections struct {
	mock.Mock
}

func (m *mockGitHubConnections) GetGitHubConnection(ctx context.Context, repoFullName string) (*types.GitHubConnection, error) {
	args := m.Called(ctx, repoFullName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.GitHubConnection), args.Error(1)
}

func (m *mockGitHubConnections) GetGitHubConnectionByInstallationId(ctx context.Context, installationId string) (*types.GitHubConnection, error) {
	args := m.Called(ctx, installationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.GitHubConnection), args.Error(1)
}

func (m *mockGitHubConnections) UpsertGitHubConnection(ctx context.Context, connection *types.GitHubConnection) error {
	args := m.Called(ctx, connection)
	return args.Error(0)
}

func (m *mockGitHubConnections) ResetGitHubConnection(ctx context.Context, installationId string) error {
	args := m.Called(ctx, installationId)
	return args.Error(0)
}

// Compile-time interface checks.
var _ ClientInterface = (*mockClient)(nil)
var _ ports.ForSecrets = (*mockSecrets)(nil)
var _ ports.ForEventBus = (*mockEventBus)(nil)
var _ repositories.GitHubConnectionRepository = (*mockGitHubConnections)(nil)

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

func TestGitHubPlatformSuite(t *testing.T) {
	suite.Run(t, new(GitHubPlatformSuite))
}

type GitHubPlatformSuite struct {
	suite.Suite
	client      *mockClient
	secrets     *mockSecrets
	events      *mockEventBus
	connections *mockGitHubConnections
	platform    *githubPlatform
}

func (s *GitHubPlatformSuite) SetupTest() {
	s.client = new(mockClient)
	s.secrets = new(mockSecrets)
	s.events = new(mockEventBus)
	s.connections = new(mockGitHubConnections)

	s.platform = &githubPlatform{
		config: GitHubPlatformConfig{
			ForSecrets:        s.secrets,
			ForEvents:         s.events,
			GitHubConnections: s.connections,
			Client:            s.client,
			BotLoginName:      "workdock-bot",
		},
		access: newGitHubAccess(githubAccessConfig{
			ForSecrets:        s.secrets,
			ForEvent:          s.events,
			GitHubConnections: s.connections,
			Client:            s.client,
		}),
	}
}

// ---------------------------------------------------------------------------
// New
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestNew() {
	p := New(GitHubPlatformConfig{
		Client:            s.client,
		ForSecrets:        s.secrets,
		ForEvents:         s.events,
		GitHubConnections: s.connections,
	})
	s.NotNil(p)
}

// ---------------------------------------------------------------------------
// Ingest
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestIngest_NonWebhookEvent() {
	err := s.platform.Ingest(context.Background(), "not-a-webhook-event")
	s.Error(err)
	s.Contains(err.Error(), "failed to cast event to GitHub Webhook Event")
}

func (s *GitHubPlatformSuite) TestIngest_Ping() {
	event := &WebhookEvent{EventType: "ping"}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
}

func (s *GitHubPlatformSuite) TestIngest_Installation() {
	event := &WebhookEvent{
		EventType: "installation",
		Action:    "created",
		Installation: &Installation{
			ID: 42,
		},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.client.AssertCalled(s.T(), "CreateInstallationAccessToken", 42)
	s.secrets.AssertCalled(s.T(), "Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything)
}

func (s *GitHubPlatformSuite) TestIngest_Issues() {
	event := &WebhookEvent{EventType: "issues", Action: "opened"}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
}

func (s *GitHubPlatformSuite) TestIngest_PullRequestComment() {
	event := &WebhookEvent{
		EventType: "pull_request_review_comment",
		Action:    "created",
		Sender:    &User{Login: "someone"},
		PullRequest: &PullRequest{
			Head: Head{
				Ref: "feature-branch",
				Repo: Repo{
					FullName: "org/repo",
				},
			},
		},
		Installation: &Installation{ID: 42},
	}

	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertCalled(s.T(), "Publish", mock.Anything, mock.MatchedBy(func(e types.PullRequestCommentedEvent) bool {
		return e.GitRef == "feature-branch" && e.RepoFullName == "org/repo" && e.InstallationId == "42"
	}))
}

func (s *GitHubPlatformSuite) TestIngest_UnknownType() {
	event := &WebhookEvent{EventType: "unknown_event"}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
}

// ---------------------------------------------------------------------------
// handleInstallation
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestIngest_Installation_NilInstallation() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: nil,
	}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Deleted() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "deleted",
		Installation: &Installation{ID: 42},
	}

	s.connections.On("ResetGitHubConnection", mock.Anything, "42").Return(nil)
	s.secrets.On("Delete", mock.Anything, GitHub_SecretPath, "42").Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "ResetGitHubConnection", mock.Anything, "42")
	s.secrets.AssertCalled(s.T(), "Delete", mock.Anything, GitHub_SecretPath, "42")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Deleted_Error() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "deleted",
		Installation: &Installation{ID: 42},
	}

	s.connections.On("ResetGitHubConnection", mock.Anything, "42").Return(errors.New("reset failed"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "reset failed")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Removed() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "removed",
		Installation: &Installation{ID: 42},
	}

	s.connections.On("ResetGitHubConnection", mock.Anything, "42").Return(nil)
	s.secrets.On("Delete", mock.Anything, GitHub_SecretPath, "42").Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "ResetGitHubConnection", mock.Anything, "42")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_NoRepos() {
	event := &WebhookEvent{
		EventType:         "installation",
		Action:            "created",
		Installation:      &Installation{ID: 42},
		Repositories:      []Repository{},
		RepositoriesAdded: []Repository{},
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.client.AssertNotCalled(s.T(), "CreateInstallationAccessToken")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_NilRepos() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.client.AssertNotCalled(s.T(), "CreateInstallationAccessToken")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_TokenError() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(nil, errors.New("token failed"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "token failed")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_SecretStoreError() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(errors.New("secret store failed"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "secret store failed")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)

	err := s.platform.Ingest(ctx, event)
	s.Error(err)
	s.ErrorIs(err, context.Canceled)
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_GetConnectionError() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, errors.New("db error"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "db error")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_CompleteConnectionError() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(errors.New("connection failed"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "connection failed")
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_RepositoriesAdded() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo-new"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo-new").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.client.AssertCalled(s.T(), "CreateInstallationAccessToken", 42)
}

func (s *GitHubPlatformSuite) TestIngest_Installation_Created_MultipleRepos() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "created",
		Installation: &Installation{ID: 42},
		Repositories: []Repository{
			{ID: 1, FullName: "org/repo1"},
			{ID: 2, FullName: "org/repo2"},
		},
	}

	token := &InstallationAccessToken{
		Token:     "ghs_xxx",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	s.client.On("CreateInstallationAccessToken", 42).Return(token, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo1").Return(nil, nil)
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo2").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertNumberOfCalls(s.T(), "UpsertGitHubConnection", 2)
	s.events.AssertNumberOfCalls(s.T(), "Publish", 2)
}

func (s *GitHubPlatformSuite) TestIngest_Installation_NonCreatedAction() {
	event := &WebhookEvent{
		EventType:    "installation",
		Action:       "suspend",
		Installation: &Installation{ID: 42},
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.client.AssertNotCalled(s.T(), "CreateInstallationAccessToken")
}

// ---------------------------------------------------------------------------
// handlePullRequestComment
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestIngest_PRComment_BotSender() {
	event := &WebhookEvent{
		EventType: "pull_request_review_comment",
		Action:    "created",
		Sender:    &User{Login: "workdock-bot"},
		PullRequest: &PullRequest{
			Head: Head{
				Ref: "feature",
				Repo: Repo{FullName: "org/repo"},
			},
		},
		Installation: &Installation{ID: 42},
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertNotCalled(s.T(), "Publish", mock.Anything, mock.Anything)
}

func (s *GitHubPlatformSuite) TestIngest_PRComment_DeletedAction() {
	event := &WebhookEvent{
		EventType: "pull_request_review_comment",
		Action:    "deleted",
		Sender:    &User{Login: "someone"},
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertNotCalled(s.T(), "Publish", mock.Anything, mock.Anything)
}

func (s *GitHubPlatformSuite) TestIngest_PRComment_NilPullRequest() {
	event := &WebhookEvent{
		EventType:    "pull_request_review_comment",
		Action:       "created",
		Sender:       &User{Login: "someone"},
		PullRequest:  nil,
		Installation: &Installation{ID: 42},
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertNotCalled(s.T(), "Publish", mock.Anything, mock.Anything)
}

func (s *GitHubPlatformSuite) TestIngest_PRComment_NilInstallation() {
	event := &WebhookEvent{
		EventType: "pull_request_review_comment",
		Action:    "created",
		Sender:    &User{Login: "someone"},
		PullRequest: &PullRequest{
			Head: Head{
				Ref: "feature",
				Repo: Repo{FullName: "org/repo"},
			},
		},
		Installation: nil,
	}

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertNotCalled(s.T(), "Publish", mock.Anything, mock.Anything)
}

func (s *GitHubPlatformSuite) TestIngest_PRComment_Valid() {
	event := &WebhookEvent{
		DeliveryID: "delivery-123",
		EventType:  "pull_request_review_comment",
		Action:     "created",
		Sender:     &User{Login: "someone"},
		PullRequest: &PullRequest{
			Head: Head{
				Ref: "my-branch",
				Repo: Repo{FullName: "org/my-repo"},
			},
		},
		Installation: &Installation{ID: 42},
	}

	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertCalled(s.T(), "Publish", mock.Anything, mock.MatchedBy(func(e types.PullRequestCommentedEvent) bool {
		return e.GitRef == "my-branch" && e.RepoFullName == "org/my-repo" && e.InstallationId == "42"
	}))
}

func (s *GitHubPlatformSuite) TestIngest_PRComment_NilSender() {
	event := &WebhookEvent{
		EventType: "pull_request_review_comment",
		Action:    "created",
		Sender:    nil,
		PullRequest: &PullRequest{
			Head: Head{
				Ref: "feature",
				Repo: Repo{FullName: "org/repo"},
			},
		},
		Installation: &Installation{ID: 42},
	}

	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.events.AssertCalled(s.T(), "Publish", mock.Anything, mock.Anything)
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestWebhook() {
	req := types.WebhookRequest{
		Headers: map[string][]string{"X-Hub-Signature": {"xxx"}},
	}
	expected := &WebhookEvent{EventType: "push"}

	s.client.On("Webhook", mock.Anything, req).Return(expected, nil)

	result, eventType, err := s.platform.Webhook(context.Background(), req)
	s.NoError(err)
	s.Equal(types.WebhookEventType_Git, eventType)
	s.Equal(expected, result)
}

// ---------------------------------------------------------------------------
// VerifyRepoAccess (delegates to access)
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestVerifyRepoAccess_PublicRepo_NoConnection() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(true, nil)

	ok, token, err := s.platform.VerifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubPlatformSuite) TestVerifyRepoAccess_PublicRepo_WithConnection() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	raw := `{"token":"ghs_public_token","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	ok, token, err := s.platform.VerifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.True(ok)
	s.Equal("ghs_public_token", token)
}

func (s *GitHubPlatformSuite) TestVerifyRepoAccess_NilRepo() {
	ok, token, err := s.platform.VerifyRepoAccess(context.Background(), "evt-1", nil)
	s.NoError(err)
	s.True(ok)
	s.Empty(token)
}

// ---------------------------------------------------------------------------
// RequestConnection (delegates to access)
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestRequestConnection() {
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.RequestConnection(context.Background(), "evt-1", "org/repo")
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "UpsertGitHubConnection", mock.Anything, mock.MatchedBy(func(c *types.GitHubConnection) bool {
		return c.RepoFullName == "org/repo" && c.SessionEventIdentifier != nil && *c.SessionEventIdentifier == "evt-1" && !c.Connected && c.InstallationId == nil
	}))
}

// ---------------------------------------------------------------------------
// handleInstallationRepositories
// ---------------------------------------------------------------------------

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_NilInstallation() {
	event := &WebhookEvent{
		EventType:          "installation_repositories",
		Action:             "added",
		Installation:       nil,
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_NonAddedAction() {
	event := &WebhookEvent{
		EventType:    "installation_repositories",
		Action:       "removed",
		Installation: &Installation{ID: 42},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo"},
		},
	}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertNotCalled(s.T(), "UpsertGitHubConnection")
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_NoReposAdded() {
	event := &WebhookEvent{
		EventType:          "installation_repositories",
		Action:             "added",
		Installation:       &Installation{ID: 42},
		RepositoriesAdded: []Repository{},
	}
	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertNotCalled(s.T(), "UpsertGitHubConnection")
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_Valid() {
	event := &WebhookEvent{
		EventType:    "installation_repositories",
		Action:       "added",
		Installation: &Installation{ID: 42},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo-new"},
		},
	}

	s.connections.On("GetGitHubConnectionByInstallationId", mock.Anything, "42").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "UpsertGitHubConnection", mock.Anything, mock.MatchedBy(func(c *types.GitHubConnection) bool {
		return c.RepoFullName == "org/repo-new" && c.Connected == true && *c.InstallationId == "42"
	}))
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_CompleteConnectionError() {
	event := &WebhookEvent{
		EventType:    "installation_repositories",
		Action:       "added",
		Installation: &Installation{ID: 42},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo-new"},
		},
	}

	s.connections.On("GetGitHubConnectionByInstallationId", mock.Anything, "42").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(errors.New("connection failed"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "connection failed")
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_MultipleRepos() {
	event := &WebhookEvent{
		EventType:    "installation_repositories",
		Action:       "added",
		Installation: &Installation{ID: 42},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo-new-1"},
			{ID: 2, FullName: "org/repo-new-2"},
		},
	}

	s.connections.On("GetGitHubConnectionByInstallationId", mock.Anything, "42").Return(nil, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertNumberOfCalls(s.T(), "UpsertGitHubConnection", 2)
	s.events.AssertNumberOfCalls(s.T(), "Publish", 2)
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_WithExistingConnection() {
	event := &WebhookEvent{
		EventType:    "installation_repositories",
		Action:       "added",
		Installation: &Installation{ID: 42},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo-new"},
		},
	}

	sessionEventId := "existing-evt-id"
	existingConn := &types.GitHubConnection{
		SessionEventIdentifier: &sessionEventId,
		RepoFullName:          "org/existing-repo",
		Connected:             true,
		InstallationId:        strPtr("42"),
	}
	s.connections.On("GetGitHubConnectionByInstallationId", mock.Anything, "42").Return(existingConn, nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.platform.Ingest(context.Background(), event)
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "UpsertGitHubConnection", mock.Anything, mock.MatchedBy(func(c *types.GitHubConnection) bool {
		return c.RepoFullName == "org/repo-new" && c.Connected == true && *c.InstallationId == "42" && c.SessionEventIdentifier != nil && *c.SessionEventIdentifier == "existing-evt-id"
	}))
}

func (s *GitHubPlatformSuite) TestIngest_InstallationRepositories_GetByInstallationIdError() {
	event := &WebhookEvent{
		EventType:    "installation_repositories",
		Action:       "added",
		Installation: &Installation{ID: 42},
		RepositoriesAdded: []Repository{
			{ID: 1, FullName: "org/repo-new"},
		},
	}

	s.connections.On("GetGitHubConnectionByInstallationId", mock.Anything, "42").Return(nil, errors.New("db error"))

	err := s.platform.Ingest(context.Background(), event)
	s.Error(err)
	s.Contains(err.Error(), "db error")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string {
	return &s
}
