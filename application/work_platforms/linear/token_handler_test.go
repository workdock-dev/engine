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
	"errors"
	"testing"
	"time"

	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

func TestTokenHandlerSuite(t *testing.T) {
	suite.Run(t, new(TokenHandlerSuite))
}

type TokenHandlerSuite struct {
	suite.Suite
	client  *mockLinearClient
	secrets *mockSecrets
	handler *tokenHandler
}

func (s *TokenHandlerSuite) SetupTest() {
	s.client = new(mockLinearClient)
	s.secrets = new(mockSecrets)

	s.handler = newTokenHandler(tokenHandlerConfig{
		ForSecrets: s.secrets,
		Client:     s.client,
	})
}

// ---------------------------------------------------------------------------
// GetLinearAccessToken
// ---------------------------------------------------------------------------

func (s *TokenHandlerSuite) TestGetAccessToken_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	token, err := s.handler.GetLinearAccessToken(ctx, "org-1")
	s.Error(err)
	s.ErrorIs(err, context.Canceled)
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_GetStoredTokenError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("secrets error"))

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.Error(err)
	s.Contains(err.Error(), "failed to get token")
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_UnmarshalError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("not-json", nil)

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.Error(err)
	s.Contains(err.Error(), "failed to unmarshal token")
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ValidToken() {
	raw := `{"access_token":"at_valid","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.NoError(err)
	s.Equal("at_valid", token)
	s.client.AssertNotCalled(s.T(), "RefreshToken")
	s.secrets.AssertNotCalled(s.T(), "Set", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_NoRefreshToken() {
	raw := `{"access_token":"at_expired","refresh_token":"","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.ErrorIs(err, types.ErrLinearTokenExpired)
	s.Empty(token)
	s.client.AssertNotCalled(s.T(), "RefreshToken")
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_RefreshContextCancelled() {
	raw := `{"access_token":"at_expired","refresh_token":"rt","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	ctx := context.Background()
	s.client.On("RefreshToken", ctx, "rt").Return(nil, context.Canceled)

	token, err := s.handler.GetLinearAccessToken(ctx, "org-1")
	s.Error(err)
	s.ErrorIs(err, context.Canceled)
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_RefreshDeadlineExceeded() {
	raw := `{"access_token":"at_expired","refresh_token":"rt","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	ctx := context.Background()
	s.client.On("RefreshToken", ctx, "rt").Return(nil, context.DeadlineExceeded)

	token, err := s.handler.GetLinearAccessToken(ctx, "org-1")
	s.Error(err)
	s.ErrorIs(err, context.DeadlineExceeded)
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_RefreshOtherError() {
	raw := `{"access_token":"at_expired","refresh_token":"rt","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)
	s.client.On("RefreshToken", mock.Anything, "rt").Return(nil, errors.New("api error"))

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.ErrorIs(err, types.ErrLinearTokenRefreshFailed)
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_StoreError() {
	raw := `{"access_token":"at_expired","refresh_token":"rt","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	refreshed := &Token{
		AccessToken:  "at_refreshed",
		RefreshToken: "rt_new",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	s.client.On("RefreshToken", mock.Anything, "rt").Return(refreshed, nil)
	s.secrets.On("Set", mock.Anything, SecretsPath, "org-1", mock.Anything).Return(errors.New("store failed"))

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.Error(err)
	s.Contains(err.Error(), "failed to store token")
	s.Empty(token)
}

func (s *TokenHandlerSuite) TestGetAccessToken_ExpiredToken_Success() {
	raw := `{"access_token":"at_expired","refresh_token":"rt","expires_at":"2020-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	refreshed := &Token{
		AccessToken:  "at_refreshed",
		RefreshToken: "rt_new",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	s.client.On("RefreshToken", mock.Anything, "rt").Return(refreshed, nil)
	s.secrets.On("Set", mock.Anything, SecretsPath, "org-1", mock.Anything).Return(nil)

	token, err := s.handler.GetLinearAccessToken(context.Background(), "org-1")
	s.NoError(err)
	s.Equal("at_refreshed", token)
	s.secrets.AssertCalled(s.T(), "Set", mock.Anything, SecretsPath, "org-1", mock.Anything)
}

// ---------------------------------------------------------------------------
// getStoredLinearToken (tested via GetLinearAccessToken)
// ---------------------------------------------------------------------------

func (s *TokenHandlerSuite) TestGetStoredLinearToken_GetError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("not found"))

	token, err := s.handler.getStoredLinearToken(context.Background(), "org-1")
	s.Error(err)
	s.Nil(token)
}

func (s *TokenHandlerSuite) TestGetStoredLinearToken_UnmarshalError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("bad-json", nil)

	token, err := s.handler.getStoredLinearToken(context.Background(), "org-1")
	s.Error(err)
	s.Nil(token)
}

func (s *TokenHandlerSuite) TestGetStoredLinearToken_Success() {
	raw := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(raw, nil)

	token, err := s.handler.getStoredLinearToken(context.Background(), "org-1")
	s.NoError(err)
	s.NotNil(token)
	s.Equal("at", token.AccessToken)
	s.Equal("rt", token.RefreshToken)
}

// ---------------------------------------------------------------------------
// storeLinearToken
// ---------------------------------------------------------------------------

func (s *TokenHandlerSuite) TestStoreToken_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	token := &Token{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)}
	err := s.handler.storeLinearToken(ctx, "org-1", token)
	s.Error(err)
	s.ErrorIs(err, context.Canceled)
}

func (s *TokenHandlerSuite) TestStoreToken_MarshalError() {
	token := &Token{AccessToken: "at", ExpiresAt: time.Now().Add(time.Hour)}
	s.secrets.On("Set", mock.Anything, SecretsPath, "org-1", mock.Anything).Return(nil)

	err := s.handler.storeLinearToken(context.Background(), "org-1", token)
	// Token marshals fine, so this should succeed
	s.NoError(err)
}

func (s *TokenHandlerSuite) TestStoreToken_SetError() {
	token := &Token{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)}
	s.secrets.On("Set", mock.Anything, SecretsPath, "org-1", mock.Anything).Return(errors.New("set failed"))

	err := s.handler.storeLinearToken(context.Background(), "org-1", token)
	s.Error(err)
	s.Contains(err.Error(), "failed to store token")
}

func (s *TokenHandlerSuite) TestStoreToken_Success() {
	token := &Token{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)}
	s.secrets.On("Set", mock.Anything, SecretsPath, "org-1", mock.Anything).Return(nil)

	err := s.handler.storeLinearToken(context.Background(), "org-1", token)
	s.NoError(err)
	s.secrets.AssertCalled(s.T(), "Set", mock.Anything, SecretsPath, "org-1", mock.Anything)
}
