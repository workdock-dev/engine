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

package pidev

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_metrics "github.com/workdock-dev/engine/features/agent_session/metrics"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/pidev/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	METRICS_NAME       = "pidev"
	WORKSPACE_PATH     = "/home/${USER}/workspace"
	CONFIG_FILE_PATH   = "/home/${USER}/.pi/agent/settings.json"
	MODELS_FILE_PATH   = "/home/${USER}/.pi/agent/models.json"
	MCP_FILE_PATH      = "/home/${USER}/.pi/agent/mcp.json"
	PROMPT_FILE_PATH   = "/tmp/prompt.txt"
	DEFAULT_API        = "openai-completions"
	DEFAULT_MODEL_NAME = "ollama"
	DEFAULT_BASE_URL   = "https://ollama.com/v1"
	MCP_ADAPTER_SOURCE = "npm:pi-mcp-adapter"
)

// NPM_COMMAND suppresses notice-level install output (package counts,
// funding and audit notices) that npm prints on every package operation.
var NPM_COMMAND = []string{"npm", "--no-fund", "--no-audit", "--loglevel=silent"}

// defaultTools are the pi built-in tools enabled when the config does not
// provide any. bash is included so the agent can clone the repository and
// drive git and the gh cli.
var defaultTools = []string{"read", "bash", "edit", "grep", "find", "ls", "write"}

var (
	//go:embed scripts/install.sh
	PI_INSTALL string
)

type HarnessHandler struct {
	config types.Config
	tracer trace.Tracer
}

func NewHarnessHandler(config types.Config) agent_session_interfaces.HandlerHarness {
	return &HarnessHandler{
		config: config,
		tracer: otel.Tracer("workdock.pidev.gen_ai"),
	}
}

func (h *HarnessHandler) GetConfigurationCommands() []string {
	return []string{
		strings.ReplaceAll(PI_INSTALL, "VERSION_ARG", h.config.Version),
	}
}

func (h *HarnessHandler) GetCommands() []string {
	return nil
}

func (h *HarnessHandler) GetPromptFile(prompt string) (string, []byte) {
	return PROMPT_FILE_PATH, []byte(prompt)
}

// GetConfigFile returns the global settings file for
// ~/.pi/agent/settings.json. Built-in providers resolve their API keys from
// the sandbox environment. defaultTools selects the built-in tools while
// extension tools such as the MCP adapter remain enabled.
// https://pi.dev/docs/latest/settings
func (h *HarnessHandler) GetConfigFile(config *agent_session_interfaces.HarnessConfig) (string, []byte, error) {
	tools := h.config.Tools

	if len(tools) == 0 {
		tools = defaultTools
	}

	settings := map[string]any{
		"defaultProjectTrust": "always",
		"defaultTools":        tools,
	}

	if h.config.ThinkingLevel != "" {
		settings["defaultThinkingLevel"] = h.config.ThinkingLevel
	}

	if h.config.McpAdapterVersion != "" {
		settings["packages"] = []string{
			fmt.Sprintf("%s@%s", MCP_ADAPTER_SOURCE, h.config.McpAdapterVersion),
		}

		// npm runs with stdout and stderr routed to pi's stderr in JSON mode;
		// silence notice-level package output while keeping real errors
		settings["npmCommand"] = NPM_COMMAND
	}

	data, err := json.Marshal(settings)

	if err != nil {
		slog.Error("[harness][pidev] failed to marshal settings", "err", err)
		return "", nil, err
	}

	return CONFIG_FILE_PATH, data, nil
}

