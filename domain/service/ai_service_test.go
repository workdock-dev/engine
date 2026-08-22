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

type AIServiceSuite struct {
	suite.Suite
	eventBus     *mocks.EventBus
	workPlat     *mocks.WorkPlatform
	sessions     *mocks.SessionRepository
	orgs         *mocks.OrganizationRepository
	registry     ports.WorkPlatformRegistry
	provider     types.PlatformProvider
	session      *types.Session
	sessionEvent *types.SessionEvent
	org          *types.Organization
}

func TestAIServiceSuite(t *testing.T) {
	suite.Run(t, new(AIServiceSuite))
}

func (s *AIServiceSuite) SetupTest() {
	s.eventBus = mocks.NewEventBus()
	s.workPlat = new(mocks.WorkPlatform)
	s.sessions = new(mocks.SessionRepository)
	s.orgs = new(mocks.OrganizationRepository)
	s.provider = types.PlatformProvider_Linear
	s.registry = ports.WorkPlatformRegistry{
		s.provider: s.workPlat,
	}
	s.org = &types.Organization{
		Identifier: "org-1",
		Provider:   s.provider,
		Name:       "Test Org",
	}
	s.session = &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "sess-1",
		Provider:               s.provider,
		IssueId:                "issue-1",
		Creator:                "user-1",
	}
	s.sessionEvent = &types.SessionEvent{
		SessionIdentifier: "sess-1",
		Identifier:        "evt-1",
		Payload:           []byte(`{"test":"data"}`),
	}
}

