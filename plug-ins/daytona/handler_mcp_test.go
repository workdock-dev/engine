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
	"testing"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	agentTypes "github.com/workdock-dev/engine/features/agent_session/types"
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
			h := newTestMCPServer(test.apiKey, nil)
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
	h := newTestMCPServer("api", func(context.Context, string) (string, error) { return "stored", nil })
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
	s.ElementsMatch([]string{"git_clone", "git_push", "git_pull"}, registered)
	for _, prohibited := range []string{"git_status", "git_branches", "git_history", "git_create_branch", "git_checkout", "git_delete_branch", "git_add", "git_commit", "git_init", "git_reset", "git_restore", "git_get_config", "git_set_config", "git_authenticate", "git_configure_user", "git_remotes", "git_remote_get"} {
		s.NotContains(registered, prohibited)
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "git_push",
		Arguments: map[string]any{"agentSessionId": "not-active", "agentSessionToken": "stored", "path": "/workspace/repo"},
	})
	s.Require().NoError(err)
	s.True(result.IsError)
}

func (s *MCPSuite) TestExecutionSessionAuthenticationAndIsolation() {
	stored := map[string]string{"session-one": "token-one", "session-two": "token-two"}
	h := newTestMCPServer("api", func(_ context.Context, id string) (string, error) { return stored[id], nil })

	authorized, err := h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one", Token: "token-one"})
	s.Require().NoError(err)
	s.NotNil(authorized)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "unknown", Token: "token-one"})
	s.Error(err)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one", Token: "wrong"})
	s.Error(err)
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one"})
	s.Error(err)

	secondAuthorized, err := h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-two", Token: "token-two"})
	s.Require().NoError(err)
	s.NotNil(secondAuthorized)

	delete(stored, "session-one")
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-one", Token: "token-one"})
	s.Error(err)
	delete(stored, "session-two")
	_, err = h.authorized(context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true), types.AgentSession{Id: "session-two", Token: "token-two"})
	s.Error(err)
}

func (s *MCPSuite) TestExecutionRequiresAPIKeyAuthentication() {
	h := newTestMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })

	_, err := h.authorized(context.Background(), types.AgentSession{Id: "session", Token: "token"})

	s.ErrorContains(err, "API key authentication required")
}

func (s *MCPSuite) TestAllGitToolsRejectInvalidExecutionBeforeDaytonaOperation() {
	h := newTestMCPServer("api", func(_ context.Context, id string) (string, error) {
		if id == "active" {
			return "token", nil
		}

		return "", nil
	})
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
	h := newTestMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })
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
		h := newTestMCPServer("api", lookup)

		_, err := h.authorized(ctx, types.AgentSession{Id: "session", Token: "token"})

		s.ErrorContains(err, "active agent session required")
	}
}

type mcpGitHandler struct {
	interfaces.HandlerGit
	getAccess func(context.Context, *agentTypes.GitConnection) (*interfaces.GitAccess, error)
}

func (h *mcpGitHandler) GetGitAccess(ctx context.Context, connection *agentTypes.GitConnection) (*interfaces.GitAccess, error) {
	return h.getAccess(ctx, connection)
}

func (s *MCPSuite) TestGitAccessIsRefreshedForEveryInvocation() {
	h := NewMCPServer(types.Config{}, http.NewServeMux())
	connection := &agentTypes.GitConnection{RepoFullName: "workdock-dev/engine"}
	calls := 0
	gitHandler := &mcpGitHandler{getAccess: func(_ context.Context, got *agentTypes.GitConnection) (*interfaces.GitAccess, error) {
		s.Same(connection, got)
		calls++
		return &interfaces.GitAccess{Granted: true, Secret: fmt.Sprintf("fresh-%d", calls)}, nil
	}}
	execution := &types.MCPExecution{GitHandler: gitHandler, GitConnection: connection}

	first, err := h.gitAccess(context.Background(), execution)
	s.Require().NoError(err)
	second, err := h.gitAccess(context.Background(), execution)
	s.Require().NoError(err)

	s.Equal("fresh-1", first.Secret)
	s.Equal("fresh-2", second.Secret)
	s.Equal(2, calls)
}