// GetFiles returns additional custom files: the custom provider declaration
// for ~/.pi/agent/models.json and the engine-configured MCP servers for
// ~/.pi/agent/mcp.json consumed by pi-mcp-adapter.
// https://pi.dev/docs/latest/models and https://pi.dev/packages/pi-mcp-adapter
func (h *HarnessHandler) GetFiles(config *agent_session_interfaces.HarnessConfig) ([]map[string][]byte, error) {
	files := make([]map[string][]byte, 0)

	// *-------------------------------------------------------------------------*
	// * config through params overrides handler default config                  *
	// *-------------------------------------------------------------------------*

	if config.Provider != nil {
		data, err := h.modelsJson(&types.ProviderConfig{
			Name:   config.Provider.Name,
			Api:    DEFAULT_API,
			ApiKey: fmt.Sprintf("${%s}", config.Provider.AuthEnvVar),
			Models: []types.ModelConfig{{Id: config.Provider.Model}},
		})

		if err != nil {
			return nil, err
		}

		files = append(files, map[string][]byte{MODELS_FILE_PATH: data})
	} else if h.config.Provider != nil {
		data, err := h.modelsJson(h.config.Provider)

		if err != nil {
			return nil, err
		}

		files = append(files, map[string][]byte{MODELS_FILE_PATH: data})
	}

	if len(config.Mcps) > 0 {
		data, err := h.mcpJson(config.Mcps)

		if err != nil {
			return nil, err
		}

		files = append(files, map[string][]byte{MCP_FILE_PATH: data})
	}

	if len(files) == 0 {
		return nil, nil
	}

	return files, nil
}

func (h *HarnessHandler) mcpJson(mcps []agent_session_interfaces.MCPConfig) ([]byte, error) {
	servers := make(map[string]any, len(mcps))

	for _, mcp := range mcps {
		servers[mcp.Name] = map[string]any{
			"url": mcp.Url,
			"headers": map[string]string{
				"Authorization": fmt.Sprintf("Bearer ${%s}", mcp.AuthKey),
			},
			"lifecycle": "lazy",
		}
	}

	data, err := json.Marshal(map[string]any{
		"mcpServers": servers,
	})

	if err != nil {
		slog.Error("[harness][pidev] failed to marshal mcp config", "err", err)
		return nil, err
	}

	return data, nil
}

func (h *HarnessHandler) modelsJson(provider *types.ProviderConfig) ([]byte, error) {
	name := provider.Name

	if name == "" {
		name = DEFAULT_MODEL_NAME
	}

	baseUrl := provider.BaseUrl

	if baseUrl == "" {
		baseUrl = DEFAULT_BASE_URL
	}

	api := provider.Api

	if api == "" {
		api = DEFAULT_API
	}

	models := make([]map[string]any, 0, len(provider.Models))

	for _, model := range provider.Models {
		entry := map[string]any{"id": model.Id}

		if model.Name != "" {
			entry["name"] = model.Name
		}

		if model.Reasoning {
			entry["reasoning"] = true
		}

		if model.ContextWindow > 0 {
			entry["contextWindow"] = model.ContextWindow
		}

		if model.MaxTokens > 0 {
			entry["maxTokens"] = model.MaxTokens
		}

		models = append(models, entry)
	}

	data, err := json.Marshal(map[string]any{
		"providers": map[string]any{
			name: map[string]any{
				"baseUrl": baseUrl,
				"api":     api,
				"apiKey":  provider.ApiKey,
				"models":  models,
			},
		},
	})

	if err != nil {
		slog.Error("[harness][pidev] failed to marshal models config", "err", err)
		return nil, err
	}

	return data, nil
}

func (h *HarnessHandler) RunCommand() string {
	flags := "--mode json"

	if h.config.Provider != nil {
		model := ""

		if len(h.config.Provider.Models) > 0 {
			model = h.config.Provider.Models[0].Id
		}

		flags += fmt.Sprintf(" --provider %s --model %s", h.config.Provider.Name, model)
	}

	if h.config.ThinkingLevel != "" {
		flags = fmt.Sprintf("%s --thinking %s", flags, h.config.ThinkingLevel)
	}

	// --mode json    output all events as JSON lines
	// --provider     provider, such as ollama or openrouter
	// --model        model pattern or ID
	// --thinking     thinking level
	// -c             continue the most recent session
	//
	// Built-in tools are selected through the defaultTools settings key
	// instead of --tools, which is a strict allowlist over all tools and
	// would disable extension tools such as the MCP adapter.
	return fmt.Sprintf(`mkdir -p %[1]s && cd %[1]s && PI_SKIP_VERSION_CHECK=1 pi %[2]s -c < %[3]s`,
		WORKSPACE_PATH, flags, PROMPT_FILE_PATH,
	)
}

