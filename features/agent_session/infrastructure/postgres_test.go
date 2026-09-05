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
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
)

type PostgresSuite struct {
	suite.Suite
	repo PostgresRepo
	pool *mockPool
}

func TestPostgresSuite(t *testing.T) {
	suite.Run(t, new(PostgresSuite))
}

func (s *PostgresSuite) SetupTest() {
	s.pool = &mockPool{}
	s.repo = NewPostgres(s.pool)
	s.Require().IsType(&postgres{}, s.repo)
}

// --- GetOrganization ---

func (s *PostgresSuite) TestGetOrganization_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal(GetOrganizationSql, sql)
		s.Equal([]any{"org-1"}, args)
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "org-1"
			*dest[1].(*shared.PlatformProvider) = shared.PlatformProvider_Linear
			*dest[2].(*string) = "Acme"
			return nil
		}}
	}

	org, err := s.repo.GetOrganization(context.Background(), "org-1")

	s.Require().NoError(err)
	s.Require().NotNil(org)
	s.Equal("org-1", org.Identifier)
	s.Equal(shared.PlatformProvider_Linear, org.Provider)
	s.Equal("Acme", org.Name)
}

func (s *PostgresSuite) TestGetOrganization_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	}

	org, err := s.repo.GetOrganization(context.Background(), "org-1")

	s.NoError(err)
	s.Nil(org)
}

func (s *PostgresSuite) TestGetOrganization_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
	}

	org, err := s.repo.GetOrganization(context.Background(), "org-1")

	s.Error(err)
	s.Nil(org)
}

// --- GetAgentSession ---

func (s *PostgresSuite) TestGetAgentSession_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal(GetAgentSessionSql, sql)
		s.Equal([]any{"sess-1"}, args)
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "org-1"
			*dest[1].(*string) = "sess-1"
			*dest[2].(*shared.PlatformProvider) = shared.PlatformProvider_Linear
			*dest[3].(*string) = "issue-1"
			*dest[4].(*string) = "user-1"
			*dest[5].(**string) = nil
			return nil
		}}
	}

	session, err := s.repo.GetAgentSession(context.Background(), "sess-1")

	s.Require().NoError(err)
	s.Require().NotNil(session)
	s.Equal("sess-1", session.Identifier)
	s.Equal("org-1", session.OrganizationIdentifier)
	s.Nil(session.RepoFullName)
}

func (s *PostgresSuite) TestGetAgentSession_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	}

	session, err := s.repo.GetAgentSession(context.Background(), "sess-1")

	s.NoError(err)
	s.Nil(session)
}

func (s *PostgresSuite) TestGetAgentSession_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
	}

	session, err := s.repo.GetAgentSession(context.Background(), "sess-1")

	s.Error(err)
	s.Nil(session)
}

// --- GetAgentSessionsByIssueId ---

func (s *PostgresSuite) TestGetAgentSessionsByIssueId_Success() {
	s.pool.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		s.Equal(GetAgentSessionsByIssueIdSql, sql)
		s.Equal([]any{"issue-1"}, args)
		return &mockRows{rows: []mockRowValues{
			{values: []any{"org-1", "sess-1", shared.PlatformProvider_Linear, "issue-1", "user-1", nil}},
			{values: []any{"org-1", "sess-2", shared.PlatformProvider_Linear, "issue-1", "user-2", "workdock/repo"}},
		}}, nil
	}

	sessions, err := s.repo.GetAgentSessionsByIssueId(context.Background(), "issue-1")

	s.Require().NoError(err)
	s.Require().Len(sessions, 2)
	s.Equal("sess-1", sessions[0].Identifier)
	s.Nil(sessions[0].RepoFullName)
	s.Equal("sess-2", sessions[1].Identifier)
	s.Require().NotNil(sessions[1].RepoFullName)
	s.Equal("workdock/repo", *sessions[1].RepoFullName)
}

func (s *PostgresSuite) TestGetAgentSessionsByIssueId_Empty() {
	s.pool.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &mockRows{}, nil
	}

	sessions, err := s.repo.GetAgentSessionsByIssueId(context.Background(), "issue-1")

	s.Require().NoError(err)
	s.Empty(sessions)
}

func (s *PostgresSuite) TestGetAgentSessionsByIssueId_QueryError() {
	s.pool.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return nil, fmt.Errorf("db error")
	}

	sessions, err := s.repo.GetAgentSessionsByIssueId(context.Background(), "issue-1")

	s.Error(err)
	s.Nil(sessions)
}

