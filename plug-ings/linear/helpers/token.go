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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/workdock-dev/engine/plug-ings/linear/interfaces"
	"github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/shared"
)

type TokenHandler struct {
	secretManager shared.ForSecrets
	client        interfaces.Client
}

func NewTokenHandler(secretManager shared.ForSecrets, client interfaces.Client) *TokenHandler {
	return &TokenHandler{
		secretManager: secretManager,
		client:        client,
	}
}

// GetLinearAccessToken retrieves the Linear API access token for a given
// organization from the secrets store.
//
// If the token is expired or expires within the next 5 minutes, it is renewed
// using the stored refresh token and the refreshed credentials are persisted
// back to the secrets store before returning.
func (s *TokenHandler) GetLinearAccessToken(ctx context.Context, byName string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	token, err := s.getStoredLinearToken(ctx, byName)

	if err != nil {
		return "", err
	}

	now := time.Now()
	decision := shared.ShouldRenewToken(token.ExpiresAt, token.RefreshToken != "", now, shared.DefaultTokenRefreshWindow)

	if decision == shared.TokenLifecycleKeep {
		slog.Debug("Got linear access token", "secret_name", byName)
		return token.AccessToken, nil
	}

	if decision == shared.TokenLifecycleExpired {
		slog.Error("linear access token expired and no refresh token is available", "organization_id", byName)
		return "", shared.ErrLinearTokenExpired
	}

	slog.Debug("Linear access token is expired or expiring soon, refreshing", "secret_name", byName, "expires_at", token.ExpiresAt)

	refreshed, err := s.client.RefreshToken(ctx, token.RefreshToken)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}

		slog.Error("failed to refresh linear access token", "err", err, "organization_id", byName)
		return "", shared.ErrLinearTokenRefreshFailed
	}

	if err := s.storeLinearToken(ctx, byName, refreshed); err != nil {
		return "", err
	}

	slog.Debug("Refreshed linear access token", "secret_name", byName)
	return refreshed.AccessToken, nil
}

// storeLinearToken serializes and persists the Linear token for a given
// organization in the secrets store.
func (s *TokenHandler) storeLinearToken(ctx context.Context, byName string, token *types.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.Marshal(token)

	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := s.secretManager.Set(ctx, types.SecretsPath, byName, string(data)); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	return nil
}

// getStoredLinearToken retrieves and deserializes the Linear token for a given
// organization from the secrets store.
func (s *TokenHandler) getStoredLinearToken(ctx context.Context, byName string) (*types.Token, error) {
	raw, err := s.secretManager.Get(ctx, types.SecretsPath, byName)

	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	var token types.Token

	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}

	return &token, nil
}
