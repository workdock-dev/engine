package infrastructure

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
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

	//go:embed sql/insert_session_event.sql
	InsertSessionEventSql string

	//go:embed sql/insert_job.sql
	InsertJobSql string

	//go:embed sql/upsert_agent_session.sql
	UpsertAgentSessionSql string

	//go:embed sql/update_session_event_result.sql
	UpdateSessionEventResultSql string

	//go:embed sql/cancel.sql
	CancelSql string
)

type PostgresRepo interface {
	interfaces.Repository
	interfaces.RepositoryOrg
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
			slog.Debug("organization doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("failed to get organization by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetAgentSession(ctx context.Context, identifier string) (*shared.Session, error) {
	var row shared.Session

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
			slog.Debug("agent session doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("failed to get agent session by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*shared.Session, error) {
	rows, err := p.client.Query(ctx, GetAgentSessionsByIssueIdSql, issueId)

	if err != nil {
		slog.Error("failed to get agent sessions by issue id", "err", err, "issue_id", issueId)
		return nil, err
	}

	defer rows.Close()

	var sessions []*shared.Session

	for rows.Next() {
		var row shared.Session

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

func (p *postgres) GetAgentSessionEvent(ctx context.Context, identifier string) (*shared.SessionEvent, error) {
	var row shared.SessionEvent

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
			slog.Debug("agent session event doesn't exist in the database", "identifier", identifier)
			return nil, nil
		}

		slog.Error("failed to get agent session event by identifier", "err", err, "identifier", identifier)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) GetAgentSessionEventByGitRef(ctx context.Context, ref string, repoFullName string) (*shared.SessionEvent, error) {
	var row shared.SessionEvent

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
			slog.Debug("agent session event doesn't exist in the database", "git_ref", ref)
			return nil, nil
		}

		slog.Error("failed to get agent session event by identifier", "err", err, "ref", ref)
		return nil, err
	}

	return &row, nil
}

func (p *postgres) CreateSessionEvent(ctx context.Context, event *shared.SessionEvent) error {
	tx, err := p.client.Begin(ctx)

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
		event.Reason,
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

func (p *postgres) UpsertAgentSession(ctx context.Context, session *shared.Session) error {
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
		slog.Error("failed to upsert agent session", "err", err, "org_identifier", session.OrganizationIdentifier, "provider", session.Provider, "session_identifier", session.Identifier)
		return err
	}

	return nil
}

func (p *postgres) UpdateSessionEventResult(ctx context.Context, event *shared.SessionEvent) error {
	_, err := p.client.Exec(
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

func (p *postgres) CancelSession(ctx context.Context, queuedBy, reason string) (int, error) {
	tags, err := p.client.Exec(ctx, CancelSql, queuedBy, reason)

	if err != nil {
		slog.Error("failed to cancel jobs queued by", "queued_by", queuedBy, "err", err)
		return 0, err
	}

	return int(tags.RowsAffected()), nil
}