func (s *PostgresSuite) TestGetAgentSessionsByIssueId_ScanError() {
	s.pool.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &mockRows{rows: []mockRowValues{
			{scanErr: errors.New("scan failed")},
		}}, nil
	}

	sessions, err := s.repo.GetAgentSessionsByIssueId(context.Background(), "issue-1")

	s.Error(err)
	s.Nil(sessions)
}

func (s *PostgresSuite) TestGetAgentSessionsByIssueId_RowsIterationError() {
	s.pool.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &mockRows{rowsErr: errors.New("iteration failed")}, nil
	}

	sessions, err := s.repo.GetAgentSessionsByIssueId(context.Background(), "issue-1")

	s.Error(err)
	s.Nil(sessions)
}

// --- GetAgentSessionEvent ---

func (s *PostgresSuite) TestGetAgentSessionEvent_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal(GetAgentSessionEventSql, sql)
		s.Equal([]any{"evt-1"}, args)
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "sess-1"
			*dest[1].(*string) = "evt-1"
			*dest[2].(*json.RawMessage) = json.RawMessage(`{"a":1}`)
			*dest[3].(**string) = nil
			*dest[4].(**string) = nil
			*dest[5].(**types.SessionEventResult) = nil
			*dest[6].(*types.AgentSessionEventReason) = types.AgentSessionEventReason_Prompt
			return nil
		}}
	}

	event, err := s.repo.GetAgentSessionEvent(context.Background(), "evt-1")

	s.Require().NoError(err)
	s.Require().NotNil(event)
	s.Equal("evt-1", event.Identifier)
	s.Equal("sess-1", event.SessionIdentifier)
	s.Equal(types.AgentSessionEventReason_Prompt, event.Reason)
}

func (s *PostgresSuite) TestGetAgentSessionEvent_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	}

	event, err := s.repo.GetAgentSessionEvent(context.Background(), "evt-1")

	s.NoError(err)
	s.Nil(event)
}

func (s *PostgresSuite) TestGetAgentSessionEvent_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
	}

	event, err := s.repo.GetAgentSessionEvent(context.Background(), "evt-1")

	s.Error(err)
	s.Nil(event)
}

// --- GetAgentSessionEventByGitRef ---

func (s *PostgresSuite) TestGetAgentSessionEventByGitRef_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal(GetAgentSessionEventByGitRefSql, sql)
		s.Equal([]any{"workdock/main", "workdock/repo"}, args)
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "sess-1"
			*dest[1].(*string) = "evt-1"
			*dest[2].(*json.RawMessage) = json.RawMessage(`{"a":1}`)
			*dest[3].(**string) = nil
			ref := "workdock/main"
			*dest[4].(**string) = &ref
			*dest[5].(**types.SessionEventResult) = nil
			*dest[6].(*types.AgentSessionEventReason) = types.AgentSessionEventReason_PRComment
			return nil
		}}
	}

	event, err := s.repo.GetAgentSessionEventByGitRef(context.Background(), "workdock/main", "workdock/repo")

	s.Require().NoError(err)
	s.Require().NotNil(event)
	s.Equal("evt-1", event.Identifier)
	s.Require().NotNil(event.GitRef)
	s.Equal("workdock/main", *event.GitRef)
}

func (s *PostgresSuite) TestGetAgentSessionEventByGitRef_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	}

	event, err := s.repo.GetAgentSessionEventByGitRef(context.Background(), "workdock/main", "workdock/repo")

	s.NoError(err)
	s.Nil(event)
}

func (s *PostgresSuite) TestGetAgentSessionEventByGitRef_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
	}

	event, err := s.repo.GetAgentSessionEventByGitRef(context.Background(), "workdock/main", "workdock/repo")

	s.Error(err)
	s.Nil(event)
}

// --- GetConnection ---

func (s *PostgresSuite) TestGetConnection_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal(GetGitConnectionSql, sql)
		s.Equal([]any{"workdock/repo"}, args)
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(**string) = nil
			*dest[1].(*string) = "workdock/repo"
			*dest[2].(*bool) = true
			installationId := "install-1"
			*dest[3].(**string) = &installationId
			return nil
		}}
	}

	connection, err := s.repo.GetConnection(context.Background(), "workdock/repo")

	s.Require().NoError(err)
	s.Require().NotNil(connection)
	s.Equal("workdock/repo", connection.RepoFullName)
	s.True(connection.Connected)
	s.Require().NotNil(connection.InstallationId)
	s.Equal("install-1", *connection.InstallationId)
}

