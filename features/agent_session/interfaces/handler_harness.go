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

type Provider struct {
	Name         string
	Model        string
	ModelOptions string
	AuthEnvVar   string
}

type HarnessConfig struct {
	Provider    *Provider
	Mcps        []MCPConfig
	Permissions map[string]any
}

type HandlerHarness interface {
	// GetConfigurationCommands may return a list of commands the harness handler
	// provider requires to be install in the sandbox
	GetConfigurationCommands() []string

	// GetCommands may return a list of commands the harness handler provider
	// requires to be run on every sandbox execution
	GetCommands() []string

	// GetPromptFile pass in the user's prompt
	// return file path+data
	//
	// Upload the prompt, we do it this way because of https://github.com/anomalyco/opencode/issues/38723
	// opencode run hangs waiting for an input (stdin) when running for the first time, but this never
	// happens. Thus, opencode run stays stuck. The work arround is to pipe stdin. This can happen in other
	// harnesses; thus, we standarized this form.
	GetPromptFile(prompt string) (string, []byte)

	// GetConfigFile pass in custom configuration
	// return file path+data
	GetConfigFile(config HarnessConfig) (string, []byte, error)

	// RunCommand returns the harness command for execution
	RunCommand() string

	// Parse parses the harness part output, which is expected to be JSON.
	// It dispatches to the appropriate parser based on the LLM part type and
	// invokes the corresponding callback to update the agent handler.
	Parse(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,

		// sendThought sends the thinking state to the provider
		sendThought func(ctx context.Context, text string) error,

		// sendResponse sends text chunks/parts to the provider
		sendResponse func(ctx context.Context, text string) error,

		// sendACtion sends an action required to be executed by the user
		sendAction func(ctx context.Context, action shared.AgentAction) error,

		// sendElicitation sends a collection of questions to be answer by the user
		sendElicitation func(ctx context.Context, elicitation shared.AgentElicitation) error,

		// sendServerInternalError sends a geneeric server internal error
		sendServerInternalError func(ctx context.Context) error,
	)
}
