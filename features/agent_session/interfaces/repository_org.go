package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/shared"
)

type RepositoryOrg interface {
	GetOrganization(ctx context.Context, identifier string) (*shared.Organization, error)
}
