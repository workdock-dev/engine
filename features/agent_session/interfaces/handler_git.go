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

package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/features/agent_session/types"
)

type GitAccess struct {
	Secret  string
	Granted bool
}

type CreatePullRequestInput struct {
	RepoFullName string
	AccessToken  string
	Title        string
	Body         string
	Head         string
	Base         string
	Draft        bool
}

// HandlerGit is the interface to interact with the git hosting provider
// for the intial setup of the sandbox
type HandlerGit interface {
	// GetInstallationUrl returns the installation URL where user can
	// grant access
	GetInstallationUrl() string

	// GetGitAccess returns the git access configuration for the given provider
	GetGitAccess(ctx context.Context, connection *types.GitConnection) (*GitAccess, error)

	// CreatePullRequest creates a pull request against the given git hosting provider
	CreatePullRequest(ctx context.Context, input CreatePullRequestInput) (*types.PullRequest, error)
}
