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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_metrics "github.com/workdock-dev/engine/features/agent_session/metrics"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ings/opencode/types"
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

func (r *parseRecorder) sendServerInternalError(ctx context.Context) error { return nil }

// wire builds a single WireEvent payload as raw JSON.
func wire(eventType string, part json.RawMessage) []byte {
	data, err := json.Marshal(types.WireEvent{
		Type:      eventType,
		Timestamp: 1,
		SessionID: "sess-1",
		Part:      part,
	})
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
	metrics       *agent_session_metrics.HarnessMetrics
	meterReader   *sdkmetric.ManualReader
}

func TestHarnessSuite(t *testing.T) {
	suite.Run(t, new(HarnessSuite))
}

func (s *HarnessSuite) SetupTest() {
	s.handler = *NewHarnessHandler(types.Config{
		Version: "1.2.3",
		Model:   "openai/gpt-4o",
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
// the metrics recorded by Parse and parseToolPart.
func (s *HarnessSuite) setupMetrics() {
	s.T().Helper()

	s.meterReader = sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(s.meterReader))
	otel.SetMeterProvider(provider)

	metrics, err := agent_session_metrics.NewHarnessMetrics(METRICS_NAME, otel.Meter("opencode"))
	s.Require().NoError(err)
	s.metrics = metrics
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
	h := NewHarnessHandler(types.Config{Version: "0.1.0", Model: "m"})

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
	s.Contains(commands[0], "--version 1.2.3")
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
		"mkdir -p %[1]s && %[2]s/opencode run --format json --thinking --dir %[1]s -c < %[3]s",
		WORKSPACE_PATH, BIN_PATH, PROMPT_FILE_PATH,
	)

	s.Equal(expected, s.handler.RunCommand())
}

// ---------------------------------------------------------------------------
// GetConfigFile
// ---------------------------------------------------------------------------

// configFileShape extracts the interesting keys of the generated config JSON.
type configFileShape struct {
	Permission json.RawMessage `json:"permission"`
	Model      string          `json:"model"`
	Provider   json.RawMessage `json:"provider"`
	Mcp        json.RawMessage `json:"mcp"`
}

func (s *HarnessSuite) unmarshalConfig(config agent_session_interfaces.HarnessConfig) configFileShape {
	s.T().Helper()

	path, data, err := s.handler.GetConfigFile(&config)
	s.Require().NoError(err)
	s.Require().Equal(CONFIG_FILE_PATH, path)

	var parsed configFileShape
	s.Require().NoError(json.Unmarshal(data, &parsed))
	return parsed
}

func (s *HarnessSuite) TestGetConfigFile_Defaults() {
	parsed := s.unmarshalConfig(agent_session_interfaces.HarnessConfig{})

	s.Equal(json.RawMessage(`{"*":"allow"}`), parsed.Permission)
	s.Equal(json.RawMessage(`{}`), parsed.Provider)
	s.Equal(json.RawMessage(`{}`), parsed.Mcp)
	s.Equal("openai/gpt-4o", parsed.Model)
}

func (s *HarnessSuite) TestGetConfigFile_HandlerConfigOverrides() {
	s.handler.config = types.Config{
		Version:    "1.2.3",
		Model:      "openai/gpt-4o",
		Permission: map[string]any{"bash": "deny"},
		Provider:   map[string]any{"openai": map[string]any{"apiKey": "sk"}},
	}

	parsed := s.unmarshalConfig(agent_session_interfaces.HarnessConfig{})

	s.Equal(json.RawMessage(`{"bash":"deny"}`), parsed.Permission)
	s.Equal(json.RawMessage(`{"openai":{"apiKey":"sk"}}`), parsed.Provider)
	s.Equal("openai/gpt-4o", parsed.Model)
}

func (s *HarnessSuite) TestGetConfigFile_ParamProviderOverridesModel() {
	config := agent_session_interfaces.HarnessConfig{
		Provider: &agent_session_interfaces.Provider{
			Name:         "anthropic",
			Model:        "claude-3",
			ModelOptions: `{"maxTokens": 100}`,
			AuthEnvVar:   "ANTHROPIC_API_KEY",
		},
	}

	parsed := s.unmarshalConfig(config)

	var provider map[string]any
	s.Require().NoError(json.Unmarshal(parsed.Provider, &provider))

	anthropic, ok := provider["anthropic"].(map[string]any)
	s.Require().True(ok)
	options, ok := anthropic["options"].(map[string]any)
	s.Require().True(ok)
	s.Equal("{env:ANTHROPIC_API_KEY}", options["apiKey"])
	models, ok := anthropic["models"].(map[string]any)
	s.Require().True(ok)
	s.Equal(`{"maxTokens": 100}`, models["claude-3"])

	s.Equal("anthropic/claude-3", parsed.Model)
}

func (s *HarnessSuite) TestGetConfigFile_Mcps() {
	config := agent_session_interfaces.HarnessConfig{
		Mcps: []agent_session_interfaces.MCPConfig{
			{Name: "linear", Url: "https://mcp.linear.app/sse", AuthKey: "LINEAR_TOKEN"},
			{Name: "github", Url: "https://mcp.github.dev", AuthKey: "GITHUB_TOKEN"},
		},
	}

	parsed := s.unmarshalConfig(config)

	var mcps map[string]any
	s.Require().NoError(json.Unmarshal(parsed.Mcp, &mcps))
	s.Len(mcps, 2)

	linear, ok := mcps["linear"].(map[string]any)
	s.Require().True(ok)
	s.Equal("remote", linear["type"])
	s.Equal("https://mcp.linear.app/sse", linear["url"])
	s.Equal(true, linear["enabled"])
	s.Equal(false, linear["oauth"])

	headers, ok := linear["headers"].(map[string]any)
	s.Require().True(ok)
	s.Equal("Bearer {env:LINEAR_TOKEN}", headers["Authorization"])

	_, ok = mcps["github"]
	s.True(ok)
}

func (s *HarnessSuite) TestGetConfigFile_ParamPermissionsOverride() {
	config := agent_session_interfaces.HarnessConfig{
		Permissions: map[string]any{"edit": "ask"},
	}

	parsed := s.unmarshalConfig(config)

	s.Equal(json.RawMessage(`{"edit":"ask"}`), parsed.Permission)
}

// ---------------------------------------------------------------------------
// GetConfigFile marshal errors
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestGetConfigFile_MarshalErrors() {
	// chan values cannot be marshaled to JSON.
	unmarshalable := map[string]any{"key": make(chan int)}

	tests := []struct {
		name   string
		config types.Config
	}{
		{name: "permission", config: types.Config{Permission: unmarshalable}},
		{name: "provider", config: types.Config{Provider: unmarshalable}},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			handler := HarnessHandler{config: tt.config}

			path, data, err := handler.GetConfigFile(&agent_session_interfaces.HarnessConfig{})
			s.Error(err)
			s.Empty(path)
			s.Nil(data)
		})
	}
}