func (s *AIServiceSuite) expectSubscriptions() {
	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(s.provider), mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.On("Subscribe", types.EventType_GitHubConnected, mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.On("Subscribe", types.EventType_PullRequestCommented, mock.AnythingOfType("ports.EventHandler"))
}

func (s *AIServiceSuite) newService() *AIService {
	return NewAIService(AIServiceConfig{
		WorkPlatformRegistry: s.registry,
		ForEvent:             s.eventBus,
		Organizations:        s.orgs,
		Sessions:             s.sessions,
	})
}

func (s *AIServiceSuite) newServiceWithRegistry(registry ports.WorkPlatformRegistry) *AIService {
	return NewAIService(AIServiceConfig{
		WorkPlatformRegistry: registry,
		ForEvent:             s.eventBus,
		Organizations:        s.orgs,
		Sessions:             s.sessions,
	})
}

func (s *AIServiceSuite) expectCoreSubscriptions() {
	s.eventBus.On("Subscribe", types.EventType_GitHubConnected, mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.On("Subscribe", types.EventType_PullRequestCommented, mock.AnythingOfType("ports.EventHandler"))
}

// ---------------------------------------------------------------------------
// Constructor subscription tests
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestNewAIService_SubscribesWebhookEvents() {
	s.expectSubscriptions()
	s.newService()

	s.eventBus.AssertCalled(s.T(), "Subscribe", types.PlatformWebhookEvent(s.provider), mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.AssertCalled(s.T(), "Subscribe", types.EventType_GitHubConnected, mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.AssertCalled(s.T(), "Subscribe", types.EventType_PullRequestCommented, mock.AnythingOfType("ports.EventHandler"))
}

// ---------------------------------------------------------------------------
// Webhook handler tests (via captured handler)
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestWebhookHandler_NonCancelDelegatesToSession() {
	s.expectSubscriptions()
	s.newService()

	payload := []byte(`{"data":"value"}`)
	s.workPlat.On("IsCancelSignal", mock.Anything, payload).Return(false, nil)
	s.workPlat.On("Ingest", payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, s.session).Return(nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(nil)

	event := types.WebhookEvent{Provider: s.provider, Payload: payload, Type: types.WebhookEventType_AIRequest}
	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(s.provider), event)

	s.NoError(err)
	s.workPlat.AssertCalled(s.T(), "IsCancelSignal", mock.Anything, payload)
	s.workPlat.AssertCalled(s.T(), "Ingest", payload, (*string)(nil), (*types.SessionEvent)(nil))
}

func (s *AIServiceSuite) TestWebhookHandler_CancelDelegatesToCancel() {
	s.expectSubscriptions()
	s.newService()

	cancelPayload := []byte(`{"cancel":true}`)
	s.workPlat.On("IsCancelSignal", mock.Anything, cancelPayload).Return(true, nil)
	s.workPlat.On("Ingest", cancelPayload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.workPlat.On("Cancel", mock.Anything, s.session).Return(nil)
	s.sessions.On("CancelSession", mock.Anything, "sess-1", "cancelled by user").Return(1, nil)

	event := types.WebhookEvent{Provider: s.provider, Payload: cancelPayload, Type: types.WebhookEventType_AIRequest}
	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(s.provider), event)

	s.NoError(err)
	s.workPlat.AssertCalled(s.T(), "Cancel", mock.Anything, s.session)
	s.sessions.AssertCalled(s.T(), "CancelSession", mock.Anything, "sess-1", "cancelled by user")
}

func (s *AIServiceSuite) TestWebhookHandler_WrongEventType() {
	s.expectSubscriptions()
	s.newService()

	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(s.provider), types.GitHubConnectedEvent{})

	s.Error(err)
	s.Contains(err.Error(), "expected a webhook event")
}

func (s *AIServiceSuite) TestWebhookHandler_IsCancelSignalError() {
	s.expectSubscriptions()
	s.newService()

	payload := []byte(`{"data":"value"}`)
	cancelErr := errors.New("cancel check failed")
	s.workPlat.On("IsCancelSignal", mock.Anything, payload).Return(false, cancelErr)

	event := types.WebhookEvent{Provider: s.provider, Payload: payload, Type: types.WebhookEventType_AIRequest}
	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(s.provider), event)

	s.ErrorIs(err, cancelErr)
}

func (s *AIServiceSuite) TestWebhookHandler_PlatformNotFound() {
	otherProvider := types.PlatformProvider("other")
	s.eventBus.On("Subscribe", types.PlatformWebhookEvent(otherProvider), mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.On("Subscribe", types.EventType_GitHubConnected, mock.AnythingOfType("ports.EventHandler"))
	s.eventBus.On("Subscribe", types.EventType_PullRequestCommented, mock.AnythingOfType("ports.EventHandler"))

	s.newServiceWithRegistry(ports.WorkPlatformRegistry{
		otherProvider: s.workPlat,
	})

	event := types.WebhookEvent{Provider: s.provider, Payload: []byte(`{}`), Type: types.WebhookEventType_AIRequest}
	err := s.eventBus.Invoke(context.Background(), types.PlatformWebhookEvent(otherProvider), event)

	s.Error(err)
	s.Contains(err.Error(), "failed to load work platform from registry")
}

// ---------------------------------------------------------------------------
// GitHub connected handler tests
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestGitHubConnectedHandler_Success() {
	s.expectSubscriptions()
	s.newService()

	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(s.session, nil)
	s.workPlat.On("Ingest", s.sessionEvent.Payload, mock.AnythingOfType("*string"), s.sessionEvent).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(nil)

	event := types.GitHubConnectedEvent{
		Connection: types.GitHubConnection{SessionEventIdentifier: strPtr("evt-1"), RepoFullName: "org/repo", Connected: true},
	}
	err := s.eventBus.Invoke(context.Background(), types.EventType_GitHubConnected, event)

	s.NoError(err)
	s.sessions.AssertCalled(s.T(), "GetAgentSessionEvent", mock.Anything, "evt-1")
	s.sessions.AssertCalled(s.T(), "GetAgentSession", mock.Anything, "sess-1")
}

func (s *AIServiceSuite) TestGitHubConnectedHandler_WrongEventType() {
	s.expectSubscriptions()
	s.newService()

	err := s.eventBus.Invoke(context.Background(), types.EventType_GitHubConnected, types.WebhookEvent{})

	s.Error(err)
	s.Contains(err.Error(), "expected a github connection event")
}

func (s *AIServiceSuite) TestGitHubConnectedHandler_SessionEventFetchError() {
	s.expectSubscriptions()
	s.newService()

	fetchErr := errors.New("db error")
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(nil, fetchErr)

	event := types.GitHubConnectedEvent{
		Connection: types.GitHubConnection{SessionEventIdentifier: strPtr("evt-1")},
	}
	err := s.eventBus.Invoke(context.Background(), types.EventType_GitHubConnected, event)

	s.ErrorIs(err, fetchErr)
}

func (s *AIServiceSuite) TestGitHubConnectedHandler_SessionEventNotFound() {
	s.expectSubscriptions()
	s.newService()

	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(nil, nil)

	event := types.GitHubConnectedEvent{
		Connection: types.GitHubConnection{SessionEventIdentifier: strPtr("evt-1")},
	}
	err := s.eventBus.Invoke(context.Background(), types.EventType_GitHubConnected, event)

	s.NoError(err)
}

func (s *AIServiceSuite) TestGitHubConnectedHandler_SessionFetchError() {
	s.expectSubscriptions()
	s.newService()

	fetchErr := errors.New("db error")
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, fetchErr)

	event := types.GitHubConnectedEvent{
		Connection: types.GitHubConnection{SessionEventIdentifier: strPtr("evt-1")},
	}
	err := s.eventBus.Invoke(context.Background(), types.EventType_GitHubConnected, event)

	s.ErrorIs(err, fetchErr)
}

func (s *AIServiceSuite) TestGitHubConnectedHandler_SessionNotFound() {
	s.expectSubscriptions()
	s.newService()

	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)

	event := types.GitHubConnectedEvent{
		Connection: types.GitHubConnection{SessionEventIdentifier: strPtr("evt-1")},
	}
	err := s.eventBus.Invoke(context.Background(), types.EventType_GitHubConnected, event)

	s.Error(err)
	s.Contains(err.Error(), "session not found: sess-1")
}

// ---------------------------------------------------------------------------
// Pull request commented handler tests
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestPRCommentedHandler_Success() {
	s.expectSubscriptions()
	s.newService()

	s.sessions.On("GetAgentSessionEventByGitRef", mock.Anything, "abc123", "org/repo").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(s.session, nil)
	s.workPlat.On("Ingest", s.sessionEvent.Payload, mock.AnythingOfType("*string"), s.sessionEvent).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(nil)

	event := types.PullRequestCommentedEvent{
		Provider:       s.provider,
		GitRef:         "abc123",
		InstallationId: "inst-1",
		RepoFullName:   "org/repo",
	}
	err := s.eventBus.Invoke(context.Background(), types.EventType_PullRequestCommented, event)

	s.NoError(err)
	s.sessions.AssertCalled(s.T(), "GetAgentSessionEventByGitRef", mock.Anything, "abc123", "org/repo")
}

func (s *AIServiceSuite) TestPRCommentedHandler_WrongEventType() {
	s.expectSubscriptions()
	s.newService()

	err := s.eventBus.Invoke(context.Background(), types.EventType_PullRequestCommented, types.WebhookEvent{})

	s.Error(err)
	s.Contains(err.Error(), "expected a pull request commented event")
}

func (s *AIServiceSuite) TestPRCommentedHandler_SessionEventFetchError() {
	s.expectSubscriptions()
	s.newService()

	fetchErr := errors.New("db error")
	s.sessions.On("GetAgentSessionEventByGitRef", mock.Anything, "abc123", "org/repo").Return(nil, fetchErr)

	event := types.PullRequestCommentedEvent{Provider: s.provider, GitRef: "abc123", RepoFullName: "org/repo"}
	err := s.eventBus.Invoke(context.Background(), types.EventType_PullRequestCommented, event)

	s.ErrorIs(err, fetchErr)
}

func (s *AIServiceSuite) TestPRCommentedHandler_SessionEventNotFound() {
	s.expectSubscriptions()
	s.newService()

	s.sessions.On("GetAgentSessionEventByGitRef", mock.Anything, "abc123", "org/repo").Return(nil, nil)

	event := types.PullRequestCommentedEvent{Provider: s.provider, GitRef: "abc123", RepoFullName: "org/repo"}
	err := s.eventBus.Invoke(context.Background(), types.EventType_PullRequestCommented, event)

	s.Error(err)
	s.Contains(err.Error(), "session event not found: abc123@org/repo")
}

func (s *AIServiceSuite) TestPRCommentedHandler_SessionFetchError() {
	s.expectSubscriptions()
	s.newService()

	fetchErr := errors.New("db error")
	s.sessions.On("GetAgentSessionEventByGitRef", mock.Anything, "abc123", "org/repo").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, fetchErr)

	event := types.PullRequestCommentedEvent{Provider: s.provider, GitRef: "abc123", RepoFullName: "org/repo"}
	err := s.eventBus.Invoke(context.Background(), types.EventType_PullRequestCommented, event)

	s.ErrorIs(err, fetchErr)
}

