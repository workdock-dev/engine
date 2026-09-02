package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/plug-ings/github/types"
)

type Client interface {
	// GenerateJWT() (string, error)
	IsRepositoryPublic(ctx context.Context, repo string) (bool, error)
	CreateInstallationAccessToken(installationId int) (*types.InstallationAccessToken, error)
	// Webhook(ctx context.Context, req types.WebhookRequest) (*WebhookEvent, error)
}
