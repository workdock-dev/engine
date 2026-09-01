package opencode

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/workdock-dev/engine/domain/types"
	"github.com/workdock-dev/engine/pipelines/runners"
)

const (
	BIN_PATH         = "/home/${USER}/.opencode/bin"
	WORKSPACE_PATH   = "/home/${USER}/workspace"
	CONFIG_FILE_PATH = "/home/${USER}/.config/opencode/opencode.json"
	PROMPT_FILE_PATH = "/tmp/prompt.txt"
)

var (
	//go:embed install.sh
	OPENCODE_INSTALL string
)

type HarnessHandler struct {
	version    string
	permission map[string]any
}

func NewHarnessHandler(version string, permission map[string]any) runners.HarnessHandler {
	return &HarnessHandler{
		version: version,
	}
}

func (h *HarnessHandler) GetConfigurationCommands() []string {
	return []string{
		fmt.Sprintf("%s %s", OPENCODE_INSTALL, h.version),
	}
}

func (h *HarnessHandler) GetCommands() []string {
	return nil
}

func (h *HarnessHandler) GetPromptFile(prompt string) (string, []byte) {
	return PROMPT_FILE_PATH, []byte(prompt)
}

func (h *HarnessHandler) GetConfigFile(config runners.HarnessConfig) (string, []byte, error) {
	permissions := []byte("{\"*\":\"allow\"}")
	mcps := []byte("{}")

	provider, err := json.Marshal(map[string]any{
		config.Provider.Name: map[string]any{
			"options": map[string]any{
				"apiKey": fmt.Sprintf("{env:%s}", config.Provider.AuthEnvVar),
			},
			"models": map[string]any{
				config.Provider.Model: config.Provider.ModelOptions,
			},
		},
	})

	if err != nil {
		slog.Error("failed to marshal opencode model provider", "err", err)
		return "", nil, err
	}

	if h.permission != nil {
		data, err := json.Marshal(h.permission)

		if err != nil {
			slog.Error("failed to marshal opencode config permissions", "err", err)
			return "", nil, err
		}

		permissions = data
	}

	if config.Mcps != nil {
		mcp := make(map[string]any)

		for key, value := range config.Mcps {
			mcp[key] = map[string]any{
				"type":    "remote",
				"url":     value.Url,
				"enabled": true,
				"oauth":   false,
				"headers": map[string]string{
					"Authorization": fmt.Sprintf("Bearer {env:%s}", value.AuthEnvVar),
				},
			}
		}

		data, err := json.Marshal(mcp)

		if err != nil {
			slog.Error("failed to marshal opencode custom mcps", "err", err)
			return "", nil, err
		}

		mcps = data
	}

	if config.Permissions != nil {
		data, err := json.Marshal(config.Permissions)

		if err != nil {
			slog.Error("failed to marshal opencode custom permissions", "err", err)
			return "", nil, err
		}

		permissions = data
	}

	openCodeConfig := fmt.Appendf(nil, `
{
  "$schema": "https://opencode.ai/config.json",
  "share": "disabled",
  "autoupdate": false,
  "permission": %s,
  "provider": %s,
  "mcp": %s
}
	`, string(permissions), string(provider), string(mcps))

	return CONFIG_FILE_PATH, openCodeConfig, nil
}

func (h *HarnessHandler) RunCommand() string {
	// --format           format: default (formatted) or json (raw JSON events) [string] [choices: "default", "json"] [default: "default"]
	// --thinking         show thinking blocks
	// -m, --model        model to use in the format of provider/model
	// --dir              directory to run in, path on remote server if attaching
	// -c, --continue     continue the last session
	return fmt.Sprintf(`mkdir -p %[1]s && %[2]s/opencode run --format json --thinking --dir %[1]s -c < %s`,
		WORKSPACE_PATH, BIN_PATH, PROMPT_FILE_PATH,
	)
}
func (h *HarnessHandler) Parse(
	ctx context.Context,
	part <-chan string,

	// SendThought sends the thinking state to the provider
	sendThought func(ctx context.Context, text string) error,

	// SendResponse sends text chunks/parts to the provider
	sendResponse func(ctx context.Context, text string) error,

	// SendACtion sends an action required to be executed by the user
	sendAction func(ctx context.Context, action types.AgentAction) error,

	// SendElicitation sends a collection of questions to be answer by the user
	sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,

	// SendServerInternalError sends a geneeric server internal error
	sendServerInternalError func(ctx context.Context) error,
) {

}
