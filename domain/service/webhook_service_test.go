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

	"github.com/jazielguerrero/workdock/domain/mocks"
	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type WebhookServiceSuite struct {
	suite.Suite
	eventBus *mocks.EventBus
	platform *mocks.Webhooks
}

func TestWebhookServiceSuite(t *testing.T) {
	suite.Run(t, new(WebhookServiceSuite))
}

func (s *WebhookServiceSuite) newService(registry ports.WebhooksRegistry) *WebhookService {
	return NewWebhookService(WebhookServiceConfig{
		WebhooksRegistry: registry,
		ForEventBus:      s.eventBus,
	})
}

func (s *WebhookServiceSuite) SetupTest() {
	s.eventBus = mocks.NewEventBus()
	s.platform = new(mocks.Webhooks)
}

func (s *WebhookServiceSuite) TestOn_Success() {
	provider := types.PlatformProvider_GitHub
	req := types.WebhookRequest{Headers: map[string][]string{"X-Test": {"value"}}}
	payload := "parsed-payload"

	s.platform.On("Webhook", mock.Anything, req).Return(payload, nil)
	s.eventBus.On("Publish", mock.Anything, types.WebhookEvent{
		Provider: provider,
		Payload:  payload,
	}).Return(nil)

	svc := s.newService(ports.WebhooksRegistry{
		provider: s.platform,
	})

	err := svc.On(context.Background(), provider, req)

	s.NoError(err)
	s.platform.AssertExpectations(s.T())
	s.eventBus.AssertExpectations(s.T())
}

func (s *WebhookServiceSuite) TestOn_PlatformNotFound() {
	provider := types.PlatformProvider_GitHub
	req := types.WebhookRequest{}

	svc := s.newService(ports.WebhooksRegistry{})

	err := svc.On(context.Background(), provider, req)

	s.ErrorIs(err, types.ErrInternalServerError)
}

func (s *WebhookServiceSuite) TestOn_WebhookError() {
	provider := types.PlatformProvider_GitHub
	req := types.WebhookRequest{}
	webhookErr := errors.New("invalid signature")

	s.platform.On("Webhook", mock.Anything, req).Return(nil, webhookErr)

	svc := s.newService(ports.WebhooksRegistry{
		provider: s.platform,
	})

	err := svc.On(context.Background(), provider, req)

	s.ErrorIs(err, webhookErr)
	s.platform.AssertExpectations(s.T())
	s.eventBus.AssertNotCalled(s.T(), "Publish")
}

// --- OnIssueStatusChange tests ---

func (s *WebhookServiceSuite) TestOnIssueStatusChange_PlatformNotInRegistry() {
	svc := s.newService(ports.WebhooksRegistry{})

	err := svc.OnIssueStatusChange(context.Background(), types.PlatformProvider_Linear, types.WebhookRequest{})

	s.NoError(err)
}

func (s *WebhookServiceSuite) TestOnIssueStatusChange_PlatformDoesNotSupportInterface() {
	svc := s.newService(ports.WebhooksRegistry{
		types.PlatformProvider_Linear: s.platform,
	})

	err := svc.OnIssueStatusChange(context.Background(), types.PlatformProvider_Linear, types.WebhookRequest{})

	s.NoError(err)
}

func (s *WebhookServiceSuite) TestOnIssueStatusChange_ParseReturnsNil() {
	platformWithStatus := new(mocks.WebhooksWithIssueStatusChanges)
	platformWithStatus.On("ParseIssueStatusChange", mock.Anything, mock.Anything).Return(nil, nil)
	s.eventBus.On("Publish", mock.Anything, mock.Anything).Return(nil)

	svc := s.newService(ports.WebhooksRegistry{
		types.PlatformProvider_Linear: platformWithStatus,
	})

	err := svc.OnIssueStatusChange(context.Background(), types.PlatformProvider_Linear, types.WebhookRequest{})

	s.NoError(err)
	platformWithStatus.AssertExpectations(s.T())
	s.eventBus.AssertNotCalled(s.T(), "Publish")
}

func (s *WebhookServiceSuite) TestOnIssueStatusChange_ParseError() {
	platformWithStatus := new(mocks.WebhooksWithIssueStatusChanges)
	parseErr := errors.New("parse error")
	platformWithStatus.On("ParseIssueStatusChange", mock.Anything, mock.Anything).Return(nil, parseErr)

	svc := s.newService(ports.WebhooksRegistry{
		types.PlatformProvider_Linear: platformWithStatus,
	})

	err := svc.OnIssueStatusChange(context.Background(), types.PlatformProvider_Linear, types.WebhookRequest{})

	s.ErrorIs(err, parseErr)
	platformWithStatus.AssertExpectations(s.T())
}

func (s *WebhookServiceSuite) TestOnIssueStatusChange_Success() {
	platformWithStatus := new(mocks.WebhooksWithIssueStatusChanges)
	payload := &types.IssueStatusChangePayload{
		OrganizationID: "org-1",
		IssueId:        "issue-1",
		PreviousStatus: "In Progress",
		NewStatus:      "Done",
	}
	platformWithStatus.On("ParseIssueStatusChange", mock.Anything, mock.Anything).Return(payload, nil)
	s.eventBus.On("Publish", mock.Anything, types.IssueStatusChangedEvent{
		Provider:               types.PlatformProvider_Linear,
		OrganizationIdentifier: "org-1",
		IssueId:                "issue-1",
		PreviousStatus:         "In Progress",
		NewStatus:              "Done",
	}).Return(nil)

	svc := s.newService(ports.WebhooksRegistry{
		types.PlatformProvider_Linear: platformWithStatus,
	})

	err := svc.OnIssueStatusChange(context.Background(), types.PlatformProvider_Linear, types.WebhookRequest{})

	s.NoError(err)
	platformWithStatus.AssertExpectations(s.T())
	s.eventBus.AssertExpectations(s.T())
}