func (s *PostgresSuite) TestGetConnection_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	}

	connection, err := s.repo.GetConnection(context.Background(), "workdock/repo")

	s.NoError(err)
	s.Nil(connection)
}

func (s *PostgresSuite) TestGetConnection_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
	}

	connection, err := s.repo.GetConnection(context.Background(), "workdock/repo")

	s.Error(err)
	s.Nil(connection)
}

// --- CreateSessionEvent (transactional) ---

func (s *PostgresSuite) TestCreateSessionEvent_Success() {
	event := &types.SessionEvent{
		SessionIdentifier: "sess-1",
		Identifier:        "evt-1",
		Payload:           []byte(`{"a":1}`),
		Reason:            types.AgentSessionEventReason_Prompt,
	}

	var execCalls []string
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				execCalls = append(execCalls, sql)
				if sql == InsertSessionEventSql {
					s.Equal([]any{
						event.SessionIdentifier,
						event.Identifier,
						event.Payload,
						event.Seed,
						event.GitRef,
						event.Result,
						event.Reason,
					}, args)
				}
				if sql == InsertJobSql {
					s.Equal([]any{"evt-1", "sess-1"}, args)
				}
				return pgconn.CommandTag{}, nil
			},
			commitFn: func(ctx context.Context) error { return nil },
		}, nil
	}

	err := s.repo.CreateSessionEvent(context.Background(), event)

	s.Require().NoError(err)
	s.Equal([]string{InsertSessionEventSql, InsertJobSql}, execCalls)
}

func (s *PostgresSuite) TestCreateSessionEvent_BeginError() {
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return nil, errors.New("begin failed")
	}

	err := s.repo.CreateSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "begin failed")
}

func (s *PostgresSuite) TestCreateSessionEvent_InsertEventError_Rollbacks() {
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if sql == InsertSessionEventSql {
					return pgconn.CommandTag{}, errors.New("insert event failed")
				}
				return pgconn.CommandTag{}, nil
			},
			rollbackFn: func(ctx context.Context) error { return nil },
		}, nil
	}

	err := s.repo.CreateSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "insert event failed")
}

func (s *PostgresSuite) TestCreateSessionEvent_InsertJobError_Rollbacks() {
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if sql == InsertJobSql {
					return pgconn.CommandTag{}, errors.New("insert job failed")
				}
				return pgconn.CommandTag{}, nil
			},
			rollbackFn: func(ctx context.Context) error { return nil },
		}, nil
	}

	err := s.repo.CreateSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "insert job failed")
}

func (s *PostgresSuite) TestCreateSessionEvent_CommitError_Rollbacks() {
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
			commitFn:   func(ctx context.Context) error { return errors.New("commit failed") },
			rollbackFn: func(ctx context.Context) error { return nil },
		}, nil
	}

	err := s.repo.CreateSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "commit failed")
}

func (s *PostgresSuite) TestCreateSessionEvent_RollbackErrorsAreLoggedNotMasked() {
	rollbackCalls := 0
	failingRollback := func(ctx context.Context) error {
		rollbackCalls++
		return errors.New("rollback failed")
	}

	tests := []struct {
		name      string
		execFn    func(sql string) error
		commitErr bool
		cause     string
	}{
		{
			name:  "insert event error",
			cause: "insert event failed",
			execFn: func(sql string) error {
				if sql == InsertSessionEventSql {
					return errors.New("insert event failed")
				}
				return nil
			},
		},
		{
			name:  "insert job error",
			cause: "insert job failed",
			execFn: func(sql string) error {
				if sql == InsertJobSql {
					return errors.New("insert job failed")
				}
				return nil
			},
		},
		{
			name:      "commit error",
			cause:     "commit failed",
			execFn:    func(sql string) error { return nil },
			commitErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			rollbackCalls = 0
			s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
				return &mockTx{
					execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
						if err := tt.execFn(sql); err != nil {
							return pgconn.CommandTag{}, err
						}
						return pgconn.CommandTag{}, nil
					},
					commitFn: func(ctx context.Context) error {
						if tt.commitErr {
							return errors.New("commit failed")
						}
						return nil
					},
					rollbackFn: failingRollback,
				}, nil
			}

			err := s.repo.CreateSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

			s.Error(err)
			s.ErrorContains(err, tt.cause)
			s.Equal(1, rollbackCalls, "rollback must run on the failure path")
		})
	}
}

