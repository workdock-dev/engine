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

const (
	EventType_IssueChange             = "issue.changed"
	EventType_AgentSessionPrompt      = "agent_session.prompt"
	EventType_AgentSessionResume      = "agent_session.resume"
	EventType_AgentSessionStop        = "agent_session.stop"
	EventType_OrganizationCreate      = "organization.create"
	EventType_GitResetConnection      = "git.reset_connection"
	EventType_GitCompleteConnection   = "git.complete_connection"
	EventType_PullRequestCommented    = "pull_request.comment"
	EventType_PullRequestChecksFailed = "pull_request.checks_failed"
)

// *--------------------------------------------------------------------------*

type IssueChangedEvent struct {
	Provider string
	Payload  any
}

func (e IssueChangedEvent) EventType() string {
	return EventType_IssueChange
}

// *--------------------------------------------------------------------------*

type AgentSessionPromptEvent struct {
	Provider string
	Payload  any
}

func (e AgentSessionPromptEvent) EventType() string {
	return EventType_AgentSessionPrompt
}

// *--------------------------------------------------------------------------*

type AgentSessionStopEvent struct {
	Provider               string
	OrganizationIdentifier string
	SessionIdentifier      string
}

func (e AgentSessionStopEvent) EventType() string {
	return EventType_AgentSessionStop
}

// *--------------------------------------------------------------------------*

type AgentSessionResumeEvent struct {
	SessionEventIdentifier string
}

func (e AgentSessionResumeEvent) EventType() string {
	return EventType_AgentSessionResume
}

// *--------------------------------------------------------------------------*

type OrganizationCreateEvent struct {
	Organization Organization
}

func (e OrganizationCreateEvent) EventType() string {
	return EventType_OrganizationCreate
}

// *--------------------------------------------------------------------------*

type GitCompleteConnectionEvent struct {
	Repos          []string
	InstallationId string
	Token          []byte
}

func (e GitCompleteConnectionEvent) EventType() string {
	return EventType_GitCompleteConnection
}

// *--------------------------------------------------------------------------*

type GitResetConnectionEvent struct {
	Repos          []string
	InstallationId string
	Delete         bool
}

func (e GitResetConnectionEvent) EventType() string {
	return EventType_GitResetConnection
}

// *--------------------------------------------------------------------------*

type PullRequestCommentedEvent struct {
	Provider       PlatformProvider
	GitRef         string
	InstallationId string
	RepoFullName   string
}

func (e PullRequestCommentedEvent) EventType() string {
	return EventType_PullRequestCommented
}

// *--------------------------------------------------------------------------*

type PullRequestChecksFailedEvent struct {
	Provider       PlatformProvider
	GitRef         string
	InstallationId string
	RepoFullName   string
	ChecksFailed   []string
}

func (e PullRequestChecksFailedEvent) EventType() string {
	return EventType_PullRequestChecksFailed
}
