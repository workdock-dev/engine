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

package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/plug-ins/linear/interfaces"
	"github.com/workdock-dev/engine/plug-ins/linear/types"
	"github.com/workdock-dev/engine/shared"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockSecretManager struct {
	getFn func(ctx context.Context, secretPath, secretName string) (string, error)
	setFn func(ctx context.Context, secretPath, secretName, secretValue string) error

	gets []string
	sets []string
}

func (m *mockSecretManager) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	m.gets = append(m.gets, secretPath+"/"+secretName)
	if m.getFn != nil {
		return m.getFn(ctx, secretPath, secretName)
	}
	return "", nil
}

func (m *mockSecretManager) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	m.sets = append(m.sets, secretPath+"/"+secretName+"="+secretValue)
	if m.setFn != nil {
		return m.setFn(ctx, secretPath, secretName, secretValue)
	}
	return nil
}

func (m *mockSecretManager) Delete(ctx context.Context, secretPath, secretName string) error {
	return nil
}

type mockClient struct {
	refreshFn func(ctx context.Context, refreshToken string) (*types.Token, error)

	refreshedWith []string
}

func (m *mockClient) GetIssueLabels(ctx context.Context, issueId, accessToken string) ([]string, error) {
	return nil, nil
}

func (m *mockClient) GetIssue(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
	return nil, nil
}

func (m *mockClient) GetTeamWorkflowStates(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
	return nil, nil
}

func (m *mockClient) UpdateIssueState(ctx context.Context, accessToken, issueId, stateId string) error {
	return nil
}

func (m *mockClient) ExchangeCode(ctx context.Context, code string) (*types.TokenExchanged, error) {
	return nil, nil
}

func (m *mockClient) GetWorkspaceInfo(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error) {
	return nil, nil
}

func (m *mockClient) RefreshToken(ctx context.Context, refreshToken string) (*types.Token, error) {
	m.refreshedWith = append(m.refreshedWith, refreshToken)
	if m.refreshFn != nil {
		return m.refreshFn(ctx, refreshToken)
	}
	return &types.Token{AccessToken: "new-access", RefreshToken: "new-refresh"}, nil
}

func (m *mockClient) CreateAgentActivity(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error {
	return nil
}

func (m *mockClient) SendInitialThought(ctx context.Context, sessionId, organizationId string) error {
	return nil
}

func (m *mockClient) GetCredentials(ctx context.Context, organizationId string) (string, error) {
	return "", nil
}

// ---------------------------------------------------------------------------
// TokenHandlerSuite
// ---------------------------------------------------------------------------

type TokenHandlerSuite struct {
	suite.Suite
	secrets *mockSecretManager
	client  *mockClient
	handler *TokenHandler
}

func TestTokenHandlerSuite(t *testing.T) {
	suite.Run(t, new(TokenHandlerSuite))
}

func (s *TokenHandlerSuite) SetupTest() {
	s.secrets = &mockSecretManager{}
	s.client = &mockClient{}
	s.handler = NewTokenHandler(s.secrets, s.client)
}

func (s *TokenHandlerSuite) storedToken(token types.Token) {
	s.T().Helper()

	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		s.Equal(types.SecretsPath, secretPath)
		s.Equal("org-1", secretName)
		data, err := json.Marshal(token)
		s.Require().NoError(err)
		return string(data), nil
	}
}

func (s *TokenHandlerSuite) expiredToken() types.Token {
	return types.Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
}

func (s *TokenHandlerSuite) TestNewTokenHandler() {
	var _ interfaces.Client = s.client // interface compliance of the mock

	s.NotNil(s.handler)
	s.Equal(s.secrets, s.handler.secretManager)
	s.Equal(s.client, s.handler.client)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	token, err := s.handler.GetLinearAccessToken(ctx, "org-1")

	s.ErrorIs(err, context.Canceled)
	s.Empty(token)
	s.Empty(s.secrets.gets)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_SecretGetError() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return "", fmt.Errorf("boom")
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.Error(err)
	s.Contains(err.Error(), "failed to get token")
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_UnmarshalError() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return "{not-json", nil
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.Error(err)
	s.Contains(err.Error(), "failed to unmarshal token")
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_FreshToken() {
	fresh := types.Token{
		AccessToken:  "valid-access",
		RefreshToken: "valid-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	s.storedToken(fresh)

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.Require().NoError(err)
	s.Equal("valid-access", token)
	s.Empty(s.client.refreshedWith)
	s.Empty(s.secrets.sets)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_ExpiredNoRefreshToken() {
	expired := types.Token{
		AccessToken: "old-access",
		ExpiresAt:   time.Now().Add(-time.Hour),
	}
	s.storedToken(expired)

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.ErrorIs(err, shared.ErrLinearTokenExpired)
	s.Empty(s.client.refreshedWith)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_RefreshError() {
	s.storedToken(s.expiredToken())
	s.client.refreshFn = func(ctx context.Context, refreshToken string) (*types.Token, error) {
		return nil, fmt.Errorf("api down")
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.ErrorIs(err, shared.ErrLinearTokenRefreshFailed)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_RefreshContextCancelled() {
	s.storedToken(s.expiredToken())
	s.client.refreshFn = func(ctx context.Context, refreshToken string) (*types.Token, error) {
		return nil, context.Canceled
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.ErrorIs(err, context.Canceled)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_RefreshContextDeadline() {
	s.storedToken(s.expiredToken())
	s.client.refreshFn = func(ctx context.Context, refreshToken string) (*types.Token, error) {
		return nil, context.DeadlineExceeded
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.ErrorIs(err, context.DeadlineExceeded)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_RefreshSuccess() {
	s.storedToken(s.expiredToken())
	s.client.refreshFn = func(ctx context.Context, refreshToken string) (*types.Token, error) {
		s.Equal("old-refresh", refreshToken)
		return &types.Token{AccessToken: "new-access", RefreshToken: "new-refresh"}, nil
	}

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.Require().NoError(err)
	s.Equal("new-access", token)

	s.Require().Len(s.secrets.sets, 1)
	s.Contains(s.secrets.sets[0], types.SecretsPath)
	s.Contains(s.secrets.sets[0], "new-access")

	var stored types.Token
	s.Require().NoError(json.Unmarshal([]byte(
		s.secrets.sets[0][len(types.SecretsPath+"/org-1="):]), &stored))
	s.Equal("new-access", stored.AccessToken)
	s.Equal("new-refresh", stored.RefreshToken)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_RefreshThenStoreContextCancelled() {
	s.storedToken(s.expiredToken())
	// Cancel the context during refresh, so storeLinearToken observes a dead
	// context and fails before writing the secret.
	s.client.refreshFn = func(ctx context.Context, refreshToken string) (*types.Token, error) {
		return &types.Token{AccessToken: "new-access", RefreshToken: "new-refresh"}, context.Canceled
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.ErrorIs(err, context.Canceled)
	s.Empty(s.secrets.sets)
}

func (s *TokenHandlerSuite) TestGetLinearAccessToken_RefreshThenStoreSetError() {
	s.storedToken(s.expiredToken())
	s.secrets.setFn = func(ctx context.Context, secretPath, secretName, secretValue string) error {
		return fmt.Errorf("store down")
	}

	_, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")

	s.Error(err)
	s.Contains(err.Error(), "failed to store token")
}
