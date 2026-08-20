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

package ports

import (
	"context"

	"github.com/jazielguerrero/workdock/domain/types"
)

// ForWebhooks defines the port for integrating with platforms that
// deliver events to the application through webhooks.
type ForWebhooks interface {
	// Webhook handles an incoming webhook request from the platform.
	Webhook(ctx context.Context, req types.WebhookRequest) (any, error)
}

// ForIssueStatusChanges is an optional extension of ForWebhooks for platforms
// that can parse Issue data change webhooks with status transitions.
// Platforms that support this interface can react to issue status changes
// (e.g., archiving sandboxes when an issue is marked as done).
type ForIssueStatusChanges interface {
	// ParseIssueStatusChange validates and parses a Linear Issue data change
	// webhook that indicates an issue's status has changed. Returns nil if
	// the webhook is not an issue status change.
	ParseIssueStatusChange(ctx context.Context, req types.WebhookRequest) (*types.IssueStatusChangePayload, error)
}

// WebhooksRegistry maps platform providers to their webhook adapters.
type WebhooksRegistry map[types.PlatformProvider]ForWebhooks