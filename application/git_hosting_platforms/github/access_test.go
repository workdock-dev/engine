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

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/application"
	"github.com/workdock-dev/engine/domain/types"
)

func TestGitHubAccessSuite(t *testing.T) {
	suite.Run(t, new(GitHubAccessSuite))
}

type GitHubAccessSuite struct {
	suite.Suite
	client      *mockClient
	secrets     *mockSecrets
	events      *mockEventBus
	connections *mockGitHubConnections
	access      *githubAccess
}

func (s *GitHubAccessSuite) SetupTest() {
	s.client = new(mockClient)
	s.secrets = new(mockSecrets)
	s.events = new(mockEventBus)
	s.connections = new(mockGitHubConnections)

	s.events.On("Subscribe", mock.Anything, mock.Anything).Return().Maybe()

	app := application.New()
	application.WithSecretManager(app, s.secrets)
	application.WithEventBus(app, s.events)
	application.WithGitHubRepository(app, s.connections)
	app.Init()

	s.access = newGitHubAccess(githubAccessConfig{
		Client: s.client,
		app:    app,
	})
}

// ---------------------------------------------------------------------------
// verifyRepoAccess
// ---------------------------------------------------------------------------

func (s *GitHubAccessSuite) TestVerifyRepoAccess_NilRepo() {
	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", nil)
	s.NoError(err)
	s.True(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_IsPublicError() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(false, errors.New("check failed"))

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.Error(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_PublicRepo_NoConnection() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(true, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_PrivateRepo_NoConnection() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(false, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_PublicRepo_WithConnection() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	raw := `{"token":"ghs_public_token","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.True(ok)
	s.Equal("ghs_public_token", token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_GetConnectionError() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, errors.New("db error"))

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.Error(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_NilConnection() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(false, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_NotConnected() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(&types.GitHubConnection{Connected: false}, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(false, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_NoInstallationId() {
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(&types.GitHubConnection{Connected: true, InstallationId: nil}, nil)
	s.client.On("IsRepositoryPublic", mock.Anything, "org/repo").Return(true, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_TokenError_NonUnavailable() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return("", errors.New("secrets read failed"))

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.Error(err)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_TokenUnavailable_ResetsAndReRequests() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId, RepoFullName: "org/repo"}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return("", types.ErrGitHubInstallationUnavailable)
	s.connections.On("ResetGitHubConnection", mock.Anything, "42", mock.Anything).Return(nil)
	s.secrets.On("Delete", mock.Anything, GitHub_SecretPath, "42").Return(nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.ErrorIs(err, types.ErrGitHubConnectionReRequested)
	s.False(ok)
	s.Empty(token)
	s.connections.AssertCalled(s.T(), "ResetGitHubConnection", mock.Anything, "42", mock.Anything)
	s.connections.AssertCalled(s.T(), "UpsertGitHubConnection", mock.Anything, mock.Anything)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_TokenUnavailable_ResetFails_StillRequestsConnection() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId, RepoFullName: "org/repo"}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return("", types.ErrGitHubInstallationUnavailable)
	s.connections.On("ResetGitHubConnection", mock.Anything, "42", mock.Anything).Return(errors.New("reset failed"))
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	// ResetInstallation returns error (ignored), then RequestConnection succeeds → returns ErrGitHubConnectionReRequested
	s.ErrorIs(err, types.ErrGitHubConnectionReRequested)
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_TokenUnavailable_RequestConnectionFails() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId, RepoFullName: "org/repo"}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return("", types.ErrGitHubInstallationUnavailable)
	s.connections.On("ResetGitHubConnection", mock.Anything, "42", mock.Anything).Return(nil)
	s.secrets.On("Delete", mock.Anything, GitHub_SecretPath, "42").Return(nil)
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(errors.New("upsert failed"))

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.Error(err)
	s.Contains(err.Error(), "upsert failed")
	s.False(ok)
	s.Empty(token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_ValidToken() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	raw := `{"token":"ghs_valid_token","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.True(ok)
	s.Equal("ghs_valid_token", token)
}

func (s *GitHubAccessSuite) TestVerifyRepoAccess_TokenExpired_RenewsAndReturns() {
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	renewed := &InstallationAccessToken{
		Token:     "ghs_renewed",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	s.client.On("CreateInstallationAccessToken", 42).Return(renewed, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)

	ok, token, err := s.access.verifyRepoAccess(context.Background(), "evt-1", strPtr("org/repo"))
	s.NoError(err)
	s.True(ok)
	s.Equal("ghs_renewed", token)
}

// ---------------------------------------------------------------------------
// RequestConnection
// ---------------------------------------------------------------------------

func (s *GitHubAccessSuite) TestRequestConnection() {
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)

	err := s.access.RequestConnection(context.Background(), "evt-1", "org/repo")
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "UpsertGitHubConnection", mock.Anything, mock.MatchedBy(func(c *types.GitHubConnection) bool {
		return c.RepoFullName == "org/repo" && c.SessionEventIdentifier != nil && *c.SessionEventIdentifier == "evt-1" && !c.Connected && c.InstallationId == nil
	}))
}

func (s *GitHubAccessSuite) TestRequestConnection_Error() {
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(errors.New("db error"))

	err := s.access.RequestConnection(context.Background(), "evt-1", "org/repo")
	s.Error(err)
	s.Contains(err.Error(), "db error")
}

// ---------------------------------------------------------------------------
// ResetInstallation
// ---------------------------------------------------------------------------

func (s *GitHubAccessSuite) TestResetInstallation() {
	repos := []string{"org/repo1", "org/repo2"}
	s.connections.On("ResetGitHubConnection", mock.Anything, "42", mock.Anything).Return(nil)
	s.secrets.On("Delete", mock.Anything, GitHub_SecretPath, "42").Return(nil)

	err := s.access.ResetInstallation(context.Background(), "42", repos)
	s.NoError(err)
	s.connections.AssertCalled(s.T(), "ResetGitHubConnection", mock.Anything, "42", mock.Anything)
	s.secrets.AssertCalled(s.T(), "Delete", mock.Anything, GitHub_SecretPath, "42")
}

func (s *GitHubAccessSuite) TestResetInstallation_ResetError() {
	repos := []string{"org/repo1"}
	s.connections.On("ResetGitHubConnection", mock.Anything, "42", mock.Anything).Return(errors.New("reset failed"))

	err := s.access.ResetInstallation(context.Background(), "42", repos)
	s.Error(err)
	s.Contains(err.Error(), "reset failed")
	s.secrets.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything, mock.Anything)
}

func (s *GitHubAccessSuite) TestResetInstallation_DeleteError() {
	repos := []string{"org/repo1"}
	s.connections.On("ResetGitHubConnection", mock.Anything, "42", mock.Anything).Return(nil)
	s.secrets.On("Delete", mock.Anything, GitHub_SecretPath, "42").Return(errors.New("delete failed"))

	err := s.access.ResetInstallation(context.Background(), "42", repos)
	s.Error(err)
	s.Contains(err.Error(), "delete failed")
}

// ---------------------------------------------------------------------------
// CompleteConnection
// ---------------------------------------------------------------------------

func (s *GitHubAccessSuite) TestCompleteConnection() {
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := s.access.CompleteConnection(context.Background(), "42", []string{"org/repo1", "org/repo2"})
	s.NoError(err)
	s.connections.AssertNumberOfCalls(s.T(), "UpsertGitHubConnection", 2)
	s.events.AssertNumberOfCalls(s.T(), "Publish", 2)
}

func (s *GitHubAccessSuite) TestCompleteConnection_UpsertError() {
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(errors.New("upsert failed"))

	err := s.access.CompleteConnection(context.Background(), "42", []string{"org/repo1"})
	s.Error(err)
	s.Contains(err.Error(), "upsert failed")
	s.events.AssertNotCalled(s.T(), "Publish", mock.Anything, mock.Anything)
}

func (s *GitHubAccessSuite) TestCompleteConnection_PublishesCorrectEvent() {
	var publishedEvent types.GitHubConnectedEvent
	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		publishedEvent = args.Get(1).(types.GitHubConnectedEvent)
	}).Return(nil)

	installId := "42"
	err := s.access.CompleteConnection(context.Background(), "42", []string{"org/repo"})
	s.NoError(err)
	s.Equal("org/repo", publishedEvent.Connection.RepoFullName)
	s.True(publishedEvent.Connection.Connected)
	s.Equal(&installId, publishedEvent.Connection.InstallationId)
	s.Nil(publishedEvent.Connection.SessionEventIdentifier)
}

func (s *GitHubAccessSuite) TestCompleteConnection_MultipleRepos() {
	var publishedEvents []types.GitHubConnectedEvent

	s.connections.On("UpsertGitHubConnection", mock.Anything, mock.Anything).Return(nil)
	s.events.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		publishedEvents = append(publishedEvents, args.Get(1).(types.GitHubConnectedEvent))
	}).Return(nil)

	err := s.access.CompleteConnection(context.Background(), "42", []string{"org/repo1", "org/repo2"})
	s.NoError(err)
	s.connections.AssertNumberOfCalls(s.T(), "UpsertGitHubConnection", 2)
	s.Len(publishedEvents, 2)
	s.Equal("org/repo1", publishedEvents[0].Connection.RepoFullName)
	s.Nil(publishedEvents[0].Connection.SessionEventIdentifier)
	s.Equal("org/repo2", publishedEvents[1].Connection.RepoFullName)
	s.Nil(publishedEvents[1].Connection.SessionEventIdentifier)
}
