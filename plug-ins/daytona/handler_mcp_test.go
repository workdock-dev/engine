package daytona

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
)

type MCPSuite struct{ suite.Suite }

func TestMCPSuite(t *testing.T) { suite.Run(t, new(MCPSuite)) }

func (s *MCPSuite) TestAPIKeyAuthentication() {
	h := NewMCPServer("secret", func(context.Context, string) (string, error) { return "token", nil }).Handler()
	for _, test := range []struct { name, header string; status int }{
		{name: "missing", status: 401},
		{name: "invalid", header: "Bearer wrong", status: 401},
	} {
		s.Run(test.name, func() {
			req := httptest.NewRequest("POST", "/mcp", nil)
			if test.header != "" { req.Header.Set("Authorization", test.header) }
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			s.Equal(test.status, res.Code)
		})
	}
	valid := httptest.NewRequest("POST", "/mcp", nil)
	valid.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, valid)
	s.NotEqual(401, res.Code)
}

func (s *MCPSuite) TestGitHubURLValidationRejectsEmbeddedCredentialsAndOtherHosts() {
	s.True(isGitHubHTTPSURL("https://github.com/workdock-dev/engine.git"))
	for _, raw := range []string{
		"https://token@github.com/workdock-dev/engine.git",
		"http://github.com/workdock-dev/engine.git",
		"https://example.com/workdock-dev/engine.git",
		"git@github.com:workdock-dev/engine.git",
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
	for _, tool := range tools.Tools { registered = append(registered, tool.Name) }
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
		Name: "daytona_git_status",
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
	h.RegisterExecution("session-one", first, "")
	h.RegisterExecution("session-two", second, "")

	authorized, err := h.authorized(context.Background(), MCPAuth{AgentSessionID: "session-one", AgentSessionToken: "token-one"})
	s.Require().NoError(err)
	s.Same(first, authorized.sandbox)
	_, err = h.authorized(context.Background(), MCPAuth{AgentSessionID: "unknown", AgentSessionToken: "token-one"})
	s.Error(err)
	_, err = h.authorized(context.Background(), MCPAuth{AgentSessionID: "session-one", AgentSessionToken: "wrong"})
	s.Error(err)
	_, err = h.authorized(context.Background(), MCPAuth{AgentSessionID: "session-one"})
	s.Error(err)

	secondAuthorized, err := h.authorized(context.Background(), MCPAuth{AgentSessionID: "session-two", AgentSessionToken: "token-two"})
	s.Require().NoError(err)
	s.Same(second, secondAuthorized.sandbox)

	delete(stored, "session-one")
	_, err = h.authorized(context.Background(), MCPAuth{AgentSessionID: "session-one", AgentSessionToken: "token-one"})
	s.Error(err)
	h.RemoveExecution("session-two")
	_, err = h.authorized(context.Background(), MCPAuth{AgentSessionID: "session-two", AgentSessionToken: "token-two"})
	s.Error(err)
}