func (s *HarnessSuite) TestGetConfigFile_MarshalError_ParamPermissions() {
	handler := HarnessHandler{config: types.Config{}}

	config := agent_session_interfaces.HarnessConfig{
		Permissions: map[string]any{"key": make(chan int)},
	}

	path, data, err := handler.GetConfigFile(&config)
	s.Error(err)
	s.Empty(path)
	s.Nil(data)
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

func (s *HarnessSuite) TestParse_ThoughtOnlyParts() {
	parts := []string{"retry", "step_start", "file", "subtask", "snapshot", "patch", "agent", "compaction"}

	for _, partType := range parts {
		rec := s.parse(context.Background(), wire(partType, json.RawMessage(`{}`)))

		s.Require().Len(rec.thoughts, 1, "part type %s", partType)
		s.Equal("compacting", rec.thoughts[0])
		s.Empty(rec.responses)
	}
}

func (s *HarnessSuite) TestParse_Reasoning() {
	rec := s.parse(context.Background(), wire("reasoning", json.RawMessage(`{"text":"thinking hard"}`)))

	s.Equal([]string{"thinking hard"}, rec.thoughts)
	s.Empty(rec.responses)
}

func (s *HarnessSuite) TestParse_Reasoning_InvalidPart() {
	ch := make(chan []byte, 1)
	ch <- wire("reasoning", json.RawMessage(`{"text":["not-a-string"]}`))
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(context.Background(), s.harnessConfig, ch, "evt-1",
		rec.sendThought, rec.sendResponse, rec.sendAction, rec.sendElicitation, rec.sendServerInternalError)

	s.Error(err)
}

func (s *HarnessSuite) TestParse_Text() {
	rec := s.parse(context.Background(), wire("text", json.RawMessage(`{"text":"hello world"}`)))

	s.Equal([]string{"hello world"}, rec.responses)
	s.Empty(rec.thoughts)
}

func (s *HarnessSuite) TestParse_Text_InvalidPart() {
	ch := make(chan []byte, 1)
	ch <- wire("text", json.RawMessage(`{"text":{"nested":"object"}}`))
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(context.Background(), s.harnessConfig, ch, "evt-1",
		rec.sendThought, rec.sendResponse, rec.sendAction, rec.sendElicitation, rec.sendServerInternalError)

	s.Error(err)
}

func (s *HarnessSuite) TestParse_StepFinish_SendsEmptyResponse() {
	part := `{"reason":"stop","tokens":{"total":10,"input":5,"output":3,"reasoning":2,"cache":{"read":1,"write":2}}}`
	rec := s.parse(context.Background(), wire("step_finish", json.RawMessage(part)))

	s.Equal([]string{""}, rec.responses)
}

func (s *HarnessSuite) TestParse_StepFinish_InvalidPart() {
	ch := make(chan []byte, 1)
	ch <- wire("step_finish", json.RawMessage(`{"tokens":"not-an-object"}`))
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(context.Background(), s.harnessConfig, ch, "evt-1",
		rec.sendThought, rec.sendResponse, rec.sendAction, rec.sendElicitation, rec.sendServerInternalError)

	s.Error(err)
}

func (s *HarnessSuite) TestParse_UnexpectedPartType() {
	rec := s.parse(context.Background(), wire("mystery", json.RawMessage(`{"id":"1"}`)))

	s.Require().Len(rec.responses, 1)
	s.Contains(rec.responses[0], "An unexpected format has been received by the harness:")
	s.Contains(rec.responses[0], `"id":"1"`)
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
// Parse — tool events
// ---------------------------------------------------------------------------

func (s *HarnessSuite) toolPart(tool string, input map[string]any, output string) json.RawMessage {
	data, err := json.Marshal(types.ToolPart{
		Type:  "tool",
		Tool:  tool,
		State: types.ToolState{Status: "done", Input: input, Output: output},
	})
	s.Require().NoError(err)
	return data
}

func (s *HarnessSuite) TestParse_ToolUseAliasRoutesToTool() {
	rec := s.parse(context.Background(), wire("tool_use", s.toolPart("read", map[string]any{"filePath": "/tmp/a.go"}, "contents")))

	s.Require().Len(rec.actions, 1)
	s.Equal(agent_session_types.AgentAction{Name: "read", Input: "/tmp/a.go", Output: "contents"}, rec.actions[0])
}

func (s *HarnessSuite) TestParse_Tool_InvalidPart() {
	ch := make(chan []byte, 1)
	ch <- wire("tool", json.RawMessage(`{"tool":"bash","state":"not-an-object"}`))
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(context.Background(), s.harnessConfig, ch, "evt-1",
		rec.sendThought, rec.sendResponse, rec.sendAction, rec.sendElicitation, rec.sendServerInternalError)

	s.Error(err)
}

func (s *HarnessSuite) TestParse_Tool_QuestionSendsElicitations() {
	part := s.toolPart("question", map[string]any{
		"questions": []any{
			map[string]any{
				"question": "Which database?",
				"header":   "DB",
				"multiple": true,
				"options": []any{
					map[string]any{"label": "Postgres", "description": "relational"},
					map[string]any{"label": "SQLite", "description": "embedded"},
				},
			},
			map[string]any{
				"question": "Deploy?",
				"options": []any{
					map[string]any{"label": "Yes", "description": "do it"},
				},
			},
		},
	}, "")

	rec := s.parse(context.Background(), wire("tool", part))

	s.Require().Len(rec.elicitations, 2)
	s.Empty(rec.actions)

	first := rec.elicitations[0]
	s.Equal("Which database?", first.Question)
	s.True(first.Multiple)
	s.Equal([]agent_session_types.AgentOption{
		{Label: "Postgres", Description: "relational"},
		{Label: "SQLite", Description: "embedded"},
	}, first.Options)

	second := rec.elicitations[1]
	s.Equal("Deploy?", second.Question)
	s.False(second.Multiple)
	s.Equal([]agent_session_types.AgentOption{{Label: "Yes", Description: "do it"}}, second.Options)
}

func (s *HarnessSuite) TestParse_Tool_NonQuestionSendsAction() {
	rec := s.parse(context.Background(),
		wire("tool", s.toolPart("bash", map[string]any{"command": "go test ./..."}, "ok\n")))

	s.Require().Len(rec.actions, 1)
	s.Empty(rec.elicitations)
	s.Equal(agent_session_types.AgentAction{Name: "bash", Input: "go test ./...", Output: "ok\n"}, rec.actions[0])
}

// ---------------------------------------------------------------------------
// parseToolPart
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParseToolPart_SimpleTools() {
	tests := []struct {
		tool     string
		inputKey string
		inputVal any
		expected string
	}{
		{tool: "bash", inputKey: "command", inputVal: "ls -la"},
		{tool: "read", inputKey: "filePath", inputVal: "/tmp/main.go"},
		{tool: "webfetch", inputKey: "url", inputVal: "https://example.com"},
		{tool: "websearch", inputKey: "query", inputVal: "golang channels"},
		{tool: "write", inputKey: "filePath", inputVal: "/tmp/out.txt"},
		{tool: "edit", inputKey: "filePath", inputVal: "/tmp/in.txt"},
		{tool: "task", inputKey: "description", inputVal: "refactor"},
		{tool: "execute", inputKey: "command", inputVal: "make build"},
		{tool: "skill", inputKey: "name", inputVal: "commit"},
	}

	for _, tt := range tests {
		s.Run(tt.tool, func() {
			input := map[string]any{tt.inputKey: tt.inputVal}
			part := types.ToolPart{Tool: tt.tool, State: types.ToolState{Input: input, Output: "out"}}

			gotInput, gotOutput := s.handler.parseToolPart(context.Background(), part, s.metrics)

			s.Equal(tt.inputVal, gotInput)
			s.Equal("out", gotOutput)
		})
	}
}

func (s *HarnessSuite) TestParseToolPart_GlobAndGrep() {
	tests := []struct {
		tool    string
		input   map[string]any
		wantIn  string
		wantOut string
	}{
		{tool: "glob", input: map[string]any{"pattern": "*.go", "path": "/src"}, wantIn: "*.go in /src", wantOut: "out"},
		{tool: "glob", input: map[string]any{"pattern": "*.go"}, wantIn: "*.go", wantOut: "out"},
		{tool: "grep", input: map[string]any{"pattern": "TODO", "path": "/src"}, wantIn: "TODO in /src", wantOut: "out"},
		{tool: "grep", input: map[string]any{"pattern": "TODO"}, wantIn: "TODO", wantOut: "out"},
	}

	for _, tt := range tests {
		s.Run(tt.tool+fmt.Sprintf("%v", tt.input), func() {
			part := types.ToolPart{Tool: tt.tool, State: types.ToolState{Input: tt.input, Output: tt.wantOut}}

			input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

			s.Equal(tt.wantIn, input)
			s.Equal(tt.wantOut, output)
		})
	}
}

func (s *HarnessSuite) TestParseToolPart_ApplyPatch() {
	part := types.ToolPart{Tool: "apply_patch", State: types.ToolState{
		Input: map[string]any{
			"files": []any{
				map[string]any{"filePath": "/a.go"},
				map[string]any{"filePath": "/b.go"},
				map[string]any{"filePath": 42},
				"not-a-map",
			},
		},
		Output: "out",
	}}

	input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

	s.Equal("/a.go, /b.go", input)
	s.Equal("out", output)
}

func (s *HarnessSuite) TestParseToolPart_ApplyPatch_MissingFiles() {
	part := types.ToolPart{Tool: "apply_patch", State: types.ToolState{
		Input:  map[string]any{"files": "not-a-list"},
		Output: "out",
	}}

	input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

	s.Empty(input)
	s.Equal("out", output)
}

func (s *HarnessSuite) TestParseToolPart_Todowrite() {
	part := types.ToolPart{Tool: "todowrite", State: types.ToolState{
		Input: map[string]any{
			"todos": []any{
				map[string]any{"content": "first"},
				map[string]any{"content": "second"},
				map[string]any{"content": 42},
				42,
			},
		},
		Output: "out",
	}}

	input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

	s.Equal("first, second", input)
	s.Equal("out", output)
}

func (s *HarnessSuite) TestParseToolPart_Todowrite_MissingTodos() {
	part := types.ToolPart{Tool: "todowrite", State: types.ToolState{
		Input:  map[string]any{"todos": 7},
		Output: "out",
	}}

	input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

	s.Empty(input)
	s.Equal("out", output)
}

func (s *HarnessSuite) TestParseToolPart_QuestionReturnsEmpty() {
	part := types.ToolPart{Tool: "question", State: types.ToolState{Input: map[string]any{"questions": []any{}}}}

	input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

	s.Empty(input)
	s.Empty(output)
}

func (s *HarnessSuite) TestParseToolPart_UnknownToolFallback() {
	part := types.ToolPart{Tool: "mystery", State: types.ToolState{
		Input:  map[string]any{"key": "value", "n": 3},
		Output: "out",
	}}

	input, output := s.handler.parseToolPart(context.Background(), part, s.metrics)

	s.Equal(fmt.Sprintf("%v", map[string]any{"key": "value", "n": 3}), input)
	s.Equal("out", output)
}

// ---------------------------------------------------------------------------
// parseQuestions
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParseQuestions_MissingOrMalformedInput() {
	tests := []struct {
		name  string
		input map[string]any
	}{
		{name: "missing key", input: map[string]any{}},
		{name: "not a list", input: map[string]any{"questions": "nope"}},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Nil(s.handler.parseQuestions(tt.input))
		})
	}
}

