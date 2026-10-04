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
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/daytona/clients/sdk-go/pkg/options"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func NewMCPServer(apiKey string, lookupToken func(context.Context, string) (string, error)) *MCPServer {
	h := &MCPServer{
		apiKey:      apiKey,
		lookupToken: lookupToken,
		sessions:    make(map[string]types.MCPExecution),
		server:      mcp.NewServer(&mcp.Implementation{Name: "workdock-daytona", Version: "1.0.0"}, nil),
	}

	h.registerTools()
	h.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return h.server }, nil)

	return h
}

func (h *MCPServer) RegisterExecution(sessionID string, sandbox *daytona.Sandbox, gitToken string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if sessionID == "" || sandbox == nil {
		return errors.New("MCP execution requires a session and sandbox")
	}

	if _, exists := h.sessions[sessionID]; exists {
		return errors.New("MCP execution is already active")
	}

	h.sessions[sessionID] = types.MCPExecution{Sandbox: sandbox, GitToken: gitToken}

	return nil
}

func (h *MCPServer) RemoveExecution(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.sessions, sessionID)
}

func (h *MCPServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")

		if !ok || h.apiKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(h.apiKey)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), types.MCPAuthenticatedKey{}, true)
		h.handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *MCPServer) authorized(ctx context.Context, auth types.MCPAuth) (*types.MCPExecution, error) {
	if ctx.Value(types.MCPAuthenticatedKey{}) != true {
		return nil, errors.New("MCP API key authentication required")
	}

	if auth.AgentSessionID == "" || auth.AgentSessionToken == "" || h.lookupToken == nil {
		return nil, errors.New("active Workdock agent session required")
	}

	token, err := h.lookupToken(ctx, auth.AgentSessionID)

	if err != nil || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(auth.AgentSessionToken)) != 1 {
		return nil, errors.New("active Workdock agent session required")
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	execution, ok := h.sessions[auth.AgentSessionID]

	if !ok || execution.Sandbox == nil {
		return nil, errors.New("active Workdock agent session required")
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
		return err
	}

	if !isGitHubHTTPSURL(remoteURL) {
		return errors.New("GitHub installation authentication is restricted to github.com remotes")
	}

	return nil
}

func (h *MCPServer) registerTools() {
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_clone", Description: "Clone a repository in this execution's Daytona sandbox."}, h.gitClone)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_status", Description: "Get repository status."}, h.gitStatus)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_branches", Description: "List repository branches."}, h.gitBranches)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_history", Description: "Get repository commit history."}, h.gitHistory)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_create_branch", Description: "Create a repository branch."}, h.gitCreateBranch)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_checkout", Description: "Check out a branch or commit."}, h.gitCheckout)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_delete_branch", Description: "Delete a branch."}, h.gitDeleteBranch)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_add", Description: "Stage files in a repository."}, h.gitAdd)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_commit", Description: "Commit staged changes."}, h.gitCommit)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_push", Description: "Push commits using Workdock's GitHub installation token."}, h.gitPush)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_pull", Description: "Pull changes using Workdock's GitHub installation token."}, h.gitPull)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_init", Description: "Initialize a repository."}, h.gitInit)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_reset", Description: "Reset repository changes."}, h.gitReset)
	mcp.AddTool(h.server, &mcp.Tool{Name: "daytona_git_restore", Description: "Restore repository files."}, h.gitRestore)
}

