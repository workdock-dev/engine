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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/suite"
)

type PostgresServiceSuite struct {
	suite.Suite
	service *PostgresService
	pool    *mockPool
}

func TestPostgresServiceSuite(t *testing.T) {
	suite.Run(t, new(PostgresServiceSuite))
}

func (s *PostgresServiceSuite) SetupTest() {
	s.pool = &mockPool{}
	s.service = &PostgresService{
		config: PostgresServiceConfig{DatabaseUrl: "postgres://test"},
		client: s.pool,
	}
}

// --- GetOrganization ---

func (s *PostgresServiceSuite) TestGetOrganization_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal("org-1", args[0])
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "org-1"
			*dest[1].(*types.PlatformProvider) = types.PlatformProvider_Linear
			*dest[2].(*string) = "Test Org"
			return nil
		}}
	}
	org, err := s.service.GetOrganization(context.Background(), "org-1")
	s.NoError(err)
	s.NotNil(org)
	s.Equal("org-1", org.Identifier)
	s.Equal(types.PlatformProvider_Linear, org.Provider)
	s.Equal("Test Org", org.Name)
}

func (s *PostgresServiceSuite) TestGetOrganization_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return pgx.ErrNoRows
		}}
	}
	org, err := s.service.GetOrganization(context.Background(), "missing")
	s.NoError(err)
	s.Nil(org)
}

func (s *PostgresServiceSuite) TestGetOrganization_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("connection lost")
		}}
	}
	org, err := s.service.GetOrganization(context.Background(), "org-1")
	s.Error(err)
	s.Nil(org)
}

// --- GetAgentSession ---

func (s *PostgresServiceSuite) TestGetAgentSession_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "org-1"
			*dest[1].(*string) = "session-1"
			*dest[2].(*types.PlatformProvider) = types.PlatformProvider_GitHub
			*dest[3].(*string) = "issue-1"
			*dest[4].(*string) = "user-1"
			*dest[5].(**string) = strPtr("owner/repo")
			return nil
		}}
	}
	session, err := s.service.GetAgentSession(context.Background(), "session-1")
	s.NoError(err)
	s.NotNil(session)
	s.Equal("session-1", session.Identifier)
	s.Equal("org-1", session.OrganizationIdentifier)
	s.Equal(types.PlatformProvider_GitHub, session.Provider)
	s.Equal("issue-1", session.IssueId)
	s.Equal("user-1", session.Creator)
	s.Equal("owner/repo", *session.RepoFullName)
}

func (s *PostgresServiceSuite) TestGetAgentSession_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return pgx.ErrNoRows
		}}
	}
	session, err := s.service.GetAgentSession(context.Background(), "missing")
	s.NoError(err)
	s.Nil(session)
}

func (s *PostgresServiceSuite) TestGetAgentSession_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	session, err := s.service.GetAgentSession(context.Background(), "session-1")
	s.Error(err)
	s.Nil(session)
}

// --- GetAgentSessionEvent ---

func (s *PostgresServiceSuite) TestGetAgentSessionEvent_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "session-1"
			*dest[1].(*string) = "event-1"
			*dest[2].(*json.RawMessage) = json.RawMessage(`{"type":"test"}`)
			*dest[3].(**string) = strPtr("seed-1")
			*dest[4].(**string) = strPtr("main")
			*dest[5].(**types.SessionEventResult) = nil
			return nil
		}}
	}
	event, err := s.service.GetAgentSessionEvent(context.Background(), "event-1")
	s.NoError(err)
	s.NotNil(event)
	s.Equal("event-1", event.Identifier)
	s.Equal("session-1", event.SessionIdentifier)
	s.Equal("seed-1", *event.Seed)
	s.Equal("main", *event.GitRef)
}

func (s *PostgresServiceSuite) TestGetAgentSessionEvent_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return pgx.ErrNoRows
		}}
	}
	event, err := s.service.GetAgentSessionEvent(context.Background(), "missing")
	s.NoError(err)
	s.Nil(event)
}

func (s *PostgresServiceSuite) TestGetAgentSessionEvent_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	event, err := s.service.GetAgentSessionEvent(context.Background(), "event-1")
	s.Error(err)
	s.Nil(event)
}

// --- GetAgentSessionEventByGitRef ---

