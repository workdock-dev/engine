package linear

import (
	"context"

	"github.com/workdock-dev/engine/domain/types"
	"github.com/workdock-dev/engine/pipelines/runners"
)

type LinearClientInterface interface {
	Webhook(ctx context.Context, event runners.WEvent) (any, types.WebhookEventType, error)
}
