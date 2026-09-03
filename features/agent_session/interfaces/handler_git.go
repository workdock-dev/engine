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

	"github.com/workdock-dev/engine/shared"
)

type GitAccess struct {
	EnvVarName string
	Secret     string
	Hosts      []string
	Granted    bool
}

// GitHandler is the interface to interact with the git hosting provider
// for the intial setup of the sandbox
type GitHandler interface {
	// GetInstallationUrl returns the installation URL where user can
	// grant access
	GetInstallationUrl() string

	// GetConfigurationCommands may return a list of commands the git handler
	// provider requires to be install in the sandbox
	GetConfigurationCommands() []string

	// GetCommands may return a list of commands the git handler provider
	// requires to be run on every sandbox execution
	GetCommands() []string

	// GetLatestChangesComand returns the command to verify if a pull request or commit with push
	// was created
	GetLatestChangesComand() string

	// GetGitAccess returns the git access configuration for the given provider
	GetGitAccess(ctx context.Context, connection *shared.GitHubConnection) (*GitAccess, error)

	// ParseLatestChangesResult receives the changes procude by the latest changes command
	// and parse it to a concrete domain type
	ParseLatestChangesResult(changes string) *shared.PullRequest
}
