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

package opencode

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/plug-ings/opencode/types"
	"github.com/workdock-dev/engine/shared"
)

const (
	BIN_PATH         = "/home/${USER}/.opencode/bin"
	WORKSPACE_PATH   = "/home/${USER}/workspace"
	CONFIG_FILE_PATH = "/home/${USER}/.config/opencode/opencode.json"
	PROMPT_FILE_PATH = "/tmp/prompt.txt"
)

var (
	//go:embed scripts/install.sh
	OPENCODE_INSTALL string
)

type HarnessHandler struct {
	version    string
	permission map[string]any
}

func NewHarnessHandler(config types.Config) agent_session_interfaces.HarnessHandler {
	return &HarnessHandler{
		version:    config.Version,
		permission: config.Permission,
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

func (h *HarnessHandler) GetConfigFile(config agent_session_interfaces.HarnessConfig) (string, []byte, error) {
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
	part <-chan []byte,
	sessionEventIdentifier string,

	// SendThought sends the thinking state to the provider
	sendThought func(ctx context.Context, text string) error,

	// SendResponse sends text chunks/parts to the provider
	sendResponse func(ctx context.Context, text string) error,

	// SendACtion sends an action required to be executed by the user
	sendAction func(ctx context.Context, action shared.AgentAction) error,

	// SendElicitation sends a collection of questions to be answer by the user
	sendElicitation func(ctx context.Context, elicitation shared.AgentElicitation) error,

	// SendServerInternalError sends a geneeric server internal error
	sendServerInternalError func(ctx context.Context) error,
) {

	for {
		select {
		case message, ok := <-part:
			if !ok {
				return
			}

			var event types.WireEvent

			if err := json.Unmarshal(message, &event); err != nil {
				// TODO: Report error to span
				slog.Error("failed to unmarshal opencode output", "err", err, "message", message, "event_identifier", sessionEventIdentifier)
			} else {
				partType := event.Type

				if partType == "tool_use" {
					partType = "tool"
				}

				slog.Debug("OpenCode received message", "type", partType, "event_identifier", sessionEventIdentifier)

				switch partType {
				case "retry":
					fallthrough
				case "step_start":
					fallthrough
				case "file":
					fallthrough
				case "subtask":
					fallthrough
				case "snapshot":
					fallthrough
				case "patch":
					fallthrough
				case "agent":
					fallthrough
				case "compaction":
					sendThought(ctx, "compacting")
				case "reasoning":
					var p types.ReasoningPart

					if err := json.Unmarshal(event.Part, &p); err != nil {
						// TODO: Report error to span
						slog.Error("unmarshal reasoning", "event_identifier", sessionEventIdentifier, "error", err)
						return
					}

					sendThought(ctx, p.Text)
				case "text":
					var p types.TextPart

					if err := json.Unmarshal(event.Part, &p); err != nil {
						// TODO: Report error to span
						slog.Error("unmarshal text", "event_identifier", sessionEventIdentifier, "error", err)
						return
					}

					sendResponse(ctx, p.Text)
				case "tool":
					var p types.ToolPart

					if err := json.Unmarshal(event.Part, &p); err != nil {
						// TODO: Report error to span
						slog.Error("unmarshal tool", "event_identifier", sessionEventIdentifier, "error", err)
						return
					}

					if p.Tool == "question" {
						questions := h.parseQuestions(p.State.Input)

						for _, q := range questions {
							options := make([]shared.AgentOption, 0, len(q.Options))

							for _, opt := range q.Options {
								options = append(options, shared.AgentOption{
									Label:       opt.Label,
									Description: opt.Description,
								})
							}

							sendElicitation(ctx, shared.AgentElicitation{
								Question: q.Question,
								Multiple: q.Multiple,
								Options:  options,
							})
						}
					} else {
						input, output := h.parseToolPart(p)
						sendAction(ctx, shared.AgentAction{
							Name:   p.Tool,
							Input:  input,
							Output: output,
						})
					}

				case "step_finish":
					var p types.StepFinishPart

					if err := json.Unmarshal(event.Part, &p); err != nil {
						// TODO: Report error to span
						slog.Error("unmarshal step-finish", "event_identifier", sessionEventIdentifier, "error", err)
						return
					}

					slog.Debug("OpenCoded finished",
						"event_identifier", sessionEventIdentifier,
						"reason", p.Reason,
						"reasoning", p.Tokens.Reasoning,
						"tokens_total", p.Tokens.Total,
						"tokens_input", p.Tokens.Input,
						"tokens_output", p.Tokens.Output,
						"cache_read", p.Tokens.Cache.Read,
						"cache_write", p.Tokens.Cache.Write,
					)

					sendResponse(ctx, "")
				default:
					slog.Warn("opencode received unexpected part type",
						"event_identifier", sessionEventIdentifier,
						"part_type", partType,
					)

					// TODO: Type to pase error message
					sendResponse(ctx, fmt.Sprintf("An unexpected format has been received by the harness:\n\n%s", message))
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

func (h *HarnessHandler) parseToolPart(p types.ToolPart) (string, string) {
	var input string
	var output string

	switch p.Tool {
	case "bash":
		input, _ = p.State.Input["command"].(string)
		output = p.State.Output

	case "glob":
		pattern, _ := p.State.Input["pattern"].(string)
		path, _ := p.State.Input["path"].(string)

		if path != "" {
			input = pattern + " in " + path
		} else {
			input = pattern
		}

		output = p.State.Output
	case "read":
		input, _ = p.State.Input["filePath"].(string)
		output = p.State.Output
	case "grep":
		pattern, _ := p.State.Input["pattern"].(string)
		path, _ := p.State.Input["path"].(string)

		if path != "" {
			input = pattern + " in " + path
		} else {
			input = pattern
		}

		output = p.State.Output
	case "webfetch":
		input, _ = p.State.Input["url"].(string)
		output = p.State.Output
	case "websearch":
		input, _ = p.State.Input["query"].(string)
		output = p.State.Output
	case "write":
		input, _ = p.State.Input["filePath"].(string)
		output = p.State.Output
	case "edit":
		input, _ = p.State.Input["filePath"].(string)
		output = p.State.Output
	case "task":
		input, _ = p.State.Input["description"].(string)
		output = p.State.Output
	case "execute":
		input, _ = p.State.Input["command"].(string)
		output = p.State.Output
	case "apply_patch":
		if files, ok := p.State.Input["files"].([]any); ok {
			paths := make([]string, 0, len(files))

			for _, f := range files {
				if m, ok := f.(map[string]any); ok {
					if fp, ok := m["filePath"].(string); ok {
						paths = append(paths, fp)
					}
				}
			}

			input = strings.Join(paths, ", ")
		}

		output = p.State.Output
	case "todowrite":
		if todos, ok := p.State.Input["todos"].([]any); ok {
			items := make([]string, 0, len(todos))

			for _, t := range todos {
				if m, ok := t.(map[string]any); ok {
					if content, ok := m["content"].(string); ok {
						items = append(items, content)
					}
				}
			}

			input = strings.Join(items, ", ")
		}

		output = p.State.Output
	case "question":
		// Expected to be handle outside this function
		return "", ""
	case "skill":
		input, _ = p.State.Input["name"].(string)
		output = p.State.Output
	default:
		input = fmt.Sprintf("%v", p.State.Input)
		output = p.State.Output
	}

	return input, output
}

func (h *HarnessHandler) parseQuestions(input map[string]any) []types.QuestionInfo {
	questionsRaw, ok := input["questions"].([]any)

	if !ok {
		return nil
	}

	var questions []types.QuestionInfo

	for _, q := range questionsRaw {
		m, ok := q.(map[string]any)

		if !ok {
			continue
		}

		info := types.QuestionInfo{}
		info.Question, _ = m["question"].(string)
		info.Header, _ = m["header"].(string)

		if mult, ok := m["multiple"].(bool); ok {
			info.Multiple = mult
		}

		if opts, ok := m["options"].([]any); ok {
			for _, o := range opts {
				om, ok := o.(map[string]any)
				if !ok {
					continue
				}
				opt := types.QuestionOption{}
				opt.Label, _ = om["label"].(string)
				opt.Description, _ = om["description"].(string)
				info.Options = append(info.Options, opt)
			}
		}

		questions = append(questions, info)
	}

	return questions
}
