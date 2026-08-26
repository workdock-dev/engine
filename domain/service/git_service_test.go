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

package domain_service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/domain/mocks"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
)

type GitServiceSuite struct {
	suite.Suite
	eventBus *mocks.EventBus
	platform *mocks.GitHostingPlatform
}

func TestGitServiceSuite(t *testing.T) {
	suite.Run(t, new(GitServiceSuite))
}

func (s *GitServiceSuite) SetupTest() {
	s.eventBus = mocks.NewEventBus()
	s.platform = new(mocks.GitHostingPlatform)
}

func (s *GitServiceSuite) TestNewGitService_SubscribesForAllProviders() {
	provider := types.PlatformProvider_GitHub

	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(provider), mock.AnythingOfType("ports.EventHandler"))

	NewGitService(GitServiceConfig{
		GitHostingPlatformRegistry: ports.GitHostingPlatformRegistry{
			provider: s.platform,
		},
		ForEvent: s.eventBus,
	})

	s.eventBus.AssertExpectations(s.T())
	s.Contains(s.eventBus.Handlers, types.PlatformWebhookEvent(provider))
}

func (s *GitServiceSuite) TestWebhookHandler_Success() {
	provider := types.PlatformProvider_GitHub
	payload := "some-payload"
	event := types.WebhookEvent{Provider: provider, Payload: payload}

	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(provider), mock.AnythingOfType("ports.EventHandler"))
	s.platform.On("Ingest", mock.Anything, payload).Return(nil)

	NewGitService(GitServiceConfig{
		GitHostingPlatformRegistry: ports.GitHostingPlatformRegistry{
			provider: s.platform,
		},
		ForEvent: s.eventBus,
	})

	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(provider), event)

	s.NoError(err)
	s.platform.AssertExpectations(s.T())
}

func (s *GitServiceSuite) TestWebhookHandler_WrongEventType() {
	provider := types.PlatformProvider_GitHub
	event := types.GitHubConnectedEvent{}

	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(provider), mock.AnythingOfType("ports.EventHandler"))

	NewGitService(GitServiceConfig{
		GitHostingPlatformRegistry: ports.GitHostingPlatformRegistry{
			provider: s.platform,
		},
		ForEvent: s.eventBus,
	})

	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(provider), event)

	s.Error(err)
	s.Contains(err.Error(), "expected a github connection event")
}

func (s *GitServiceSuite) TestWebhookHandler_PlatformNotFound() {
	provider := types.PlatformProvider_GitHub
	unknownProvider := types.PlatformProvider("unknown")
	event := types.WebhookEvent{Provider: unknownProvider, Payload: "payload"}

	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(provider), mock.AnythingOfType("ports.EventHandler"))

	NewGitService(GitServiceConfig{
		GitHostingPlatformRegistry: ports.GitHostingPlatformRegistry{
			provider: s.platform,
		},
		ForEvent: s.eventBus,
	})

	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(provider), event)

	s.Error(err)
	s.Contains(err.Error(), "failed to load git hosting platform from registry")
}

func (s *GitServiceSuite) TestWebhookHandler_IngestError() {
	provider := types.PlatformProvider_GitHub
	payload := "bad-payload"
	event := types.WebhookEvent{Provider: provider, Payload: payload}
	ingestErr := errors.New("ingest failed")

	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(provider), mock.AnythingOfType("ports.EventHandler"))
	s.platform.On("Ingest", mock.Anything, payload).Return(ingestErr)

	NewGitService(GitServiceConfig{
		GitHostingPlatformRegistry: ports.GitHostingPlatformRegistry{
			provider: s.platform,
		},
		ForEvent: s.eventBus,
	})

	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(provider), event)

	s.ErrorIs(err, ingestErr)
	s.platform.AssertExpectations(s.T())
}
