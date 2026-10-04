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
			h := NewMCPServer(test.apiKey, nil)
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
	h := NewMCPServer("api", func(context.Context, string) (string, error) { return "stored", nil })
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
		"daytona_git_clone", "daytona_git_status", "daytona_git_branches", "daytona_git_history",
		"daytona_git_create_branch", "daytona_git_checkout", "daytona_git_delete_branch",
		"daytona_git_add", "daytona_git_commit", "daytona_git_push", "daytona_git_pull",
		"daytona_git_init", "daytona_git_reset", "daytona_git_restore",
	}, registered)
	for _, prohibited := range []string{"daytona_git_get_config", "daytona_git_set_config", "daytona_git_authenticate", "daytona_git_configure_user", "daytona_git_remotes", "daytona_git_remote_get"} {
		s.NotContains(registered, prohibited)
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "daytona_git_status",
		Arguments: map[string]any{"agentSessionId": "not-active", "agentSessionToken": "stored", "path": "/workspace/repo"},
	})
	s.Require().NoError(err)
	s.True(result.IsError)
}

func (s *MCPSuite) TestExecutionSessionAuthenticationAndIsolation() {
	stored := map[string]string{"session-one": "token-one", "session-two": "token-two"}
	h := NewMCPServer("api", func(_ context.Context, id string) (string, error) { return stored[id], nil })
	first := &daytona.Sandbox{}
	second := &daytona.Sandbox{}
	s.Require().NoError(h.RegisterExecution("session-one", first, ""))
	s.Require().NoError(h.RegisterExecution("session-two", second, ""))

	authorized, err := h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "session-one", AgentSessionToken: "token-one"})
	s.Require().NoError(err)
	s.Same(first, authorized.Sandbox)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "unknown", AgentSessionToken: "token-one"})
	s.Error(err)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "session-one", AgentSessionToken: "wrong"})
	s.Error(err)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "session-one"})
	s.Error(err)

	secondAuthorized, err := h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "session-two", AgentSessionToken: "token-two"})
	s.Require().NoError(err)
	s.Same(second, secondAuthorized.Sandbox)

	delete(stored, "session-one")
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "session-one", AgentSessionToken: "token-one"})
	s.Error(err)
	h.RemoveExecution("session-two")
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.MCPAuth{AgentSessionID: "session-two", AgentSessionToken: "token-two"})
	s.Error(err)
}

func (s *MCPSuite) TestExecutionRequiresAPIKeyAuthentication() {
	h := NewMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })
	s.Require().NoError(h.RegisterExecution("session", &daytona.Sandbox{}, ""))

	_, err := h.authorized(context.Background(), types.MCPAuth{AgentSessionID: "session", AgentSessionToken: "token"})

	s.ErrorContains(err, "API key authentication required")
}

func (s *MCPSuite) TestDuplicateExecutionDoesNotReplaceSandbox() {
	h := NewMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })
	first := &daytona.Sandbox{}
	s.Require().NoError(h.RegisterExecution("session", first, ""))
	s.Error(h.RegisterExecution("session", &daytona.Sandbox{}, ""))

	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	execution, err := h.authorized(ctx, types.MCPAuth{AgentSessionID: "session", AgentSessionToken: "token"})

	s.Require().NoError(err)
	s.Same(first, execution.Sandbox)
}

func (s *MCPSuite) TestExecutionRegistrationRequiresSessionAndSandbox() {
	h := NewMCPServer("api", nil)

	s.Error(h.RegisterExecution("", &daytona.Sandbox{}, ""))
	s.Error(h.RegisterExecution("session", nil, ""))
}

func (s *MCPSuite) TestAllGitToolsRejectInvalidExecutionBeforeDaytonaOperation() {
	h := NewMCPServer("api", func(_ context.Context, id string) (string, error) {
		if id == "active" {
			return "token", nil
		}

		return "", nil
	})
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
			for _, auth := range []types.MCPAuth{
				{AgentSessionID: "unknown", AgentSessionToken: "token"},
				{AgentSessionID: "active", AgentSessionToken: "wrong"},
				{AgentSessionID: "active"},
				{},
			} {
				arguments := map[string]any{
					"agentSessionId":    auth.AgentSessionID,
					"agentSessionToken": auth.AgentSessionToken,
					"path":              "/workspace/repo",
				}

				switch tool.Name {
				case "daytona_git_clone":
					arguments["url"] = "https://github.com/workdock-dev/engine.git"
				case "daytona_git_create_branch", "daytona_git_checkout", "daytona_git_delete_branch":
					arguments["name"] = "feature"
				case "daytona_git_add", "daytona_git_restore":
					arguments["files"] = []string{"file.txt"}
				case "daytona_git_commit":
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
				s.Contains(result.Content[0].(*mcp.TextContent).Text, "active Workdock agent session required")
			}
		})
	}
}

func (s *MCPSuite) TestAuthenticatedCloneRejectsCredentialBearingURL() {
	h := NewMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })
	s.Require().NoError(h.RegisterExecution("session", &daytona.Sandbox{}, ""))
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)

	_, _, err := h.gitClone(ctx, nil, types.GitCloneInput{
		MCPAuth: types.MCPAuth{AgentSessionID: "session", AgentSessionToken: "token"},
		URL:     "https://credential@github.com/workdock-dev/engine.git",
		Path:    "/workspace/repo",
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
		h := NewMCPServer("api", lookup)
		s.Require().NoError(h.RegisterExecution("session", &daytona.Sandbox{}, ""))

		_, err := h.authorized(ctx, types.MCPAuth{AgentSessionID: "session", AgentSessionToken: "token"})

		s.ErrorContains(err, "active Workdock agent session required")
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

	h := NewMCPServer("api", func(_ context.Context, id string) (string, error) { return stored[id], nil })
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	results := make(chan error, len(stored))
	var executions sync.WaitGroup

	for id, sandbox := range sandboxes {
		executions.Go(func() {
			if err := h.RegisterExecution(id, sandbox, ""); err != nil {
				results <- err
				return
			}

			execution, err := h.authorized(ctx, types.MCPAuth{AgentSessionID: id, AgentSessionToken: stored[id]})

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
