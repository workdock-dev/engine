package linear

import "context"

type LinearClientInterface interface {
	RefreshToken(ctx context.Context, refreshToken string) (*Token, error)
	CreateAgentActivity(ctx context.Context, accessToken string, input CreateAgentActivityInput) error
}
