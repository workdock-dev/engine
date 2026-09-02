package infrastructure

import (
	"context"
	_ "embed"
	"log/slog"

	"github.com/workdock-dev/engine/features/organization/interfaces"
	"github.com/workdock-dev/engine/shared"
)

var (
	//go:embed upsert_organization.sql
	UpsertOrganizationSql string
)

type postgres struct {
	client shared.PostgresPool
}

func NewPostgres(client shared.PostgresPool) interfaces.Repository {
	return &postgres{
		client: client,
	}
}

func (p *postgres) UpsertOrganization(ctx context.Context, org *shared.Organization) error {
	_, err := p.client.Exec(ctx, UpsertOrganizationSql, org.Identifier, org.Provider, org.Name)

	if err != nil {
		slog.Error("failed to upsert organization", "err", err, "org_identifier", org.Identifier, "provider", org.Provider)
		return err
	}

	return nil
}
