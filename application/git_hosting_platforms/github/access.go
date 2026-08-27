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
	"log/slog"

	"github.com/workdock-dev/engine/application"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
)

type githubAccessConfig struct {
	ForSecrets ports.ForSecrets
	Client    ClientInterface
}

type githubAccess struct {
	app          *application.App
	config       githubAccessConfig
	tokenHandler tokenHandler
}

func newGitHubAccess(config githubAccessConfig, app *application.App) *githubAccess {
	return &githubAccess{
		app: app,
		config: config,
		tokenHandler: *newTokenHandler(tokenHandlerConfig{
			ForSecrets: config.ForSecrets,
			Client:     config.Client,
		}),
	}
}

func (s *githubAccess) verifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repoFullName *string) (bool, string, error) {
	if repoFullName == nil {
		return true, "", nil
	}

	connection, err := s.app.GetGitHubConnections().GetGitHubConnection(ctx, *repoFullName)
	if err != nil {
		return false, "", err
	}

	repoAccessPolicy := s.app.GetRepoAccessPolicyService()

	if repoAccessPolicy.ShouldRequestConnection(connection) {
		public, publicErr := s.config.Client.IsRepositoryPublic(ctx, *repoFullName)
		if publicErr != nil {
			return false, "", publicErr
		}

		slog.Debug("No GitHub connection found", "public", public, "has_access", false)
		return false, "", nil
	}

	tokenFetchResult := s.tokenHandler.getGitHubAccessToken(ctx, *connection.InstallationId)

	if tokenFetchResult.Error != nil {
		if errors.Is(tokenFetchResult.Error, types.ErrGitHubInstallationUnavailable) {
			slog.Debug(
				"GitHub installation unavailable, resetting connection",
				"installation_id", *connection.InstallationId,
				"event_identifier", sessionEventIdentifier,
			)

			s.ResetInstallation(ctx, *connection.InstallationId, []string{*repoFullName})

			if err := s.RequestConnection(ctx, sessionEventIdentifier, *repoFullName); err != nil {
				return false, "", err
			}

			return false, "", types.ErrGitHubConnectionReRequested
		}

		return false, "", tokenFetchResult.Error
	}

	policyResult := repoAccessPolicy.ShouldAllowAccess(repoFullName, connection, tokenFetchResult)

	if policyResult.NeedsReset {
		slog.Debug(
			"GitHub installation unavailable, resetting connection",
			"installation_id", *connection.InstallationId,
			"event_identifier", sessionEventIdentifier,
		)

		s.ResetInstallation(ctx, *connection.InstallationId, []string{*repoFullName})

		if err := s.RequestConnection(ctx, sessionEventIdentifier, *repoFullName); err != nil {
			return false, "", err
		}

		return false, "", types.ErrGitHubConnectionReRequested
	}

	if policyResult.HasAccess {
		slog.Debug("Verified repo access", "has_access", true)
		return true, policyResult.Token, nil
	}

	return false, "", nil
}

func (s *githubAccess) RequestConnection(ctx context.Context, sessionEventIdentifier, repoFullName string) error {
	sessionEventId := sessionEventIdentifier
	return s.app.GetGitHubConnections().UpsertGitHubConnection(
		ctx,
		&types.GitHubConnection{
			SessionEventIdentifier: &sessionEventId,
			RepoFullName:           repoFullName,
			Connected:              false,
			InstallationId:         nil,
		},
	)
}

func (s *githubAccess) ResetInstallation(ctx context.Context, installationId string, repos []string) error {
	if err := s.app.GetGitHubConnections().ResetGitHubConnection(ctx, installationId, repos); err != nil {
		return err
	}

	if err := s.config.ForSecrets.Delete(ctx, GitHub_SecretPath, installationId); err != nil {
		return err
	}

	return nil
}

func (s *githubAccess) CompleteConnection(ctx context.Context, installationId string, repos []string) error {
	for _, repo := range repos {
		connection := &types.GitHubConnection{
			SessionEventIdentifier: nil,
			RepoFullName:           repo,
			Connected:              true,
			InstallationId:         &installationId,
		}

		if err := s.app.GetGitHubConnections().UpsertGitHubConnection(ctx, connection); err != nil {
			return err
		}

		s.app.GetEventBus().Publish(ctx, types.GitHubConnectedEvent{
			Connection: *connection,
		})
	}

	return nil
}
