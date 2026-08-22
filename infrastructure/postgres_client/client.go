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

package postgres_client

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/types"
)

var (
	_ repositories.OrganizationRepository     = (*PostgresService)(nil)
	_ repositories.SessionRepository          = (*PostgresService)(nil)
	_ repositories.GitHubConnectionRepository = (*PostgresService)(nil)
)

var (
	//go:embed get_organization.sql
	GetOrganizationSql string

	//go:embed get_agent_session.sql
	GetAgentSessionSql string

	//go:embed get_agent_sessions_by_issue_id.sql
	GetAgentSessionsByIssueIdSql string

	//go:embed get_agent_session_event.sql
	GetAgentSessionEventSql string

	//go:embed get_agent_session_event_by_git_ref.sql
	GetAgentSessionEventByGitRefSql string

	//go:embed get_github_connection.sql
	GetGitHubConnectionSql string

	//go:embed insert_session_event.sql
	InsertSessionEventSql string

	//go:embed insert_job.sql
	InsertJobSql string

	//go:embed upsert_organization.sql
	UpsertOrganizationSql string

	//go:embed upsert_agent_session.sql
	UpsertAgentSessionSql string

	//go:embed upsert_github_connection.sql
	UpsertGitHubConnectionSql string

	//go:embed update_session_event_result.sql
	UpdateSessionEventResultSql string

	//go:embed reset_github_connection.sql
	ResetGitHubConnectionSql string

	//go:embed cancel.sql
	CancelSql string
)

type PostgresServiceConfig struct {
	DatabaseUrl string `yaml:"database_url"`
}

type dbPool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
}

type PostgresService struct {
	config PostgresServiceConfig
	client dbPool
}

func New(ctx context.Context, config PostgresServiceConfig) (*PostgresService, error) {
	client, err := pgxpool.New(context.Background(), config.DatabaseUrl)

	if err != nil {
		slog.Error("failed to start postgres connection pool", "err", err)
		return nil, err
	}

	return &PostgresService{
		config: config,
		client: client,
	}, nil
}

