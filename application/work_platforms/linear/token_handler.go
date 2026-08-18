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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/jazielguerrero/workdock/domain/ports"
)

type tokenHandlerConfig struct {
	ForSecrets ports.ForSecrets
	Client     LinearClientInterface
}

type tokenHandler struct {
	config tokenHandlerConfig
}

func newTokenHandler(config tokenHandlerConfig) *tokenHandler {
	return &tokenHandler{
		config: config,
	}
}

// GetLinearAccessToken retrieves the Linear API access token for a given
// organization from the secrets store.
//
// If the token is expired or expires within the next 5 minutes, it is renewed
// using the stored refresh token and the refreshed credentials are persisted
// back to the secrets store before returning.
func (s *tokenHandler) GetLinearAccessToken(ctx context.Context, byName string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	token, err := s.getStoredLinearToken(ctx, byName)

	if err != nil {
		return "", err
	}

	if time.Until(token.ExpiresAt) > 5*time.Minute {
		slog.Debug("Got linear access token", "secret_name", byName)
		return token.AccessToken, nil
	}

	slog.Debug("Linear access token is expired or expiring soon, refreshing", "secret_name", byName, "expires_at", token.ExpiresAt)

	if token.RefreshToken == "" {
		slog.Error("linear access token expired and no refresh token is available", "organization_id", byName)
		return "", types.ErrLinearTokenExpired
	}

	refreshed, err := s.config.Client.RefreshToken(ctx, token.RefreshToken)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}

		slog.Error("failed to refresh linear access token", "err", err, "organization_id", byName)
		return "", types.ErrLinearTokenRefreshFailed
	}

	if err := s.storeLinearToken(ctx, byName, refreshed); err != nil {
		return "", err
	}

	slog.Debug("Refreshed linear access token", "secret_name", byName)
	return refreshed.AccessToken, nil
}

// storeLinearToken serializes and persists the Linear token for a given
// organization in the secrets store.
func (s *tokenHandler) storeLinearToken(ctx context.Context, byName string, token *Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.Marshal(token)

	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := s.config.ForSecrets.Set(ctx, SecretsPath, byName, string(data)); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	return nil
}

// getStoredLinearToken retrieves and deserializes the Linear token for a given
// organization from the secrets store.
func (s *tokenHandler) getStoredLinearToken(ctx context.Context, byName string) (*Token, error) {
	raw, err := s.config.ForSecrets.Get(ctx, SecretsPath, byName)

	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	var token Token

	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}

	return &token, nil
}
