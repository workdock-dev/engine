package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/plug-ings/linear/types"
)

type Client interface {
	RefreshToken(ctx context.Context, refreshToken string) (*types.Token, error)
	CreateAgentActivity(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error
}
