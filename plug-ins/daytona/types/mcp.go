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

package types

import "github.com/daytona/clients/sdk-go/pkg/daytona"

type MCPAuthenticatedKey struct{}

type MCPExecution struct {
	Sandbox  *daytona.Sandbox
	GitToken string
}

type MCPAuth struct {
	AgentSessionID    string `json:"agentSessionId" jsonschema:"Workdock agent session identifier"`
	AgentSessionToken string `json:"agentSessionToken" jsonschema:"ephemeral token for this Workdock execution"`
}

type GitCloneInput struct {
	MCPAuth
	URL      string `json:"url"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	CommitID string `json:"commitId,omitempty"`
}

type GitStatusInput struct {
	MCPAuth
	Path string `json:"path"`
}

type GitBranchesInput struct {
	MCPAuth
	Path string `json:"path"`
}

type GitHistoryInput struct {
	MCPAuth
	Path string `json:"path"`
}

type GitCreateBranchInput struct {
	MCPAuth
	Path string `json:"path"`
	Name string `json:"name"`
}

type GitCheckoutInput struct {
	MCPAuth
	Path string `json:"path"`
	Name string `json:"name"`
}

type GitDeleteBranchInput struct {
	MCPAuth
	Path  string `json:"path"`
	Name  string `json:"name"`
	Force bool   `json:"force,omitempty"`
}

type GitAddInput struct {
	MCPAuth
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

type GitCommitInput struct {
	MCPAuth
	Path    string `json:"path"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Email   string `json:"email"`
}

type GitPushInput struct {
	MCPAuth
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Remote string `json:"remote,omitempty"`
}

type GitPullInput struct {
	MCPAuth
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Remote string `json:"remote,omitempty"`
}

type GitInitInput struct {
	MCPAuth
	Path          string `json:"path"`
	Bare          bool   `json:"bare,omitempty"`
	InitialBranch string `json:"initialBranch,omitempty"`
}

type GitResetInput struct {
	MCPAuth
	Path   string   `json:"path"`
	Mode   string   `json:"mode,omitempty"`
	Target string   `json:"target,omitempty"`
	Files  []string `json:"files,omitempty"`
}

type GitRestoreInput struct {
	MCPAuth
	Path     string   `json:"path"`
	Files    []string `json:"files"`
	Staged   bool     `json:"staged,omitempty"`
	Worktree bool     `json:"worktree,omitempty"`
	Source   string   `json:"source,omitempty"`
}
