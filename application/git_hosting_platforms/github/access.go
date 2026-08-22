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

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/types"
)

// GitRepoAccessService owns the git repository access lifecycle: verifying
// access to a repository and managing its connection and installation state.
// GitHub is currently the only repository provider it integrates with.
type githubAccessConfig struct {
	ForSecrets        ports.ForSecrets
	ForEvent          ports.ForEventBus
	GitHubConnections repositories.GitHubConnectionRepository
	Client            ClientInterface
}

type githubAccess struct {
	config       githubAccessConfig
	tokenHandler tokenHandler
}

func newGitHubAccess(config githubAccessConfig) *githubAccess {
	return &githubAccess{
		config: config,
		tokenHandler: *newTokenHandler(tokenHandlerConfig{
			ForSecrets: config.ForSecrets,
			Client:     config.Client,
		}),
	}
}

// verifyRepoAccess reports whether the AI agent can access a repository and
// returns the access token when access is granted through a connected
// installation. Sessions without a repository are always accessible (no token
// needed).
//
// For both public and private repositories, a GitHub App installation connection
// is required to obtain an access token with write permissions. Public repos
// are readable without authentication, but pushing branches, creating PRs, and
// other write operations require a token — so the engine always requests a
// connection regardless of visibility.
//
// When the installation is no longer available, it resets the installation and
// requests a fresh connection, returning ErrGitHubConnectionReRequested to
// signal that the user should be prompted to re-authorize.
func (s *githubAccess) verifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repoFullName *string) (bool, string, error) {
	if repoFullName == nil {
		return true, "", nil
	}

	connection, err := s.config.GitHubConnections.GetGitHubConnection(ctx, *repoFullName)

	if err != nil {
		return false, "", err
	}

	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		public, publicErr := s.config.Client.IsRepositoryPublic(ctx, *repoFullName)

		if publicErr != nil {
			return false, "", publicErr
		}

		slog.Debug("No GitHub connection found", "public", public, "has_access", false)
		return false, "", nil
	}

	token, err := s.tokenHandler.getGitHubAccessToken(ctx, *connection.InstallationId)

	if err != nil {
		if errors.Is(err, types.ErrGitHubInstallationUnavailable) {
			slog.Debug(
				"GitHub installation unavailable, resetting connection",
				"installation_id", *connection.InstallationId,
				"event_identifier", sessionEventIdentifier,
			)

			// TODO: Safe to ignore returned error?
			s.ResetInstallation(ctx, *connection.InstallationId, []string{*repoFullName})

			if err := s.RequestConnection(ctx, sessionEventIdentifier, *repoFullName); err != nil {
				return false, "", err
			}

			return false, "", types.ErrGitHubConnectionReRequested
		}

		return false, "", err
	}

	slog.Debug("Verified repo access", "has_access", true)
	return true, token, nil
}

// RequestConnection persists a pending GitHub connection for a repository so
// that the user is prompted to link it and future sessions can pick it up.
func (s *githubAccess) RequestConnection(ctx context.Context, sessionEventIdentifier, repoFullName string) error {
	sessionEventId := sessionEventIdentifier
	return s.config.GitHubConnections.UpsertGitHubConnection(
		ctx,
		&types.GitHubConnection{
			SessionEventIdentifier: &sessionEventId,
			RepoFullName:           repoFullName,
			Connected:              false,
			InstallationId:         nil,
		},
	)
}

// ResetInstallation cleans up a GitHub installation that is no longer
// available: it disconnects the specified repositories linked to it and deletes its
// stored credentials, so future sessions request a fresh GitHub connection.
func (s *githubAccess) ResetInstallation(ctx context.Context, installationId string, repos []string) error {
	if err := s.config.GitHubConnections.ResetGitHubConnection(ctx, installationId, repos); err != nil {
		return err
	}

	if err := s.config.ForSecrets.Delete(ctx, GitHub_SecretPath, installationId); err != nil {
		return err
	}

	return nil
}

// CompleteConnection links each repository to the given installation once a
// GitHub installation has been created and its token stored.
//
// When a user authorizes the GitHub app for multiple repositories at once,
// additional repos that weren't part of the original connection request still
// need a valid session_event_identifier. We resolve this by copying the
// session_event_identifier from an existing connection belonging to the same
// authorization flow (i.e., another repo in the same batch that already has a
// connection record).
func (s *githubAccess) CompleteConnection(ctx context.Context, installationId string, repos []string) error {
	for _, repo := range repos {
		connection := &types.GitHubConnection{
			SessionEventIdentifier: nil,
			RepoFullName:           repo,
			Connected:              true,
			InstallationId:         &installationId,
		}

		if err := s.config.GitHubConnections.UpsertGitHubConnection(ctx, connection); err != nil {
			return err
		}

		s.config.ForEvent.Publish(ctx, types.GitHubConnectedEvent{
			Connection: *connection,
		})
	}

	return nil
}