func (s *AIServiceSuite) TestPRCommentedHandler_SessionNotFound() {
	s.expectSubscriptions()
	s.newService()

	s.sessions.On("GetAgentSessionEventByGitRef", mock.Anything, "abc123", "org/repo").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)

	event := types.PullRequestCommentedEvent{Provider: s.provider, GitRef: "abc123", RepoFullName: "org/repo"}
	err := s.eventBus.Invoke(context.Background(), types.EventType_PullRequestCommented, event)

	s.Error(err)
	s.Contains(err.Error(), "session not found: sess-1")
}

// ---------------------------------------------------------------------------
// Session() method tests
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestSession_NewSession() {
	s.expectSubscriptions()
	svc := s.newService()

	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, s.session).Return(nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(nil)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.NoError(err)
	s.sessions.AssertCalled(s.T(), "UpsertAgentSession", mock.Anything, s.session)
	s.sessions.AssertCalled(s.T(), "CreateSessionEvent", mock.Anything, s.sessionEvent)
}

func (s *AIServiceSuite) TestSession_ExistingSessionNewEvent() {
	s.expectSubscriptions()
	svc := s.newService()

	existingSession := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "sess-1",
		Provider:               s.provider,
	}

	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(existingSession, nil)
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(nil, nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(nil)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.NoError(err)
	s.sessions.AssertNotCalled(s.T(), "UpsertAgentSession")
	s.sessions.AssertCalled(s.T(), "CreateSessionEvent", mock.Anything, s.sessionEvent)
}