func (s *HarnessSuite) TestParseQuestions_SkipsNonMapEntries() {
	input := map[string]any{
		"questions": []any{"not-a-map", map[string]any{"question": "real one"}},
	}

	questions := s.handler.parseQuestions(input)

	s.Require().Len(questions, 1)
	s.Equal("real one", questions[0].Question)
	s.Empty(questions[0].Options)
}

func (s *HarnessSuite) TestParseQuestions_Full() {
	input := map[string]any{
		"questions": []any{
			map[string]any{
				"question": "Pick one",
				"header":   "HDR",
				"multiple": false,
				"options": []any{
					map[string]any{"label": "A", "description": "alpha"},
					map[string]any{"label": "B"},
					"not-a-map",
				},
			},
		},
	}

	questions := s.handler.parseQuestions(input)

	s.Require().Len(questions, 1)
	q := questions[0]
	s.Equal("Pick one", q.Question)
	s.Equal("HDR", q.Header)
	s.False(q.Multiple)
	s.Equal([]types.QuestionOption{
		{Label: "A", Description: "alpha"},
		{Label: "B", Description: ""},
	}, q.Options)
}

func (s *HarnessSuite) TestParseQuestions_MultipleFlagTrue() {
	input := map[string]any{
		"questions": []any{
			map[string]any{"question": "q", "multiple": true},
		},
	}

	questions := s.handler.parseQuestions(input)

	s.Require().Len(questions, 1)
	s.True(questions[0].Multiple)
}

