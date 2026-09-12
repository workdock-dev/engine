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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/pidev/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// ---------------------------------------------------------------------------
// Recorder callbacks
// ---------------------------------------------------------------------------

type parseRecorder struct {
	thoughts     []string
	responses    []string
	actions      []agent_session_types.AgentAction
	elicitations []agent_session_types.AgentElicitation
	serverErrors int
}

func (r *parseRecorder) sendThought(ctx context.Context, text string) error {
	r.thoughts = append(r.thoughts, text)
	return nil
}

func (r *parseRecorder) sendResponse(ctx context.Context, text string) error {
	r.responses = append(r.responses, text)
	return nil
}

func (r *parseRecorder) sendAction(ctx context.Context, action agent_session_types.AgentAction) error {
	r.actions = append(r.actions, action)
	return nil
}

func (r *parseRecorder) sendElicitation(ctx context.Context, elicitation agent_session_types.AgentElicitation) error {
	r.elicitations = append(r.elicitations, elicitation)
	return nil
}

func (r *parseRecorder) sendServerInternalError(ctx context.Context) error {
	r.serverErrors++
	return nil
}

// wire builds a single WireEvent payload as raw JSON.
func wire(eventType string, mutate func(*types.WireEvent)) []byte {
	event := types.WireEvent{Type: eventType}

	if mutate != nil {
		mutate(&event)
	}

	data, err := json.Marshal(event)

	if err != nil {
		panic(err)
	}

	return data
}

// ---------------------------------------------------------------------------
// HarnessSuite
// ---------------------------------------------------------------------------

type HarnessSuite struct {
	suite.Suite
	handler       HarnessHandler
	harnessConfig *agent_session_interfaces.HarnessConfig
	meterReader   *sdkmetric.ManualReader
}

func TestHarnessSuite(t *testing.T) {
	suite.Run(t, new(HarnessSuite))
}

func (s *HarnessSuite) SetupTest() {
	s.handler = *NewHarnessHandler(types.Config{
		Version: "0.85.1",
	}).(*HarnessHandler)

	s.harnessConfig = &agent_session_interfaces.HarnessConfig{
		Provider: &agent_session_interfaces.Provider{
			Name:  "openai",
			Model: "gpt-4o",
		},
	}

	s.setupMetrics()
}

// setupMetrics installs a manual-read meter provider so tests can assert on
// the metrics recorded by Parse.
func (s *HarnessSuite) setupMetrics() {
	s.T().Helper()

	s.meterReader = sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(s.meterReader))
	otel.SetMeterProvider(provider)
}

