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
)

func TestTokenHandlerSuite(t *testing.T) {
	suite.Run(t, new(TokenHandlerSuite))
}

type TokenHandlerSuite struct {
	suite.Suite
	client  *mockClient
	secrets *mockSecrets
	handler *tokenHandler
}

func (s *TokenHandlerSuite) SetupTest() {
	s.client = new(mockClient)
	s.secrets = new(mockSecrets)

	s.handler = newTokenHandler(tokenHandlerConfig{
		ForSecrets: s.secrets,
		Client:     s.client,
	})
}

// ---------------------------------------------------------------------------
// getGitHubAccessToken
// ---------------------------------------------------------------------------

func (s *TokenHandlerSuite) TestGetAccessToken_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := s.handler.getGitHubAccessToken(ctx, "42")
	s.Error(result.Error)
	s.ErrorIs(result.Error, context.Canceled)
	s.Empty(result.Token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_SecretsGetError() {
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return("", errors.New("secrets error"))

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.Error(result.Error)
	s.Contains(result.Error.Error(), "failed to get github token")
	s.Empty(result.Token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_UnmarshalError() {
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return("not-json", nil)

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.Error(result.Error)
	s.Contains(result.Error.Error(), "failed to unmarshal github token")
	s.Empty(result.Token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ValidToken() {
	raw := `{"token":"ghs_valid","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.NoError(result.Error)
	s.Equal("ghs_valid", result.Token)
	s.False(result.Expired)
	s.secrets.AssertNotCalled(s.T(), "Set", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	s.client.AssertNotCalled(s.T(), "CreateInstallationAccessToken")
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken() {
	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	renewed := &InstallationAccessToken{
		Token:     "ghs_renewed",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	s.client.On("CreateInstallationAccessToken", 42).Return(renewed, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.NoError(result.Error)
	s.Equal("ghs_renewed", result.Token)
	s.True(result.Expired)
	s.client.AssertCalled(s.T(), "CreateInstallationAccessToken", 42)
	s.secrets.AssertCalled(s.T(), "Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiringSoonToken() {
	raw := `{"token":"ghs_expiring","expires_at":"` + time.Now().Add(3*time.Minute).Format(time.RFC3339) + `"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	renewed := &InstallationAccessToken{
		Token:     "ghs_renewed",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	s.client.On("CreateInstallationAccessToken", 42).Return(renewed, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.NoError(result.Error)
	s.Equal("ghs_renewed", result.Token)
	s.True(result.Expired)
	s.client.AssertCalled(s.T(), "CreateInstallationAccessToken", 42)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_RenewalError() {
	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	s.client.On("CreateInstallationAccessToken", 42).Return(nil, errors.New("github api error"))

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.Error(result.Error)
	s.Contains(result.Error.Error(), "failed to renew github access token")
	s.Empty(result.Token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_SetError() {
	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	renewed := &InstallationAccessToken{
		Token:     "ghs_renewed",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	s.client.On("CreateInstallationAccessToken", 42).Return(renewed, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(errors.New("store failed"))

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.Error(result.Error)
	s.Contains(result.Error.Error(), "failed to store github token")
	s.Empty(result.Token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_InvalidInstallationId() {
	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	result := s.handler.getGitHubAccessToken(context.Background(), "not-a-number")
	s.Error(result.Error)
	s.Contains(result.Error.Error(), "failed to parse installation id")
	s.Empty(result.Token)
	s.client.AssertNotCalled(s.T(), "CreateInstallationAccessToken")
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_MarshalError() {
	raw := `{"token":"ghs_expired","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, GitHub_SecretPath, "42").Return(raw, nil)

	renewed := &InstallationAccessToken{
		Token:     "ghs_renewed",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	s.client.On("CreateInstallationAccessToken", 42).Return(renewed, nil)
	s.secrets.On("Set", mock.Anything, GitHub_SecretPath, "42", mock.Anything).Return(nil)

	result := s.handler.getGitHubAccessToken(context.Background(), "42")
	s.NoError(result.Error)
	s.Equal("ghs_renewed", result.Token)
}
