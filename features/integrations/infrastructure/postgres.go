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
	"log/slog"

	"github.com/workdock-dev/engine/features/integrations/interfaces"
	"github.com/workdock-dev/engine/shared"
)

var (
	//go:embed upsert_integration.sql
	UpsertIntegrationSql string
)

type postgres struct {
	client shared.PostgresPool
}

func NewPostgres(client shared.PostgresPool) interfaces.Repository {
	return &postgres{
		client: client,
	}
}

func (p *postgres) UpsertIntegration(ctx context.Context, integration *shared.Integration) error {
	_, err := p.client.Exec(ctx, UpsertIntegrationSql, integration.Identifier, integration.Provider, integration.Name)

	if err != nil {
		slog.Error("[integrations][postgres] failed to upsert integration", "err", err, "integration_identifier", integration.Identifier, "provider", integration.Provider)
		return err
	}

	return nil
}
