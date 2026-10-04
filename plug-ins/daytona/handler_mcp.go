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
	"sync"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/daytona/clients/sdk-go/pkg/options"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpSession struct {
	sandbox *daytona.Sandbox
	gitToken string
}

// MCPServer is the private MCP interface used by Workdock-dispatched agents.
// Execution sessions are registered only while their Daytona sandbox is live.
type MCPServer struct {
	apiKey string
	lookupToken func(context.Context, string) (string, error)
	mu sync.RWMutex
	sessions map[string]mcpSession
	server *mcp.Server
	handler http.Handler
}

func NewMCPServer(apiKey string, lookupToken func(context.Context, string) (string, error)) *MCPServer {
	h := &MCPServer{apiKey: apiKey, lookupToken: lookupToken, sessions: make(map[string]mcpSession)}
	s := mcp.NewServer(&mcp.Implementation{Name: "workdock-daytona", Version: "1.0.0"}, nil)
	h.server = s
	h.registerTools()
	h.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return h.server }, nil)
	return h
}

func (h *MCPServer) RegisterExecution(sessionID string, sandbox *daytona.Sandbox, gitToken string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[sessionID] = mcpSession{sandbox: sandbox, gitToken: gitToken}
}

func (h *MCPServer) RemoveExecution(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, sessionID)
}

func (h *MCPServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if h.apiKey == "" || len(auth) < len(prefix) || auth[:len(prefix)] != prefix || subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(h.apiKey)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.handler.ServeHTTP(w, r)
	})
}

type MCPAuth struct {
	AgentSessionID string `json:"agentSessionId" jsonschema:"Workdock agent session identifier"`
	AgentSessionToken string `json:"agentSessionToken" jsonschema:"ephemeral token for this Workdock execution"`
}

func (h *MCPServer) authorized(ctx context.Context, auth MCPAuth) (*mcpSession, error) {
	if auth.AgentSessionID == "" || auth.AgentSessionToken == "" || h.lookupToken == nil {
		return nil, errors.New("active Workdock agent session required")
	}
	token, err := h.lookupToken(ctx, auth.AgentSessionID)
	if err != nil {
		slog.Error("[daytona] failed to validate MCP agent session", "session_id", auth.AgentSessionID, "err", err)
		return nil, errors.New("active Workdock agent session required")
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(auth.AgentSessionToken)) != 1 {
		return nil, errors.New("active Workdock agent session required")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	registered, ok := h.sessions[auth.AgentSessionID]
	if !ok || registered.sandbox == nil {
		return nil, errors.New("active Workdock agent session required")
	}
	return &registered, nil
}

func isGitHubHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Hostname() == "github.com"
}

func (e *mcpSession) authorizeRemote(ctx context.Context, path, remote string) error {
	if remote == "" { remote = "origin" }
	remoteURL, err := e.sandbox.Git.RemoteGet(ctx, path, remote)
	if err != nil { return err }
	if !isGitHubHTTPSURL(remoteURL) { return errors.New("GitHub installation authentication is restricted to github.com remotes") }
	return nil
}

func addMCPTool[In, Out any](server *mcp.Server, name, description string, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description}, handler)
}

