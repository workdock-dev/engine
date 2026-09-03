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

package shared

import (
	"encoding/json"
)

type PlatformProvider string
type HarnessProvider string
type AgentSessionEventReason string

const (
	AgentSessionEventReason_Prompt    AgentSessionEventReason = "user_prompt"
	AgentSessionEventReason_PRComment AgentSessionEventReason = "pr_comment"
	AgentSessionEventReason_CheckRun  AgentSessionEventReason = "check_run"
)

const (
	PlatformProvider_Linear  PlatformProvider = "linear"
	PlatformProvider_GitHub  PlatformProvider = "github"
	PlatformProvider_Daytona PlatformProvider = "daytona"

	HarnessProvider_OpenCode HarnessProvider = "opencode"
)

type Organization struct {
	Identifier string
	Provider   PlatformProvider
	Name       string
}

type Session struct {
	OrganizationIdentifier string
	Identifier             string
	Provider               PlatformProvider
	IssueId                string
	Creator                string
	RepoFullName           *string
}

type Issue struct {
	Title       string
	Identifier  string
	Description string
}

type SessionEvent struct {
	SessionIdentifier string
	Identifier        string
	Payload           json.RawMessage
	Seed              *string // TODO: Refactor this name to parent (session event's parent), this is set on event that generated pr review comments
	GitRef            *string
	Result            *SessionEventResult
	Reason            AgentSessionEventReason // Indicate the origin/why this event
}

type SessionEventResult struct {
	PullRequest *PullRequest
}

type GitHubConnection struct {
	SessionEventIdentifier *string
	RepoFullName           string
	Connected              bool
	InstallationId         *string
}

type PullRequest struct {
	HeadRefName string `json:"headRefName"`
	HeadRefOID  string `json:"headRefOid"`
	Number      int    `json:"number"`
	URL         string `json:"url"`
}

type AgentAction struct {
	Name   string
	Input  string
	Output string
}

type AgentElicitation struct {
	Question string
	Options  []AgentOption
	Multiple bool
}

type AgentOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

const (
	GitHubUrl = "https://github.com/"
)

type ExternalURL struct {
	Label string
	URL   string
}
