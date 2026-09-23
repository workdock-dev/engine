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
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/codex/types"
)

type HarnessSuite struct {
	suite.Suite
	handler *HarnessHandler
}

func TestHarnessSuite(t *testing.T) {
	suite.Run(t, new(HarnessSuite))
}

func (s *HarnessSuite) SetupTest() {
	s.handler = NewHarnessHandler(types.Config{
		Version:        "0.1.0",
		Model:          "gpt-5.6-sol",
		ReasoningEffort: "medium",
	}).(*HarnessHandler)
}

func (s *HarnessSuite) TestNewHarnessHandler() {
	var handler agent_session_interfaces.HandlerHarness = s.handler
	s.NotNil(handler)
}

func (s *HarnessSuite) TestGetConfigurationCommands() {
	commands := s.handler.GetConfigurationCommands()

	s.Require().Len(commands, 1)
	s.NotContains(commands[0], "VERSION_ARG")
	s.Contains(commands[0], "0.1.0")
}

func (s *HarnessSuite) TestGetConfigFileForcesChatGPTFileCredentials() {
	path, data, err := s.handler.GetConfigFile(&agent_session_interfaces.HarnessConfig{})

	s.Require().NoError(err)
	s.Equal(CONFIG_FILE_PATH, path)
	s.Contains(string(data), `cli_auth_credentials_store = "file"`)
	s.Contains(string(data), `forced_login_method = "chatgpt"`)
	s.Contains(string(data), `sandbox_mode = "danger-full-access"`)
	s.Contains(string(data), `approval_policy = "never"`)
	s.Contains(string(data), `unified_exec = false`)
	s.Contains(string(data), `model = "gpt-5.6-sol"`)
	s.Contains(string(data), `model_reasoning_effort = "medium"`)
}

func (s *HarnessSuite) TestGetConfigFileConfiguresMCPs() {
	_, data, err := s.handler.GetConfigFile(&agent_session_interfaces.HarnessConfig{Mcps: []agent_session_interfaces.MCPConfig{
		{
			Name:             "workdock",
			Url:              "https://mcp.example.com",
			AuthSecretEnvVar: "MCP_TOKEN",
		},
		{
			Name:            "custom-header",
			Url:             "https://custom.example.com",
			AuthHeaderKey:   "X-API-Key",
			AuthSecretEnvVar: "CUSTOM_TOKEN",
		},
	}})

	s.Require().NoError(err)
	config := string(data)
	s.Contains(config, `[mcp_servers."workdock"]`)
	s.Contains(config, `url = "https://mcp.example.com"`)
	s.Contains(config, `bearer_token_env_var = "MCP_TOKEN"`)
	s.Contains(config, `[mcp_servers."custom-header"]`)
	s.Contains(config, `env_http_headers = { "X-API-Key" = "CUSTOM_TOKEN" }`)
	s.False(strings.Contains(config, "{env:"))
}

func (s *HarnessSuite) TestGetAuthenticationUsesConfiguredSecret() {
	s.handler.config.AuthJson = `{"tokens":"credential"}`
	authentication, required := s.handler.GetAuthentication(&agent_session_types.Session{})

	s.True(required)
	s.Require().NotNil(authentication)
	s.Equal(AUTH_FILE_PATH, authentication.CredentialFilePath)
	s.Equal([]byte(`{"tokens":"credential"}`), authentication.Credential)
}

func (s *HarnessSuite) TestGetAuthenticationIsAlwaysRequired() {
	authentication, required := s.handler.GetAuthentication(nil)
	s.Nil(authentication)
	s.True(required)
}

func (s *HarnessSuite) TestRunCommandUsesCodexHomeAndNoAPIKey() {
	command := s.handler.RunCommand()

	s.Contains(command, "CODEX_HOME="+CODEX_HOME)
	s.Contains(command, "PATH=/home/${USER}/.local/bin:$PATH")
	s.Contains(command, "codex exec --json")
	s.Contains(command, "--skip-git-repo-check")
	s.Contains(command, "resume --last")
	s.Contains(command, "sed '/^Reading prompt from stdin\\.\\.\\.$/d'")
	s.NotContains(command, "--full-auto")
	s.NotContains(command, "OPENAI_API_KEY")
}

func (s *HarnessSuite) TestParseForwardsCompletedItems() {
	parts := make(chan []byte, 3)
	parts <- []byte(`{"type":"item.completed","item":{"type":"reasoning","text":"thinking"}}`)
	parts <- []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}`)
	parts <- []byte(`{"type":"item.completed","item":{"type":"command_execution","command":"git status","aggregated_output":"clean"}}`)
	close(parts)

	var thoughts []string
	var responses []string
	var actions []agent_session_types.AgentAction

	err := s.handler.Parse(
		context.Background(),
		&agent_session_interfaces.HarnessConfig{},
		parts,
		"evt-1",
		func(ctx context.Context, text string) error { thoughts = append(thoughts, text); return nil },
		func(ctx context.Context, text string) error { responses = append(responses, text); return nil },
		func(ctx context.Context, action agent_session_types.AgentAction) error { actions = append(actions, action); return nil },
		func(ctx context.Context, elicitation agent_session_types.AgentElicitation) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	s.Require().NoError(err)
	s.Equal([]string{"thinking"}, thoughts)
	s.Equal([]string{"answer"}, responses)
	s.Equal([]agent_session_types.AgentAction{{Name: "command_execution", Input: "git status", Output: "clean"}}, actions)
}

func (s *HarnessSuite) TestParseReportsFailure() {
	parts := make(chan []byte, 1)
	parts <- []byte(`{"type":"turn.failed"}`)
	close(parts)

	failures := 0
	err := s.handler.Parse(
		context.Background(),
		&agent_session_interfaces.HarnessConfig{},
		parts,
		"evt-1",
		func(context.Context, string) error { return nil },
		func(context.Context, string) error { return nil },
		func(context.Context, agent_session_types.AgentAction) error { return nil },
		func(context.Context, agent_session_types.AgentElicitation) error { return nil },
		func(context.Context) error { failures++; return nil },
	)

	s.Require().NoError(err)
	s.Equal(1, failures)
}

func (s *HarnessSuite) TestParseCompletesTurn() {
	parts := make(chan []byte, 1)
	parts <- []byte(`{"type":"turn.completed","usage":{"input_tokens":1}}`)
	close(parts)

	err := s.handler.Parse(
		context.Background(),
		&agent_session_interfaces.HarnessConfig{},
		parts,
		"evt-1",
		func(context.Context, string) error { return nil },
		func(context.Context, string) error { return nil },
		func(context.Context, agent_session_types.AgentAction) error { return nil },
		func(context.Context, agent_session_types.AgentElicitation) error { return nil },
		func(context.Context) error { return nil },
	)

	s.NoError(err)
}
