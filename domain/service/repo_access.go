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

var ErrGitHubInstallationUnavailable = types.ErrGitHubInstallationUnavailable

type RepoAccessPolicy struct{}

func NewRepoAccessPolicy() *RepoAccessPolicy {
	return &RepoAccessPolicy{}
}

func (p *RepoAccessPolicy) ShouldAllowAccess(
	repoFullName *string,
	connection *types.GitHubConnection,
	token string,
	expired bool,
	fetchErr error,
) types.RepoAccessPolicyResult {
	if repoFullName == nil {
		return types.RepoAccessPolicyResult{
			HasAccess: true,
			Token:    "",
		}
	}

	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		return types.RepoAccessPolicyResult{
			HasAccess: false,
			Token:    "",
		}
	}

	if fetchErr != nil {
		if errors.Is(fetchErr, ErrGitHubInstallationUnavailable) {
			return types.RepoAccessPolicyResult{
				HasAccess:    false,
				Token:       "",
				NeedsReset:  true,
				NeedsReAuth: true,
			}
		}
		return types.RepoAccessPolicyResult{}
	}

	return types.RepoAccessPolicyResult{
		HasAccess: true,
		Token:    token,
	}
}

func (p *RepoAccessPolicy) ShouldRequestConnection(connection *types.GitHubConnection) bool {
	return connection == nil || !connection.Connected || connection.InstallationId == nil
}

func (p *RepoAccessPolicy) ShouldResetConnection(fetchErr error) bool {
	if fetchErr == nil {
		return false
	}
	return errors.Is(fetchErr, ErrGitHubInstallationUnavailable)
}
