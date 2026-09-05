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

package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/shared"
)

type Repository interface {
	GetAgentSession(ctx context.Context, identifier string) (*shared.Session, error)
	GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*shared.Session, error)
	GetAgentSessionEvent(ctx context.Context, identifier string) (*shared.SessionEvent, error)
	GetAgentSessionEventByGitRef(ctx context.Context, identifier string, repoFullName string) (*shared.SessionEvent, error)
	CreateSessionEvent(ctx context.Context, event *shared.SessionEvent) error
	ResumeSessionEvent(ctx context.Context, event *shared.SessionEvent) error
	UpsertAgentSession(ctx context.Context, session *shared.Session) error
	UpdateSessionEventResult(ctx context.Context, event *shared.SessionEvent) error
	CancelSession(ctx context.Context, queuedBy, reason string) (int, error)
}
