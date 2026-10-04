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

package daytona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/plug-ins/daytona/types"
)

type MCPSuite struct {
	suite.Suite
}

func TestMCPSuite(t *testing.T) {
	suite.Run(t, new(MCPSuite))
}

func (s *MCPSuite) TestAPIKeyAuthentication() {
	for _, test := range []struct {
		name   string
		apiKey string
		header string
		status int
	}{
		{name: "missing", apiKey: "secret", status: http.StatusUnauthorized},
		{name: "invalid", apiKey: "secret", header: "Bearer wrong", status: http.StatusUnauthorized},
		{name: "wrong scheme", apiKey: "secret", header: "Basic secret", status: http.StatusUnauthorized},
		{name: "empty configuration", header: "Bearer secret", status: http.StatusUnauthorized},
		{name: "valid", apiKey: "secret", header: "Bearer secret", status: http.StatusNoContent},
	} {
		s.Run(test.name, func() {
			h := NewMCPServer(types.Config{MCPApiKey: test.apiKey, MCPTokenLookup: nil}, http.NewServeMux())
			called := false
			h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				s.Equal(true, r.Context().Value(types.MCPAuthenticatedKey{}))
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest("POST", "/mcp", nil)
			req.Header.Set("Authorization", test.header)
			res := httptest.NewRecorder()

			h.Handler().ServeHTTP(res, req)

			s.Equal(test.status, res.Code)
			s.Equal(test.status == http.StatusNoContent, called)
		})
	}
}

func (s *MCPSuite) TestGitHubURLValidationRejectsEmbeddedCredentialsAndOtherHosts() {
	s.True(isGitHubHTTPSURL("https://github.com/workdock-dev/engine.git"))
	for _, raw := range []string{
		"https://token@github.com/workdock-dev/engine.git",
		"http://github.com/workdock-dev/engine.git",
		"https://example.com/workdock-dev/engine.git",
		"git@github.com:workdock-dev/engine.git",
		"https://github.com:443/workdock-dev/engine.git",
		"https://github.com/workdock-dev/engine.git?token=secret",
		"https://github.com/workdock-dev/engine.git#secret",
		"https://github.com.attacker.example/workdock-dev/engine.git",
	} {
		s.False(isGitHubHTTPSURL(raw), raw)
	}
}

func (s *MCPSuite) TestToolCallRejectsUnassociatedExecutionBeforeDaytonaOperation() {
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(context.Context, string) (string, error) { return "stored", nil }}, http.NewServeMux())
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverSession, err := h.server.Connect(ctx, serverTransport, nil)
	s.Require().NoError(err)
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "workdock-mcp-test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	s.Require().NoError(err)
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	s.Require().NoError(err)
	registered := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		registered = append(registered, tool.Name)
		s.NotContains(tool.Name, "daytona")
		s.NotContains(tool.Description, "Daytona")
		s.NotContains(tool.Description, "Workdock")
		schemaData, err := json.Marshal(tool.InputSchema)
		s.Require().NoError(err)
		var schema struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
		}
		s.Require().NoError(json.Unmarshal(schemaData, &schema))
		s.Contains(schema.Required, "agentSessionId", tool.Name)
		s.Contains(schema.Required, "agentSessionToken", tool.Name)

		for _, prohibited := range []string{"password", "username", "token", "credentials", "config", "credentialHelper", "scope", "insecureSkipTLS"} {
			s.NotContains(schema.Properties, prohibited, tool.Name)
		}
	}
	s.ElementsMatch([]string{
		"git_clone", "git_status", "git_branches", "git_history",
		"git_create_branch", "git_checkout", "git_delete_branch",
		"git_add", "git_commit", "git_push", "git_pull",
		"git_init", "git_reset", "git_restore",
	}, registered)
	for _, prohibited := range []string{"git_get_config", "git_set_config", "git_authenticate", "git_configure_user", "git_remotes", "git_remote_get"} {
		s.NotContains(registered, prohibited)
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "git_status",
		Arguments: map[string]any{"agentSessionId": "not-active", "agentSessionToken": "stored", "path": "/workspace/repo"},
	})
	s.Require().NoError(err)
	s.True(result.IsError)
}

func (s *MCPSuite) TestExecutionSessionAuthenticationAndIsolation() {
	stored := map[string]string{"session-one": "token-one", "session-two": "token-two"}
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(_ context.Context, id string) (string, error) { return stored[id], nil }}, http.NewServeMux())
	first := &daytona.Sandbox{}
	second := &daytona.Sandbox{}
	s.Require().NoError(h.RegisterExecution("session-one", first, ""))
	s.Require().NoError(h.RegisterExecution("session-two", second, ""))

	authorized, err := h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one", Token: "token-one"})
	s.Require().NoError(err)
	s.Same(first, authorized.Sandbox)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "unknown", Token: "token-one"})
	s.Error(err)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one", Token: "wrong"})
	s.Error(err)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one"})
	s.Error(err)

	secondAuthorized, err := h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-two", Token: "token-two"})
	s.Require().NoError(err)
	s.Same(second, secondAuthorized.Sandbox)

	delete(stored, "session-one")
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one", Token: "token-one"})
	s.Error(err)
	h.RemoveExecution("session-two")
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-two", Token: "token-two"})
	s.Error(err)
}

