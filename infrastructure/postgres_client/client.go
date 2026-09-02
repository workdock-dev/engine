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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workdock-dev/engine/shared"
)

var (

	//go:embed get_github_connection.sql
	GetGitHubConnectionSql string

	//go:embed upsert_github_connection.sql
	UpsertGitHubConnectionSql string

	//go:embed reset_github_connection.sql
	ResetGitHubConnectionSql string
)

type PostgresServiceConfig struct {
	DatabaseUrl string `yaml:"database_url"`
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

func (s *PostgresService) GetGitHubConnection(ctx context.Context, repoFullName string) (*shared.GitHubConnection, error) {
	var row shared.GitHubConnection

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

func (s *PostgresService) CreateSessionEvent(ctx context.Context, event *shared.SessionEvent) error {
}

func (s *PostgresService) UpsertAgentSession(ctx context.Context, session *shared.Session) error {
}

func (s *PostgresService) UpdateSessionEventResult(ctx context.Context, event *shared.SessionEvent) error {
}

func (s *PostgresService) UpsertGitHubConnection(ctx context.Context, githubConnection *shared.GitHubConnection) error {
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
}

func (s *PostgresService) Close() {
	s.client.Close()
}
