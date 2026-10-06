package agent_session

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
)

const (
	AuthenticatedKey = "authenticated"
)

type GitCloneInput struct {
	Session  string `json:"session" jsonschema:"Agent session configuration. This value MUST be read from the AGENT_SESSION_CONFIG environment variable; do not generate, infer, or modify it."`
	URL      string `json:"url"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	CommitID string `json:"commitId,omitempty"`
}

type GitPushInput struct {
	Session string `json:"session" jsonschema:"Agent session configuration. This value MUST be read from the AGENT_SESSION_CONFIG environment variable; do not generate, infer, or modify it."`
	Path    string `json:"path"`
	Branch  string `json:"branch,omitempty"`
	Remote  string `json:"remote,omitempty"`
}

type GitPullInput struct {
	Session string `json:"session" jsonschema:"Agent session configuration. This value MUST be read from the AGENT_SESSION_CONFIG environment variable; do not generate, infer, or modify it."`
	Path    string `json:"path"`
	Branch  string `json:"branch,omitempty"`
	Remote  string `json:"remote,omitempty"`
}

type AgentSessionMCP struct {
	apiKey                    string
	mcp                       *mcp.Server
	handler                   http.Handler
	session                   interfaces.Repository
	git                       interfaces.RepositoryGit
	sandboxHandlerRegistry    SandboxHandlerRegistry
	gitHostingHandlerRegistry GitHandlerRegistry
}

func NewMCP(
	mux *http.ServeMux,
	apiKey string,
	session interfaces.Repository,
	git interfaces.RepositoryGit,
	sandboxHandlerRegistry SandboxHandlerRegistry,
	gitHostingHandlerRegistry GitHandlerRegistry,
) *AgentSessionMCP {
	m := &AgentSessionMCP{
		apiKey: apiKey,
		mcp: mcp.NewServer(&mcp.Implementation{
			Name:    "workdock-mcp",
			Version: "1.0.0",
		}, nil),
		session:                   session,
		git:                       git,
		sandboxHandlerRegistry:    sandboxHandlerRegistry,
		gitHostingHandlerRegistry: gitHostingHandlerRegistry,
	}

	m.registerTools()
	m.handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server {
			return m.mcp
		},
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		},
	)
	mux.Handle("/api/v1/mcp/git", m.Handler())

	slog.Debug("[daytona] Git MCP server configured")
	return m
}

func (m *AgentSessionMCP) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")

		if !ok || m.apiKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(m.apiKey)) != 1 {
			// Safe to log because we don't provide api keys to users, the only reason
			// for the api key mistmatch is 1) malicious actors, 2) incorrect configuration
			slog.Error("[agent_session][mcp] invalid api key", "api_key", key)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), AuthenticatedKey, true)
		m.handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *AgentSessionMCP) registerTools() {
	mcp.AddTool(m.mcp, &mcp.Tool{Name: "work_report", Description: "Required before completing any execution: report changes, next actions, or questions. Use zero counts and an empty commits list when no changes were made."}, m.workReport)
	mcp.AddTool(m.mcp, &mcp.Tool{Name: "create_pull_request", Description: "Create a pull request only when requested by the user."}, m.createPullRequest)

	mcp.AddTool(m.mcp, &mcp.Tool{
		Name:        "git_clone",
		Description: "Clone a repository.",
	}, m.gitClone)

	mcp.AddTool(m.mcp, &mcp.Tool{
		Name:        "git_push",
		Description: "Push repository commits.",
	}, m.gitPush)

	mcp.AddTool(m.mcp, &mcp.Tool{
		Name:        "git_pull",
		Description: "Pull repository changes.",
	}, m.gitPull)
}

func (m *AgentSessionMCP) authorized(ctx context.Context, session string) (string, error) {
	if ctx.Value(AuthenticatedKey) != true {
		err := errors.New("request not authenticated")
		slog.Error("[agent_session][mcp] failed to authorize request", "err", err)
		return "", err
	}

	// TODO: Check if session is in cache, if it is, session is valid

	config := strings.Split(session, "|")

	if len(config) != 2 || config[0] == "" || config[1] == "" {
		err := errors.New("invalid agent session configuration format")
		slog.Error("[agent_session][mcp] failed to authorize request", "err", err)
		return "", err
	}

	sessionId := config[0]
	sessionToken := config[1]

	token, err := m.session.GetMCPToken(ctx, sessionId)

	if err != nil {
		return "", err
	}

	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(sessionToken)) != 1 {
		err := errors.New("invalid agent session mcp token")
		slog.Error("[agent_session][mcp] failed to authorize request", "err", err, "session_id", sessionId)
		return "", err
	}

	// TODO: Cache the session to avoid trips to the database

	return sessionId, nil
}

func (m *AgentSessionMCP) gitAccess(ctx context.Context, sessionId string) (string, error) {
	session, err := m.session.GetAgentSession(ctx, sessionId)

	if err != nil {
		return "", err
	}

	if session == nil || session.RepoFullName == nil {
		err := errors.New("agent session repository not set")
		slog.Error("[agent_session][mcp] failed to validate git access", "err", err, "session_id", sessionId)
		return "", err
	}

	connection, err := m.git.GetConnection(ctx, *session.RepoFullName)

	if err != nil {
		return "", err
	}

	gitHandler, ok := m.gitHostingHandlerRegistry[string(shared.PlatformProvider_GitHub)]

	if !ok {
		err := fmt.Errorf("provider %s not configured for git hosting handler", shared.PlatformProvider_GitHub)
		slog.Error("[agent-session][mcp] failed to validate git access", "err", err, "session_id", sessionId)
		return "", err
	}

	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		err := errors.New("git is not connected")
		slog.Error("[agent-session][mcp] failed to validate git access", "err", err, "session_id", sessionId)
		return "", err
	}

	access, err := gitHandler.GetGitAccess(ctx, connection)

	if err != nil {
		return "", err
	}

	if access != nil && access.Granted && access.Secret != "" {
		return access.Secret, nil
	}

	err = errors.New("git access not granded")
	slog.Error("[agent-session][map] failed to get git access", "err", err, "session_id", sessionId)
	return "", err
}

func (m *AgentSessionMCP) gitClone(ctx context.Context, _ *mcp.CallToolRequest, input GitCloneInput) (*mcp.CallToolResult, any, error) {
	sessionId, err := m.authorized(ctx, input.Session)

	if err != nil {
		return nil, nil, err
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := m.sandboxHandlerRegistry[string(shared.PlatformProvider_Daytona)]

	if !ok {
		err := fmt.Errorf("[agent-session][mcp] provider %s not configured for sandbox handler", shared.PlatformProvider_Daytona)
		slog.Error("[agent_session][mcp] failed to git clone", "err", err)
		return nil, nil, err
	}

	accessToken, err := m.gitAccess(ctx, sessionId)

	if err != nil {
		return nil, nil, err
	}

	err = sandboxHandler.GitClone(ctx, interfaces.GitCloneInput{
		SessionId:   sessionId,
		AccessToken: accessToken,
		Url:         input.URL,
		Path:        input.Path,
		Branch:      input.Branch,
		CommitId:    input.CommitID,
	})
	return nil, map[string]bool{"success": err == nil}, err
}

func (m *AgentSessionMCP) gitPush(ctx context.Context, _ *mcp.CallToolRequest, input GitPushInput) (*mcp.CallToolResult, any, error) {
	sessionId, err := m.authorized(ctx, input.Session)

	if err != nil {
		return nil, nil, err
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := m.sandboxHandlerRegistry[string(shared.PlatformProvider_Daytona)]

	if !ok {
		err := fmt.Errorf("[agent-session][mcp] provider %s not configured for sandbox handler", shared.PlatformProvider_Daytona)
		slog.Error("[agent_session][mcp] failed to git clone", "err", err)
		return nil, nil, err
	}

	accessToken, err := m.gitAccess(ctx, sessionId)

	if err != nil {
		return nil, nil, err
	}

	err = sandboxHandler.GitPush(ctx, interfaces.GitPushInput{
		SessionId:   sessionId,
		AccessToken: accessToken,
		Path:        input.Path,
		Remote:      input.Remote,
		Branch:      input.Branch,
	})
	return nil, map[string]bool{"success": err == nil}, err
}

func (m *AgentSessionMCP) gitPull(ctx context.Context, _ *mcp.CallToolRequest, input GitPullInput) (*mcp.CallToolResult, any, error) {
	sessionId, err := m.authorized(ctx, input.Session)

	if err != nil {
		return nil, nil, err
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := m.sandboxHandlerRegistry[string(shared.PlatformProvider_Daytona)]

	if !ok {
		err := fmt.Errorf("[agent-session][mcp] provider %s not configured for sandbox handler", shared.PlatformProvider_Daytona)
		slog.Error("[agent_session][mcp] failed to git clone", "err", err)
		return nil, nil, err
	}

	accessToken, err := m.gitAccess(ctx, sessionId)

	if err != nil {
		return nil, nil, err
	}

	err = sandboxHandler.GitPull(ctx, interfaces.GitPullInput{
		SessionId:   sessionId,
		AccessToken: accessToken,
		Path:        input.Path,
		Remote:      input.Remote,
		Branch:      input.Branch,
	})
	return nil, map[string]bool{"success": err == nil}, err
}

func (m *AgentSessionMCP) workReport(ctx context.Context, _ *mcp.CallToolRequest, input types.WorkReportInput) (*mcp.CallToolResult, any, error) {
	sessionID, err := m.authorized(ctx, input.Session)

	if err != nil {
		return nil, nil, err
	}

	if input.LinesAdded < 0 || input.LinesRemoved < 0 || strings.TrimSpace(input.Report) == "" || !utf8.ValidString(input.Report) || utf8.RuneCountInString(input.Report) > 280 {
		err := errors.New("work report requires nonnegative line counts and a report of 1 to 280 characters")
		slog.Error("[agent_session][mcp] invalid work report", "err", err)
		return nil, nil, err
	}

	if input.Commits == nil {
		input.Commits = []string{}
	}

	err = m.session.SaveMCPReport(ctx, sessionID, &types.SessionEventResult{
		LinesAdded: input.LinesAdded, LinesRemoved: input.LinesRemoved,
		Commits: input.Commits, Report: input.Report,
	})

	return nil, map[string]bool{"success": err == nil}, err
}

func (m *AgentSessionMCP) createPullRequest(ctx context.Context, _ *mcp.CallToolRequest, input types.CreatePullRequestInput) (*mcp.CallToolResult, any, error) {
	sessionID, err := m.authorized(ctx, input.Session)

	if err != nil {
		return nil, nil, err
	}

	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Head) == "" || strings.TrimSpace(input.Base) == "" {
		err := errors.New("pull request title, head, and base are required")
		slog.Error("[agent_session][mcp] invalid pull request", "err", err)
		return nil, nil, err
	}

	accessToken, err := m.gitAccess(ctx, sessionID)

	if err != nil {
		return nil, nil, err
	}

	session, err := m.session.GetAgentSession(ctx, sessionID)

	if err != nil {
		return nil, nil, err
	}

	if session == nil || session.RepoFullName == nil {
		return nil, nil, errors.New("agent session repository not set")
	}

	gitHandler := m.gitHostingHandlerRegistry[string(shared.PlatformProvider_GitHub)]
	pr, err := gitHandler.CreatePullRequest(ctx, interfaces.CreatePullRequestInput{
		RepoFullName: *session.RepoFullName, AccessToken: accessToken,
		Title: input.Title, Body: input.Body, Head: input.Head, Base: input.Base, Draft: input.Draft,
	})

	if err != nil {
		return nil, nil, err
	}

	if pr == nil {
		return nil, nil, errors.New("pull request creation returned no result")
	}

	if err := m.session.SaveMCPPullRequest(ctx, sessionID, pr); err != nil {
		return nil, nil, err
	}

	return nil, pr, nil
}