// parse runs Parse over the given messages and returns the recorder, so tests
// can assert on every callback invocation.
func (s *HarnessSuite) parse(ctx context.Context, messages ...[]byte) *parseRecorder {
	s.T().Helper()

	ch := make(chan []byte, len(messages)+1)
	for _, m := range messages {
		ch <- m
	}
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(ctx, s.harnessConfig, ch, "evt-1",
		rec.sendThought,
		rec.sendResponse,
		rec.sendAction,
		rec.sendElicitation,
		rec.sendServerInternalError,
	)
	s.Require().NoError(err)
	return rec
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestNewHarnessHandler() {
	h := NewHarnessHandler(types.Config{Version: "0.1.0"})

	var iface agent_session_interfaces.HandlerHarness = h
	s.NotNil(iface)
	s.IsType(&HarnessHandler{}, iface)
}

// ---------------------------------------------------------------------------
// GetConfigurationCommands / GetCommands / GetPromptFile / RunCommand
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestGetConfigurationCommands_ReplacesVersionArg() {
	commands := s.handler.GetConfigurationCommands()

	s.Require().Len(commands, 1)
	s.NotContains(commands[0], "VERSION_ARG")
	s.Contains(commands[0], "pi-coding-agent@0.85.1")
}

func (s *HarnessSuite) TestGetCommands_Nil() {
	s.Nil(s.handler.GetCommands())
}

func (s *HarnessSuite) TestGetPromptFile() {
	path, data := s.handler.GetPromptFile("do the work")

	s.Equal(PROMPT_FILE_PATH, path)
	s.Equal([]byte("do the work"), data)
}

func (s *HarnessSuite) TestRunCommand_Format() {
	expected := fmt.Sprintf(
		"mkdir -p %[1]s && cd %[1]s && PI_SKIP_VERSION_CHECK=1 pi --mode json -c < %[2]s",
		WORKSPACE_PATH, PROMPT_FILE_PATH,
	)

	s.Equal(expected, s.handler.RunCommand())
}

func (s *HarnessSuite) TestRunCommand_ProviderAndModel() {
	s.handler.config = types.Config{
		Provider: &types.ProviderConfig{
			Name:    "ollama",
			BaseUrl: "https://ollama.com/v1",
			Api:     "openai-completions",
			ApiKey:  "$OLLAMA_API_KEY",
			Models: []types.ModelConfig{
				{Id: "glm-5.3-flash:cloud"},
			},
		},
	}

	expected := fmt.Sprintf(
		"mkdir -p %[1]s && cd %[1]s && PI_SKIP_VERSION_CHECK=1 pi --mode json --provider ollama --model glm-5.3-flash:cloud -c < %[2]s",
		WORKSPACE_PATH, PROMPT_FILE_PATH,
	)

	s.Equal(expected, s.handler.RunCommand())
}

func (s *HarnessSuite) TestRunCommand_ThinkingLevel() {
	s.handler.config = types.Config{
		ThinkingLevel: "high",
	}

	expected := fmt.Sprintf(
		"mkdir -p %[1]s && cd %[1]s && PI_SKIP_VERSION_CHECK=1 pi --mode json --thinking high -c < %[2]s",
		WORKSPACE_PATH, PROMPT_FILE_PATH,
	)

	s.Equal(expected, s.handler.RunCommand())
}

// ---------------------------------------------------------------------------
// GetConfigFile
// ---------------------------------------------------------------------------

// settingsFileShape extracts the interesting keys of the generated settings JSON.
type settingsFileShape struct {
	DefaultThinkingLevel string   `json:"defaultThinkingLevel"`
	DefaultProjectTrust  string   `json:"defaultProjectTrust"`
	DefaultTools         []string `json:"defaultTools"`
	Packages             []string `json:"packages"`
	NpmCommand           []string `json:"npmCommand"`
}

// modelsFileShape extracts the interesting keys of the generated models.json.
type modelsFileShape struct {
	Providers map[string]struct {
		BaseUrl string `json:"baseUrl"`
		Api     string `json:"api"`
		ApiKey  string `json:"apiKey"`
		Models  []struct {
			Id            string `json:"id"`
			Name          string `json:"name"`
			Reasoning     bool   `json:"reasoning"`
			ContextWindow int    `json:"contextWindow"`
			MaxTokens     int    `json:"maxTokens"`
		} `json:"models"`
	} `json:"providers"`
}

// mcpFileShape extracts the interesting keys of the generated mcp.json.
type mcpFileShape struct {
	McpServers map[string]struct {
		Url       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		Lifecycle string            `json:"lifecycle"`
	} `json:"mcpServers"`
}

func (s *HarnessSuite) unmarshalSettings(config agent_session_interfaces.HarnessConfig) settingsFileShape {
	s.T().Helper()

	path, data, err := s.handler.GetConfigFile(&config)
	s.Require().NoError(err)
	s.Require().Equal(CONFIG_FILE_PATH, path)

	var parsed settingsFileShape
	s.Require().NoError(json.Unmarshal(data, &parsed))
	return parsed
}

// findFile extracts the data of the file at the given path from GetFiles.
func (s *HarnessSuite) findFile(files []map[string][]byte, path string) []byte {
	s.T().Helper()

	for _, file := range files {
		if data, ok := file[path]; ok {
			return data
		}
	}

	s.FailNow("file not found in GetFiles result", "path %s", path)
	return nil
}

func (s *HarnessSuite) unmarshalModelsFromFiles(files []map[string][]byte) modelsFileShape {
	s.T().Helper()

	data := s.findFile(files, MODELS_FILE_PATH)

	var parsed modelsFileShape
	s.Require().NoError(json.Unmarshal(data, &parsed))
	return parsed
}

func (s *HarnessSuite) unmarshalMcpFromFiles(files []map[string][]byte) mcpFileShape {
	s.T().Helper()

	data := s.findFile(files, MCP_FILE_PATH)

	var parsed mcpFileShape
	s.Require().NoError(json.Unmarshal(data, &parsed))
	return parsed
}

func (s *HarnessSuite) TestGetConfigFile_Defaults() {
	s.handler.config = types.Config{}

	parsed := s.unmarshalSettings(agent_session_interfaces.HarnessConfig{})

	s.Equal("always", parsed.DefaultProjectTrust)
	s.Empty(parsed.DefaultThinkingLevel)
	s.Equal(defaultTools, parsed.DefaultTools)
	s.Empty(parsed.Packages)
	s.Empty(parsed.NpmCommand)
}

func (s *HarnessSuite) TestGetConfigFile_ThinkingLevelAndTools() {
	s.handler.config = types.Config{
		ThinkingLevel: "high",
		Tools:         []string{"read", "grep", "ls"},
	}

	parsed := s.unmarshalSettings(agent_session_interfaces.HarnessConfig{})

	s.Equal("high", parsed.DefaultThinkingLevel)
	s.Equal("always", parsed.DefaultProjectTrust)
	s.Equal([]string{"read", "grep", "ls"}, parsed.DefaultTools)
}

func (s *HarnessSuite) TestGetConfigFile_McpAdapterPackage() {
	s.handler.config = types.Config{
		McpAdapterVersion: "2.33.0",
	}

	parsed := s.unmarshalSettings(agent_session_interfaces.HarnessConfig{})

	s.Equal([]string{"npm:pi-mcp-adapter@2.33.0"}, parsed.Packages)
	s.Equal(NPM_COMMAND, parsed.NpmCommand)
}

func (s *HarnessSuite) TestGetFiles_NoExtrasReturnsNil() {
	s.handler.config = types.Config{}

	files, err := s.handler.GetFiles(&agent_session_interfaces.HarnessConfig{})

	s.Require().NoError(err)
	s.Nil(files)
}

func (s *HarnessSuite) TestGetFiles_HandlerProvider() {
	s.handler.config = types.Config{
		Provider: &types.ProviderConfig{
			Name:    "ollama",
			BaseUrl: "https://ollama.com/v1",
			Api:     "openai-completions",
			ApiKey:  "$OLLAMA_API_KEY",
			Models: []types.ModelConfig{
				{
					Id:            "glm-5.3-flash:cloud",
					Name:          "GLM 5.3 Flash",
					Reasoning:     true,
					ContextWindow: 128000,
					MaxTokens:     32000,
				},
			},
		},
	}

	files, err := s.handler.GetFiles(&agent_session_interfaces.HarnessConfig{})
	s.Require().NoError(err)
	s.Len(files, 1)

	parsed := s.unmarshalModelsFromFiles(files)

	provider, ok := parsed.Providers["ollama"]
	s.Require().True(ok)
	s.Equal("https://ollama.com/v1", provider.BaseUrl)
	s.Equal("openai-completions", provider.Api)
	s.Equal("$OLLAMA_API_KEY", provider.ApiKey)
	s.Require().Len(provider.Models, 1)
	s.Equal("glm-5.3-flash:cloud", provider.Models[0].Id)
	s.Equal("GLM 5.3 Flash", provider.Models[0].Name)
	s.True(provider.Models[0].Reasoning)
	s.Equal(128000, provider.Models[0].ContextWindow)
	s.Equal(32000, provider.Models[0].MaxTokens)
}

func (s *HarnessSuite) TestGetFiles_HandlerProviderDefaults() {
	s.handler.config = types.Config{
		Provider: &types.ProviderConfig{
			ApiKey: "ollama",
			Models: []types.ModelConfig{
				{Id: "llama3.1:8b"},
			},
		},
	}

	files, err := s.handler.GetFiles(&agent_session_interfaces.HarnessConfig{})
	s.Require().NoError(err)
	s.Len(files, 1)

	parsed := s.unmarshalModelsFromFiles(files)

	provider, ok := parsed.Providers["ollama"]
	s.Require().True(ok)
	s.Equal("https://ollama.com/v1", provider.BaseUrl)
	s.Equal("openai-completions", provider.Api)
	s.Equal("ollama", provider.ApiKey)
	s.Require().Len(provider.Models, 1)
	s.Equal("llama3.1:8b", provider.Models[0].Id)
	s.Empty(provider.Models[0].Name)
	s.False(provider.Models[0].Reasoning)
	s.Zero(provider.Models[0].ContextWindow)
	s.Zero(provider.Models[0].MaxTokens)
}

func (s *HarnessSuite) TestGetFiles_ParamProviderOverridesHandlerProvider() {
	s.handler.config = types.Config{
		Provider: &types.ProviderConfig{
			Name:    "ollama",
			BaseUrl: "https://ollama.com/v1",
			ApiKey:  "$OLLAMA_API_KEY",
			Models: []types.ModelConfig{
				{Id: "glm-5.3-flash:cloud"},
			},
		},
	}

	config := agent_session_interfaces.HarnessConfig{
		Provider: &agent_session_interfaces.Provider{
			Name:         "openrouter",
			Model:        "anthropic/claude-3.5-sonnet",
			ModelOptions: `{"maxTokens": 100}`,
			AuthEnvVar:   "OPENROUTER_API_KEY",
		},
	}

	files, err := s.handler.GetFiles(&config)
	s.Require().NoError(err)
	s.Len(files, 1)

	parsed := s.unmarshalModelsFromFiles(files)

	s.Require().Len(parsed.Providers, 1)
	provider, ok := parsed.Providers["openrouter"]
	s.Require().True(ok)
	s.Equal("https://ollama.com/v1", provider.BaseUrl, "custom providers without a base URL fall back to the documented default")
	s.Equal("openai-completions", provider.Api)
	s.Equal("${OPENROUTER_API_KEY}", provider.ApiKey)
	s.Require().Len(provider.Models, 1)
	s.Equal("anthropic/claude-3.5-sonnet", provider.Models[0].Id)
}

func (s *HarnessSuite) TestGetFiles_Mcps() {
	config := agent_session_interfaces.HarnessConfig{
		Mcps: []agent_session_interfaces.MCPConfig{
			{
				Name:    "My MCP",
				Url:     "https://example.com/mcp",
				AuthKey: "MY_MCP_AUTH_SECRET_ENV_VAR_NAME",
			},
			{
				Name:    "Other MCP",
				Url:     "https://other.example.com/mcp",
				AuthKey: "OTHER_ENV_VAR",
			},
		},
	}

	files, err := s.handler.GetFiles(&config)
	s.Require().NoError(err)
	s.Len(files, 1)

	parsed := s.unmarshalMcpFromFiles(files)
	s.Require().Len(parsed.McpServers, 2)

	server, ok := parsed.McpServers["My MCP"]
	s.Require().True(ok)
	s.Equal("https://example.com/mcp", server.Url)
	s.Equal("keep-alive", server.Lifecycle)
	s.Equal(
		map[string]string{"Authorization": "Bearer ${MY_MCP_AUTH_SECRET_ENV_VAR_NAME}"},
		server.Headers,
	)

	other, ok := parsed.McpServers["Other MCP"]
	s.Require().True(ok)
	s.Equal("https://other.example.com/mcp", other.Url)
	s.Equal(
		map[string]string{"Authorization": "Bearer ${OTHER_ENV_VAR}"},
		other.Headers,
	)
}

func (s *HarnessSuite) TestGetFiles_ProviderAndMcps() {
	s.handler.config = types.Config{
		Provider: &types.ProviderConfig{
			Name:   "ollama",
			ApiKey: "$OLLAMA_API_KEY",
			Models: []types.ModelConfig{
				{Id: "glm-5.3-flash:cloud"},
			},
		},
	}

	config := agent_session_interfaces.HarnessConfig{
		Mcps: []agent_session_interfaces.MCPConfig{
			{Name: "My MCP", Url: "https://example.com/mcp", AuthKey: "ENV_VAR"},
		},
	}

	files, err := s.handler.GetFiles(&config)
	s.Require().NoError(err)
	s.Len(files, 2)

	s.unmarshalModelsFromFiles(files)
	s.unmarshalMcpFromFiles(files)
}

// ---------------------------------------------------------------------------
// Parse
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParse_ClosedChannelReturnsNil() {
	s.parse(context.Background())
}

func (s *HarnessSuite) TestParse_InvalidJsonIsSkipped() {
	rec := s.parse(context.Background(), []byte(`{not-json`))

	s.Empty(rec.thoughts)
	s.Empty(rec.responses)
	s.Empty(rec.actions)
	s.Empty(rec.elicitations)
}

func (s *HarnessSuite) TestParse_SessionHeaderIsAccepted() {
	rec := s.parse(context.Background(), []byte(`{"type":"session","version":3,"id":"abc","timestamp":"now","cwd":"/workspace"}`))

	s.Empty(rec.thoughts)
	s.Empty(rec.responses)
	s.Empty(rec.actions)
}

func (s *HarnessSuite) TestParse_AgentLifecycleClearsThought() {
	for _, eventType := range []string{"agent_start", "turn_start"} {
		rec := s.parse(context.Background(), wire(eventType, nil))

		s.Require().Len(rec.thoughts, 1, "event type %s", eventType)
		s.Empty(rec.thoughts[0])
		s.Empty(rec.responses)
	}
}

func assistantMessage(mutate func(*types.AssistantMessage)) *types.AssistantMessage {
	msg := &types.AssistantMessage{
		Role:     "assistant",
		Provider: "openai",
		Model:    "gpt-4o",
		Usage: types.Usage{
			Input:       5,
			Output:      3,
			CacheRead:   1,
			CacheWrite:  2,
			Reasoning:   2,
			TotalTokens: 11,
			Cost:        types.Cost{Total: 0.25},
		},
	}

	if mutate != nil {
		mutate(msg)
	}

	return msg
}

func (s *HarnessSuite) TestParse_AgentEnd_IsBookkeepingOnly() {
	rec := s.parse(context.Background(), wire("agent_end", func(e *types.WireEvent) {
		e.Messages = []types.AssistantMessage{
			*assistantMessage(func(m *types.AssistantMessage) {
				m.StopReason = "stop"
				m.Content = []types.MessageContent{{Type: "text", Text: "would duplicate message_end"}}
			}),
		}
	}))

	// The final authoritative message arrives on message_end; agent_end must
	// not forward it a second time.
	s.Empty(rec.responses)
	s.Empty(rec.thoughts)
	s.Empty(rec.serverErrors)
}

func (s *HarnessSuite) TestParse_TurnEndAndMessageStartAreBookkeepingOnly() {
	rec := s.parse(context.Background(),
		wire("message_start", func(e *types.WireEvent) {
			e.Message = assistantMessage(nil)
		}),
		wire("turn_end", func(e *types.WireEvent) {
			e.Message = assistantMessage(nil)
		}),
	)

	s.Empty(rec.responses)
	s.Empty(rec.thoughts)
	s.Empty(rec.serverErrors)
}

func (s *HarnessSuite) TestParse_MessageEnd_SendsTextAndUsage() {
	rec := s.parse(context.Background(), wire("message_end", func(e *types.WireEvent) {
		e.Message = assistantMessage(func(m *types.AssistantMessage) {
			m.StopReason = "stop"
			m.Content = []types.MessageContent{{Type: "text", Text: "final answer"}}
		})
	}))

	s.Require().Len(rec.responses, 2)
	s.Equal("final answer", rec.responses[0])
	s.Empty(rec.responses[1])
	s.Empty(rec.serverErrors)
}

func (s *HarnessSuite) TestParse_MessageEnd_AbortedTriggersServerInternalError() {
	rec := s.parse(context.Background(), wire("message_end", func(e *types.WireEvent) {
		e.Message = assistantMessage(func(m *types.AssistantMessage) {
			m.StopReason = "aborted"
		})
	}))

	s.Empty(rec.responses[0])
	s.Equal(1, rec.serverErrors)
}

func (s *HarnessSuite) TestParse_MessageEnd_UserMessageIsIgnored() {
	rec := s.parse(context.Background(), wire("message_end", func(e *types.WireEvent) {
		e.Message = &types.AssistantMessage{
			Role: "user",
			Content: []types.MessageContent{
				{Type: "text", Text: "Your objective is to complete the requested work."},
			},
		}
	}))

	// The engine-prepared prompt must never be echoed back to the client
	s.Empty(rec.responses)
	s.Empty(rec.thoughts)
	s.Equal(0, rec.serverErrors)

	messageCount, ok := s.collectMetrics(context.Background())[METRICS_NAME+".message.count"]
	if ok {
		s.Equal(int64(0), s.sumInt64(messageCount))
	}
}

func (s *HarnessSuite) TestParse_MessageEnd_ToolResultMessageIsIgnored() {
	rec := s.parse(context.Background(), wire("message_end", func(e *types.WireEvent) {
		e.Message = &types.AssistantMessage{
			Role: "toolResult",
			Content: []types.MessageContent{
				{Type: "text", Text: "tool output"},
			},
		}
	}))

	s.Empty(rec.responses)
	s.Empty(rec.thoughts)
	s.Equal(0, rec.serverErrors)
}

func (s *HarnessSuite) TestParse_MessageEnd_WithoutMessageIsIgnored() {
	rec := s.parse(context.Background(), wire("message_end", nil))

	s.Empty(rec.responses)
}

func (s *HarnessSuite) TestParse_MessageUpdate_NilAssistantEventIsIgnored() {
	rec := s.parse(context.Background(), wire("message_update", nil))

	s.Empty(rec.thoughts)
	s.Empty(rec.responses)
}

func (s *HarnessSuite) TestParse_MessageUpdate_ThinkingDeltaAccumulates() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: 0, Delta: "deep "}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: 0, Delta: "thought"}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_end", ContentIndex: 0}
		}),
	)

	s.Equal([]string{"deep thought"}, rec.thoughts)
	s.Empty(rec.responses)
}

