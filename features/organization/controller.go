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

package organization

import (
	"context"
	"fmt"

	"github.com/workdock-dev/engine/features/organization/interfaces"
	"github.com/workdock-dev/engine/shared"
)

type controller struct {
	eventBus   *shared.EventBus
	repository interfaces.Repository
}

func New(
	eventBus *shared.EventBus,
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