func (h *MCPServer) registerTools() {
	addMCPTool(h.server, "daytona_git_clone", "Clone a repository in this execution's Daytona sandbox.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; URL string `json:"url"`; Path string `json:"path"`; Branch string `json:"branch,omitempty"`; CommitID string `json:"commitId,omitempty"` }) (*mcp.CallToolResult, any, error) {
		e, err := h.authorized(ctx, in.MCPAuth); if err != nil { return nil, nil, err }
		if !isGitHubHTTPSURL(in.URL) { return nil, nil, errors.New("repository URL must be an HTTPS GitHub URL without embedded credentials") }
		opts := []func(*options.GitClone){}
		if e.gitToken != "" { opts = append(opts, options.WithUsername("x-access-token"), options.WithPassword(e.gitToken)) }
		if in.Branch != "" { opts = append(opts, options.WithBranch(in.Branch)) }; if in.CommitID != "" { opts = append(opts, options.WithCommitId(in.CommitID)) }
		return nil, map[string]bool{"success": true}, e.sandbox.Git.Clone(ctx, in.URL, in.Path, opts...)
	})
	addMCPTool(h.server, "daytona_git_status", "Get repository status.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"` }) (*mcp.CallToolResult, any, error) { e, err := h.authorized(ctx, in.MCPAuth); if err != nil { return nil,nil,err }; out,err:=e.sandbox.Git.Status(ctx,in.Path); return nil,out,err })
	addMCPTool(h.server, "daytona_git_branches", "List repository branches.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; out,err:=e.sandbox.Git.Branches(ctx,in.Path); return nil,out,err })
	addMCPTool(h.server, "daytona_git_history", "Get repository commit history.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; out,_,err:=e.sandbox.ToolboxClient.GitAPI.GetCommitHistory(ctx).Path(in.Path).Execute(); return nil,out,err })
	addMCPTool(h.server, "daytona_git_create_branch", "Create a repository branch.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Name string `json:"name"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; err=e.sandbox.Git.CreateBranch(ctx,in.Path,in.Name); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_checkout", "Check out a branch or commit.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Name string `json:"name"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; err=e.sandbox.Git.Checkout(ctx,in.Path,in.Name); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_delete_branch", "Delete a branch.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Name string `json:"name"`; Force bool `json:"force,omitempty"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; var opts []func(*options.GitDeleteBranch); if in.Force {opts=append(opts,options.WithForce(true))}; err=e.sandbox.Git.DeleteBranch(ctx,in.Path,in.Name,opts...); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_add", "Stage files in a repository.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Files []string `json:"files"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; err=e.sandbox.Git.Add(ctx,in.Path,in.Files); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_commit", "Commit staged changes.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Message string `json:"message"`; Author string `json:"author"`; Email string `json:"email"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; out,err:=e.sandbox.Git.Commit(ctx,in.Path,in.Message,in.Author,in.Email); return nil,out,err })
	addMCPTool(h.server, "daytona_git_push", "Push commits using Workdock's GitHub installation token.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Branch string `json:"branch,omitempty"`; Remote string `json:"remote,omitempty"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; if err=e.authorizeRemote(ctx,in.Path,in.Remote);err!=nil{return nil,nil,err}; opts:=[]func(*options.GitPush){}; if e.gitToken!=""{opts=append(opts,options.WithPushUsername("x-access-token"),options.WithPushPassword(e.gitToken))}; if in.Branch!=""{opts=append(opts,options.WithPushBranch(in.Branch))}; if in.Remote!=""{opts=append(opts,options.WithPushRemote(in.Remote))}; err=e.sandbox.Git.Push(ctx,in.Path,opts...); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_pull", "Pull changes using Workdock's GitHub installation token.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Branch string `json:"branch,omitempty"`; Remote string `json:"remote,omitempty"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; if err=e.authorizeRemote(ctx,in.Path,in.Remote);err!=nil{return nil,nil,err}; opts:=[]func(*options.GitPull){}; if e.gitToken!=""{opts=append(opts,options.WithPullUsername("x-access-token"),options.WithPullPassword(e.gitToken))}; if in.Branch!=""{opts=append(opts,options.WithPullBranch(in.Branch))}; if in.Remote!=""{opts=append(opts,options.WithPullRemote(in.Remote))}; err=e.sandbox.Git.Pull(ctx,in.Path,opts...); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_init", "Initialize a repository.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Bare bool `json:"bare,omitempty"`; InitialBranch string `json:"initialBranch,omitempty"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; opts:=[]func(*options.GitInit){options.WithBare(in.Bare)}; if in.InitialBranch!=""{opts=append(opts,options.WithInitialBranch(in.InitialBranch))}; err=e.sandbox.Git.Init(ctx,in.Path,opts...); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_reset", "Reset repository changes.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Mode string `json:"mode,omitempty"`; Target string `json:"target,omitempty"`; Files []string `json:"files,omitempty"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; opts:=[]func(*options.GitReset){}; if in.Mode!=""{opts=append(opts,options.WithResetMode(in.Mode))}; if in.Target!=""{opts=append(opts,options.WithResetTarget(in.Target))}; if len(in.Files)>0{opts=append(opts,options.WithResetFiles(in.Files))}; err=e.sandbox.Git.Reset(ctx,in.Path,opts...); return nil,map[string]bool{"success":err==nil},err })
	addMCPTool(h.server, "daytona_git_restore", "Restore repository files.", func(ctx context.Context, _ *mcp.CallToolRequest, in struct { MCPAuth; Path string `json:"path"`; Files []string `json:"files"`; Staged bool `json:"staged,omitempty"`; Worktree bool `json:"worktree,omitempty"`; Source string `json:"source,omitempty"` }) (*mcp.CallToolResult, any, error) { e,err:=h.authorized(ctx,in.MCPAuth); if err!=nil{return nil,nil,err}; opts:=[]func(*options.GitRestore){options.WithRestoreStaged(in.Staged),options.WithRestoreWorktree(in.Worktree)}; if in.Source!=""{opts=append(opts,options.WithRestoreSource(in.Source))}; err=e.sandbox.Git.Restore(ctx,in.Path,in.Files,opts...); return nil,map[string]bool{"success":err==nil},err })
}
