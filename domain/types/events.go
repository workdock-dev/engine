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
	EventType_GitHubConnected      = "github.connected"
	EventType_Webhook              = "webhook"
	EventType_PullRequestCommented = "pull_request.comment"
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
