package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/shared"
)

type HarnessConfig struct {
	Provider struct {
		Name         string
		Model        string
		ModelOptions string
		AuthEnvVar   string
	}
	Mcps map[string]struct {
		Url        string
		AuthEnvVar string
	}
	Permissions map[string]any
}

type HarnessHandler interface {
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
