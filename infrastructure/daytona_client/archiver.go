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

package daytona_client

import (
	"context"
	"log/slog"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	sdkerrors "github.com/daytona/clients/sdk-go/pkg/errors"
	"github.com/daytona/clients/sdk-go/pkg/types"
	"github.com/jazielguerrero/workdock/domain/ports"
)

// SandboxArchiver implements ports.ForSandboxArchiver using the Daytona
// client. It looks up sandboxes by session identifier, stops and archives
// them when an issue is done, and relies on GetOrCreateSandbox's automatic
// restore-from-archive behaviour when an issue is reopened.
type SandboxArchiver struct {
	config SandboxConfig
}

func NewSandboxArchiver(config SandboxConfig) ports.ForSandboxArchiver {
	return &SandboxArchiver{config: config}
}

// ArchiveSandbox stops and archives the sandbox associated with the given
// session identifier. If the sandbox does not exist, it is a no-op.
// The sandbox must be stopped before archiving; this method handles that.
func (a *SandboxArchiver) ArchiveSandbox(ctx context.Context, sessionIdentifier string) error {
	target := a.config.Target

	if target == "" {
		target = "us"
	}

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey:     a.config.ApiKey,
		APIUrl:     a.config.ApiUrl,
		Target:     target,
		HTTPClient: daytonaHTTPClient,
	})

	if err != nil {
		slog.Error("failed to create daytona client for archive", "err", err, "session_identifier", sessionIdentifier)
		return err
	}

	sandbox, err := client.Get(ctx, sessionIdentifier)

	if err != nil {
		if sdkerrors.IsNotFound(err) {
			slog.Debug("sandbox not found for archive, skipping", "session_identifier", sessionIdentifier)
			return nil
		}

		slog.Error("failed to get daytona sandbox for archive", "err", err, "session_identifier", sessionIdentifier)
		return err
	}

	state := sandbox.State

	if state == "Archived" {
		slog.Debug("sandbox already archived, skipping", "session_identifier", sessionIdentifier)
		return nil
	}

	// Stop the sandbox before archiving (archive requires stopped state)
	if state != "Stopped" {
		slog.Debug("stopping sandbox before archive", "session_identifier", sessionIdentifier, "state", state)

		if err := sandbox.Stop(ctx); err != nil {
			slog.Error("failed to stop daytona sandbox before archive", "err", err, "session_identifier", sessionIdentifier)
			return err
		}
	}

	if err := sandbox.Archive(ctx); err != nil {
		slog.Error("failed to archive daytona sandbox", "err", err, "session_identifier", sessionIdentifier)
		return err
	}

	slog.Info("archived daytona sandbox for done issue", "session_identifier", sessionIdentifier)
	return nil
}