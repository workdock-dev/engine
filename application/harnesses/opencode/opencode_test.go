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
	"testing"
	"time"

	"github.com/workdock-dev/engine/application"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel"
	otelmetricnoop "go.opentelemetry.io/otel/metric/noop"
	oteltracenoop "go.opentelemetry.io/otel/trace/noop"
	"gopkg.in/yaml.v3"
)

type OpenCodeSuite struct {
	suite.Suite
	sandbox *mockSandbox
	parts   *mockParts
	mockApp *application.App
}

func TestOpenCodeSuite(t *testing.T) {
	otel.SetTracerProvider(oteltracenoop.NewTracerProvider())
	otel.SetMeterProvider(otelmetricnoop.NewMeterProvider())
	suite.Run(t, new(OpenCodeSuite))
}

func (s *OpenCodeSuite) SetupTest() {
	s.sandbox = new(mockSandbox)
	s.parts = new(mockParts)
	s.mockApp, _ = application.New(application.Config{})
}

func (s *OpenCodeSuite) baseConfig() Config {
	return Config{
		ConfigExternal: ConfigExternal{
			Model: "anthropic/claude-3",
		},
		Parts:        s.parts,
		Sandbox:      s.sandbox,
		Session:      defaultSession(),
		SessionEvent: defaultSessionEvent(),
		Prompt:       "fix the bug",
		Secrets: map[string]string{
			"linearAccessToken": "lin_at_123",
		},
	}
}

func (s *OpenCodeSuite) newHarness(secrets map[string]string) *OpenCode {
	cfg := s.baseConfig()
	if secrets != nil {
		cfg.Secrets = secrets
	}
	h, _ := New(cfg, s.mockApp)
	return h
}

func (s *OpenCodeSuite) fullHappyPath(created bool, prMeta string) {
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(created, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	if !created {
		s.sandbox.On("UpdateExistingSandbox", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	}
	if created {
		s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
			return containsStr(cmd, "opencode.ai/install")
		}), time.Minute*2).Return("", nil)
		s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
			return containsStr(cmd, "githubcli-archive-keyring")
		}), time.Minute*5).Return("", nil)
		s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	}
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return(prMeta, nil)
}

// --- New() tests ---

func (s *OpenCodeSuite) TestNew_Success() {
	h := s.newHarness(nil)
	s.NotNil(h)
	s.Equal("lin_at_123", h.linearAccessToken)
	s.Equal("", h.githubAccessToken)
}

func (s *OpenCodeSuite) TestNew_MissingLinearAccessToken() {
	_, err := New(Config{
		ConfigExternal: ConfigExternal{},
		Parts:          s.parts,
		Sandbox:        s.sandbox,
		Session:        defaultSession(),
		SessionEvent:   defaultSessionEvent(),
		Secrets:        map[string]string{},
	}, s.mockApp)
	s.Error(err)
	s.Contains(err.Error(), "missing linear access token")
}

func (s *OpenCodeSuite) TestNew_WithGitHubToken() {
	h := s.newHarness(map[string]string{
		"linearAccessToken": "lin_at_123",
		"githubAccessToken": "ghp_abc",
	})
	s.Equal("ghp_abc", h.githubAccessToken)
}

// --- ConfigExternal yaml parsing tests ---

// Regression test: secret value and hosts used to be unexported struct
// fields, so yaml unmarshalling silently skipped them and every dynamic
// secret was created with an empty value and no host allowlist, making
// provider calls (e.g. ollama) fail with Unauthorized.
func (s *OpenCodeSuite) TestConfigExternal_SecretsYamlParsing() {
	yml := `
model: ollama/glm-5.1:cloud
permission:
  "external_directory": "deny"
  "*": "allow"
secrets:
  OLLAMA_API_KEY:
    value: ollama-secret-api-key
    hosts: ["ollama.com"]
  OPENROUTER_API_KEY:
    value: openrouter-secret-api-key
    hosts: ["*.openrouter.ai"]
provider:
  ollama:
    options:
      baseURL: https://ollama.com/v1
      apiKey: "{env:OLLAMA_API_KEY}"
    models:
      glm-5.1:cloud: {}
`
	var cfg ConfigExternal
	s.Require().NoError(yaml.Unmarshal([]byte(yml), &cfg))

	s.Equal("ollama/glm-5.1:cloud", cfg.Model)
	s.Len(cfg.Secrets, 2)

	ollama, ok := cfg.Secrets["OLLAMA_API_KEY"]
	s.Require().True(ok, "OLLAMA_API_KEY should be present")
	s.Equal("ollama-secret-api-key", ollama.Value, "secret value must be parsed from yaml")
	s.Equal([]string{"ollama.com"}, ollama.Hosts, "secret hosts must be parsed from yaml")

	openrouter, ok := cfg.Secrets["OPENROUTER_API_KEY"]
	s.Require().True(ok, "OPENROUTER_API_KEY should be present")
	s.Equal("openrouter-secret-api-key", openrouter.Value)
	s.Equal([]string{"*.openrouter.ai"}, openrouter.Hosts)
}