// --- ResumeSessionEvent ---

func (s *PostgresSuite) TestResumeSessionEvent_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal(ResumeJobSql, sql)
		s.Equal([]any{"evt-1"}, args)
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}

	err := s.repo.ResumeSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.NoError(err)
}

func (s *PostgresSuite) TestResumeSessionEvent_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db error")
	}

	err := s.repo.ResumeSessionEvent(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.Error(err)
}

// --- UpsertAgentSession ---

func (s *PostgresSuite) TestUpsertAgentSession_Success() {
	repoName := "workdock/repo"
	session := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "sess-1",
		Provider:               shared.PlatformProvider_Linear,
		IssueId:                "issue-1",
		Creator:                "user-1",
		RepoFullName:           &repoName,
	}

	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal(UpsertAgentSessionSql, sql)
		s.Equal([]any{"org-1", "sess-1", shared.PlatformProvider_Linear, "issue-1", "user-1", &repoName}, args)
		return pgconn.CommandTag{}, nil
	}

	err := s.repo.UpsertAgentSession(context.Background(), session)

	s.NoError(err)
}

func (s *PostgresSuite) TestUpsertAgentSession_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db error")
	}

	err := s.repo.UpsertAgentSession(context.Background(), &types.Session{Identifier: "sess-1"})

	s.Error(err)
}

// --- UpdateSessionEventResult ---

func (s *PostgresSuite) TestUpdateSessionEventResult_Success() {
	ref := "workdock/main"
	event := &types.SessionEvent{
		Identifier: "evt-1",
		GitRef:     &ref,
		Result:     &types.SessionEventResult{},
	}

	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal(UpdateSessionEventResultSql, sql)
		s.Equal([]any{"evt-1", &ref, event.Result}, args)
		return pgconn.CommandTag{}, nil
	}

	err := s.repo.UpdateSessionEventResult(context.Background(), event)

	s.NoError(err)
}

func (s *PostgresSuite) TestUpdateSessionEventResult_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db error")
	}

	err := s.repo.UpdateSessionEventResult(context.Background(), &types.SessionEvent{Identifier: "evt-1"})

	s.Error(err)
}

// --- UpsertConnection ---

func (s *PostgresSuite) TestUpsertConnection_Success() {
	connection := &types.GitConnection{
		RepoFullName:   "workdock/repo",
		Connected:      true,
		InstallationId: nil,
	}

	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal(UpsertGitConnectionSql, sql)
		return &mockRow{scanFn: func(dest ...any) error {
			id := "install-1"
			*dest[0].(**string) = &id
			return nil
		}}
	}

	err := s.repo.UpsertConnection(context.Background(), connection)

	s.Require().NoError(err)
	s.Require().NotNil(connection.SessionEventIdentifier)
	s.Equal("install-1", *connection.SessionEventIdentifier)
}

func (s *PostgresSuite) TestUpsertConnection_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
	}

	err := s.repo.UpsertConnection(context.Background(), &types.GitConnection{RepoFullName: "workdock/repo"})

	s.Error(err)
}

// --- ResetConnection ---

func (s *PostgresSuite) TestResetConnection_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal(ResetGitConnectionSql, sql)
		s.Equal([]any{"install-1", []string{"workdock/repo"}}, args)
		return pgconn.CommandTag{}, nil
	}

	err := s.repo.ResetConnection(context.Background(), "install-1", []string{"workdock/repo"})

	s.NoError(err)
}

func (s *PostgresSuite) TestResetConnection_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db error")
	}

	err := s.repo.ResetConnection(context.Background(), "install-1", []string{"workdock/repo"})

	s.Error(err)
}

// --- CancelSession ---

func (s *PostgresSuite) TestCancelSession_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal(CancelSql, sql)
		s.Equal([]any{"sess-1", "cancelled by user"}, args)
		return pgconn.NewCommandTag("UPDATE 3"), nil
	}

	count, err := s.repo.CancelSession(context.Background(), "sess-1", "cancelled by user")

	s.Require().NoError(err)
	s.Equal(3, count)
}

func (s *PostgresSuite) TestCancelSession_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db error")
	}

	count, err := s.repo.CancelSession(context.Background(), "sess-1", "cancelled by user")

	s.Error(err)
	s.Equal(0, count)
}
