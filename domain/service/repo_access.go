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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/repositories"
	"github.com/workdock-dev/engine/domain/types"
)

type RepoAccessConfig struct {
	GitHubConnections repositories.GitHubConnectionRepository
	ForSecrets        ports.ForSecrets
	ForEvent          ports.ForEventBus
}

type RepoAccessService struct {
	config RepoAccessConfig
}

func NewRepoAccessService(config RepoAccessConfig) *RepoAccessService {
	return &RepoAccessService{
		config: config,
	}
}

type VerifyRepoAccessInput struct {
	SessionEventIdentifier string
	RepoFullName           *string
}

type VerifyRepoAccessResult struct {
	HasAccess     bool
	Token         string
	Decision      RepoAccessDecision
	InstallationId *string
}

type RepoAccessDecision string

const (
	RepoAccessGranted          RepoAccessDecision = "granted"
	RepoAccessDenied           RepoAccessDecision = "denied"
	RepoAccessReRequest        RepoAccessDecision = "re_request"
	RepoAccessResetAndReRequest RepoAccessDecision = "reset_and_re_request"
)

type TokenDecision string

const (
	TokenKeep   TokenDecision = "keep"
	TokenRenew  TokenDecision = "renew"
	TokenExpired TokenDecision = "expired"
)

type RenewTokenInput struct {
	InstallationId string
	RawToken       string
	ExpiresAt      time.Time
}

type RenewTokenResult struct {
	Token     string
	ExpiresAt time.Time
}

func (s *RepoAccessService) VerifyRepoAccess(ctx context.Context, input VerifyRepoAccessInput) (*VerifyRepoAccessResult, error) {
	if input.RepoFullName == nil {
		return &VerifyRepoAccessResult{
			HasAccess: true,
			Token:     "",
			Decision:  RepoAccessGranted,
		}, nil
	}

	connection, err := s.config.GitHubConnections.GetGitHubConnection(ctx, *input.RepoFullName)
	if err != nil {
		return nil, err
	}

	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		return &VerifyRepoAccessResult{
			HasAccess: false,
			Token:     "",
			Decision:  RepoAccessDenied,
		}, nil
	}

	installationId := connection.InstallationId
	tokenData, err := s.config.ForSecrets.Get(ctx, "/github/installations", *installationId)
	if err != nil {
		if errors.Is(err, types.ErrGitHubInstallationUnavailable) {
			return &VerifyRepoAccessResult{
				HasAccess:     false,
				Token:         "",
				Decision:      RepoAccessResetAndReRequest,
				InstallationId: installationId,
			}, nil
		}
		return nil, err
	}

	token, expiresAt, err := parseTokenData(tokenData)
	if err != nil {
		return nil, err
	}

	tokenDecision := s.evaluateTokenDecision(expiresAt, true, time.Now(), types.DefaultTokenRefreshWindow)

	switch tokenDecision {
	case TokenKeep:
		return &VerifyRepoAccessResult{
			HasAccess:     true,
			Token:         token,
			Decision:      RepoAccessGranted,
			InstallationId: installationId,
		}, nil
	case TokenExpired:
		return nil, errors.New("github access token expired and cannot be renewed")
	case TokenRenew:
		return &VerifyRepoAccessResult{
			HasAccess:     true,
			Token:         token,
			Decision:      RepoAccessGranted,
			InstallationId: installationId,
		}, nil
	}

	return &VerifyRepoAccessResult{
		HasAccess:     false,
		Token:         "",
		Decision:      RepoAccessDenied,
		InstallationId: nil,
	}, nil
}

func (s *RepoAccessService) evaluateTokenDecision(expiresAt time.Time, hasRefresh bool, now time.Time, refreshWindow time.Duration) TokenDecision {
	decision := types.ShouldRenewToken(expiresAt, hasRefresh, now, refreshWindow)
	switch decision {
	case types.TokenLifecycleKeep:
		return TokenKeep
	case types.TokenLifecycleRenew:
		return TokenRenew
	case types.TokenLifecycleExpired:
		return TokenExpired
	default:
		return TokenKeep
	}
}

func (s *RepoAccessService) BuildConnectionRequest(sessionEventIdentifier, repoFullName string) *types.GitHubConnection {
	sessionEventId := sessionEventIdentifier
	return &types.GitHubConnection{
		SessionEventIdentifier: &sessionEventId,
		RepoFullName:           repoFullName,
		Connected:              false,
		InstallationId:         nil,
	}
}

func (s *RepoAccessService) BuildCompleteConnection(installationId, repoFullName string) *types.GitHubConnection {
	return &types.GitHubConnection{
		SessionEventIdentifier: nil,
		RepoFullName:           repoFullName,
		Connected:              true,
		InstallationId:         &installationId,
	}
}

func (s *RepoAccessService) ShouldResetInstallation(err error) bool {
	return errors.Is(err, types.ErrGitHubInstallationUnavailable)
}

func parseTokenData(tokenData string) (string, time.Time, error) {
	var token struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	if err := json.Unmarshal([]byte(tokenData), &token); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to unmarshal github token: %w", err)
	}

	return token.Token, token.ExpiresAt, nil
}