func (s *HarnessSuite) TestParse_MessageUpdate_ThinkingEnd_UsesContentWhenPresent() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: 0, Delta: "stale"}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_end", ContentIndex: 0, Content: "authoritative"}
		}),
	)

	s.Equal([]string{"authoritative"}, rec.thoughts)
}

func (s *HarnessSuite) TestParse_MessageUpdate_TextDeltaAccumulates() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "text_delta", ContentIndex: 1, Delta: "hel"}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "text_delta", ContentIndex: 1, Delta: "lo"}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "text_end", ContentIndex: 1, Content: "hello"}
		}),
	)

	// Text is only forwarded when the final message arrives; deltas are buffered
	s.Empty(rec.responses)
}

func (s *HarnessSuite) TestParse_MessageUpdate_StartClearsThought() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "start", ContentIndex: 0}
		}),
	)

	s.Equal([]string{""}, rec.thoughts)
}

func (s *HarnessSuite) TestParse_MessageUpdate_ToolcallStartClearsThought() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: 0, Id: "c1", ToolName: "read"}
		}),
	)

	s.Equal([]string{""}, rec.thoughts)
}

func (s *HarnessSuite) TestParse_MessageUpdate_ToolcallDeltaAndEndAreNoops() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: 0, Delta: `{}`}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: 0}
		}),
	)

	s.Empty(rec.thoughts)
	s.Empty(rec.responses)
	s.Empty(rec.actions)
}