func (h *MCPServer) gitClone(ctx context.Context, _ *mcp.CallToolRequest, in types.GitCloneInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	if !isGitHubHTTPSURL(in.URL) {
		return nil, nil, errors.New("repository URL must be an HTTPS GitHub URL without embedded credentials")
	}

	var opts []func(*options.GitClone)

	if execution.GitToken != "" {
		opts = append(opts, options.WithUsername("x-access-token"), options.WithPassword(execution.GitToken))
	}

	if in.Branch != "" {
		opts = append(opts, options.WithBranch(in.Branch))
	}

	if in.CommitID != "" {
		opts = append(opts, options.WithCommitId(in.CommitID))
	}

	err = execution.Sandbox.Git.Clone(ctx, in.URL, in.Path, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitStatus(ctx context.Context, _ *mcp.CallToolRequest, in types.GitStatusInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	out, err := execution.Sandbox.Git.Status(ctx, in.Path)

	return nil, out, err
}

func (h *MCPServer) gitBranches(ctx context.Context, _ *mcp.CallToolRequest, in types.GitBranchesInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	out, err := execution.Sandbox.Git.Branches(ctx, in.Path)

	return nil, map[string]any{"branches": out}, err
}

func (h *MCPServer) gitHistory(ctx context.Context, _ *mcp.CallToolRequest, in types.GitHistoryInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	out, _, err := execution.Sandbox.ToolboxClient.GitAPI.GetCommitHistory(ctx).Path(in.Path).Execute()

	return nil, map[string]any{"commits": out}, err
}

func (h *MCPServer) gitCreateBranch(ctx context.Context, _ *mcp.CallToolRequest, in types.GitCreateBranchInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	err = execution.Sandbox.Git.CreateBranch(ctx, in.Path, in.Name)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitCheckout(ctx context.Context, _ *mcp.CallToolRequest, in types.GitCheckoutInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	err = execution.Sandbox.Git.Checkout(ctx, in.Path, in.Name)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitDeleteBranch(ctx context.Context, _ *mcp.CallToolRequest, in types.GitDeleteBranchInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	var opts []func(*options.GitDeleteBranch)

	if in.Force {
		opts = append(opts, options.WithForce(true))
	}

	err = execution.Sandbox.Git.DeleteBranch(ctx, in.Path, in.Name, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitAdd(ctx context.Context, _ *mcp.CallToolRequest, in types.GitAddInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	err = execution.Sandbox.Git.Add(ctx, in.Path, in.Files)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitCommit(ctx context.Context, _ *mcp.CallToolRequest, in types.GitCommitInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	out, err := execution.Sandbox.Git.Commit(ctx, in.Path, in.Message, in.Author, in.Email)

	return nil, out, err
}

func (h *MCPServer) gitPush(ctx context.Context, _ *mcp.CallToolRequest, in types.GitPushInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	if err := h.authorizeRemote(ctx, execution, in.Path, in.Remote); err != nil {
		return nil, nil, err
	}

	var opts []func(*options.GitPush)

	if execution.GitToken != "" {
		opts = append(opts, options.WithPushUsername("x-access-token"), options.WithPushPassword(execution.GitToken))
	}

	if in.Branch != "" {
		opts = append(opts, options.WithPushBranch(in.Branch))
	}

	if in.Remote != "" {
		opts = append(opts, options.WithPushRemote(in.Remote))
	}

	err = execution.Sandbox.Git.Push(ctx, in.Path, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitPull(ctx context.Context, _ *mcp.CallToolRequest, in types.GitPullInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	if err := h.authorizeRemote(ctx, execution, in.Path, in.Remote); err != nil {
		return nil, nil, err
	}

	var opts []func(*options.GitPull)

	if execution.GitToken != "" {
		opts = append(opts, options.WithPullUsername("x-access-token"), options.WithPullPassword(execution.GitToken))
	}

	if in.Branch != "" {
		opts = append(opts, options.WithPullBranch(in.Branch))
	}

	if in.Remote != "" {
		opts = append(opts, options.WithPullRemote(in.Remote))
	}

	err = execution.Sandbox.Git.Pull(ctx, in.Path, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitInit(ctx context.Context, _ *mcp.CallToolRequest, in types.GitInitInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	opts := []func(*options.GitInit){options.WithBare(in.Bare)}

	if in.InitialBranch != "" {
		opts = append(opts, options.WithInitialBranch(in.InitialBranch))
	}

	err = execution.Sandbox.Git.Init(ctx, in.Path, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitReset(ctx context.Context, _ *mcp.CallToolRequest, in types.GitResetInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	var opts []func(*options.GitReset)

	if in.Mode != "" {
		opts = append(opts, options.WithResetMode(in.Mode))
	}

	if in.Target != "" {
		opts = append(opts, options.WithResetTarget(in.Target))
	}

	if len(in.Files) > 0 {
		opts = append(opts, options.WithResetFiles(in.Files))
	}

	err = execution.Sandbox.Git.Reset(ctx, in.Path, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}

func (h *MCPServer) gitRestore(ctx context.Context, _ *mcp.CallToolRequest, in types.GitRestoreInput) (*mcp.CallToolResult, any, error) {
	execution, err := h.authorized(ctx, in.MCPAuth)

	if err != nil {
		return nil, nil, err
	}

	opts := []func(*options.GitRestore){options.WithRestoreStaged(in.Staged), options.WithRestoreWorktree(in.Worktree)}

	if in.Source != "" {
		opts = append(opts, options.WithRestoreSource(in.Source))
	}

	err = execution.Sandbox.Git.Restore(ctx, in.Path, in.Files, opts...)

	return nil, map[string]bool{"success": err == nil}, err
}