func (s *AIServiceSuite) TestSession_DuplicateEvent() {
	s.expectSubscriptions()
	svc := s.newService()

	existingSession := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "sess-1",
		Provider:               s.provider,
	}
	existingEvent := &types.SessionEvent{
		SessionIdentifier: "sess-1",
		Identifier:        "evt-1",
	}

	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(existingSession, nil)
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(existingEvent, nil)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.NoError(err)
	s.sessions.AssertNotCalled(s.T(), "UpsertAgentSession")
	s.sessions.AssertNotCalled(s.T(), "CreateSessionEvent")
}

func (s *AIServiceSuite) TestSession_PlatformNotFound() {
	s.expectCoreSubscriptions()
	svc := s.newServiceWithRegistry(ports.WorkPlatformRegistry{})

	err := svc.Session(context.Background(), s.provider, nil, nil, nil)

	s.Error(err)
	s.Contains(err.Error(), "failed to load work platform from registry")
}

func (s *AIServiceSuite) TestSession_IngestError() {
	s.expectSubscriptions()
	svc := s.newService()

	ingestErr := errors.New("ingest failed")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(nil, nil, ingestErr)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.ErrorIs(err, ingestErr)
}

func (s *AIServiceSuite) TestSession_GetOrganizationError() {
	s.expectSubscriptions()
	svc := s.newService()

	orgErr := errors.New("org db error")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(nil, orgErr)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.ErrorIs(err, orgErr)
}

func (s *AIServiceSuite) TestSession_OrganizationNil() {
	s.expectSubscriptions()
	svc := s.newService()

	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(nil, nil)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.NoError(err)
}

