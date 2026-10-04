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

import (
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	agentTypes "github.com/workdock-dev/engine/features/agent_session/types"
)

type MCPAuthenticatedKey struct{}

type MCPExecution struct {
	GitHandler    interfaces.HandlerGit
	GitConnection *agentTypes.GitConnection
}

type AgentSession struct {
	Id    string `json:"agentSessionId" jsonschema:"agent session identifier"`
	Token string `json:"agentSessionToken" jsonschema:"ephemeral token for this agent execution"`
}

type GitCloneInput struct {
	AgentSession
	URL      string `json:"url"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	CommitID string `json:"commitId,omitempty"`
}

type GitPushInput struct {
	AgentSession
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Remote string `json:"remote,omitempty"`
}

type GitPullInput struct {
	AgentSession
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Remote string `json:"remote,omitempty"`
}