func (s *HarnessSuite) TestParse_MessageUpdate_StreamError() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{
				Type:   "error",
				Reason: "error",
				Error: &types.AssistantMessage{
					StopReason:   "error",
					ErrorMessage: "boom",
				},
			}
		}),
	)

	s.Empty(rec.responses)
	s.Empty(rec.serverErrors)
}

func (s *HarnessSuite) TestParse_MessageUpdate_UnknownAssistantEventIsWarned() {
	rec := s.parse(context.Background(),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "mystery"}
		}),
	)

	s.Empty(rec.responses)
}

func (s *HarnessSuite) TestParse_UnexpectedEventType() {
	rec := s.parse(context.Background(), wire("mystery", func(e *types.WireEvent) {
		e.Delta = "ignored"
	}))

	s.Require().Len(rec.responses, 1)
	s.Contains(rec.responses[0], "An unexpected format has been received by the harness:")
	s.Contains(rec.responses[0], `"type":"mystery"`)
}

func (s *HarnessSuite) TestParse_ContextCancelled() {
	ch := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.handler.Parse(ctx, s.harnessConfig, ch, "evt-1",
		func(ctx context.Context, text string) error { return nil },
		func(ctx context.Context, text string) error { return nil },
		func(ctx context.Context, action agent_session_types.AgentAction) error { return nil },
		func(ctx context.Context, e agent_session_types.AgentElicitation) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	s.ErrorIs(err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Parse — tool execution events
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParse_ToolExecution_SendsAction() {
	rec := s.parse(context.Background(),
		wire("tool_execution_start", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.ToolName = "read"
			e.Args = map[string]any{"path": "/tmp/main.go"}
		}),
		wire("tool_execution_end", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.ToolName = "read"
			e.Result = &types.ToolResult{
				Content: []types.ResultContent{{Type: "text", Text: "contents"}},
			}
		}),
	)

	s.Require().Len(rec.actions, 1)
	s.Equal(agent_session_types.AgentAction{Name: "read", Input: "/tmp/main.go", Output: "contents"}, rec.actions[0])
}

func (s *HarnessSuite) TestParse_ToolExecution_EndWithoutStart() {
	rec := s.parse(context.Background(),
		wire("tool_execution_end", func(e *types.WireEvent) {
			e.ToolCallId = "c9"
			e.ToolName = "bash"
			e.Args = map[string]any{"command": "echo hi"}
			e.Result = &types.ToolResult{
				Content: []types.ResultContent{{Type: "text", Text: "hi\n"}},
			}
		}),
	)

	s.Require().Len(rec.actions, 1)
	s.Equal(agent_session_types.AgentAction{Name: "bash", Input: "echo hi", Output: "hi\n"}, rec.actions[0])
}

func (s *HarnessSuite) TestParse_ToolExecution_IsError() {
	rec := s.parse(context.Background(),
		wire("tool_execution_start", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.ToolName = "bash"
		}),
		wire("tool_execution_end", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.IsError = true
		}),
	)

	s.Require().Len(rec.actions, 1)
	s.Equal("bash", rec.actions[0].Name)

	toolCount, ok := s.collectMetrics(context.Background())[METRICS_NAME+".tool.count"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64ByBoolAttribute(toolCount, "success", false))
}

