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

const (
	EventType_GitHubConnected         = "github.connected"
	EventType_Webhook               = "webhook"
	EventType_PullRequestCommented  = "pull_request.comment"
	EventType_PullRequestChecksFailed = "pull_request.checks_failed"
)

// WebhookEventType indicates the kind of domain event a webhook payload
// represents. Platforms return this alongside the parsed payload so the
// application can route the event to the correct handler.
type WebhookEventType string

const (
	// WebhookEventType_Unknown is the default webhook event type when the
	// specific type cannot be determined or is not relevant.
	WebhookEventType_Unknown WebhookEventType = "unknown"

	// WebhookEventType_AIRequest is returned when a webhook represents an
	// AI agent session request (e.g. a new comment or agent session event).
	WebhookEventType_AIRequest WebhookEventType = "ai-request"

	// WebhookEventType_IssueStateUpdated is returned when a webhook represents
	// an issue state change (e.g. a ticket moved to "Done").
	WebhookEventType_IssueStateUpdated WebhookEventType = "issue-state-updated"

	// WebhookEventType_Git is returned when a webhook represents a Git event
	// (e.g. a pull request comment).
	WebhookEventType_Git WebhookEventType = "git"
)

type GitHubConnectedEvent struct {
	Connection GitHubConnection
}

func (e GitHubConnectedEvent) EventType() string {
	return EventType_GitHubConnected
}

type WebhookEvent struct {
	Provider PlatformProvider
	Payload  any
	Type     WebhookEventType
}

func (e WebhookEvent) EventType() string {
	return EventType_Webhook + "." + string(e.Provider)
}

func PlatformWebhookEvent(name PlatformProvider) string {
	return EventType_Webhook + "." + string(name)
}

type PullRequestCommentedEvent struct {
	Provider       PlatformProvider
	GitRef         string
	InstallationId string
	RepoFullName   string
}

func (e PullRequestCommentedEvent) EventType() string {
	return EventType_PullRequestCommented
}

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
