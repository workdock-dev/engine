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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/workdock-dev/engine/domain/types"
)

func TestRepoAccessPolicy_ShouldAllowAccess(t *testing.T) {
	policy := NewRepoAccessPolicy()
	installId := "42"

	tests := []struct {
		name       string
		repoName   *string
		connection *types.GitHubConnection
		tokenResult TokenFetchResult
		want       RepoAccessPolicyResult
	}{
		{
			name:       "nil repo returns access",
			repoName:   nil,
			connection: nil,
			tokenResult: TokenFetchResult{},
			want: RepoAccessPolicyResult{
				HasAccess: true,
				Token:    "",
			},
		},
		{
			name:     "nil connection returns no access",
			repoName: strPtr("org/repo"),
			connection: nil,
			tokenResult: TokenFetchResult{},
			want: RepoAccessPolicyResult{
				HasAccess: false,
				Token:    "",
			},
		},
		{
			name:     "disconnected connection returns no access",
			repoName: strPtr("org/repo"),
			connection: &types.GitHubConnection{
				Connected: false,
			},
			tokenResult: TokenFetchResult{},
			want: RepoAccessPolicyResult{
				HasAccess: false,
				Token:    "",
			},
		},
		{
			name:     "connected but no installation id returns no access",
			repoName: strPtr("org/repo"),
			connection: &types.GitHubConnection{
				Connected:      true,
				InstallationId: nil,
			},
			tokenResult: TokenFetchResult{},
			want: RepoAccessPolicyResult{
				HasAccess: false,
				Token:    "",
			},
		},
		{
			name:     "valid token returns access with token",
			repoName: strPtr("org/repo"),
			connection: &types.GitHubConnection{
				Connected:      true,
				InstallationId: &installId,
			},
			tokenResult: TokenFetchResult{
				Token:  "ghs_valid_token",
				Expired: false,
			},
			want: RepoAccessPolicyResult{
				HasAccess: true,
				Token:    "ghs_valid_token",
			},
		},
		{
			name:     "installation unavailable triggers reset",
			repoName: strPtr("org/repo"),
			connection: &types.GitHubConnection{
				Connected:      true,
				InstallationId: &installId,
			},
			tokenResult: TokenFetchResult{
				Error: ErrGitHubInstallationUnavailable,
			},
			want: RepoAccessPolicyResult{
				HasAccess:    false,
				Token:       "",
				NeedsReset:  true,
				NeedsReAuth: true,
			},
		},
		{
			name:     "other token error returns empty result",
			repoName: strPtr("org/repo"),
			connection: &types.GitHubConnection{
				Connected:      true,
				InstallationId: &installId,
			},
			tokenResult: TokenFetchResult{
				Error: errors.New("some other error"),
			},
			want: RepoAccessPolicyResult{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policy.ShouldAllowAccess(tt.repoName, tt.connection, tt.tokenResult)
			assert.Equal(t, tt.want.HasAccess, got.HasAccess)
			assert.Equal(t, tt.want.Token, got.Token)
			assert.Equal(t, tt.want.NeedsReset, got.NeedsReset)
			assert.Equal(t, tt.want.NeedsReAuth, got.NeedsReAuth)
		})
	}
}

func TestRepoAccessPolicy_ShouldRequestConnection(t *testing.T) {
	policy := NewRepoAccessPolicy()

	tests := []struct {
		name       string
		connection *types.GitHubConnection
		want       bool
	}{
		{
			name:       "nil connection requests connection",
			connection: nil,
			want:       true,
		},
		{
			name: "disconnected requests connection",
			connection: &types.GitHubConnection{
				Connected: false,
			},
			want: true,
		},
		{
			name: "connected but no installation id requests connection",
			connection: &types.GitHubConnection{
				Connected:      true,
				InstallationId: nil,
			},
			want: true,
		},
		{
			name: "fully connected does not request connection",
			connection: &types.GitHubConnection{
				Connected:      true,
				InstallationId: strPtr("42"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policy.ShouldRequestConnection(tt.connection)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepoAccessPolicy_ShouldResetConnection(t *testing.T) {
	policy := NewRepoAccessPolicy()

	tests := []struct {
		name       string
		tokenResult TokenFetchResult
		want       bool
	}{
		{
			name: "no error does not reset",
			tokenResult: TokenFetchResult{
				Token: "ghs_valid",
			},
			want: false,
		},
		{
			name: "installation unavailable triggers reset",
			tokenResult: TokenFetchResult{
				Error: ErrGitHubInstallationUnavailable,
			},
			want: true,
		},
		{
			name: "other error does not reset",
			tokenResult: TokenFetchResult{
				Error: errors.New("some error"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policy.ShouldResetConnection(tt.tokenResult)
			assert.Equal(t, tt.want, got)
		})
	}
}
