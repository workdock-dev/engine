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

package codex

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/codex/types"
)

const (
	CODEX_HOME       = "/home/${USER}/.codex"
	CONFIG_FILE_PATH = CODEX_HOME + "/config.toml"
	AUTH_FILE_PATH   = CODEX_HOME + "/auth.json"
	PROMPT_FILE_PATH = "/tmp/prompt.txt"
	WORKSPACE_PATH   = "/home/${USER}/workspace"
)

var (
	//go:embed config.toml
	CODEX_CONFIG string

	//go:embed scripts/install.sh
	CODEX_INSTALL string

	//go:embed scripts/run.sh
	CODEX_RUN string
)

type HarnessHandler struct {
	config types.Config
}

func NewHarnessHandler(config types.Config) agent_session_interfaces.HandlerHarness {
	return &HarnessHandler{config: config}
}

func (h *HarnessHandler) GetConfigurationCommands() []string {
	return []string{strings.ReplaceAll(CODEX_INSTALL, "VERSION_ARG", h.config.Version)}
}

func (h *HarnessHandler) GetCommands() []string {
	return []string{"chmod 700 " + CODEX_HOME, "chmod 600 " + AUTH_FILE_PATH}
}

func (h *HarnessHandler) GetPromptFile(prompt string) (string, []byte) {
	return PROMPT_FILE_PATH, []byte(prompt)
}

func (h *HarnessHandler) GetConfigFile(config *agent_session_interfaces.HarnessConfig) (string, []byte, error) {
	var data strings.Builder
	data.WriteString(CODEX_CONFIG)

	if h.config.Model != "" {
		fmt.Fprintf(&data, "model = %q\n", h.config.Model)
	}

	if h.config.ReasoningEffort != "" {
		fmt.Fprintf(&data, "model_reasoning_effort = %q\n", h.config.ReasoningEffort)
	}

	for _, mcp := range config.Mcps {
		fmt.Fprintf(&data, "\n[mcp_servers.%s]\nurl = %s\n", strconv.Quote(mcp.Name), strconv.Quote(mcp.Url))

		header := mcp.AuthHeaderKey

		if header == "" {
			header = "Authorization"
		}

		if mcp.AuthSecretEnvVar == "" {
			if mcp.AuthHeaderValue != "" {
				fmt.Fprintf(&data, "http_headers = { %s = %s }\n", strconv.Quote(header), strconv.Quote(mcp.AuthHeaderValue))
			}
		} else if header == "Authorization" {
			fmt.Fprintf(&data, "bearer_token_env_var = %s\n", strconv.Quote(mcp.AuthSecretEnvVar))
		} else {
			fmt.Fprintf(&data, "env_http_headers = { %s = %s }\n", strconv.Quote(header), strconv.Quote(mcp.AuthSecretEnvVar))
		}
	}

	data.WriteString("\n[features]\nunified_exec = false\n")

	return CONFIG_FILE_PATH, []byte(data.String()), nil
}

func (h *HarnessHandler) GetFiles(config *agent_session_interfaces.HarnessConfig) ([]map[string][]byte, error) {
	if h.config.AuthJson == "" {
		err := fmt.Errorf("[codex] auth.json is not configured")
		slog.Error("[codex] auth.json is not configured", "err", err)
		return nil, err
	}

	return []map[string][]byte{{AUTH_FILE_PATH: []byte(h.config.AuthJson)}}, nil
}

func (h *HarnessHandler) RunCommand() string {
	return strings.NewReplacer(
		"WORKSPACE_PATH_ARG", WORKSPACE_PATH,
		"CODEX_HOME_ARG", CODEX_HOME,
		"PROMPT_FILE_PATH_ARG", PROMPT_FILE_PATH,
	).Replace(CODEX_RUN)
}

func (h *HarnessHandler) Parse(
	ctx context.Context,
	harnessConfig *agent_session_interfaces.HarnessConfig,
	part <-chan []byte,
	sessionEventIdentifier string,
	sendThought func(context.Context, string) error,
	sendResponse func(context.Context, string) error,
	sendAction func(context.Context, agent_session_types.AgentAction) error,
	sendElicitation func(context.Context, agent_session_types.AgentElicitation) error,
	sendServerInternalError func(context.Context) error,
) error {
	for {
		select {
		case message, ok := <-part:
			if !ok {
				return nil
			}

			slog.Debug("[harness][codex] received output")

			var event event

			if err := json.Unmarshal(message, &event); err != nil {
				slog.Error("[codex] failed to unmarshal output", "err", err, "event_identifier", sessionEventIdentifier)
				continue
			}

			switch event.Type {
			case "item.completed":
				h.handleCompletedItem(ctx, event.Item, sendThought, sendResponse, sendAction)
			case "turn.completed":
				return nil
			case "turn.failed", "error":
				if err := sendServerInternalError(ctx); err != nil {
					return err
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (h *HarnessHandler) handleCompletedItem(
	ctx context.Context,
	item item,
	sendThought func(context.Context, string) error,
	sendResponse func(context.Context, string) error,
	sendAction func(context.Context, agent_session_types.AgentAction) error,
) {
	switch item.Type {
	case "reasoning":
		if item.Text != "" {
			_ = sendThought(ctx, item.Text)
		}
	case "agent_message":
		if item.Text != "" {
			_ = sendResponse(ctx, item.Text)
		}
	case "command_execution":
		_ = sendAction(ctx, agent_session_types.AgentAction{
			Name:   "command_execution",
			Input:  item.Command,
			Output: item.AggregatedOutput,
		})
	}
}

type event struct {
	Type string `json:"type"`
	Item item   `json:"item"`
}

type item struct {
	Type             string `json:"type"`
	Text             string `json:"text"`
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
}