// ---------------------------------------------------------------------------
// Parse — full stream integration
// ---------------------------------------------------------------------------

func (s *HarnessSuite) TestParse_MixedStream() {
	rec := s.parse(context.Background(),
		[]byte(`{not-json`),
		wire("step_start", json.RawMessage(`{}`)),
		wire("reasoning", json.RawMessage(`{"text":"deep thought"}`)),
		wire("text", json.RawMessage(`{"text":"here is the answer"}`)),
		wire("tool_use", s.toolPart("bash", map[string]any{"command": "go version"}, "go1.24")),
		wire("step_finish", json.RawMessage(`{"reason":"stop","tokens":{"total":5}}`)),
	)

	s.Empty(rec.actions[1:])
	s.Len(rec.thoughts, 2)                 // step_start + reasoning
	s.Equal("compacting", rec.thoughts[0]) // step_start
	s.Equal("deep thought", rec.thoughts[1])
	s.Equal([]string{"here is the answer", ""}, rec.responses) // text + step_finish

	actions := rec.actions
	s.Require().Len(actions, 1)
	s.Equal(agent_session_types.AgentAction{Name: "bash", Input: "go version", Output: "go1.24"}, actions[0])
}

func (s *HarnessSuite) TestParse_StreamOrderPreserved() {
	rec := s.parse(context.Background(),
		wire("text", json.RawMessage(`{"text":"a"}`)),
		wire("text", json.RawMessage(`{"text":"b"}`)),
		wire("text", json.RawMessage(`{"text":"c"}`)),
	)

	s.Equal([]string{"a", "b", "c"}, rec.responses)
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

func (s *HarnessSuite) TestParse_StepFinish_RecordsUsageMetrics() {
	part := `{"reason":"stop","cost":0.25,"tokens":{"total":11,"input":5,"output":3,"reasoning":2,"cache":{"read":1,"write":2}}}`
	rec := s.parse(context.Background(), wire("step_finish", json.RawMessage(part)))
	s.Require().Equal([]string{""}, rec.responses)

	ctx := context.Background()
	metrics := s.collectMetrics(ctx)

	tokenUsage, ok := metrics[METRICS_NAME+".token.usage"]
	s.Require().True(ok)
	s.Equal(int64(5), s.sumInt64ByAttribute(tokenUsage, "type", "input"))
	s.Equal(int64(3), s.sumInt64ByAttribute(tokenUsage, "type", "output"))
	s.Equal(int64(2), s.sumInt64ByAttribute(tokenUsage, "type", "reasoning"))
	s.Equal(int64(1), s.sumInt64ByAttribute(tokenUsage, "type", "cacheRead"))
	s.Equal(int64(2), s.sumInt64ByAttribute(tokenUsage, "type", "cacheCreation"))

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
	if ok {
		s.Equal(int64(0), s.sumInt64(messageCount))
	}

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

func (s *HarnessSuite) TestParse_Text_IncrementsMessageCount() {
	rec := s.parse(context.Background(),
		wire("text", json.RawMessage(`{"text":"a"}`)),
		wire("text", json.RawMessage(`{"text":"b"}`)),
	)
	s.Require().Equal([]string{"a", "b"}, rec.responses)

	messageCount, ok := s.collectMetrics(context.Background())[METRICS_NAME+".message.count"]
	s.Require().True(ok)
	s.Equal(int64(2), s.sumInt64(messageCount))
}

func (s *HarnessSuite) TestParse_RetryIncrementsRetryCount() {
	rec := s.parse(context.Background(), wire("retry", json.RawMessage(`{}`)))
	s.Require().Equal([]string{"compacting"}, rec.thoughts)

	retryCount, ok := s.collectMetrics(context.Background())[METRICS_NAME+".retry.count"]
	s.Require().True(ok)
	s.Equal(int64(1), s.sumInt64ByAttribute(retryCount, "gen_ai.provider.name", "openai"))
}

func (s *HarnessSuite) TestParse_NilProviderConfigDoesNotPanic() {
	ch := make(chan []byte, 1)
	ch <- wire("text", json.RawMessage(`{"text":"hello"}`))
	close(ch)

	rec := &parseRecorder{}
	err := s.handler.Parse(context.Background(), &agent_session_interfaces.HarnessConfig{}, ch, "evt-1",
		rec.sendThought, rec.sendResponse, rec.sendAction, rec.sendElicitation, rec.sendServerInternalError)

	s.Require().NoError(err)
	s.Equal([]string{"hello"}, rec.responses)
}

// ---------------------------------------------------------------------------
// parseToolPart — tool timing metrics
// ---------------------------------------------------------------------------

func int64Ptr(v int64) *int64 { return &v }

func (s *HarnessSuite) TestParseToolPart_ToolTiming() {
	tests := []struct {
		name            string
		part            types.ToolPart
		preStart        map[string]int64
		wantStarts      map[string]int64
		wantToolCount   int64
		wantSuccess     string
		wantDurationOps uint64
	}{
		{
			name:       "started stores start time",
			part:       types.ToolPart{Tool: "bash", CallID: "c1", State: types.ToolState{Status: "running", Time: &types.ToolTime{Start: 100}}},
			wantStarts: map[string]int64{"c1": 100},
		},
		{
			name:            "finished with prior start records duration",
			part:            types.ToolPart{Tool: "bash", CallID: "c1", State: types.ToolState{Status: "done", Time: &types.ToolTime{End: int64Ptr(250)}}},
			preStart:        map[string]int64{"c1": 100},
			wantStarts:      map[string]int64{},
			wantToolCount:   1,
			wantSuccess:     "true",
			wantDurationOps: 1,
		},
		{
			name:          "finished without start records count only",
			part:          types.ToolPart{Tool: "bash", CallID: "c2", State: types.ToolState{Status: "done", Time: &types.ToolTime{End: int64Ptr(250)}}},
			wantStarts:    map[string]int64{},
			wantToolCount: 1,
			wantSuccess:   "true",
		},
		{
			name:          "error status marks failure",
			part:          types.ToolPart{Tool: "bash", CallID: "c3", State: types.ToolState{Status: "error", Error: "boom", Time: &types.ToolTime{End: int64Ptr(250)}}},
			wantStarts:    map[string]int64{},
			wantToolCount: 1,
			wantSuccess:   "false",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupMetrics()
			s.metrics.ToolStarts = make(map[string]int64)
			for callID, start := range tt.preStart {
				s.metrics.ToolStarts[callID] = start
			}

			input, output := s.handler.parseToolPart(context.Background(), tt.part, s.metrics)

			s.Empty(input)
			s.Empty(output)
			s.Equal(tt.wantStarts, s.metrics.ToolStarts)

			metrics := s.collectMetrics(context.Background())

			if tt.wantToolCount == 0 {
				_, ok := metrics[METRICS_NAME+".tool.count"]
				s.False(ok)
				return
			}

			toolCount, ok := metrics[METRICS_NAME+".tool.count"]
			s.Require().True(ok)
			s.Equal(tt.wantToolCount, s.sumInt64ByBoolAttribute(toolCount, "success", tt.wantSuccess == "true"))

			duration, ok := metrics[METRICS_NAME+".tool.duration"]
			if !ok {
				s.Zero(tt.wantDurationOps, "expected tool.duration to be recorded")
				return
			}
			s.Equal(tt.wantDurationOps, s.histogramFloat64Count(duration))
		})
	}
}

// compile-time interface check
var _ agent_session_interfaces.HandlerHarness = (*HarnessHandler)(nil)
