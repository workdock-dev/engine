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

package domain_service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
)

func TestRepoAccessServiceSuite(t *testing.T) {
	suite.Run(t, new(RepoAccessServiceSuite))
}

type RepoAccessServiceSuite struct {
	suite.Suite
	secrets     *mockSecrets
	events      *mockEventBus
	connections *mockGitHubConnections
	service     *RepoAccessService
}

type mockSecrets struct {
	mock.Mock
}

func (m *mockSecrets) Get(ctx context.Context, path, key string) (string, error) {
	args := m.Called(ctx, path, key)
	return args.String(0), args.Error(1)
}

func (m *mockSecrets) Set(ctx context.Context, path, key, value string) error {
	args := m.Called(ctx, path, key, value)
	return args.Error(0)
}

func (m *mockSecrets) Delete(ctx context.Context, path, key string) error {
	args := m.Called(ctx, path, key)
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

func (m *mockGitHubConnections) UpsertGitHubConnection(ctx context.Context, connection *types.GitHubConnection) error {
	args := m.Called(ctx, connection)
	return args.Error(0)
}

func (m *mockGitHubConnections) ResetGitHubConnection(ctx context.Context, installationId string, repos []string) error {
	args := m.Called(ctx, installationId, repos)
	return args.Error(0)
}

func (s *RepoAccessServiceSuite) SetupTest() {
	s.secrets = new(mockSecrets)
	s.events = new(mockEventBus)
	s.connections = new(mockGitHubConnections)

	s.service = NewRepoAccessService(RepoAccessConfig{
		GitHubConnections: s.connections,
		ForSecrets:        s.secrets,
		ForEvent:          s.events,
	})
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_NilRepo() {
	result, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           nil,
	})

	s.NoError(err)
	s.True(result.HasAccess)
	s.Empty(result.Token)
	s.Equal(RepoAccessGranted, result.Decision)
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_NoConnection() {
	repo := "org/repo"
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, nil)

	result, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           &repo,
	})

	s.NoError(err)
	s.False(result.HasAccess)
	s.Empty(result.Token)
	s.Equal(RepoAccessDenied, result.Decision)
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_NotConnected() {
	repo := "org/repo"
	conn := &types.GitHubConnection{Connected: false}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	result, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           &repo,
	})

	s.NoError(err)
	s.False(result.HasAccess)
	s.Empty(result.Token)
	s.Equal(RepoAccessDenied, result.Decision)
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_GetConnectionError() {
	repo := "org/repo"
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(nil, errors.New("db error"))

	_, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           &repo,
	})

	s.Error(err)
	s.Contains(err.Error(), "db error")
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_TokenUnavailable_ResetsAndReRequests() {
	repo := "org/repo"
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId, RepoFullName: repo}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	s.secrets.On("Get", mock.Anything, "/github/installations", "42").Return("", types.ErrGitHubInstallationUnavailable)

	result, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           &repo,
	})

	s.NoError(err)
	s.False(result.HasAccess)
	s.Empty(result.Token)
	s.Equal(RepoAccessResetAndReRequest, result.Decision)
	s.Equal(installId, *result.InstallationId)
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_ValidToken() {
	repo := "org/repo"
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	raw := `{"token":"ghs_valid_token","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, "/github/installations", "42").Return(raw, nil)

	result, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           &repo,
	})

	s.NoError(err)
	s.True(result.HasAccess)
	s.Equal("ghs_valid_token", result.Token)
	s.Equal(RepoAccessGranted, result.Decision)
}

func (s *RepoAccessServiceSuite) TestVerifyRepoAccess_TokenExpired_RenewsAndReturns() {
	repo := "org/repo"
	installId := "42"
	conn := &types.GitHubConnection{Connected: true, InstallationId: &installId}
	s.connections.On("GetGitHubConnection", mock.Anything, "org/repo").Return(conn, nil)

	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, "/github/installations", "42").Return(raw, nil)

	result, err := s.service.VerifyRepoAccess(context.Background(), VerifyRepoAccessInput{
		SessionEventIdentifier: "evt-1",
		RepoFullName:           &repo,
	})

	s.NoError(err)
	s.NotNil(result)
	s.True(result.HasAccess)
	s.Equal(RepoAccessGranted, result.Decision)
	s.Equal("ghs_expired", result.Token)
}

func (s *RepoAccessServiceSuite) TestBuildConnectionRequest() {
	result := s.service.BuildConnectionRequest("evt-1", "org/repo")

	s.Equal("org/repo", result.RepoFullName)
	s.Equal("evt-1", *result.SessionEventIdentifier)
	s.False(result.Connected)
	s.Nil(result.InstallationId)
}

func (s *RepoAccessServiceSuite) TestBuildCompleteConnection() {
	result := s.service.BuildCompleteConnection("42", "org/repo")

	s.Equal("org/repo", result.RepoFullName)
	s.True(result.Connected)
	s.Equal("42", *result.InstallationId)
	s.Nil(result.SessionEventIdentifier)
}

func (s *RepoAccessServiceSuite) TestShouldResetInstallation() {
	s.True(s.service.ShouldResetInstallation(types.ErrGitHubInstallationUnavailable))
	s.False(s.service.ShouldResetInstallation(errors.New("other error")))
}

func (s *RepoAccessServiceSuite) TestEvaluateTokenDecision_Keep() {
	future := time.Now().Add(time.Hour)
	decision := s.service.evaluateTokenDecision(future, true, time.Now(), types.DefaultTokenRefreshWindow)
	s.Equal(TokenKeep, decision)
}

func (s *RepoAccessServiceSuite) TestEvaluateTokenDecision_Renew() {
	past := time.Now().Add(-10 * time.Minute)
	decision := s.service.evaluateTokenDecision(past, true, time.Now(), types.DefaultTokenRefreshWindow)
	s.Equal(TokenRenew, decision)
}

func (s *RepoAccessServiceSuite) TestEvaluateTokenDecision_Expired() {
	past := time.Now().Add(-10 * time.Minute)
	decision := s.service.evaluateTokenDecision(past, false, time.Now(), types.DefaultTokenRefreshWindow)
	s.Equal(TokenExpired, decision)
}
