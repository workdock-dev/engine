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
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/workdock-dev/engine/shared"
)

type mockRepository struct {
	upsertFn func(ctx context.Context, org *shared.Organization) error

	upsertCalled int
	upsertCtx    context.Context
	upsertOrg    *shared.Organization
}

func (m *mockRepository) UpsertOrganization(ctx context.Context, org *shared.Organization) error {
	m.upsertCalled++
	m.upsertCtx = ctx
	m.upsertOrg = org
	if m.upsertFn != nil {
		return m.upsertFn(ctx, org)
	}
	return nil
}

type mismatchedEvent struct{}

func (e mismatchedEvent) EventType() string {
	return shared.EventType_OrganizationCreate
}

type ControllerSuite struct {
	suite.Suite
	eventBus *shared.EventBus
	repo     *mockRepository
}

func TestControllerSuite(t *testing.T) {
	suite.Run(t, new(ControllerSuite))
}

func (s *ControllerSuite) SetupTest() {
	s.eventBus = shared.NewEventBus()
	s.repo = &mockRepository{}
}

func (s *ControllerSuite) TestNew_SubscribesHandler() {
	New(s.eventBus, s.repo)

	event := shared.OrganizationCreateEvent{
		Organization: shared.Organization{
			Identifier: "org-123",
			Provider:   shared.PlatformProvider_Linear,
			Name:       "Workdock",
		},
	}
	err := s.eventBus.Publish(context.Background(), event)

	s.Require().NoError(err)
	s.Require().Equal(1, s.repo.upsertCalled)
	s.Equal(context.Background(), s.repo.upsertCtx)
	s.Require().NotNil(s.repo.upsertOrg)
	s.Equal("org-123", s.repo.upsertOrg.Identifier)
	s.Equal(shared.PlatformProvider_Linear, s.repo.upsertOrg.Provider)
	s.Equal("Workdock", s.repo.upsertOrg.Name)
}

func (s *ControllerSuite) TestNew_WrongEventType_ReturnsErrorWithoutRepoCall() {
	New(s.eventBus, s.repo)

	err := s.eventBus.Publish(context.Background(), mismatchedEvent{})

	s.NoError(err)
	s.Equal(0, s.repo.upsertCalled)
}

func (s *ControllerSuite) TestNew_RepositoryError_Propagates() {
	upsertErr := errors.New("upsert failed")
	s.repo.upsertFn = func(ctx context.Context, org *shared.Organization) error {
		return upsertErr
	}
	New(s.eventBus, s.repo)

	event := shared.OrganizationCreateEvent{
		Organization: shared.Organization{
			Identifier: "org-123",
			Provider:   shared.PlatformProvider_Linear,
			Name:       "Workdock",
		},
	}
	err := s.eventBus.Publish(context.Background(), event)

	s.NoError(err)
	s.Equal(1, s.repo.upsertCalled)
}