func (s *HarnessSuite) TestParse_ToolExecution_MultiPartResult() {
	rec := s.parse(context.Background(),
		wire("tool_execution_start", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.ToolName = "ls"
		}),
		wire("tool_execution_end", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.Result = &types.ToolResult{
				Content: []types.ResultContent{
					{Type: "text", Text: "a.go"},
					{Type: "text", Text: "b.go"},
				},
			}
		}),
	)

	s.Require().Len(rec.actions, 1)
	s.Equal("a.go\nb.go", rec.actions[0].Output)
}

// ---------------------------------------------------------------------------
// formatToolInput
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestFormatToolInput() {
	tests := []struct {
		tool     string
		args     map[string]any
		expected string
	}{
		{tool: "read", args: map[string]any{"path": "/a.go"}, expected: "/a.go"},
		{tool: "edit", args: map[string]any{"path": "/b.go"}, expected: "/b.go"},
		{tool: "write", args: map[string]any{"path": "/c.go"}, expected: "/c.go"},
		{tool: "bash", args: map[string]any{"command": "go test"}, expected: "go test"},
		{tool: "grep", args: map[string]any{"pattern": "TODO", "path": "/src"}, expected: "TODO in /src"},
		{tool: "grep", args: map[string]any{"pattern": "TODO"}, expected: "TODO"},
		{tool: "find", args: map[string]any{"pattern": "*.go", "path": "/src"}, expected: "*.go in /src"},
		{tool: "ls", args: map[string]any{"path": "/src"}, expected: "/src"},
		{tool: "ls", args: map[string]any{}, expected: ""},
		{tool: "mystery", args: map[string]any{"k": "v"}, expected: fmt.Sprintf("%v", map[string]any{"k": "v"})},
	}

	for _, tt := range tests {
		s.Run(tt.tool, func() {
			s.Equal(tt.expected, formatToolInput(tt.tool, tt.args))
		})
	}
}