func (s *MCPSuite) TestAllRemoteToolsRejectMissingOrDeniedGitAccess() {
	for _, test := range []struct {
		name   string
		access *interfaces.GitAccess
		err    error
	}{
		{name: "missing access"},
		{name: "denied", access: &interfaces.GitAccess{Secret: "private"}},
		{name: "missing token", access: &interfaces.GitAccess{Granted: true}},
		{name: "handler error", err: errors.New("private handler details")},
	} {
		s.Run(test.name, func() {
			h := newTestMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })
			calls := 0
			gitHandler := &mcpGitHandler{getAccess: func(context.Context, *agentTypes.GitConnection) (*interfaces.GitAccess, error) {
				calls++
				return test.access, test.err
			}}
			h.Configure(&interfaces.SandboxMCPConfig{
				TokenLookup: func(context.Context, string) (string, error) { return "token", nil },
				GitLookup: func(context.Context, string) (interfaces.HandlerGit, *agentTypes.GitConnection, error) {
					return gitHandler, &agentTypes.GitConnection{}, nil
				},
			})
			ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
			auth := types.AgentSession{Id: "session", Token: "token"}

			_, _, cloneErr := h.gitClone(ctx, nil, types.GitCloneInput{AgentSession: auth, URL: "https://github.com/workdock-dev/engine.git", Path: "/workspace"})
			_, _, pushErr := h.gitPush(ctx, nil, types.GitPushInput{AgentSession: auth, Path: "/workspace"})
			_, _, pullErr := h.gitPull(ctx, nil, types.GitPullInput{AgentSession: auth, Path: "/workspace"})

			for _, err := range []error{cloneErr, pushErr, pullErr} {
				s.Require().Error(err)
				s.NotContains(err.Error(), "private")
			}
			s.Equal(3, calls)
		})
	}
}

func (s *MCPSuite) TestMissingGitHandlerOrConnectionRejectsAccess() {
	h := NewMCPServer(types.Config{}, http.NewServeMux())

	for _, execution := range []*types.MCPExecution{
		{},
		{GitHandler: &mcpGitHandler{}},
		{GitConnection: &agentTypes.GitConnection{}},
	} {
		_, err := h.gitAccess(context.Background(), execution)
		s.ErrorContains(err, "Git access required")
	}
}

func (s *MCPSuite) TestRejectedSessionNeverRequestsGitAccess() {
	calls := 0
	gitHandler := &mcpGitHandler{getAccess: func(context.Context, *agentTypes.GitConnection) (*interfaces.GitAccess, error) {
		calls++
		return &interfaces.GitAccess{Granted: true, Secret: "private"}, nil
	}}
	h := newTestMCPServer("api", func(context.Context, string) (string, error) { return "token", nil })
	h.Configure(&interfaces.SandboxMCPConfig{
		TokenLookup: func(context.Context, string) (string, error) { return "token", nil },
		GitLookup: func(context.Context, string) (interfaces.HandlerGit, *agentTypes.GitConnection, error) {
			return gitHandler, &agentTypes.GitConnection{}, nil
		},
	})

	for _, ctx := range []context.Context{
		context.Background(),
		context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true),
	} {
		_, _, err := h.gitClone(ctx, nil, types.GitCloneInput{
			AgentSession: types.AgentSession{Id: "session", Token: "wrong"},
			URL:          "https://github.com/workdock-dev/engine.git",
			Path:         "/workspace",
		})
		s.Error(err)
	}

	s.Zero(calls)
}

func newTestMCPServer(apiKey string, lookup func(context.Context, string) (string, error)) *MCPServer {
	h := NewMCPServer(types.Config{MCPApiKey: apiKey}, http.NewServeMux())
	h.Configure(&interfaces.SandboxMCPConfig{
		TokenLookup: lookup,
		GitLookup: func(context.Context, string) (interfaces.HandlerGit, *agentTypes.GitConnection, error) {
			return nil, nil, nil
		},
	})

	return h
}

