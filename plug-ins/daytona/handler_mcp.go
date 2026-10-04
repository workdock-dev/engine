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
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/daytona/clients/sdk-go/pkg/options"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	agentTypes "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/daytona/types"
)

type MCPServer struct {
	apiKey      string
	lookupToken func(context.Context, string) (string, error)
	mu          sync.RWMutex
	sessions    map[string]types.MCPExecution
	server      *mcp.Server
	handler     http.Handler
}

func NewMCPServer(config types.Config, mux *http.ServeMux) *MCPServer {
	h := &MCPServer{
		apiKey:      config.MCPApiKey,
		lookupToken: config.MCPTokenLookup,
		sessions:    make(map[string]types.MCPExecution),
		server:      mcp.NewServer(&mcp.Implementation{Name: "workdock", Version: "1.0.0"}, nil),
	}

	h.registerTools()
	h.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return h.server }, nil)

	mux.Handle("/api/v1/mcp/git", h.Handler())

	slog.Debug("[daytona] Git MCP server configured")

	return h
}

func (h *MCPServer) RegisterExecution(sessionID string, sandbox *daytona.Sandbox, gitHandler interfaces.HandlerGit, connection *agentTypes.GitConnection) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if sessionID == "" || sandbox == nil {
		slog.Error("[daytona] MCP execution requires a session and sandbox")
		return errors.New("MCP execution requires a session and sandbox")
	}

	if _, exists := h.sessions[sessionID]; exists {
		slog.Error("[daytona] MCP execution is already active")
		return errors.New("MCP execution is already active")
	}

	h.sessions[sessionID] = types.MCPExecution{Sandbox: sandbox, GitHandler: gitHandler, GitConnection: connection}

	slog.Debug("[daytona] MCP execution registered", "session_id", sessionID)

	return nil
}

func (h *MCPServer) RemoveExecution(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.sessions, sessionID)
	slog.Debug("[daytona] MCP execution removed", "session_id", sessionID)
}

func (h *MCPServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")

		if !ok || h.apiKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(h.apiKey)) != 1 {
			slog.Error("[daytona] MCP API key authentication rejected")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), types.MCPAuthenticatedKey{}, true)
		h.handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *MCPServer) authorized(ctx context.Context, auth types.AgentSession) (*types.MCPExecution, error) {
	if ctx.Value(types.MCPAuthenticatedKey{}) != true {
		slog.Error("[daytona] MCP API key authentication required")
		return nil, errors.New("MCP API key authentication required")
	}

	if auth.Id == "" || auth.Token == "" || h.lookupToken == nil {
		slog.Error("[daytona] active agent session required")
		return nil, errors.New("active agent session required")
	}

	token, err := h.lookupToken(ctx, auth.Id)

	if err != nil || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(auth.Token)) != 1 {
		slog.Error("[daytona] active agent session required")
		return nil, errors.New("active agent session required")
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	execution, ok := h.sessions[auth.Id]

	if !ok || execution.Sandbox == nil {
		slog.Error("[daytona] active agent session required")
		return nil, errors.New("active agent session required")
	}

	return &execution, nil
}

func isGitHubHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)

	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Host == "github.com" && u.Path != ""
}

func (h *MCPServer) authorizeRemote(ctx context.Context, execution *types.MCPExecution, path, remote string) error {
	if remote == "" {
		remote = "origin"
	}

	remoteURL, err := execution.Sandbox.Git.RemoteGet(ctx, path, remote)

	if err != nil {
		slog.Error("[daytona] failed to validate Git remote")
		return errors.New("failed to validate Git remote")
	}

	if !isGitHubHTTPSURL(remoteURL) {
		slog.Error("[daytona] GitHub installation authentication is restricted to github.com remotes")
		return errors.New("GitHub installation authentication is restricted to github.com remotes")
	}

	return nil
}

func (h *MCPServer) registerTools() {
	mcp.AddTool(h.server, &mcp.Tool{Name: "git_clone", Description: "Clone a repository."}, h.gitClone)
	mcp.AddTool(h.server, &mcp.Tool{Name: "git_push", Description: "Push repository commits."}, h.gitPush)
	mcp.AddTool(h.server, &mcp.Tool{Name: "git_pull", Description: "Pull repository changes."}, h.gitPull)
}