func (s *PostgresService) GetOrganization(ctx context.Context, identifier string) (*types.Organization, error) {
	var row types.Organization

	err := s.client.
		QueryRow(ctx, GetOrganizationSql, identifier).
		Scan(
			&row.Identifier,
			&row.Provider,
			&row.Name,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("organization doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("failed to get organization by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (s *PostgresService) GetAgentSession(ctx context.Context, identifier string) (*types.Session, error) {
	var row types.Session

	err := s.client.
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
			slog.Debug("agent session doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("failed to get agent session by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (s *PostgresService) GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*types.Session, error) {
	rows, err := s.client.Query(ctx, GetAgentSessionsByIssueIdSql, issueId)

	if err != nil {
		slog.Error("failed to get agent sessions by issue id", "err", err, "issue_id", issueId)
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
			slog.Error("failed to scan agent session row", "err", err, "issue_id", issueId)
			return nil, err
		}

		sessions = append(sessions, &row)
	}

	if err := rows.Err(); err != nil {
		slog.Error("failed to iterate agent session rows", "err", err, "issue_id", issueId)
		return nil, err
	}

	return sessions, nil
}

func (s *PostgresService) GetAgentSessionEvent(ctx context.Context, identifier string) (*types.SessionEvent, error) {
	var row types.SessionEvent

	err := s.client.
		QueryRow(ctx, GetAgentSessionEventSql, identifier).
		Scan(
			&row.SessionIdentifier,
			&row.Identifier,
			&row.Payload,
			&row.Seed,
			&row.GitRef,
			&row.Result,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("agent session event doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("failed to get agent session event by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (s *PostgresService) GetAgentSessionEventByGitRef(ctx context.Context, ref string, repoFullName string) (*types.SessionEvent, error) {
	var row types.SessionEvent

	err := s.client.
		QueryRow(ctx, GetAgentSessionEventByGitRefSql, ref, repoFullName).
		Scan(
			&row.SessionIdentifier,
			&row.Identifier,
			&row.Payload,
			&row.Seed,
			&row.GitRef,
			&row.Result,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("agent session event doesn't exist in the database", "git_ref", ref)
			return nil, nil
		}

		slog.Error("failed to get agent session event by identifier", "err", err, "ref", ref)
		return nil, err
	}

	return &row, nil
}

func (s *PostgresService) GetGitHubConnection(ctx context.Context, repoFullName string) (*types.GitHubConnection, error) {
	var row types.GitHubConnection

	err := s.client.
		QueryRow(ctx, GetGitHubConnectionSql, repoFullName).
		Scan(
			&row.SessionEventIdentifier,
			&row.RepoFullName,
			&row.Connected,
			&row.InstallationId,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("github connection doesn't exist in the database", "repo_full_name", repoFullName)
			return nil, nil
		}

		slog.Error("failed to get github connection by repo full name", "err", err, "repo_full_name", repoFullName)
		return nil, err
	}

	return &row, nil
}

func (s *PostgresService) CreateSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	tx, err := s.client.Begin(ctx)

	if err != nil {
		slog.Error("failed to being db transaction", "err", err, "event_identifier", event.Identifier)
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
	); err != nil {
		slog.Error("failed to insert session event", "event_identifier", event.Identifier, "err", err)

		if err := tx.Rollback(ctx); err != nil {
			slog.Error("failed to rollback transaction", "event_identifier", event.Identifier, "err", err)
		}

		return err
	}

	if _, err := tx.Exec(ctx, InsertJobSql, event.Identifier, event.SessionIdentifier); err != nil {
		slog.Error("failed to job", "event_identifier", event.Identifier, "err", err)

		if err := tx.Rollback(ctx); err != nil {
			slog.Error("failed to rollback transaction", "event_identifier", event.Identifier, "err", err)
		}

		return err
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("failed to commit transaction", "event_identifier", event.Identifier, "err", err)

		if err := tx.Rollback(ctx); err != nil {
			slog.Error("failed to rollback transaction", "event_identifier", event.Identifier, "err", err)
		}

		return err
	}

	return nil
}

func (s *PostgresService) UpsertOrganization(ctx context.Context, org *types.Organization) error {
	_, err := s.client.Exec(ctx, UpsertOrganizationSql, org.Identifier, org.Provider, org.Name)

	if err != nil {
		slog.Error("failed to upsert organization", "err", err, "org_identifier", org.Identifier, "provider", org.Provider)
		return err
	}

	return nil
}

func (s *PostgresService) UpsertAgentSession(ctx context.Context, session *types.Session) error {
	_, err := s.client.Exec(
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
		slog.Error("failed to upsert agent session", "err", err, "org_identifier", session.OrganizationIdentifier, "provider", session.Provider, "session_identifier", session.Identifier)
		return err
	}

	return nil
}

func (s *PostgresService) UpdateSessionEventResult(ctx context.Context, event *types.SessionEvent) error {
	_, err := s.client.Exec(
		ctx,
		UpdateSessionEventResultSql,
		event.Identifier,
		event.GitRef,
		event.Result,
	)

	if err != nil {
		slog.Error("failed to update session event result", "err", err, "event_identifier", event.Identifier)
		return err
	}

	return nil
}

func (s *PostgresService) UpsertGitHubConnection(ctx context.Context, githubConnection *types.GitHubConnection) error {
	err := s.client.
		QueryRow(
			ctx,
			UpsertGitHubConnectionSql,
			githubConnection.SessionEventIdentifier,
			githubConnection.RepoFullName,
			githubConnection.Connected,
			githubConnection.InstallationId,
		).
		Scan(
			&githubConnection.SessionEventIdentifier,
		)

	if err != nil {
		slog.Error("failed to upsert github connection", "err", err, "event_identifier", githubConnection.SessionEventIdentifier, "repo", githubConnection.RepoFullName)
		return err
	}

	return nil
}

func (s *PostgresService) ResetGitHubConnection(ctx context.Context, installationId string, repos []string) error {
	_, err := s.client.Exec(ctx, ResetGitHubConnectionSql, installationId, repos)

	if err != nil {
		slog.Error("failed to reset github connection", "err", err, "installation_id", installationId)
		return err
	}

	return nil
}

func (s *PostgresService) CancelSession(ctx context.Context, queuedBy, reason string) (int, error) {
	tags, err := s.client.Exec(ctx, CancelSql, queuedBy, reason)

	if err != nil {
		slog.Error("failed to cancel jobs queued by", "queued_by", queuedBy, "err", err)
		return 0, err
	}

	return int(tags.RowsAffected()), nil
}

func (s *PostgresService) Close() {
	s.client.Close()
}
