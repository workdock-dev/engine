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

package domain_service

import (
	"context"
	"log/slog"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/types"
)

// SandboxServiceConfig holds the dependencies required by SandboxService.
type SandboxServiceConfig struct {
	ForSandboxArchiver ports.ForSandboxArchiver
	Sessions           repositories.SessionRepository
}

// SandboxService reacts to issue status changes by archiving or restoring
// the sandboxes associated with an issue's sessions.
type SandboxService struct {
	config SandboxServiceConfig
}

func NewSandboxService(config SandboxServiceConfig) *SandboxService {
	return &SandboxService{
		config: config,
	}
}

// OnIssueStatusChanged handles an issue status change event. When an issue
// transitions to a "done" status, all sandboxes associated with the issue's
// sessions are archived. When an issue is reopened, the sandbox is
// automatically restored from archive when GetOrCreateSandbox is called for
// the next session.
func (s *SandboxService) OnIssueStatusChanged(ctx context.Context, event types.IssueStatusChangedEvent) error {
	slog.Info("issue status changed",
		"provider", event.Provider,
		"organization", event.OrganizationIdentifier,
		"issue_id", event.IssueId,
		"previous_status", event.PreviousStatus,
		"new_status", event.NewStatus,
	)

	sessions, err := s.config.Sessions.GetAgentSessionsByIssueId(ctx, event.IssueId)

	if err != nil {
		slog.Error("failed to get sessions for issue", "err", err, "issue_id", event.IssueId)
		return err
	}

	if len(sessions) == 0 {
		slog.Debug("no sessions found for issue, nothing to archive", "issue_id", event.IssueId)
		return nil
	}

	for _, session := range sessions {
		if err := s.config.ForSandboxArchiver.ArchiveSandbox(ctx, session.Identifier); err != nil {
			slog.Error("failed to archive sandbox for session",
				"err", err,
				"session_identifier", session.Identifier,
				"issue_id", event.IssueId,
			)
			// Continue archiving other sandboxes even if one fails
			continue
		}
	}

	return nil
}