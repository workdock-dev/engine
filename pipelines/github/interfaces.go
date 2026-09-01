package github

import "context"

type ClientInterface interface {
	// GenerateJWT() (string, error)
	IsRepositoryPublic(ctx context.Context, repo string) (bool, error)
	CreateInstallationAccessToken(installationId int) (*InstallationAccessToken, error)
	// Webhook(ctx context.Context, req types.WebhookRequest) (*WebhookEvent, error)
}
