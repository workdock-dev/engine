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

package infrastructure

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
)

var (
	//go:embed sql/get_organization.sql
	GetOrganizationSql string

	//go:embed sql/get_agent_session.sql
	GetAgentSessionSql string

	//go:embed sql/get_agent_sessions_by_issue_id.sql
	GetAgentSessionsByIssueIdSql string

	//go:embed sql/get_agent_session_event.sql
	GetAgentSessionEventSql string

	//go:embed sql/get_agent_session_event_by_git_ref.sql
	GetAgentSessionEventByGitRefSql string

	//go:embed sql/get_git_connection.sql
	GetGitConnectionSql string

	//go:embed sql/insert_session_event.sql
	InsertSessionEventSql string

	//go:embed sql/insert_job.sql
	InsertJobSql string

	//go:embed sql/resume.sql
	ResumeJobSql string

	//go:embed sql/upsert_agent_session.sql
	UpsertAgentSessionSql string

	//go:embed sql/update_session_event_result.sql
	UpdateSessionEventResultSql string

	//go:embed sql/upsert_git_connection.sql
	UpsertGitConnectionSql string

	//go:embed sql/reset_git_connection.sql
	ResetGitConnectionSql string

	//go:embed sql/cancel.sql
	CancelSql string
)

type PostgresRepo interface {
	interfaces.Repository
	interfaces.RepositoryOrg
	interfaces.RepositoryGit
}

type postgres struct {
	client shared.PostgresPool
}

func NewPostgres(client shared.PostgresPool) PostgresRepo {
	return &postgres{
		client: client,
	}
}

