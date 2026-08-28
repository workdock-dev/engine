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
	"errors"

	"github.com/workdock-dev/engine/domain/types"
)

// ErrGitHubInstallationUnavailable is returned when a GitHub App installation
// is no longer valid (e.g., uninstalled or suspended). When this error occurs,
// the connection must be reset and re-authorization requested.
var ErrGitHubInstallationUnavailable = types.ErrGitHubInstallationUnavailable

// RepoAccessPolicy encapsulates the repository access rules for GitHub App installations.
// It is parameterized by ports for connection storage and token fetching,
// keeping the domain logic pure and side-effect-free.
type RepoAccessPolicy struct{}

// NewRepoAccessPolicy creates a new RepoAccessPolicy instance.
func NewRepoAccessPolicy() *RepoAccessPolicy {
	return &RepoAccessPolicy{}
}

// ShouldAllowAccess determines whether a repository access should be allowed based on
// the repository name, existing connection state, and token fetch result.
// Returns a RepoAccessPolicyResult indicating whether access is granted,
// the token to use (if granted), and whether a reset/re-auth is needed.
func (p *RepoAccessPolicy) ShouldAllowAccess(
	repoFullName *string,
	connection *types.GitHubConnection,
	token string,
	expired bool,
	fetchErr error,
) types.RepoAccessPolicyResult {
	// A nil repository name means no specific repository is being accessed,
	// so access is always allowed (e.g., listing repositories).
	if repoFullName == nil {
		return types.RepoAccessPolicyResult{
			HasAccess: true,
			Token:    "",
		}
	}

	// If there is no connection, or it's disconnected, or has no installation ID,
	// we cannot allow access for write operations.
	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		return types.RepoAccessPolicyResult{
			HasAccess: false,
			Token:    "",
		}
	}

	// If token fetch returned an error, check if it's an installation error.
	// If the installation is unavailable, we need to reset and re-auth.
	if fetchErr != nil {
		if errors.Is(fetchErr, ErrGitHubInstallationUnavailable) {
			return types.RepoAccessPolicyResult{
				HasAccess:    false,
				Token:       "",
				NeedsReset:  true,
				NeedsReAuth: true,
			}
		}
		// Some other error occurred; we cannot allow access.
		return types.RepoAccessPolicyResult{}
	}

	// All checks passed; access is allowed with the provided token.
	return types.RepoAccessPolicyResult{
		HasAccess: true,
		Token:    token,
	}
}

// ShouldRequestConnection returns true if a new GitHub App connection should be
// requested for the given connection state.
func (p *RepoAccessPolicy) ShouldRequestConnection(connection *types.GitHubConnection) bool {
	return connection == nil || !connection.Connected || connection.InstallationId == nil
}

// ShouldResetConnection returns true if the existing connection should be reset
// based on the error returned during token fetch.
func (p *RepoAccessPolicy) ShouldResetConnection(fetchErr error) bool {
	if fetchErr == nil {
		return false
	}
	return errors.Is(fetchErr, ErrGitHubInstallationUnavailable)
}