func (h *HarnessHandler) Parse(
	ctx context.Context,
	harnessConfig *agent_session_interfaces.HarnessConfig,
	part <-chan []byte,
	sessionEventIdentifier string,

	// SendThought sends the thinking state to the provider
	sendThought func(ctx context.Context, text string) error,

	// SendResponse sends text chunks/parts to the provider
	sendResponse func(ctx context.Context, text string) error,

	// SendACtion sends an action required to be executed by the user
	sendAction func(ctx context.Context, action agent_session_types.AgentAction) error,

	// SendElicitation sends a collection of questions to be answer by the user
	sendElicitation func(ctx context.Context, elicitation agent_session_types.AgentElicitation) error,

	// SendServerInternalError sends a generic server internal error
	sendServerInternalError func(ctx context.Context) error,
) error {
	startedAt := time.Now()
	m, err := agent_session_metrics.NewHarnessMetrics(METRICS_NAME, otel.Meter("pidev"))

	if err != nil {
		return err
	}

	var modelName string
	var providerName string

	if harnessConfig != nil && harnessConfig.Provider != nil {
		modelName = harnessConfig.Provider.Model
		providerName = harnessConfig.Provider.Name
	}

	m.ModelUsage.Add(ctx, 1, metric.WithAttributes(
		attribute.String("gen_ai.request.model", modelName),
		attribute.String("gen_ai.provider.name", providerName),
	))
	m.SessionCount.Add(ctx, 1)

	ctx, span := h.tracer.Start(ctx, "operation.chat")
	defer func() {
		duration := time.Since(startedAt)
		m.SessionDuration.Record(ctx, float64(duration.Milliseconds()))
		m.SessionTokenTotal.Record(ctx,
			m.SessionInputTokens+
				m.SessionOutputTokens+
				m.SessionReasoningTokens,
		)
		m.SessionCostTotal.Record(ctx, m.SessionCost)
		span.End()
	}()

	// Text and thinking deltas stream token by token; they are accumulated
	// per content index and flushed to the provider when the block ends.
	deltas := make(map[int]string)

	toolExecutions := make(map[string]types.ToolExecutionState)

	for {
		select {
		case message, ok := <-part:
			if !ok {
				return nil
			}

			slog.Debug("[harness][pidev] parsed output")
			var event types.WireEvent

			if err := json.Unmarshal(message, &event); err != nil {
				// TODO: Report error to span
				slog.Error("[harness][pidev] failed to unmarshal output", "err", err, "message", message, "event_identifier", sessionEventIdentifier)
			} else {
				switch event.Type {
				case "session":
					slog.Debug("[harness][pidev] session started", "event_identifier", sessionEventIdentifier)

				case "agent_start", "turn_start":
					// A new agent run or assistant turn begins; clear any
					// stale thinking state from the previous turn
					sendThought(ctx, "")

				case "compaction_start", "compaction_end", "queue_update", "agent_settled",
					"bash_execution_update", "session_info_changed", "thinking_level_changed",
					"entry_appended", "auto_retry_end", "summarization_retry_scheduled",
					"summarization_retry_attempt_start", "summarization_retry_finished",
					"message_start", "turn_end", "agent_end", "text_start", "thinking_start", "tool_execution_update":
					// Session bookkeeping events without provider output; the
					// final authoritative message arrives on message_end.
					// text_start/thinking_start only occur as
					// assistantMessageEvent sub-types of message_update; they
					// are listed here defensively for direct wire events.

				case "message_update":
					if event.AssistantMessageEvent == nil {
						slog.Warn("[harness][pidev] received message_update without assistant message event",
							"event_identifier", sessionEventIdentifier,
						)
						break
					}

					h.parseAssistantMessageEvent(ctx, event.AssistantMessageEvent, deltas, sendThought)

				case "message_end":
					// pi emits message lifecycle events for user, assistant,
					// and toolResult messages; only assistant messages carry
					// the authoritative response
					if event.Message == nil || event.Message.Role != "assistant" {
						break
					}

					h.recordAssistantUsage(ctx, m, modelName, providerName, event.Message)
					m.MessageCount.Add(ctx, 1)
					h.sendAssistantText(ctx, event.Message, sendResponse)

					if event.Message.StopReason == "error" || event.Message.StopReason == "aborted" {
						slog.Error("[harness][pidev] assistant message failed",
							"event_identifier", sessionEventIdentifier,
							"stop_reason", event.Message.StopReason,
							"error_message", event.Message.ErrorMessage,
						)
						sendServerInternalError(ctx)
					}

				case "tool_execution_start":
					var args map[string]any

					if raw, ok := event.Args.(map[string]any); ok {
						args = raw
					}

					toolExecutions[event.ToolCallId] = types.ToolExecutionState{
						ToolName:  event.ToolName,
						Args:      args,
						StartedAt: time.Now().UnixMilli(),
					}

				case "tool_execution_end":
					h.parseToolExecutionEnd(ctx, m, event, toolExecutions, sendAction)

				case "auto_retry_start":
					m.RetryCount.Add(ctx, 1, metric.WithAttributes(
						attribute.String("gen_ai.provider.name", providerName),
					))

				default:
					slog.Warn("[harness][pidev] received unexpected event type",
						"event_identifier", sessionEventIdentifier,
						"event_type", event.Type,
					)

					sendResponse(ctx, fmt.Sprintf("An unexpected format has been received by the harness:\n\n%s", message))
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (h *HarnessHandler) parseAssistantMessageEvent(
	ctx context.Context,
	assistantEvent *types.AssistantMessageEvent,
	deltas map[int]string,
	sendThought func(ctx context.Context, text string) error,
) {
	switch assistantEvent.Type {
	case "start":
		sendThought(ctx, "")

	case "thinking_start":
		// Block boundary; content arrives through thinking_delta

	case "thinking_delta":
		deltas[assistantEvent.ContentIndex] += assistantEvent.Delta

	case "thinking_end":
		text := assistantEvent.Content

		if text == "" {
			text = deltas[assistantEvent.ContentIndex]
		}

		delete(deltas, assistantEvent.ContentIndex)
		sendThought(ctx, text)

	case "text_start":
		// Block boundary; content arrives through text_delta

	case "text_delta":
		deltas[assistantEvent.ContentIndex] += assistantEvent.Delta

	case "text_end":
		delete(deltas, assistantEvent.ContentIndex)

	case "toolcall_start":
		sendThought(ctx, "")

	case "toolcall_delta":
		// Tool call arguments stream incrementally; the parsed call arrives on toolcall_end

	case "toolcall_end":
		// The tool execution events carry the call details

	case "error":
		if assistantEvent.Error != nil {
			slog.Error("[harness][pidev] assistant stream error",
				"reason", assistantEvent.Reason,
				"error_message", assistantEvent.Error.ErrorMessage,
			)
		}

	default:
		slog.Warn("[harness][pidev] received unexpected assistant message event",
			"assistant_message_event_type", assistantEvent.Type,
		)
	}
}

func (h *HarnessHandler) sendAssistantText(ctx context.Context, message *types.AssistantMessage, sendResponse func(ctx context.Context, text string) error) {
	texts := make([]string, 0, len(message.Content))

	for _, content := range message.Content {
		if content.Type == "text" {
			texts = append(texts, content.Text)
		}
	}

	if len(texts) > 0 {
		sendResponse(ctx, strings.Join(texts, "\n"))
	}

	sendResponse(ctx, "")
}

func (h *HarnessHandler) recordAssistantUsage(
	ctx context.Context,
	m *agent_session_metrics.HarnessMetrics,
	modelName string,
	providerName string,
	message *types.AssistantMessage,
) {
	h.tokenUsageAdd(ctx, m, modelName, providerName, "input", message.Usage.Input)
	h.tokenUsageAdd(ctx, m, modelName, providerName, "output", message.Usage.Output)
	h.tokenUsageAdd(ctx, m, modelName, providerName, "reasoning", message.Usage.Reasoning)
	h.tokenUsageAdd(ctx, m, modelName, providerName, "cacheRead", message.Usage.CacheRead)
	h.tokenUsageAdd(ctx, m, modelName, providerName, "cacheWrite", message.Usage.CacheWrite)

	if message.Usage.CacheRead > 0 {
		m.CacheCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("type", "cacheRead"),
		))
	}

	if message.Usage.CacheWrite > 0 {
		m.CacheCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("type", "cacheWrite"),
		))
	}

	cost := message.Usage.Cost.Total

	m.CostUsage.Add(ctx, cost, metric.WithAttributes(
		attribute.String("gen_ai.request.model", modelName),
		attribute.String("gen_ai.provider.name", providerName),
	))

	m.SessionCost += cost
	m.SessionInputTokens += message.Usage.Input
	m.SessionOutputTokens += message.Usage.Output
	m.SessionReasoningTokens += message.Usage.Reasoning
	m.SessionCacheReadTokens += message.Usage.CacheRead
	m.SessionCacheCreationTokens += message.Usage.CacheWrite
}

func (h *HarnessHandler) parseToolExecutionEnd(
	ctx context.Context,
	m *agent_session_metrics.HarnessMetrics,
	event types.WireEvent,
	toolExecutions map[string]types.ToolExecutionState,
	sendAction func(ctx context.Context, action agent_session_types.AgentAction) error,
) {
	state, ok := toolExecutions[event.ToolCallId]

	if !ok {
		slog.Warn("[harness][pidev] received tool_execution_end without tool_execution_start",
			"tool_call_id", event.ToolCallId,
		)

		state = types.ToolExecutionState{ToolName: event.ToolName}
	} else {
		delete(toolExecutions, event.ToolCallId)
	}

	if state.StartedAt > 0 {
		m.ToolDuration.Record(ctx, float64(time.Now().UnixMilli()-state.StartedAt), metric.WithAttributes(
			attribute.String("tool", state.ToolName),
		))
	}

	m.ToolCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool", state.ToolName),
		attribute.Bool("success", !event.IsError),
	))

	var args map[string]any

	if raw, ok := event.Args.(map[string]any); ok && raw != nil {
		args = raw
	} else {
		args = state.Args
	}

	input := formatToolInput(state.ToolName, args)

	var output string

	if event.Result != nil {
		texts := make([]string, 0, len(event.Result.Content))

		for _, content := range event.Result.Content {
			if content.Text != "" {
				texts = append(texts, content.Text)
			}
		}

		output = strings.Join(texts, "\n")
	}

	sendAction(ctx, agent_session_types.AgentAction{
		Name:   state.ToolName,
		Input:  input,
		Output: output,
	})
}

func formatToolInput(tool string, args map[string]any) string {
	str := func(key string) string {
		value, _ := args[key].(string)
		return value
	}

	switch tool {
	case "read", "edit", "write":
		return str("path")

	case "bash":
		return str("command")

	case "grep", "find":
		input := str("pattern")

		if path := str("path"); path != "" {
			input = input + " in " + path
		}

		return input

	case "ls":
		return str("path")

	default:
		return fmt.Sprintf("%v", args)
	}
}

func (h *HarnessHandler) tokenUsageAdd(ctx context.Context, metrics *agent_session_metrics.HarnessMetrics, modelName string, providerName string, typ string, n int64) {
	metrics.TokenUsage.Add(ctx, n, metric.WithAttributes(
		attribute.String("type", typ),
		attribute.String("gen_ai.request.model", modelName),
		attribute.String("gen_ai.provider.name", providerName),
	))
}
