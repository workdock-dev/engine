package organization

import (
	"context"
	"fmt"

	"github.com/workdock-dev/engine/features/organization/interfaces"
	"github.com/workdock-dev/engine/shared"
)

type controller struct {
	eventBus   shared.ForEventBus
	repository interfaces.Repository
}

func New(
	eventBus shared.ForEventBus,
	repository interfaces.Repository,
) {
	c := &controller{
		eventBus:   eventBus,
		repository: repository,
	}
	c.init()
}

func (c *controller) init() {
	c.eventBus.Subscribe(shared.EventType_OrganizationCreate, func(ctx context.Context, event shared.DomainEvent) error {
		payload, ok := event.(shared.OrganizationCreateEvent)

		if !ok {
			return fmt.Errorf("failed to process domain event expected %s got %s", shared.EventType_OrganizationCreate, event.EventType())
		}

		if err := c.repository.UpsertOrganization(ctx, &shared.Organization{
			Identifier: payload.Organization.Identifier,
			Provider:   payload.Organization.Provider,
			Name:       payload.Organization.Name,
		}); err != nil {
			return err
		}

		return nil
	})
}