func (s *MCPSuite) TestReplicasAuthorizeSharedSessionWithoutLocalRegistration() {
	stored := map[string]string{"first": "first-token", "second": "second-token"}
	config := &interfaces.SandboxMCPConfig{
		TokenLookup: func(_ context.Context, id string) (string, error) { return stored[id], nil },
		GitLookup: func(_ context.Context, id string) (interfaces.HandlerGit, *agentTypes.GitConnection, error) {
			return nil, &agentTypes.GitConnection{RepoFullName: id}, nil
		},
	}
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)

	for range 2 {
		replica := NewMCPServer(types.Config{MCPApiKey: "api"}, http.NewServeMux())
		replica.Configure(config)

		for id, token := range stored {
			execution, err := replica.authorized(ctx, types.AgentSession{Id: id, Token: token})
			s.Require().NoError(err)
			s.Equal(id, execution.GitConnection.RepoFullName)
			_, err = replica.authorized(ctx, types.AgentSession{Id: id, Token: "other-token"})
			s.Error(err)
		}
	}
}

func (s *MCPSuite) TestEachToolInvocationFetchesSandboxWithoutCaching() {
	h := NewMCPServer(types.Config{MCPApiKey: "api"}, http.NewServeMux())
	h.Configure(&interfaces.SandboxMCPConfig{
		TokenLookup: func(context.Context, string) (string, error) { return "token", nil },
		GitLookup: func(context.Context, string) (interfaces.HandlerGit, *agentTypes.GitConnection, error) {
			return &mcpGitHandler{getAccess: func(context.Context, *agentTypes.GitConnection) (*interfaces.GitAccess, error) {
				return &interfaces.GitAccess{Granted: true, Secret: "private"}, nil
			}}, &agentTypes.GitConnection{}, nil
		},
	})
	calls := 0
	h.getSandbox = func(_ context.Context, id string) (*daytona.Sandbox, func(), error) {
		s.Equal("session", id)
		calls++
		return nil, nil, errors.New("sandbox unavailable")
	}
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	auth := types.AgentSession{Id: "session", Token: "token"}

	for range 2 {
		_, _, err := h.gitClone(ctx, nil, types.GitCloneInput{AgentSession: auth, URL: "https://github.com/workdock-dev/engine.git", Path: "/workspace"})
		s.ErrorContains(err, "sandbox unavailable")
		_, _, err = h.gitPush(ctx, nil, types.GitPushInput{AgentSession: auth, Path: "/workspace"})
		s.ErrorContains(err, "sandbox unavailable")
		_, _, err = h.gitPull(ctx, nil, types.GitPullInput{AgentSession: auth, Path: "/workspace"})
		s.ErrorContains(err, "sandbox unavailable")
	}

	s.Equal(6, calls)
	_, _, err := h.gitPull(ctx, nil, types.GitPullInput{AgentSession: types.AgentSession{Id: "session", Token: "wrong"}, Path: "/workspace"})
	s.Error(err)
	s.Equal(6, calls)
}

func (s *MCPSuite) TestMissingConfigurationAndGitLookupRejectInvocation() {
	h := NewMCPServer(types.Config{MCPApiKey: "api"}, http.NewServeMux())
	ctx := context.WithValue(context.Background(), types.MCPAuthenticatedKey{}, true)
	auth := types.AgentSession{Id: "session", Token: "token"}
	_, err := h.authorized(ctx, auth)
	s.ErrorContains(err, "active agent session required")

	h.Configure(&interfaces.SandboxMCPConfig{TokenLookup: func(context.Context, string) (string, error) { return "token", nil }})
	_, err = h.authorized(ctx, auth)
	s.ErrorContains(err, "Git access required")

	h.Configure(&interfaces.SandboxMCPConfig{
		TokenLookup: func(context.Context, string) (string, error) { return "token", nil },
		GitLookup: func(context.Context, string) (interfaces.HandlerGit, *agentTypes.GitConnection, error) {
			return nil, nil, errors.New("private lookup details")
		},
	})
	_, err = h.authorized(ctx, auth)
	s.ErrorContains(err, "Git access required")
	s.NotContains(err.Error(), "private")
}