func (s *PostgresServiceSuite) TestGetAgentSessionEventByGitRef_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal("abc123", args[0])
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "session-1"
			*dest[1].(*string) = "event-1"
			*dest[2].(*json.RawMessage) = json.RawMessage(`{}`)
			*dest[3].(**string) = nil
			*dest[4].(**string) = strPtr("abc123")
			*dest[5].(**types.SessionEventResult) = nil
			return nil
		}}
	}
	event, err := s.service.GetAgentSessionEventByGitRef(context.Background(), "abc123")
	s.NoError(err)
	s.NotNil(event)
	s.Equal("event-1", event.Identifier)
	s.Equal("abc123", *event.GitRef)
}

func (s *PostgresServiceSuite) TestGetAgentSessionEventByGitRef_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return pgx.ErrNoRows
		}}
	}
	event, err := s.service.GetAgentSessionEventByGitRef(context.Background(), "missing")
	s.NoError(err)
	s.Nil(event)
}

func (s *PostgresServiceSuite) TestGetAgentSessionEventByGitRef_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	event, err := s.service.GetAgentSessionEventByGitRef(context.Background(), "ref")
	s.Error(err)
	s.Nil(event)
}

// --- GetGitHubConnection ---

func (s *PostgresServiceSuite) TestGetGitHubConnection_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		s.Equal("owner/repo", args[0])
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "event-1"
			*dest[1].(*string) = "owner/repo"
			*dest[2].(*bool) = true
			*dest[3].(**string) = strPtr("inst-1")
			return nil
		}}
	}
	conn, err := s.service.GetGitHubConnection(context.Background(), "owner/repo")
	s.NoError(err)
	s.NotNil(conn)
	s.Equal("owner/repo", conn.RepoFullName)
	s.True(conn.Connected)
	s.Equal("inst-1", *conn.InstallationId)
	s.Equal("event-1", conn.SessionEventIdentifier)
}

func (s *PostgresServiceSuite) TestGetGitHubConnection_NotFound() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return pgx.ErrNoRows
		}}
	}
	conn, err := s.service.GetGitHubConnection(context.Background(), "missing/repo")
	s.NoError(err)
	s.Nil(conn)
}

func (s *PostgresServiceSuite) TestGetGitHubConnection_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	conn, err := s.service.GetGitHubConnection(context.Background(), "owner/repo")
	s.Error(err)
	s.Nil(conn)
}

// --- UpsertOrganization ---

func (s *PostgresServiceSuite) TestUpsertOrganization_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	err := s.service.UpsertOrganization(context.Background(), &types.Organization{
		Identifier: "org-1",
		Provider:   types.PlatformProvider_Linear,
		Name:       "Test Org",
	})
	s.NoError(err)
}

func (s *PostgresServiceSuite) TestUpsertOrganization_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("constraint violation")
	}
	err := s.service.UpsertOrganization(context.Background(), &types.Organization{
		Identifier: "org-1",
		Provider:   types.PlatformProvider_Linear,
		Name:       "Test Org",
	})
	s.Error(err)
}

// --- UpsertAgentSession ---

func (s *PostgresServiceSuite) TestUpsertAgentSession_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	err := s.service.UpsertAgentSession(context.Background(), &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "session-1",
		Provider:               types.PlatformProvider_GitHub,
		IssueId:                "issue-1",
		Creator:                "user-1",
		RepoFullName:           strPtr("owner/repo"),
	})
	s.NoError(err)
}

func (s *PostgresServiceSuite) TestUpsertAgentSession_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.service.UpsertAgentSession(context.Background(), &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "session-1",
	})
	s.Error(err)
}

// --- UpdateSessionEventResult ---

func (s *PostgresServiceSuite) TestUpdateSessionEventResult_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	err := s.service.UpdateSessionEventResult(context.Background(), &types.SessionEvent{
		Identifier: "event-1",
		GitRef:     strPtr("main"),
		Result:     &types.SessionEventResult{PullRequest: &types.PullRequest{Number: 1}},
	})
	s.NoError(err)
}

func (s *PostgresServiceSuite) TestUpdateSessionEventResult_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.service.UpdateSessionEventResult(context.Background(), &types.SessionEvent{
		Identifier: "event-1",
	})
	s.Error(err)
}

// --- UpsertGitHubConnection ---

