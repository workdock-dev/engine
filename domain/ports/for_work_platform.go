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

	"github.com/workdock-dev/engine/domain/types"
)

type ProcessConfig struct {
	Job          *types.EventJob
	SessionEvent *types.SessionEvent
	Session      *types.Session
}

// ForWorkPlatform defines the port for integrating a work platform such as
// Linear, Jira, or Asana with the application.
type ForWorkPlatform interface {
	// BeginOAuth returns the URL to initiate the OAuth 2.0 authorization flow.
	BeginOAuth(ctx context.Context) string

	// CompleteOAuth completes the OAuth 2.0 authorization flow using the
	// authorization code or error returned by the provider.
	CompleteOAuth(ctx context.Context, code, errorP string) (string, error)

	// Ingest transforms a platform webhook event into domain types.
	Ingest(event any, seed *string, from *types.SessionEvent) (*types.Session, *types.SessionEvent, error)

	// Process executes a previously ingested webhook event.
	Process(ctx context.Context, config ProcessConfig) error

	// Cancel cancels a running session associated with the platform event.
	Cancel(ctx context.Context, session *types.Session) error

	// IsCancelSignal determines whether a webhook event represents a cancellation request.
	IsCancelSignal(ctx context.Context, any any) (bool, error)

	ForWebhooks
}

type WorkPlatformRegistry map[types.PlatformProvider]ForWorkPlatform
