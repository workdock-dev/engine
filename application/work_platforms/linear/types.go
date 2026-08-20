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

package linear

import "time"

type AgentActivityContentType = string
type LinearSignalType = string

const (
	// Path where the linear oauth token are stored
	SecretsPath = "/linear/oauth"

	// A thought or internal note.
	AgentActivityContentType_Thought = "thought"

	// Requests clarification or confirmation from the user.
	AgentActivityContentType_Elicitation = "elicitation"

	// Describes a tool invocation. You may optionally include a result if the action has completed.
	AgentActivityContentType_Action = "action"

	// Indicates work has been completed or a final result is available.
	AgentActivityContentType_Response = "response"

	// Used to report an error or failure.
	AgentActivityContentType_Error = "error"

	// The select signal presents a list of options for the user to choose from as part of an
	// elicitation activity.
	SignalType_Select = "select"

	// The auth signal indicates that the agent requires the user to complete an account linking
	// process before it can continue.
	SignalType_Auth = "auth"

	// The stop signal indicates that the user wants to stop and cancel all work for
	// the agent session.
	SignalType_Stop = "stop"
)

type WorkspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AgentSessionEventData struct {
	PromptContext    string         `json:"promptContext"`
	Guidance         []GuidanceRule `json:"guidance"`
	OAuthClientID    string         `json:"oauthClientId"`
	AppUserID        string         `json:"appUserId"`
	OrganizationID   string         `json:"organizationId"`
	WebhookID        string         `json:"webhookId"`
	AgentActivity    *AgentActivity `json:"agentActivity"`
	AgentSession     *AgentSession  `json:"agentSession"`
	Issue            *Issue         `json:"issue"`
	PreviousComments []any          `json:"previousComments"`
	CreatedAt        string         `json:"createdAt"`
	Action           string         `json:"action"`
	Type             string         `json:"type"`
	WebhookTimestamp int64          `json:"webhookTimestamp"`
}

type AgentSession struct {
	ID              string        `json:"id"`
	CreatedAt       string        `json:"createdAt"`
	UpdatedAt       string        `json:"updatedAt"`
	CreatorID       string        `json:"creatorId"`
	AppUserID       string        `json:"appUserId"`
	CommentID       string        `json:"commentId"`
	SourceCommentID *string       `json:"sourceCommentId"`
	IssueID         string        `json:"issueId"`
	Status          string        `json:"status"`
	Plan            *string       `json:"plan"`
	WorkspaceDiff   *string       `json:"workspaceDiff"`
	Context         []any         `json:"context"`
	URL             string        `json:"url"`
	ExternalUrls    []ExternalURL `json:"externalUrls"`
	OrganizationID  string        `json:"organizationId"`
	Creator         User          `json:"creator"`
	Comment         Comment       `json:"comment"`
	Issue           Issue         `json:"issue"`
}

type ExternalURL struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type AgentActivity struct {
	ID              string          `json:"id"`
	AgentSessionID  string          `json:"agentSessionId"`
	SourceCommentID *string         `json:"sourceCommentId"`
	UserID          string          `json:"userId"`
	Signal          string          `json:"signal"`
	SignalMetadata  *string         `json:"signalMetadata"`
	Content         ActivityContent `json:"content"`
	User            User            `json:"user"`
}

type ActivityContent struct {
	Type string `json:"type"`
	Body string `json:"body"`
}

type GuidanceRule struct {
	Body string `json:"body"`
}

type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
	URL       string `json:"url"`
}

type Issue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	TeamID      string `json:"teamId"`
	Team        Team   `json:"team"`
	Identifier  string `json:"identifier"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Comment struct {
	ID      string `json:"id"`
	Body    string `json:"body"`
	IssueID string `json:"issueId"`
}

type CreateAgentActivityInput struct {
	AgentSessionID  string
	Content         AgentActivityContent
	Signal          string
	SourceCommentID string
	SignalMetadata  map[string]any
}

type AgentActivityContent struct {
	Type      string
	Body      string
	Action    string
	Parameter string
	Result    string
}

type CreateAgentActivityOutput struct {
	Success       bool `json:"success"`
	LastSyncID    int  `json:"lastSyncId"`
	AgentActivity struct {
		ID string `json:"id"`
	} `json:"agentActivity"`
}

type IssueLabel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	IsGroup     bool   `json:"isGroup"`
}

type SetExternalURLsInput struct {
	SessionID    string
	ExternalURLs []ExternalURL
}

type AgentSessionUpdatePayload struct {
	Success bool `json:"success"`
}

type IssueLabelPageInfo struct {
	HasNextPage     bool   `json:"hasNextPage"`
	HasPreviousPage bool   `json:"hasPreviousPage"`
	StartCursor     string `json:"startCursor"`
	EndCursor       string `json:"endCursor"`
}

type OAuthCallbackEvent struct {
	Token     Token         `json:"token"`
	Workspace WorkspaceInfo `json:"workspace_info"`
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// IssueData represents the data payload of a Linear Issue data change webhook.
// See: https://linear.app/developers/webhooks
type IssueData struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Identifier  string `json:"identifier"`
	TeamID      string `json:"teamId"`
	StateName   string `json:"stateName"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// IssueStatusChangePayload represents a Linear webhook payload for an Issue
// data change event. The action field is "update" when an issue's state
// changes, and UpdatedFrom contains the previous field values.
type IssueStatusChangePayload struct {
	Action          string    `json:"action"`
	Type            string    `json:"type"`
	OrganizationID  string    `json:"organizationId"`
	WebhookID      string    `json:"webhookId"`
	CreatedAt       string    `json:"createdAt"`
	WebhookTimestamp int64    `json:"webhookTimestamp"`
	Data            IssueData `json:"data"`
	UpdatedFrom     struct {
		StateName string `json:"stateName"`
	} `json:"updatedFrom"`
}