// ---------------------------------------------------------------------------
// Parse — full stream integration
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParse_MixedStream() {
	rec := s.parse(context.Background(),
		[]byte(`{not-json`),
		[]byte(`{"type":"session","version":3,"id":"abc","timestamp":"now","cwd":"/w"}`),
		wire("agent_start", nil),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: 0, Delta: "plan"}
		}),
		wire("message_update", func(e *types.WireEvent) {
			e.AssistantMessageEvent = &types.AssistantMessageEvent{Type: "thinking_end", ContentIndex: 0}
		}),
		wire("tool_execution_start", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.ToolName = "bash"
			e.Args = map[string]any{"command": "go version"}
		}),
		wire("tool_execution_end", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.Result = &types.ToolResult{Content: []types.ResultContent{{Type: "text", Text: "go1.24"}}}
		}),
		wire("message_end", func(e *types.WireEvent) {
			e.Message = assistantMessage(func(m *types.AssistantMessage) {
				m.StopReason = "stop"
				m.Content = []types.MessageContent{{Type: "text", Text: "done"}}
			})
		}),
		wire("agent_end", func(e *types.WireEvent) {
			e.Messages = []types.AssistantMessage{
				*assistantMessage(func(m *types.AssistantMessage) {
					m.Content = []types.MessageContent{{Type: "text", Text: "duplicate"}}
				}),
			}
		}),
	)

	s.Len(rec.thoughts, 2) // agent_start + thinking_end
	s.Equal("plan", rec.thoughts[1])
	s.Require().Len(rec.actions, 1)
	s.Equal(agent_session_types.AgentAction{Name: "bash", Input: "go version", Output: "go1.24"}, rec.actions[0])
	s.Equal([]string{"done", ""}, rec.responses)
}

