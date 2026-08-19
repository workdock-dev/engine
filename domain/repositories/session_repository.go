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

package repositories

import (
	"context"

	"github.com/jazielguerrero/workdock/domain/types"
)

// SessionRepository persists agent sessions, their events, and the jobs
// queued for them.
type SessionRepository interface {
	GetAgentSession(ctx context.Context, identifier string) (*types.Session, error)
	GetAgentSessionEvent(ctx context.Context, identifier string) (*types.SessionEvent, error)
	GetAgentSessionEventByGitRef(ctx context.Context, identifier string, repoFullName string) (*types.SessionEvent, error)
	CreateSessionEvent(ctx context.Context, event *types.SessionEvent) error
	UpsertAgentSession(ctx context.Context, session *types.Session) error
	UpdateSessionEventResult(ctx context.Context, event *types.SessionEvent) error
	CancelSession(ctx context.Context, queuedBy, reason string) (int, error)
}
