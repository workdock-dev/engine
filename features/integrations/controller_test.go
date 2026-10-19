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

package integrations

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/workdock-dev/engine/shared"
)

type mockRepository struct {
	upsertFn func(ctx context.Context, integration *shared.Integration) error

	upsertCalled      int
	upsertCtx         context.Context
	upsertIntegration *shared.Integration
}

func (m *mockRepository) UpsertIntegration(ctx context.Context, integration *shared.Integration) error {
	m.upsertCalled++
	m.upsertCtx = ctx
	m.upsertIntegration = integration

	if m.upsertFn != nil {
		return m.upsertFn(ctx, integration)
	}
	return nil
}

type mismatchedEvent struct{}

func (e mismatchedEvent) EventType() string {
	return shared.EventType_IntegrationCreated
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

	event := shared.IntegrationCreatedEvent{
		Integration: shared.Integration{
			Identifier: "integration-123",
			Provider:   shared.PlatformProvider_Linear,
			Name:       "Workdock",
		},
	}
	err := s.eventBus.Publish(context.Background(), event)

	s.Require().NoError(err)
	s.Require().Equal(1, s.repo.upsertCalled)
	s.Equal(context.Background(), s.repo.upsertCtx)
	s.Require().NotNil(s.repo.upsertIntegration)
	s.Equal("integration-123", s.repo.upsertIntegration.Identifier)
	s.Equal(shared.PlatformProvider_Linear, s.repo.upsertIntegration.Provider)
	s.Equal("Workdock", s.repo.upsertIntegration.Name)
}

func (s *ControllerSuite) TestNew_WrongEventType_ReturnsErrorWithoutRepoCall() {
	New(s.eventBus, s.repo)

	err := s.eventBus.Publish(context.Background(), mismatchedEvent{})

	s.NoError(err)
	s.Equal(0, s.repo.upsertCalled)
}

func (s *ControllerSuite) TestNew_RepositoryError_Propagates() {
	upsertErr := errors.New("upsert failed")
	s.repo.upsertFn = func(ctx context.Context, integration *shared.Integration) error {
		return upsertErr
	}
	New(s.eventBus, s.repo)

	event := shared.IntegrationCreatedEvent{
		Integration: shared.Integration{
			Identifier: "integration-123",
			Provider:   shared.PlatformProvider_Linear,
			Name:       "Workdock",
		},
	}
	err := s.eventBus.Publish(context.Background(), event)

	s.NoError(err)
	s.Equal(1, s.repo.upsertCalled)
}