// ---------------------------------------------------------------------------
// Parse — metrics
// ---------------------------------------------------------------------------

// collectMetrics drains the manual meter reader into a name-keyed lookup.
func (s *HarnessSuite) collectMetrics(ctx context.Context) map[string]metricdata.Metrics {
	s.T().Helper()

	var rm metricdata.ResourceMetrics
	s.Require().NoError(s.meterReader.Collect(ctx, &rm))

	byName := make(map[string]metricdata.Metrics, len(rm.ScopeMetrics))

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			byName[m.Name] = m
		}
	}

	return byName
}

func (s *HarnessSuite) sumInt64(m metricdata.Metrics) int64 {
	s.T().Helper()

	sum, ok := m.Data.(metricdata.Sum[int64])
	s.Require().True(ok, "metric %q is not an int64 sum", m.Name)

	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}

	return total
}

func (s *HarnessSuite) sumInt64ByAttribute(m metricdata.Metrics, key, value string) int64 {
	s.T().Helper()

	sum, ok := m.Data.(metricdata.Sum[int64])
	s.Require().True(ok, "metric %q is not an int64 sum", m.Name)

	var total int64

	for _, dp := range sum.DataPoints {
		if v, ok := dp.Attributes.Value(attribute.Key(key)); ok && v.AsString() == value {
			total += dp.Value
		}
	}

	return total
}

func (s *HarnessSuite) sumInt64ByBoolAttribute(m metricdata.Metrics, key string, want bool) int64 {
	s.T().Helper()

	sum, ok := m.Data.(metricdata.Sum[int64])
	s.Require().True(ok, "metric %q is not an int64 sum", m.Name)

	var total int64

	for _, dp := range sum.DataPoints {
		if v, ok := dp.Attributes.Value(attribute.Key(key)); ok && v.AsBool() == want {
			total += dp.Value
		}
	}

	return total
}

func (s *HarnessSuite) sumFloat64(m metricdata.Metrics) float64 {
	s.T().Helper()

	sum, ok := m.Data.(metricdata.Sum[float64])
	s.Require().True(ok, "metric %q is not a float64 sum", m.Name)

	var total float64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}

	return total
}

func (s *HarnessSuite) histogramFloat64Count(m metricdata.Metrics) uint64 {
	s.T().Helper()

	hist, ok := m.Data.(metricdata.Histogram[float64])
	s.Require().True(ok, "metric %q is not a float64 histogram", m.Name)

	var count uint64
	for _, dp := range hist.DataPoints {
		count += dp.Count
	}

	return count
}

