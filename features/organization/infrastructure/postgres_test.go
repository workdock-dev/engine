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
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/suite"

	"github.com/workdock-dev/engine/features/organization/interfaces"
	"github.com/workdock-dev/engine/shared"
)

type PostgresSuite struct {
	suite.Suite
	pool *mockPool
	repo interfaces.Repository
}

func TestPostgresSuite(t *testing.T) {
	suite.Run(t, new(PostgresSuite))
}

func (s *PostgresSuite) SetupTest() {
	s.pool = &mockPool{}
	s.repo = NewPostgres(s.pool)
}

func (s *PostgresSuite) TestNewPostgres_ReturnsRepository() {
	s.IsType(&postgres{}, s.repo)
}

func (s *PostgresSuite) TestUpsertOrganization_Success() {
	org := &shared.Organization{
		Identifier: "org-123",
		Provider:   shared.PlatformProvider_Linear,
		Name:       "Workdock",
	}

	err := s.repo.UpsertOrganization(context.Background(), org)

	s.Require().NoError(err)
	s.Equal(1, s.pool.execCalled)
	s.Equal(UpsertOrganizationSql, s.pool.execSql)
	s.Equal([]any{"org-123", shared.PlatformProvider_Linear, "Workdock"}, s.pool.execArgs)
}

func (s *PostgresSuite) TestUpsertOrganization_Error() {
	execErr := errors.New("connection refused")
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, execErr
	}
	org := &shared.Organization{
		Identifier: "org-123",
		Provider:   shared.PlatformProvider_Linear,
		Name:       "Workdock",
	}

	err := s.repo.UpsertOrganization(context.Background(), org)

	s.ErrorIs(err, execErr)
	s.Equal(1, s.pool.execCalled)
	s.Equal(UpsertOrganizationSql, s.pool.execSql)
}