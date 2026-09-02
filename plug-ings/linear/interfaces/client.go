package interfaces

import (
	"context"

	"github.com/workdock-dev/engine/plug-ings/linear/types"
)

type Client interface {
	ExchangeCode(ctx context.Context, code string) (*types.TokenExchanged, error)
	GetWorkspaceInfo(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error)
	RefreshToken(ctx context.Context, refreshToken string) (*types.Token, error)
	CreateAgentActivity(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error
}