func (p *postgres) GetOrganization(ctx context.Context, identifier string) (*shared.Organization, error) {
	var row shared.Organization

	err := p.client.
		QueryRow(ctx, GetOrganizationSql, identifier).
		Scan(
			&row.Identifier,
			&row.Provider,
			&row.Name,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("[agent-session][postgres] organization doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("[agent-session][postgres] failed to get organization by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetAgentSession(ctx context.Context, identifier string) (*types.Session, error) {
	var row types.Session

	err := p.client.
		QueryRow(ctx, GetAgentSessionSql, identifier).
		Scan(
			&row.OrganizationIdentifier,
			&row.Identifier,
			&row.Provider,
			&row.IssueId,
			&row.Creator,
			&row.RepoFullName,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("[agent-session][postgres] agent session doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("[agent-session][postgres] failed to get agent session by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*types.Session, error) {
	rows, err := p.client.Query(ctx, GetAgentSessionsByIssueIdSql, issueId)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to get agent sessions by issue id", "err", err, "issue_id", issueId)
		return nil, err
	}

	defer rows.Close()

	var sessions []*types.Session

	for rows.Next() {
		var row types.Session

		if err := rows.Scan(
			&row.OrganizationIdentifier,
			&row.Identifier,
			&row.Provider,
			&row.IssueId,
			&row.Creator,
			&row.RepoFullName,
		); err != nil {
			slog.Error("[agent-session][postgres] failed to scan agent session row", "err", err, "issue_id", issueId)
			return nil, err
		}

		sessions = append(sessions, &row)
	}

	if err := rows.Err(); err != nil {
		slog.Error("[agent-session][postgres] failed to iterate agent session rows", "err", err, "issue_id", issueId)
		return nil, err
	}

	return sessions, nil
}

func (p *postgres) GetAgentSessionEvent(ctx context.Context, identifier string) (*types.SessionEvent, error) {
	var row types.SessionEvent

	err := p.client.
		QueryRow(ctx, GetAgentSessionEventSql, identifier).
		Scan(
			&row.SessionIdentifier,
			&row.Identifier,
			&row.Payload,
			&row.Seed,
			&row.GitRef,
			&row.Result,
			&row.Reason,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("[agent-session][postgres] agent session event doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("[agent-session][postgres] failed to get agent session event by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetAgentSessionEventByGitRef(ctx context.Context, ref string, repoFullName string) (*types.SessionEvent, error) {
	var row types.SessionEvent

	err := p.client.
		QueryRow(ctx, GetAgentSessionEventByGitRefSql, ref, repoFullName).
		Scan(
			&row.SessionIdentifier,
			&row.Identifier,
			&row.Payload,
			&row.Seed,
			&row.GitRef,
			&row.Result,
			&row.Reason,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("[agent-session][postgres] agent session event doesn't exist in the database", "git_ref", ref)
			return nil, nil
		}

		slog.Error("[agent-session][postgres] failed to get agent session event by identifier", "err", err, "ref", ref)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetConnection(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
	var row types.GitConnection

	err := p.client.
		QueryRow(ctx, GetGitConnectionSql, repoFullName).
		Scan(
			&row.SessionEventIdentifier,
			&row.RepoFullName,
			&row.Connected,
			&row.InstallationId,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("[agent-session][postgres] git connection doesn't exist in the database", "repo_full_name", repoFullName)
			return nil, nil
		}

		slog.Error("[agent-session][postgres] failed to get git connection by repo full name", "err", err, "repo_full_name", repoFullName)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) CreateSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	tx, err := p.client.Begin(ctx)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to being db transaction", "err", err, "event_identifier", event.Identifier)
		return err
	}

	if _, err := tx.Exec(
		ctx,
		InsertSessionEventSql,
		event.SessionIdentifier,
		event.Identifier,
		event.Payload,
		event.Seed,
		event.GitRef,
		event.Result,
		event.Reason,
	); err != nil {
		slog.Error("[agent-session][postgres] failed to insert session event", "event_identifier", event.Identifier, "err", err)

		if err := tx.Rollback(ctx); err != nil {
			slog.Error("[agent-session][postgres] failed to rollback transaction", "event_identifier", event.Identifier, "err", err)
		}

		return err
	}

	if _, err := tx.Exec(ctx, InsertJobSql, event.Identifier, event.SessionIdentifier); err != nil {
		slog.Error("[agent-session][postgres] failed to job", "event_identifier", event.Identifier, "err", err)

		if err := tx.Rollback(ctx); err != nil {
			slog.Error("[agent-session][postgres] failed to rollback transaction", "event_identifier", event.Identifier, "err", err)
		}

		return err
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("[agent-session][postgres] failed to commit transaction", "event_identifier", event.Identifier, "err", err)

		if err := tx.Rollback(ctx); err != nil {
			slog.Error("[agent-session][postgres] failed to rollback transaction", "event_identifier", event.Identifier, "err", err)
		}

		return err
	}

	return nil
}

func (p *postgres) ResumeSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	_, err := p.client.Exec(
		ctx,
		ResumeJobSql,
		event.Identifier,
	)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to resume session event", "err", err, "event_identifier", event.Identifier)
		return err
	}

	return nil
}

func (p *postgres) UpsertAgentSession(ctx context.Context, session *types.Session) error {
	_, err := p.client.Exec(
		ctx,
		UpsertAgentSessionSql,
		session.OrganizationIdentifier,
		session.Identifier,
		session.Provider,
		session.IssueId,
		session.Creator,
		session.RepoFullName,
	)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to upsert agent session", "err", err, "org_identifier", session.OrganizationIdentifier, "provider", session.Provider, "session_identifier", session.Identifier)
		return err
	}

	return nil
}

func (p *postgres) UpdateSessionEventResult(ctx context.Context, event *types.SessionEvent) error {
	_, err := p.client.Exec(
		ctx,
		UpdateSessionEventResultSql,
		event.Identifier,
		event.GitRef,
		event.Result,
	)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to update session event result", "err", err, "event_identifier", event.Identifier)
		return err
	}

	return nil

}

func (p *postgres) UpsertConnection(ctx context.Context, connection *types.GitConnection) error {
	err := p.client.
		QueryRow(
			ctx,
			UpsertGitConnectionSql,
			connection.SessionEventIdentifier,
			connection.RepoFullName,
			connection.Connected,
			connection.InstallationId,
		).
		Scan(
			&connection.SessionEventIdentifier,
		)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to upsert git connection", "err", err, "event_identifier", connection.SessionEventIdentifier, "repo", connection.RepoFullName)
		return err
	}

	return nil
}

func (p *postgres) ResetConnection(ctx context.Context, installationId string, repos []string) error {
	_, err := p.client.Exec(ctx, ResetGitConnectionSql, installationId, repos)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to reset git connection", "err", err, "installation_id", installationId)
		return err
	}

	return nil
}

func (p *postgres) CancelSession(ctx context.Context, queuedBy, reason string) (int, error) {
	tags, err := p.client.Exec(ctx, CancelSql, queuedBy, reason)

	if err != nil {
		slog.Error("[agent-session][postgres] failed to cancel jobs queued by", "queued_by", queuedBy, "err", err)
		return 0, err
	}

	return int(tags.RowsAffected()), nil
}