func (s *AIServiceSuite) TestSession_GetSessionError() {
	s.expectSubscriptions()
	svc := s.newService()

	sessionErr := errors.New("session db error")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, sessionErr)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.ErrorIs(err, sessionErr)
}

func (s *AIServiceSuite) TestSession_UpsertError() {
	s.expectSubscriptions()
	svc := s.newService()

	upsertErr := errors.New("upsert failed")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, s.session).Return(upsertErr)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.ErrorIs(err, upsertErr)
}

func (s *AIServiceSuite) TestSession_DuplicateCheckError() {
	s.expectSubscriptions()
	svc := s.newService()

	dupCheckErr := errors.New("duplicate check failed")
	existingSession := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "sess-1",
		Provider:               s.provider,
	}

	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(existingSession, nil)
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(nil, dupCheckErr)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.ErrorIs(err, dupCheckErr)
}

func (s *AIServiceSuite) TestSession_CreateEventError() {
	s.expectSubscriptions()
	svc := s.newService()

	createErr := errors.New("create event failed")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, s.session).Return(nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(createErr)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, nil, nil)

	s.ErrorIs(err, createErr)
}

func (s *AIServiceSuite) TestSession_WithSeedAndFrom() {
	s.expectSubscriptions()
	svc := s.newService()

	seed := "custom-seed"
	from := &types.SessionEvent{
		SessionIdentifier: "sess-0",
		Identifier:        "evt-0",
	}

	s.workPlat.On("Ingest", s.sessionEvent.Payload, &seed, from).
		Return(s.session, s.sessionEvent, nil)
	s.orgs.On("GetOrganization", mock.Anything, "org-1").Return(s.org, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, s.session).Return(nil)
	s.sessions.On("CreateSessionEvent", mock.Anything, s.sessionEvent).Return(nil)

	err := svc.Session(context.Background(), s.provider, s.sessionEvent.Payload, &seed, from)

	s.NoError(err)
	s.workPlat.AssertCalled(s.T(), "Ingest", s.sessionEvent.Payload, &seed, from)
}

// ---------------------------------------------------------------------------
// Cancel() method tests
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestCancel_Success() {
	s.expectSubscriptions()
	svc := s.newService()

	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.workPlat.On("Cancel", mock.Anything, s.session).Return(nil)
	s.sessions.On("CancelSession", mock.Anything, "sess-1", "cancelled by user").Return(1, nil)

	err := svc.Cancel(context.Background(), s.provider, s.sessionEvent.Payload)

	s.NoError(err)
	s.workPlat.AssertCalled(s.T(), "Cancel", mock.Anything, s.session)
	s.sessions.AssertCalled(s.T(), "CancelSession", mock.Anything, "sess-1", "cancelled by user")
}

func (s *AIServiceSuite) TestCancel_PlatformNotFound() {
	s.expectCoreSubscriptions()
	svc := s.newServiceWithRegistry(ports.WorkPlatformRegistry{})

	err := svc.Cancel(context.Background(), s.provider, nil)

	s.Error(err)
	s.Contains(err.Error(), "failed to load work platform from registry")
}

func (s *AIServiceSuite) TestCancel_IngestError() {
	s.expectSubscriptions()
	svc := s.newService()

	ingestErr := errors.New("ingest failed")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(nil, nil, ingestErr)

	err := svc.Cancel(context.Background(), s.provider, s.sessionEvent.Payload)

	s.ErrorIs(err, ingestErr)
}

func (s *AIServiceSuite) TestCancel_ProviderCancelError() {
	s.expectSubscriptions()
	svc := s.newService()

	cancelErr := errors.New("provider cancel failed")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.workPlat.On("Cancel", mock.Anything, s.session).Return(cancelErr)

	err := svc.Cancel(context.Background(), s.provider, s.sessionEvent.Payload)

	s.ErrorIs(err, cancelErr)
	s.sessions.AssertNotCalled(s.T(), "CancelSession")
}