func (s *HarnessSuite) histogramInt64Value(m metricdata.Metrics) int64 {
	s.T().Helper()

	hist, ok := m.Data.(metricdata.Histogram[int64])
	s.Require().True(ok, "metric %q is not an int64 histogram", m.Name)

	var total int64
	for _, dp := range hist.DataPoints {
		total += int64(dp.Sum)
	}

	return total
}

func (s *HarnessSuite) TestParse_MessageEnd_RecordsUsageMetrics() {
	rec := s.parse(context.Background(), wire("message_end", func(e *types.WireEvent) {
		e.Message = assistantMessage(nil)
	}))
	s.Require().Len(rec.responses, 1)

	ctx := context.Background()
	metrics := s.collectMetrics(ctx)

	tokenUsage, ok := metrics[METRICS_NAME+".token.usage"]
	s.Require().True(ok)
	s.Equal(int64(5), s.sumInt64ByAttribute(tokenUsage, "type", "input"))
	s.Equal(int64(3), s.sumInt64ByAttribute(tokenUsage, "type", "output"))
	s.Equal(int64(2), s.sumInt64ByAttribute(tokenUsage, "type", "reasoning"))
	s.Equal(int64(1), s.sumInt64ByAttribute(tokenUsage, "type", "cacheRead"))
	s.Equal(int64(2), s.sumInt64ByAttribute(tokenUsage, "type", "cacheWrite"))

	cacheCount, ok := metrics[METRICS_NAME+".cache.count"]
	s.Require().True(ok)
	s.Equal(int64(2), s.sumInt64(cacheCount))

	costUsage, ok := metrics[METRICS_NAME+".cost.usage"]
	s.Require().True(ok)
	s.InDelta(0.25, s.sumFloat64(costUsage), 0.0001)

	modelUsage, ok := metrics[METRICS_NAME+".model.usage"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64(modelUsage))

	sessionCount, ok := metrics[METRICS_NAME+".session.count"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64(sessionCount))

	messageCount, ok := metrics[METRICS_NAME+".message.count"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64(messageCount))

	tokenTotal, ok := metrics[METRICS_NAME+".session.token.total"]
	s.Require().True(ok)
	s.Equal(int64(10), s.histogramInt64Value(tokenTotal)) // 5+3+2

	costTotal, ok := metrics[METRICS_NAME+".session.cost.total"]
	s.Require().True(ok)
	s.Require().Equal(uint64(1), s.histogramFloat64Count(costTotal))

	duration, ok := metrics[METRICS_NAME+".session.duration"]
	s.Require().True(ok)
	s.Equal(uint64(1), s.histogramFloat64Count(duration))
}

func (s *HarnessSuite) TestParse_AutoRetryIncrementsRetryCount() {
	s.parse(context.Background(), wire("auto_retry_start", func(e *types.WireEvent) {
		e.Attempt = 1
	}))

	retryCount, ok := s.collectMetrics(context.Background())[METRICS_NAME+".retry.count"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64ByAttribute(retryCount, "gen_ai.provider.name", "openai"))
}

func (s *HarnessSuite) TestParse_NilProviderConfigDoesNotPanic() {
	ch := make(chan []byte, 1)
	ch <- wire("message_end", func(e *types.WireEvent) {
		e.Message = assistantMessage(func(m *types.AssistantMessage) {
			m.Content = []types.MessageContent{{Type: "text", Text: "hello"}}
		})
	})
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(context.Background(), &agent_session_interfaces.HarnessConfig{}, ch, "evt-1",
		rec.sendThought, rec.sendResponse, rec.sendAction, rec.sendElicitation, rec.sendServerInternalError)

	s.Require().NoError(err)
	s.Equal([]string{"hello", ""}, rec.responses)
}

// ---------------------------------------------------------------------------
// Parse — tool timing metrics
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParse_ToolExecution_RecordsDuration() {
	rec := s.parse(context.Background(),
		wire("tool_execution_start", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.ToolName = "bash"
		}),
		wire("tool_execution_end", func(e *types.WireEvent) {
			e.ToolCallId = "c1"
			e.Result = &types.ToolResult{Content: []types.ResultContent{{Type: "text", Text: "out"}}}
		}),
	)
	s.Require().Len(rec.actions, 1)

	metrics := s.collectMetrics(context.Background())

	toolCount, ok := metrics[METRICS_NAME+".tool.count"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64ByBoolAttribute(toolCount, "success", true))

	duration, ok := metrics[METRICS_NAME+".tool.duration"]
	s.Require().True(ok)
	s.Equal(uint64(1), s.histogramFloat64Count(duration))
}

// compile-time interface check
var _ agent_session_interfaces.HandlerHarness = (*HarnessHandler)(nil)