func (s *PostgresServiceSuite) TestUpsertGitHubConnection_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "event-1"
			return nil
		}}
	}
	conn := &types.GitHubConnection{
		SessionEventIdentifier: "event-1",
		RepoFullName:           "owner/repo",
		Connected:              true,
		InstallationId:         strPtr("inst-1"),
	}
	err := s.service.UpsertGitHubConnection(context.Background(), conn)
	s.NoError(err)
	s.Equal("event-1", conn.SessionEventIdentifier)
}

func (s *PostgresServiceSuite) TestUpsertGitHubConnection_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	err := s.service.UpsertGitHubConnection(context.Background(), &types.GitHubConnection{
		SessionEventIdentifier: "event-1",
		RepoFullName:           "owner/repo",
	})
	s.Error(err)
}

// --- ResetGitHubConnection ---

func (s *PostgresServiceSuite) TestResetGitHubConnection_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("inst-1", args[0])
		return pgconn.CommandTag{}, nil
	}
	err := s.service.ResetGitHubConnection(context.Background(), "inst-1")
	s.NoError(err)
}

func (s *PostgresServiceSuite) TestResetGitHubConnection_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.service.ResetGitHubConnection(context.Background(), "inst-1")
	s.Error(err)
}

// --- CancelSession ---

func (s *PostgresServiceSuite) TestCancelSession_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("session-1", args[0])
		s.Equal("user cancelled", args[1])
		return pgconn.NewCommandTag("UPDATE 3"), nil
	}
	count, err := s.service.CancelSession(context.Background(), "session-1", "user cancelled")
	s.NoError(err)
	s.Equal(3, count)
}

func (s *PostgresServiceSuite) TestCancelSession_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	count, err := s.service.CancelSession(context.Background(), "session-1", "reason")
	s.Error(err)
	s.Equal(0, count)
}

// --- CreateSessionEvent ---

func (s *PostgresServiceSuite) TestCreateSessionEvent_Success() {
	var execCalls int
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.CommandTag{}, nil
			},
			commitFn: func(ctx context.Context) error {
				return nil
			},
		}, nil
	}
	err := s.service.CreateSessionEvent(context.Background(), &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "event-1",
		Payload:           json.RawMessage(`{"type":"test"}`),
	})
	s.NoError(err)
	s.Equal(2, execCalls)
}

func (s *PostgresServiceSuite) TestCreateSessionEvent_BeginError() {
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return nil, fmt.Errorf("connection pool exhausted")
	}
	err := s.service.CreateSessionEvent(context.Background(), &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "event-1",
	})
	s.Error(err)
}

func (s *PostgresServiceSuite) TestCreateSessionEvent_InsertEventError() {
	var rollbackCalled bool
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		callCount := 0
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				callCount++
				if callCount == 1 {
					return pgconn.CommandTag{}, fmt.Errorf("insert event failed")
				}
				return pgconn.CommandTag{}, nil
			},
			rollbackFn: func(ctx context.Context) error {
				rollbackCalled = true
				return nil
			},
		}, nil
	}
	err := s.service.CreateSessionEvent(context.Background(), &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "event-1",
	})
	s.Error(err)
	s.True(rollbackCalled)
}

func (s *PostgresServiceSuite) TestCreateSessionEvent_InsertJobError() {
	var rollbackCalled bool
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		callCount := 0
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				callCount++
				if callCount == 1 {
					return pgconn.CommandTag{}, nil // event insert succeeds
				}
				return pgconn.CommandTag{}, fmt.Errorf("insert job failed")
			},
			rollbackFn: func(ctx context.Context) error {
				rollbackCalled = true
				return nil
			},
		}, nil
	}
	err := s.service.CreateSessionEvent(context.Background(), &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "event-1",
	})
	s.Error(err)
	s.True(rollbackCalled)
}

func (s *PostgresServiceSuite) TestCreateSessionEvent_CommitError() {
	var rollbackCalled bool
	s.pool.beginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &mockTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
			commitFn: func(ctx context.Context) error {
				return fmt.Errorf("commit failed")
			},
			rollbackFn: func(ctx context.Context) error {
				rollbackCalled = true
				return nil
			},
		}, nil
	}
	err := s.service.CreateSessionEvent(context.Background(), &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "event-1",
	})
	s.Error(err)
	s.True(rollbackCalled)
}

// --- Close ---

func (s *PostgresServiceSuite) TestClose() {
	s.service.Close()
	s.True(s.pool.closed)
}

// --- helper ---

func strPtr(s string) *string {
	return &s
}