func (s *MCPSuite) TestExecutionRequiresAPIKeyAuthentication() {
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(context.Context, string) (string, error) { return "token", nil }}, http.NewServeMux())
	s.Require().NoError(h.RegisterExecution("session", &daytona.Sandbox{}, ""))

	_, err := h.authorized(context.Background(), types.AgentSession{Id: "session", Token: "token"})

	s.ErrorContains(err, "API key authentication required")
}

func (s *MCPSuite) TestDuplicateExecutionDoesNotReplaceSandbox() {
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(context.Context, string) (string, error) { return "token", nil }}, http.NewServeMux())
	first := &daytona.Sandbox{}
	s.Require().NoError(h.RegisterExecution("session", first, ""))
	s.Error(h.RegisterExecution("session", &daytona.Sandbox{}, ""))

	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	execution, err := h.authorized(ctx, types.AgentSession{Id: "session", Token: "token"})

	s.Require().NoError(err)
	s.Same(first, execution.Sandbox)
}

func (s *MCPSuite) TestExecutionRegistrationRequiresSessionAndSandbox() {
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: nil}, http.NewServeMux())

	s.Error(h.RegisterExecution("", &daytona.Sandbox{}, ""))
	s.Error(h.RegisterExecution("session", nil, ""))
}

func (s *MCPSuite) TestAllGitToolsRejectInvalidExecutionBeforeDaytonaOperation() {
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(_ context.Context, id string) (string, error) {
		if id == "active" {
			return "token", nil
		}

		return "", nil
	}}, http.NewServeMux())
	s.Require().NoError(h.RegisterExecution("active", &daytona.Sandbox{}, ""))
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := h.server.Connect(ctx, serverTransport, nil)
	s.Require().NoError(err)
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "workdock-mcp-test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	s.Require().NoError(err)
	defer clientSession.Close()
	tools, err := clientSession.ListTools(ctx, nil)
	s.Require().NoError(err)

	for _, tool := range tools.Tools {
		s.Run(tool.Name, func() {
			for _, auth := range []types.AgentSession{
				{Id: "unknown", Token: "token"},
				{Id: "active", Token: "wrong"},
				{Id: "active"},
				{},
			} {
				arguments := map[string]any{
					"agentSessionId":    auth.Id,
					"agentSessionToken": auth.Token,
					"path":              "/workspace/repo",
				}

				switch tool.Name {
				case "git_clone":
					arguments["url"] = "https://github.com/workdock-dev/engine.git"
				case "git_create_branch", "git_checkout", "git_delete_branch":
					arguments["name"] = "feature"
				case "git_add", "git_restore":
					arguments["files"] = []string{"file.txt"}
				case "git_commit":
					arguments["message"] = "commit"
					arguments["author"] = "workdock"
					arguments["email"] = "no-reply@workdock.dev"
				}

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      tool.Name,
					Arguments: arguments,
				})

				s.Require().NoError(err)
				s.Require().NotNil(result)
				s.True(result.IsError)
				s.Require().NotEmpty(result.Content)
				s.Contains(result.Content[0].(*mcp.TextContent).Text, "active agent session required")
			}
		})
	}
}

func (s *MCPSuite) TestAuthenticatedCloneRejectsCredentialBearingURL() {
	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(context.Context, string) (string, error) { return "token", nil }}, http.NewServeMux())
	s.Require().NoError(h.RegisterExecution("session", &daytona.Sandbox{}, ""))
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)

	_, _, err := h.gitClone(ctx, nil, types.GitCloneInput{
		AgentSession: types.AgentSession{Id: "session", Token: "token"},
		URL:          "https://credential@github.com/workdock-dev/engine.git",
		Path:         "/workspace/repo",
	})

	s.ErrorContains(err, "without embedded credentials")
	s.NotContains(err.Error(), "credential@")
}

func (s *MCPSuite) TestTokenLookupFailureRejectsExecution() {
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)

	for _, lookup := range []func(context.Context, string) (string, error){
		nil,
		func(context.Context, string) (string, error) { return "", nil },
		func(context.Context, string) (string, error) { return "token", errors.New("database unavailable") },
	} {
		h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: lookup}, http.NewServeMux())
		s.Require().NoError(h.RegisterExecution("session", &daytona.Sandbox{}, ""))

		_, err := h.authorized(ctx, types.AgentSession{Id: "session", Token: "token"})

		s.ErrorContains(err, "active agent session required")
	}
}

func (s *MCPSuite) TestConcurrentExecutionIsolation() {
	stored := make(map[string]string)
	sandboxes := make(map[string]*daytona.Sandbox)

	for index := range 16 {
		id := fmt.Sprintf("session-%d", index)
		stored[id] = fmt.Sprintf("token-%d", index)
		sandboxes[id] = &daytona.Sandbox{}
	}

	h := NewMCPServer(types.Config{MCPApiKey: "api", MCPTokenLookup: func(_ context.Context, id string) (string, error) { return stored[id], nil }}, http.NewServeMux())
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	results := make(chan error, len(stored))
	var executions sync.WaitGroup

	for id, sandbox := range sandboxes {
		executions.Go(func() {
			if err := h.RegisterExecution(id, sandbox, ""); err != nil {
				results <- err
				return
			}

			execution, err := h.authorized(ctx, types.AgentSession{Id: id, Token: stored[id]})

			if err == nil && execution.Sandbox != sandbox {
				err = errors.New("execution received another session's sandbox")
			}

			h.RemoveExecution(id)
			results <- err
		})
	}

	executions.Wait()
	close(results)

	for err := range results {
		s.NoError(err)
	}

	s.Empty(h.sessions)
}