// --- Run() tests ---

func (s *OpenCodeSuite) TestRun_HappyPath() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
	s.Nil(result.PullRequest)
}

func (s *OpenCodeSuite) TestRun_CreateFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("", "", assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_CreateSandboxFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(false, assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_GitHubSecretFails() {
	h := s.newHarness(map[string]string{
		"linearAccessToken": "lin_at_123",
		"githubAccessToken": "ghp_abc",
	})
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("SetSecret", mock.Anything, "ghp_abc", []string{"api.github.com", "github.com"}).Return("", "", assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_DynamicSecretFails() {
	cfg := s.baseConfig()
	cfg.ConfigExternal.Secrets = map[string]SecretSpec{
		"MY_SECRET": {Value: "s3cret", Hosts: []string{"example.com"}},
	}
	h, _ := New(cfg, s.mockApp)

	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("SetSecret", mock.Anything, "s3cret", []string{"example.com"}).Return("", "", assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_StartFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_InstallOpenCodeFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", assert.AnError)
	s.sandbox.On("DeleteSandbox", mock.Anything).Return(nil)

	_, err := h.Run(context.Background())
	s.Error(err)
	s.sandbox.AssertCalled(s.T(), "DeleteSandbox", mock.Anything)
}

func (s *OpenCodeSuite) TestRun_InstallGitHubCLIFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", assert.AnError)
	s.sandbox.On("DeleteSandbox", mock.Anything).Return(nil)

	_, err := h.Run(context.Background())
	s.Error(err)
	s.sandbox.AssertCalled(s.T(), "DeleteSandbox", mock.Anything)
}

func (s *OpenCodeSuite) TestRun_ConfigureGitUserFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(assert.AnError)
	s.sandbox.On("DeleteSandbox", mock.Anything).Return(nil)

	_, err := h.Run(context.Background())
	s.Error(err)
	s.sandbox.AssertCalled(s.T(), "DeleteSandbox", mock.Anything)
}

func (s *OpenCodeSuite) TestRun_UploadConfigFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").
		Return(assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_GitHubAuthSetupFails() {
	h := s.newHarness(map[string]string{
		"linearAccessToken": "lin_at_123",
		"githubAccessToken": "ghp_abc",
	})
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("SetSecret", mock.Anything, "ghp_abc", []string{"api.github.com", "github.com"}).Return("sid-2", "gh-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, "gh auth setup-git", time.Minute).Return("", assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_SSLConfigFails() {
	h := s.newHarness(map[string]string{
		"linearAccessToken": "lin_at_123",
		"githubAccessToken": "ghp_abc",
	})
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("SetSecret", mock.Anything, "ghp_abc", []string{"api.github.com", "github.com"}).Return("sid-2", "gh-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, "gh auth setup-git", time.Minute).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, "git config --global http.sslCAInfo /etc/daytona/netleash/ca.crt", time.Minute).
		Return("", assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_UploadPromptFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_CreateExecutionSessionFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_ExecuteSessionCommandFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{}, assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_InvalidCmdID() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": 123}, nil)

	_, err := h.Run(context.Background())
	s.Error(err)
	s.Contains(err.Error(), "invalid pid type")
}

func (s *OpenCodeSuite) TestRun_WithGitHubToken_AllPaths() {
	h := s.newHarness(map[string]string{
		"linearAccessToken": "lin_at_123",
		"githubAccessToken": "ghp_abc",
	})

	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("SetSecret", mock.Anything, "ghp_abc", []string{"api.github.com", "github.com"}).Return("sid-2", "gh-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, "gh auth setup-git", time.Minute).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, "git config --global http.sslCAInfo /etc/daytona/netleash/ca.crt", time.Minute).Return("", nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("DeleteSecret", mock.Anything, "sid-1").Return(nil)
	s.sandbox.On("DeleteSecret", mock.Anything, "sid-2").Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return("", nil)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
	s.sandbox.AssertCalled(s.T(), "ExecuteCommand", mock.Anything, "gh auth setup-git", time.Minute)
	s.sandbox.AssertCalled(s.T(), "ExecuteCommand", mock.Anything, "git config --global http.sslCAInfo /etc/daytona/netleash/ca.crt", time.Minute)
}

func (s *OpenCodeSuite) TestRun_WithDynamicSecrets() {
	cfg := s.baseConfig()
	cfg.ConfigExternal.Secrets = map[string]SecretSpec{
		"CUSTOM_VAR": {Value: "val1", Hosts: []string{"api.example.com"}},
	}
	h, _ := New(cfg, s.mockApp)

	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("SetSecret", mock.Anything, "val1", []string{"api.example.com"}).Return("sid-2", "custom-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.MatchedBy(func(secrets map[string]string) bool {
		return secrets[LINEAR_ACCESS_TOKEN_ENV_VAR] == "linear-secret" && secrets["CUSTOM_VAR"] == "custom-secret"
	}), mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("DeleteSecret", mock.Anything, "sid-1").Return(nil)
	s.sandbox.On("DeleteSecret", mock.Anything, "sid-2").Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return("", nil)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_SandboxAlreadyExists() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(false, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("UpdateExistingSandbox", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return("", nil)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
	s.sandbox.AssertNotCalled(s.T(), "ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install") || containsStr(cmd, "githubcli-archive-keyring")
	}), mock.Anything)
	s.sandbox.AssertNotCalled(s.T(), "ConfigureGitUser", mock.Anything, mock.Anything, mock.Anything)
	s.sandbox.AssertCalled(s.T(), "UpdateExistingSandbox", mock.Anything, mock.Anything, mock.Anything)
}

func (s *OpenCodeSuite) TestRun_NewSandboxDoesNotCallUpdateExisting() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "")

	_, err := h.Run(context.Background())
	s.NoError(err)
	s.sandbox.AssertNotCalled(s.T(), "UpdateExistingSandbox")
}

func (s *OpenCodeSuite) TestRun_UpdateExistingSandboxFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(false, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("UpdateExistingSandbox", mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)

	_, err := h.Run(context.Background())
	s.Error(err)
}

func (s *OpenCodeSuite) TestRun_ModelWithProviderSlash() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_ModelWithoutSlash() {
	cfg := s.baseConfig()
	cfg.ConfigExternal.Model = "gpt-4"
	h, _ := New(cfg, s.mockApp)
	s.fullHappyPath(true, "")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_EmptyModel() {
	cfg := s.baseConfig()
	cfg.ConfigExternal.Model = ""
	h, _ := New(cfg, s.mockApp)
	s.fullHappyPath(true, "")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_WithPRMetadata() {
	h := s.newHarness(nil)
	prJSON := `{"number":42,"url":"https://github.com/org/repo/pull/42","headRefName":"fix-bug","headRefOid":"abc123"}`
	s.fullHappyPath(true, prJSON)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
	s.NotNil(result.PullRequest)
	s.Equal(42, result.PullRequest.Number)
	s.Equal("https://github.com/org/repo/pull/42", result.PullRequest.URL)
	s.Equal("fix-bug", result.PullRequest.HeadRefName)
	s.Equal("abc123", result.PullRequest.HeadRefOID)
}

func (s *OpenCodeSuite) TestRun_PRMetadataEmpty() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
	s.Nil(result.PullRequest)
}

func (s *OpenCodeSuite) TestRun_PRMetadataInvalidJSON() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "not json {{{")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_PRMetadataContextCanceled() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.Run(ctx)
	_ = err
	s.sandbox.AssertNotCalled(s.T(), "ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, mock.Anything)
}

func (s *OpenCodeSuite) TestRun_WithoutDynamicSecrets() {
	cfg := s.baseConfig()
	cfg.ConfigExternal.Secrets = nil
	h, _ := New(cfg, s.mockApp)
	s.fullHappyPath(true, "")

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

// --- Dispose() tests ---

func (s *OpenCodeSuite) TestDispose_DestroyOnDispose() {
	cfg := s.baseConfig()
	cfg.DestroyOnDispose = true
	h, _ := New(cfg, s.mockApp)
	h.secretIds = []string{"sid-1", "sid-2"}

	ctx := context.Background()
	s.sandbox.On("DeleteExecutionSession", ctx).Return(nil)
	s.sandbox.On("DeleteSandbox", ctx).Return(nil)
	s.sandbox.On("DeleteSecret", ctx, "sid-1").Return(nil)
	s.sandbox.On("DeleteSecret", ctx, "sid-2").Return(nil)

	err := h.Dispose(ctx)
	s.NoError(err)
	s.sandbox.AssertCalled(s.T(), "DeleteSandbox", ctx)
	s.sandbox.AssertNotCalled(s.T(), "Shutdown", ctx)
}

func (s *OpenCodeSuite) TestDispose_Shutdown() {
	cfg := s.baseConfig()
	cfg.DestroyOnDispose = false
	h, _ := New(cfg, s.mockApp)
	h.secretIds = []string{"sid-1"}

	ctx := context.Background()
	s.sandbox.On("DeleteExecutionSession", ctx).Return(nil)
	s.sandbox.On("Shutdown", ctx).Return(nil)
	s.sandbox.On("DeleteSecret", ctx, "sid-1").Return(nil)

	err := h.Dispose(ctx)
	s.NoError(err)
	s.sandbox.AssertCalled(s.T(), "Shutdown", ctx)
	s.sandbox.AssertNotCalled(s.T(), "DeleteSandbox", ctx)
}

func (s *OpenCodeSuite) TestDispose_DeletesSecrets() {
	cfg := s.baseConfig()
	h, _ := New(cfg, s.mockApp)
	h.secretIds = []string{"sid-a", "sid-b", "sid-c"}

	ctx := context.Background()
	s.sandbox.On("DeleteExecutionSession", ctx).Return(nil)
	s.sandbox.On("Shutdown", ctx).Return(nil)
	s.sandbox.On("DeleteSecret", ctx, "sid-a").Return(nil)
	s.sandbox.On("DeleteSecret", ctx, "sid-b").Return(nil)
	s.sandbox.On("DeleteSecret", ctx, "sid-c").Return(nil)

	err := h.Dispose(ctx)
	s.NoError(err)
	s.sandbox.AssertCalled(s.T(), "DeleteSecret", ctx, "sid-a")
	s.sandbox.AssertCalled(s.T(), "DeleteSecret", ctx, "sid-b")
	s.sandbox.AssertCalled(s.T(), "DeleteSecret", ctx, "sid-c")
}

func (s *OpenCodeSuite) TestRun_StreamSessionCommandLogsFails() {
	h := s.newHarness(nil)
	s.fullHappyPath(true, "")
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(assert.AnError)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_Unhealthy() {
	cfg := s.baseConfig()
	cfg.LivenessTimeoutSecs = 1
	cfg.MaxHealthMisses = 1
	h, _ := New(cfg, s.mockApp)

	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			time.Sleep(3 * time.Second)
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return("", nil)

	result, err := h.Run(context.Background())
	s.ErrorIs(err, types.ErrHarnessUnhealthy)
	s.Nil(result)
}

func (s *OpenCodeSuite) TestRun_UploadOpenCodeConfigWithProvider() {
	cfg := s.baseConfig()
	cfg.ConfigExternal.Provider = map[string]any{"openai": map[string]any{"apiKey": "sk-123"}}
	h, _ := New(cfg, s.mockApp)

	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return("", nil)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
}

func (s *OpenCodeSuite) TestRun_PRMetadataFails() {
	h := s.newHarness(nil)
	s.sandbox.On("SetSecret", mock.Anything, "lin_at_123", []string{"mcp.linear.app"}).Return("sid-1", "linear-secret", nil)
	s.sandbox.On("GetOrCreateSandbox", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	s.sandbox.On("Start", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "opencode.ai/install")
	}), time.Minute*2).Return("", nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, mock.MatchedBy(func(cmd string) bool {
		return containsStr(cmd, "githubcli-archive-keyring")
	}), time.Minute*5).Return("", nil)
	s.sandbox.On("ConfigureGitUser", mock.Anything, "workdock[bot]", "no-reply@workdock.dev").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/home/daytona/.config/opencode/opencode.json").Return(nil)
	s.sandbox.On("UploadFile", mock.Anything, mock.AnythingOfType("[]uint8"), "/tmp/prompt.txt").Return(nil)
	s.sandbox.On("CreateExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteSessionCommand", mock.Anything, mock.AnythingOfType("string")).Return(map[string]any{"id": "cmd-1"}, nil)
	s.sandbox.On("StreamSessionCommandLogs", mock.Anything, "cmd-1", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(args.Get(2).(chan<- string))
			close(args.Get(3).(chan<- string))
		}).Return(nil)
	s.sandbox.On("DeleteExecutionSession", mock.Anything).Return(nil)
	s.sandbox.On("ExecuteCommand", mock.Anything, GITHUB_GET_PR_META, time.Minute*2).Return("", assert.AnError)

	result, err := h.Run(context.Background())
	s.NoError(err)
	s.NotNil(result)
	s.Nil(result.PullRequest)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