func (h *MCPServer) gitClone(ctx context.Context, _ *mcp.CallToolRequest, in types.GitCloneInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.AgentSession)

	if err != nil {
		return nil, nil, err
	}

	if !isGitHubHTTPSURL(in.URL) {
		slog.Error("[daytona] repository URL must be an HTTPS GitHub URL without embedded credentials")
		return nil, nil, errors.New("repository URL must be an HTTPS GitHub URL without embedded credentials")
	}

	access, err := h.gitAccess(ctx, execution)

	if err != nil {
		return nil, nil, err
	}

	opts := []func(*options.GitClone){options.WithUsername("x-access-token"), options.WithPassword(access.Secret)}

	if in.Branch != "" {
		opts = append(opts, options.WithBranch(in.Branch))
	}

	if in.CommitID != "" {
		opts = append(opts, options.WithCommitId(in.CommitID))
	}

	slog.Debug("[daytona] executing Git MCP clone", "session_id", in.Id)
	err = execution.Sandbox.Git.Clone(ctx, in.URL, in.Path, opts...)

	if err != nil {
		slog.Error("[daytona] Git MCP clone failed", "session_id", in.Id)
		return nil, nil, errors.New("Git clone failed")
	}

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitPush(ctx context.Context, _ *mcp.CallToolRequest, in types.GitPushInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.AgentSession)

	if err != nil {
		return nil, nil, err
	}

	access, err := h.gitAccess(ctx, execution)

	if err != nil {
		return nil, nil, err
	}

	if err := h.authorizeRemote(ctx, execution, in.Path, in.Remote); err != nil {
		return nil, nil, err
	}

	opts := []func(*options.GitPush){options.WithPushUsername("x-access-token"), options.WithPushPassword(access.Secret)}

	if in.Branch != "" {
		opts = append(opts, options.WithPushBranch(in.Branch))
	}

	if in.Remote != "" {
		opts = append(opts, options.WithPushRemote(in.Remote))
	}

	slog.Debug("[daytona] executing Git MCP push", "session_id", in.Id)
	err = execution.Sandbox.Git.Push(ctx, in.Path, opts...)

	if err != nil {
		slog.Error("[daytona] Git MCP push failed", "session_id", in.Id)
		return nil, nil, errors.New("Git push failed")
	}

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitPull(ctx context.Context, _ *mcp.CallToolRequest, in types.GitPullInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.AgentSession)

	if err != nil {
		return nil, nil, err
	}

	access, err := h.gitAccess(ctx, execution)

	if err != nil {
		return nil, nil, err
	}

	if err := h.authorizeRemote(ctx, execution, in.Path, in.Remote); err != nil {
		return nil, nil, err
	}

	opts := []func(*options.GitPull){options.WithPullUsername("x-access-token"), options.WithPullPassword(access.Secret)}

	if in.Branch != "" {
		opts = append(opts, options.WithPullBranch(in.Branch))
	}

	if in.Remote != "" {
		opts = append(opts, options.WithPullRemote(in.Remote))
	}

	slog.Debug("[daytona] executing Git MCP pull", "session_id", in.Id)
	err = execution.Sandbox.Git.Pull(ctx, in.Path, opts...)

	if err != nil {
		slog.Error("[daytona] Git MCP pull failed", "session_id", in.Id)
		return nil, nil, errors.New("Git pull failed")
	}

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitAccess(ctx context.Context, execution *types.MCPExecution) (*interfaces.GitAccess, error) {
	if execution.GitHandler == nil || execution.GitConnection == nil {
		slog.Error("[daytona] Git access unavailable for MCP execution")
		return nil, errors.New("Git access required")
	}

	access, err := execution.GitHandler.GetGitAccess(ctx, execution.GitConnection)

	if err != nil {
		return nil, errors.New("failed to obtain Git access")
	}

	if access == nil || !access.Granted || access.Secret == "" {
		slog.Error("[daytona] Git access denied for MCP execution")
		return nil, errors.New("Git access required")
	}

	return access, nil
}
