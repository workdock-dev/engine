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
	"log/slog"

	"github.com/workdock-dev/engine/application"
	"github.com/workdock-dev/engine/domain/service"
	"github.com/workdock-dev/engine/domain/types"
)

type githubAccessConfig struct {
	Client ClientInterface
	app    *application.App
}

type githubAccess struct {
	config         githubAccessConfig
	domainService  *domain_service.RepoAccessService
	tokenHandler   *tokenHandler
}

func newGitHubAccess(config githubAccessConfig) *githubAccess {
	return &githubAccess{
		config:        config,
		domainService: config.app.GetRepoAccessService(),
		tokenHandler: newTokenHandler(tokenHandlerConfig{
			ForSecrets: config.app.GetForSecrets(),
			Client:     config.Client,
		}),
	}
}

func (s *githubAccess) verifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repoFullName *string) (bool, string, error) {
	input := domain_service.VerifyRepoAccessInput{
		SessionEventIdentifier: sessionEventIdentifier,
		RepoFullName:           repoFullName,
	}

	result, err := s.domainService.VerifyRepoAccess(ctx, input)

	if err != nil {
		return false, "", err
	}

	switch result.Decision {
	case domain_service.RepoAccessGranted:
		if result.InstallationId == nil {
			slog.Debug("Verified repo access", "has_access", true)
			return true, "", nil
		}
		token, tokenErr := s.tokenHandler.getGitHubAccessToken(ctx, *result.InstallationId)
		if tokenErr != nil {
			return false, "", tokenErr
		}
		slog.Debug("Verified repo access", "has_access", true)
		return true, token, nil
	case domain_service.RepoAccessDenied:
		public, publicErr := s.config.Client.IsRepositoryPublic(ctx, *repoFullName)

		if publicErr != nil {
			return false, "", publicErr
		}

		slog.Debug("No GitHub connection found", "public", public, "has_access", false)
		return false, "", nil
	case domain_service.RepoAccessResetAndReRequest:
		slog.Debug(
			"GitHub installation unavailable, resetting connection",
			"installation_id", *result.InstallationId,
			"event_identifier", sessionEventIdentifier,
		)

		if err := s.ResetInstallation(ctx, *result.InstallationId, []string{*repoFullName}); err != nil {
			slog.Debug("ResetInstallation failed, but continuing to request connection", "err", err)
		}

		if err := s.RequestConnection(ctx, sessionEventIdentifier, *repoFullName); err != nil {
			return false, "", err
		}

		return false, "", types.ErrGitHubConnectionReRequested
	default:
		return false, "", nil
	}
}

func (s *githubAccess) RequestConnection(ctx context.Context, sessionEventIdentifier, repoFullName string) error {
	connection := s.domainService.BuildConnectionRequest(sessionEventIdentifier, repoFullName)
	return s.config.app.GetGitHubConnections().UpsertGitHubConnection(ctx, connection)
}

func (s *githubAccess) ResetInstallation(ctx context.Context, installationId string, repos []string) error {
	if err := s.config.app.GetGitHubConnections().ResetGitHubConnection(ctx, installationId, repos); err != nil {
		return err
	}

	if err := s.config.app.GetForSecrets().Delete(ctx, GitHub_SecretPath, installationId); err != nil {
		return err
	}

	return nil
}

func (s *githubAccess) CompleteConnection(ctx context.Context, installationId string, repos []string) error {
	for _, repo := range repos {
		connection := s.domainService.BuildCompleteConnection(installationId, repo)

		if err := s.config.app.GetGitHubConnections().UpsertGitHubConnection(ctx, connection); err != nil {
			return err
		}

		s.config.app.GetEventBus().Publish(ctx, types.GitHubConnectedEvent{
			Connection: *connection,
		})
	}

	return nil
}