func (s *AIServiceSuite) TestCancel_RepoCancelError() {
	s.expectSubscriptions()
	svc := s.newService()

	repoCancelErr := errors.New("repo cancel failed")
	s.workPlat.On("Ingest", s.sessionEvent.Payload, (*string)(nil), (*types.SessionEvent)(nil)).
		Return(s.session, s.sessionEvent, nil)
	s.workPlat.On("Cancel", mock.Anything, s.session).Return(nil)
	s.sessions.On("CancelSession", mock.Anything, "sess-1", "cancelled by user").Return(0, repoCancelErr)

	err := svc.Cancel(context.Background(), s.provider, s.sessionEvent.Payload)

	s.ErrorIs(err, repoCancelErr)
}

// ---------------------------------------------------------------------------
// Process() method tests
// ---------------------------------------------------------------------------

func (s *AIServiceSuite) TestProcess_Success() {
	s.expectSubscriptions()
	svc := s.newService()

	job := &types.EventJob{
		SessionEventIdentifier: "evt-1",
		Status:                 types.EventJobStatus_Queued,
	}

	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(s.session, nil)
	s.workPlat.On("Process", mock.Anything, ports.ProcessConfig{
		Job:          job,
		SessionEvent: s.sessionEvent,
		Session:      s.session,
	}).Return(nil)

	err := svc.Process(context.Background(), job)

	s.NoError(err)
	s.workPlat.AssertExpectations(s.T())
}

func (s *AIServiceSuite) TestProcess_SessionEventError() {
	s.expectSubscriptions()
	svc := s.newService()

	job := &types.EventJob{SessionEventIdentifier: "evt-1"}
	sessionErr := errors.New("db error")
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(nil, sessionErr)

	err := svc.Process(context.Background(), job)

	s.ErrorIs(err, sessionErr)
}

func (s *AIServiceSuite) TestProcess_SessionEventNil() {
	s.expectSubscriptions()
	svc := s.newService()

	job := &types.EventJob{SessionEventIdentifier: "evt-1"}
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(nil, nil)

	err := svc.Process(context.Background(), job)

	s.NoError(err)
}

func (s *AIServiceSuite) TestProcess_SessionError() {
	s.expectSubscriptions()
	svc := s.newService()

	job := &types.EventJob{SessionEventIdentifier: "evt-1"}
	sessionErr := errors.New("db error")
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, sessionErr)

	err := svc.Process(context.Background(), job)

	s.ErrorIs(err, sessionErr)
}

func (s *AIServiceSuite) TestProcess_SessionNil() {
	s.expectSubscriptions()
	svc := s.newService()

	job := &types.EventJob{SessionEventIdentifier: "evt-1"}
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(nil, nil)

	err := svc.Process(context.Background(), job)

	s.NoError(err)
}

func (s *AIServiceSuite) TestProcess_PlatformNotFound() {
	s.expectCoreSubscriptions()
	svc := s.newServiceWithRegistry(ports.WorkPlatformRegistry{})

	job := &types.EventJob{SessionEventIdentifier: "evt-1"}
	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(s.session, nil)

	err := svc.Process(context.Background(), job)

	s.Error(err)
	s.Contains(err.Error(), "failed to load work platform from registry")
}

func (s *AIServiceSuite) TestProcess_ProcessError() {
	s.expectSubscriptions()
	svc := s.newService()

	job := &types.EventJob{SessionEventIdentifier: "evt-1"}
	processErr := errors.New("process failed")

	s.sessions.On("GetAgentSessionEvent", mock.Anything, "evt-1").Return(s.sessionEvent, nil)
	s.sessions.On("GetAgentSession", mock.Anything, "sess-1").Return(s.session, nil)
	s.workPlat.On("Process", mock.Anything, ports.ProcessConfig{
		Job:          job,
		SessionEvent: s.sessionEvent,
		Session:      s.session,
	}).Return(processErr)

	err := svc.Process(context.Background(), job)

	s.ErrorIs(err, processErr)
}

func strPtr(s string) *string {
	return &s
}
