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

package agent_session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"
)

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

var (
	testOrg = "org-1"

	testSessionEvent = &types.SessionEvent{
		SessionIdentifier: "sess-1",
		Identifier:        "evt-1",
		Payload:           []byte(`{"a":1}`),
		Reason:            types.AgentSessionEventReason_Prompt,
	}

	testPromptContext = &interfaces.PromptContext{
		Prompt: "do the work",
		Issue:  types.Issue{Title: "Title", Identifier: "issue-1", Description: "Description"},
	}
)

// newTestSession returns a fresh session: tests mutate sessions (e.g. the
// repo label branch), so sharing one instance across tests would leak state.
func newTestSession() *types.Session {
	return &types.Session{
		OrganizationIdentifier: testOrg,
		Identifier:             "sess-1",
		Provider:               shared.PlatformProvider_Linear,
		IssueId:                "issue-1",
		Creator:                "user-1",
	}
}

func repoName(repo string) *string { return &repo }

// mismatchedEvent reports an event type it is not an instance of, so handlers
// registered for that type receive a payload they cannot type-assert.
type mismatchedEvent struct {
	eventType string
}

func (m mismatchedEvent) EventType() string { return m.eventType }

// failingMeterProvider fails every instrument registration, making
// NewTaskScheduler's metrics initialization fail. Every creation method must
// fail: otel's global provider replays previously registered instruments
// through them and handles the returned errors gracefully.
type failingMeterProvider struct {
	metric.MeterProvider
}

func (p *failingMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return &failingControllerMeter{}
}

var errMeterUnavailable = errors.New("meter unavailable")

type failingControllerMeter struct {
	metric.Meter
}

func (m *failingControllerMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return nil, errMeterUnavailable
}

func (m *failingControllerMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return nil, errMeterUnavailable
}

// ---------------------------------------------------------------------------
// ControllerSuite
// ---------------------------------------------------------------------------

type ControllerSuite struct {
	suite.Suite
	eventBus   *shared.EventBus
	secretMgr  *mockSecretManager
	orgRepo    *mockOrganizationRepository
	sessionRep *mockSessionRepository
	gitRepo    *mockGitRepository
	queue      *mockQueue
	agentHdl   *mockAgentHandler
	gitHdl     *mockGitHandler
	sandboxHdl *mockSandboxHandler
	harnessHdl *mockHarnessHandler
	mcpHdl     *mockMcpHandler

	c *controller
}

func TestControllerSuite(t *testing.T) {
	suite.Run(t, new(ControllerSuite))
}

func (s *ControllerSuite) SetupTest() {
	s.eventBus = shared.NewEventBus()
	s.secretMgr = &mockSecretManager{}
	s.orgRepo = &mockOrganizationRepository{}
	s.sessionRep = &mockSessionRepository{}
	s.gitRepo = &mockGitRepository{}
	s.queue = &mockQueue{}
	s.agentHdl = &mockAgentHandler{}
	s.gitHdl = &mockGitHandler{}
	s.sandboxHdl = &mockSandboxHandler{}
	s.harnessHdl = &mockHarnessHandler{}
	s.mcpHdl = &mockMcpHandler{}

	s.c = &controller{
		taskSchedulerConfig:       types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 1},
		eventBus:                  s.eventBus,
		secretManager:             s.secretMgr,
		agentHandlerRegistry:      AgentHandlerRegistry{"linear": s.agentHdl},
		gitHostingHandlerRegistry: GitHandlerRegistry{"github": s.gitHdl},
		sandboxHandlerRegistry:    SandboxHandlerRegistry{"daytona": s.sandboxHdl},
		harnessHandlerRegistry:    HarnessHandlerRegistry{"opencode": s.harnessHdl},
		mcpHandler:                s.mcpHdl,
		organization:              s.orgRepo,
		git:                       s.gitRepo,
		session:                   s.sessionRep,
		queue:                     s.queue,
		tracer:                    noop.NewTracerProvider().Tracer("test"),
	}
}

// initController runs c.init() and returns the underlying TaskScheduler.
func (s *ControllerSuite) initController() {
	s.Require().NoError(s.c.init())
	s.Require().NotNil(s.c.taskScheduler)
}

// publish invokes the controller's registered handler for the event type
// directly and returns its error. The handler is fetched from the bus's
// subscription list through the shared HandlerAt accessor.
func (s *ControllerSuite) publish(eventType string, event shared.DomainEvent) error {
	s.T().Helper()

	handler, ok := s.eventBus.HandlerAt(eventType, 0)
	s.Require().True(ok, "expected a controller subscription for "+eventType)
	return handler(context.Background(), event)
}

// ---------------------------------------------------------------------------
// New / init
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestNew_SubscribesAndRunsScheduler() {
	ctx, cancel := context.WithCancel(context.Background())
	schedDone := make(chan error, 1)

	go func() {
		schedDone <- New(
			ctx,
			types.TaskSchedulerConfig{Workers: 1},
			types.HarnessLivenessProbeConfig{},
			AgentHandlerRegistry{"linear": s.agentHdl},
			GitHandlerRegistry{"github": s.gitHdl},
			SandboxHandlerRegistry{"daytona": s.sandboxHdl},
			HarnessHandlerRegistry{"opencode": s.harnessHdl},
			s.mcpHdl,
			s.eventBus,
			s.secretMgr,
			s.orgRepo,
			s.sessionRep,
			s.gitRepo,
			s.queue,
		)
	}()

	cancel()

	select {
	case err := <-schedDone:
		s.NoError(err)
	default:
		// Run may take a moment to observe the cancelled context.
		<-schedDone
	}
}

func (s *ControllerSuite) TestNew_SchedulerInitError() {
	// A failing global meter provider makes NewTaskScheduler (and therefore
	// init) fail; restore to a fresh no-op provider afterwards.
	noopProvider := noopmetric.NewMeterProvider()
	otel.SetMeterProvider(&failingMeterProvider{})
	defer otel.SetMeterProvider(noopProvider)

	err := New(
		context.Background(),
		types.TaskSchedulerConfig{Workers: 1},
		types.HarnessLivenessProbeConfig{},
		AgentHandlerRegistry{"linear": s.agentHdl},
		GitHandlerRegistry{"github": s.gitHdl},
		SandboxHandlerRegistry{"daytona": s.sandboxHdl},
		HarnessHandlerRegistry{"opencode": s.harnessHdl},
		s.mcpHdl,
		s.eventBus,
		s.secretMgr,
		s.orgRepo,
		s.sessionRep,
		s.gitRepo,
		s.queue,
	)

	s.Error(err)
	s.ErrorContains(err, "meter unavailable")
}

func (s *ControllerSuite) TestInit_RegistersAllEventSubscriptions() {
	s.initController()

	for _, eventType := range []string{
		shared.EventType_AgentSessionPrompt,
		shared.EventType_AgentSessionResume,
		shared.EventType_AgentSessionStop,
		shared.EventType_IssueChange,
		shared.EventType_PullRequestCommented,
		shared.EventType_GitResetConnection,
		shared.EventType_GitCompleteConnection,
	} {
		_, ok := s.eventBus.HandlerAt(eventType, 0)
		s.True(ok, "expected subscription for "+eventType)
	}
}

// ---------------------------------------------------------------------------
// onAgentSessionPrompt
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnAgentSessionPrompt_MismatchedEventType() {
	s.initController()

	err := s.publish(shared.EventType_AgentSessionPrompt, mismatchedEvent{eventType: shared.EventType_AgentSessionPrompt})

	s.Error(err)
	s.ErrorContains(err, "expected event type agent_session.prompt")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_HandlerNotFound() {
	s.initController()
	delete(s.c.agentHandlerRegistry, "linear")

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "agent session handler not found in registry: linear")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_IngestError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return nil, nil, errors.New("ingest failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "ingest failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_OrganizationLookupError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.orgRepo.getOrganizationFn = func(ctx context.Context, identifier string) (*shared.Organization, error) {
		return nil, errors.New("org lookup failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "org lookup failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_UnknownOrganization() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.orgRepo.getOrganizationFn = func(ctx context.Context, identifier string) (*shared.Organization, error) {
		return nil, nil
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "organization org-1 not found. did you authenticated?")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_NewSession_Created() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return nil, nil
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Require().NoError(err)
	s.Require().Len(s.sessionRep.upsertedSessions, 1)
	s.Equal("sess-1", s.sessionRep.upsertedSessions[0].Identifier)

	s.Require().Len(s.sessionRep.createdEvents, 1)
	s.Equal(types.AgentSessionEventReason_Prompt, s.sessionRep.createdEvents[0].Reason)
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_UpsertError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return nil, nil // session does not exist yet
	}
	s.sessionRep.upsertAgentSessionFn = func(ctx context.Context, session *types.Session) error {
		return errors.New("upsert failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "upsert failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_SessionLookupError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return nil, errors.New("lookup failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "lookup failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_DuplicateEvent_Skips() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return newTestSession(), nil // session already exists
	}
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return testSessionEvent, nil // event already processed
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Require().NoError(err)
	s.Empty(s.sessionRep.createdEvents, "duplicate event must not create a session event")
	s.Empty(s.sessionRep.upsertedSessions)
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_SessionEventLookupError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return newTestSession(), nil
	}
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, errors.New("event lookup failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "event lookup failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_ExistingSession_UsesPersistedSession() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	persisted := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "sess-1",
		Provider:               shared.PlatformProvider_Linear,
		IssueId:                "issue-1",
		Creator:                "user-1",
		RepoFullName:           repoName("workdock/repo"),
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return persisted, nil
	}
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, nil // not a duplicate
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return []string{"other", "labels"}, nil
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Require().NoError(err)
	s.Require().Len(s.sessionRep.createdEvents, 1)
	// The persisted session (with the repo) is used for the event.
	s.Equal("sess-1", s.sessionRep.createdEvents[0].SessionIdentifier)
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_CredentialsError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getCredentialsFn = func(ctx context.Context, orgId string) (string, error) {
		return "", errors.New("credentials failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "credentials failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_LabelsError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return nil, errors.New("labels failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "labels failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_RepoLabel_UpdatesSession() {
	s.initController()
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return newTestSession(), nil // session already exists
	}
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, nil // not a duplicate
	}
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return []string{"bug", "repo=workdock/other"}, nil
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Require().NoError(err)
	s.Require().Len(s.sessionRep.upsertedSessions, 1, "repo change should upsert the session")
	s.Require().NotNil(s.sessionRep.upsertedSessions[0].RepoFullName)
	s.Equal("workdock/other", *s.sessionRep.upsertedSessions[0].RepoFullName)
	s.Require().Len(s.sessionRep.createdEvents, 1)
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_RepoLabelUnchanged_NoUpsert() {
	s.initController()
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		sessionWithRepo := *newTestSession()
		sessionWithRepo.RepoFullName = repoName("workdock/repo")
		return &sessionWithRepo, nil
	}
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, nil // not a duplicate
	}
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return []string{"repo=workdock/repo"}, nil
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Require().NoError(err)
	s.Empty(s.sessionRep.upsertedSessions, "unchanged repo should not upsert the session")
	s.Require().Len(s.sessionRep.createdEvents, 1)
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_RepoUpdateError() {
	s.initController()
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return newTestSession(), nil // existing session; the label branch triggers the upsert
	}
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, nil // not a duplicate
	}
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return []string{"repo=workdock/other"}, nil
	}
	s.sessionRep.upsertAgentSessionFn = func(ctx context.Context, session *types.Session) error {
		return errors.New("repo update failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "repo update failed")
}

func (s *ControllerSuite) TestOnAgentSessionPrompt_CreateSessionEventError() {
	s.initController()
	s.agentHdl.ingestFn = func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
		return newTestSession(), testSessionEvent, nil
	}
	s.agentHdl.getLabelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		return nil, nil
	}
	s.sessionRep.createSessionEventFn = func(ctx context.Context, event *types.SessionEvent) error {
		return errors.New("create event failed")
	}

	err := s.publish(shared.EventType_AgentSessionPrompt, shared.AgentSessionPromptEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "create event failed")
}

// ---------------------------------------------------------------------------
// onAgentSessionResume
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnAgentSessionResume_MismatchedEventType() {
	s.initController()
	err := s.publish(shared.EventType_AgentSessionResume, mismatchedEvent{eventType: shared.EventType_AgentSessionResume})

	s.Error(err)
	s.ErrorContains(err, "expected event type agent_session.resume")
}

func (s *ControllerSuite) TestOnAgentSessionResume_SessionEventLookupError() {
	s.initController()
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, errors.New("lookup failed")
	}

	err := s.publish(shared.EventType_AgentSessionResume, shared.AgentSessionResumeEvent{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "lookup failed")
}

func (s *ControllerSuite) TestOnAgentSessionResume_NotFound() {
	s.initController()
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, nil
	}

	err := s.publish(shared.EventType_AgentSessionResume, shared.AgentSessionResumeEvent{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "session event not found evt-1")
}

func (s *ControllerSuite) TestOnAgentSessionResume_Success() {
	s.initController()
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return testSessionEvent, nil
	}

	err := s.publish(shared.EventType_AgentSessionResume, shared.AgentSessionResumeEvent{SessionEventIdentifier: "evt-1"})

	s.Require().NoError(err)
	s.Require().Len(s.sessionRep.resumedEvents, 1)
	s.Equal("evt-1", s.sessionRep.resumedEvents[0].Identifier)
}

func (s *ControllerSuite) TestOnAgentSessionResume_ResumeError() {
	s.initController()
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return testSessionEvent, nil
	}
	s.sessionRep.resumeSessionEventFn = func(ctx context.Context, event *types.SessionEvent) error {
		return errors.New("resume failed")
	}

	err := s.publish(shared.EventType_AgentSessionResume, shared.AgentSessionResumeEvent{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "resume failed")
}

// ---------------------------------------------------------------------------
// onAgentSessionStop
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnAgentSessionStop_MismatchedEventType() {
	s.initController()
	err := s.publish(shared.EventType_AgentSessionStop, mismatchedEvent{eventType: shared.EventType_AgentSessionStop})

	s.Error(err)
	s.ErrorContains(err, "expected event type agent_session.stop")
}

func (s *ControllerSuite) TestOnAgentSessionStop_HandlerNotFound() {
	s.initController()
	delete(s.c.agentHandlerRegistry, "linear")

	err := s.publish(shared.EventType_AgentSessionStop, shared.AgentSessionStopEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "agent session handler not found in registry: linear")
}

func (s *ControllerSuite) TestOnAgentSessionStop_CredentialsError() {
	s.initController()
	s.agentHdl.getCredentialsFn = func(ctx context.Context, orgId string) (string, error) {
		return "", errors.New("credentials failed")
	}

	err := s.publish(shared.EventType_AgentSessionStop, shared.AgentSessionStopEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "credentials failed")
}

func (s *ControllerSuite) TestOnAgentSessionStop_Success() {
	s.initController()

	err := s.publish(shared.EventType_AgentSessionStop, shared.AgentSessionStopEvent{
		Provider:               "linear",
		OrganizationIdentifier: "org-1",
		SessionIdentifier:      "sess-1",
	})

	s.Require().NoError(err)
	s.Equal("sess-1", s.sessionRep.cancelSession)
	s.Equal("cancelled by user", s.sessionRep.cancelReason)
}

func (s *ControllerSuite) TestOnAgentSessionStop_CancelError() {
	s.initController()
	s.sessionRep.cancelSessionFn = func(ctx context.Context, queuedBy, reason string) (int, error) {
		return 0, errors.New("cancel failed")
	}

	err := s.publish(shared.EventType_AgentSessionStop, shared.AgentSessionStopEvent{Provider: "linear"})

	s.Error(err)
	s.ErrorContains(err, "cancel failed")
}

// ---------------------------------------------------------------------------
// onIssueChange
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnIssueChange_MismatchedEventType() {
	s.initController()
	err := s.publish(shared.EventType_IssueChange, mismatchedEvent{eventType: shared.EventType_IssueChange})

	s.Error(err)
	s.ErrorContains(err, "expected event type issue.changed")
}

func (s *ControllerSuite) TestOnIssueChange_Success() {
	s.initController()
	err := s.publish(shared.EventType_IssueChange, shared.IssueChangedEvent{})

	s.NoError(err)
}

// ---------------------------------------------------------------------------
// onPullRequestCommented
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnPullRequestCommented_MismatchedEventType() {
	s.initController()
	err := s.publish(shared.EventType_PullRequestCommented, mismatchedEvent{eventType: shared.EventType_PullRequestCommented})

	s.Error(err)
	s.ErrorContains(err, "expected event type pull_request.comment")
}

func (s *ControllerSuite) TestOnPullRequestCommented_EventLookupError() {
	s.initController()
	s.sessionRep.getAgentSessionEventByGitRefFn = func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
		return nil, errors.New("git ref lookup failed")
	}

	err := s.publish(shared.EventType_PullRequestCommented, shared.PullRequestCommentedEvent{GitRef: "workdock/main", RepoFullName: "workdock/repo"})

	s.Error(err)
	s.ErrorContains(err, "git ref lookup failed")
}

func (s *ControllerSuite) TestOnPullRequestCommented_EventNotFound() {
	s.initController()
	s.sessionRep.getAgentSessionEventByGitRefFn = func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
		return nil, nil
	}

	err := s.publish(shared.EventType_PullRequestCommented, shared.PullRequestCommentedEvent{GitRef: "workdock/main", RepoFullName: "workdock/repo"})

	s.Error(err)
	s.ErrorContains(err, "session event not found: workdock/main@workdock/repo")
}

func (s *ControllerSuite) TestOnPullRequestCommented_SessionLookupError() {
	s.initController()
	s.sessionRep.getAgentSessionEventByGitRefFn = func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
		return testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return nil, errors.New("session lookup failed")
	}

	err := s.publish(shared.EventType_PullRequestCommented, shared.PullRequestCommentedEvent{GitRef: "workdock/main", RepoFullName: "workdock/repo"})

	s.Error(err)
	s.ErrorContains(err, "session lookup failed")
}

func (s *ControllerSuite) TestOnPullRequestCommented_SessionNotFound() {
	s.initController()
	s.sessionRep.getAgentSessionEventByGitRefFn = func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
		return testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return nil, nil
	}

	err := s.publish(shared.EventType_PullRequestCommented, shared.PullRequestCommentedEvent{GitRef: "workdock/main", RepoFullName: "workdock/repo"})

	s.Error(err)
	s.ErrorContains(err, "session not found: sess-1")
}

func (s *ControllerSuite) TestOnPullRequestCommented_Success() {
	s.initController()
	s.sessionRep.getAgentSessionEventByGitRefFn = func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
		seedEvent := &types.SessionEvent{
			SessionIdentifier: "sess-1",
			Identifier:        "seed-evt-1",
			Payload:           []byte(`{"seeded":true}`),
			Reason:            types.AgentSessionEventReason_Prompt,
		}
		return seedEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return newTestSession(), nil
	}

	err := s.publish(shared.EventType_PullRequestCommented, shared.PullRequestCommentedEvent{GitRef: "workdock/main", RepoFullName: "workdock/repo"})

	s.Require().NoError(err)
	s.Require().Len(s.sessionRep.createdEvents, 1)

	created := s.sessionRep.createdEvents[0]
	s.Equal("sess-1", created.SessionIdentifier)
	s.Equal("seed-evt-1", *created.Seed)
	s.Require().NotNil(created.GitRef)
	s.Equal("workdock/main", *created.GitRef)
	s.Equal(types.AgentSessionEventReason_PRComment, created.Reason)
	s.Equal([]byte(`{"seeded":true}`), []byte(created.Payload))
	s.NotEmpty(created.Identifier, "new event should have a generated identifier")
}

func (s *ControllerSuite) TestOnPullRequestCommented_CreateEventError() {
	s.initController()
	s.sessionRep.getAgentSessionEventByGitRefFn = func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
		return testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return newTestSession(), nil
	}
	s.sessionRep.createSessionEventFn = func(ctx context.Context, event *types.SessionEvent) error {
		return errors.New("create failed")
	}

	err := s.publish(shared.EventType_PullRequestCommented, shared.PullRequestCommentedEvent{GitRef: "workdock/main", RepoFullName: "workdock/repo"})

	s.Error(err)
	s.ErrorContains(err, "create failed")
}

// ---------------------------------------------------------------------------
// onGitResetConnection
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnGitResetConnection_MismatchedEventType() {
	s.initController()
	err := s.publish(shared.EventType_GitResetConnection, mismatchedEvent{eventType: shared.EventType_GitResetConnection})

	s.Error(err)
	s.ErrorContains(err, "expected event type git.reset_connection")
}

func (s *ControllerSuite) TestOnGitResetConnection_WithDelete() {
	s.initController()

	err := s.publish(shared.EventType_GitResetConnection, shared.GitResetConnectionEvent{
		InstallationId: "install-1",
		Repos:          []string{"workdock/repo"},
		Delete:         true,
	})

	s.Require().NoError(err)
	s.Require().Equal(1, s.secretMgr.deleteCalls)
	s.Equal("/github/installations", s.secretMgr.deletedPath)
	s.Equal("install-1", s.secretMgr.deletedName)
	s.Equal([]string{"install-1"}, s.gitRepo.resetCalls)
}

func (s *ControllerSuite) TestOnGitResetConnection_WithoutDelete() {
	s.initController()

	err := s.publish(shared.EventType_GitResetConnection, shared.GitResetConnectionEvent{
		InstallationId: "install-1",
		Repos:          []string{"workdock/repo"},
		Delete:         false,
	})

	s.Require().NoError(err)
	s.Equal(0, s.secretMgr.deleteCalls, "secret should be kept when Delete is false")
	s.Equal([]string{"install-1"}, s.gitRepo.resetCalls)
}

func (s *ControllerSuite) TestOnGitResetConnection_SecretDeleteError() {
	s.initController()
	s.secretMgr.deleteFn = func(ctx context.Context, secretPath, secretName string) error {
		return errors.New("delete failed")
	}

	err := s.publish(shared.EventType_GitResetConnection, shared.GitResetConnectionEvent{
		InstallationId: "install-1",
		Delete:         true,
	})

	s.Error(err)
	s.ErrorContains(err, "delete failed")
	s.Empty(s.gitRepo.resetCalls, "connection reset should not run when the secret deletion fails")
}

func (s *ControllerSuite) TestOnGitResetConnection_ResetError() {
	s.initController()
	s.gitRepo.resetConnectionFn = func(ctx context.Context, installationId string, repos []string) error {
		return errors.New("reset failed")
	}

	err := s.publish(shared.EventType_GitResetConnection, shared.GitResetConnectionEvent{InstallationId: "install-1"})

	s.Error(err)
	s.ErrorContains(err, "reset failed")
}

// ---------------------------------------------------------------------------
// onGitCompleteConnection
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestOnGitCompleteConnection_MismatchedEventType() {
	s.initController()
	err := s.publish(shared.EventType_GitCompleteConnection, mismatchedEvent{eventType: shared.EventType_GitCompleteConnection})

	s.Error(err)
	s.ErrorContains(err, "expected event type git.complete_connection")
}

func (s *ControllerSuite) TestOnGitCompleteConnection_WithToken() {
	s.initController()

	err := s.publish(shared.EventType_GitCompleteConnection, shared.GitCompleteConnectionEvent{
		InstallationId: "install-1",
		Token:          []byte("secret-token"),
		Repos:          []string{"workdock/repo"},
	})

	s.Require().NoError(err)
	s.Require().Equal(1, s.secretMgr.setCalls)
	s.Equal("/github/installations", s.secretMgr.setPath)
	s.Equal("install-1", s.secretMgr.setName)
	s.Equal("secret-token", s.secretMgr.setValue)

	s.Require().Len(s.gitRepo.upsertedConnections, 1)
	s.Equal("workdock/repo", s.gitRepo.upsertedConnections[0].RepoFullName)
	s.True(s.gitRepo.upsertedConnections[0].Connected)
}

func (s *ControllerSuite) TestOnGitCompleteConnection_TokenSetError() {
	s.initController()
	s.secretMgr.setFn = func(ctx context.Context, secretPath, secretName, secretValue string) error {
		return errors.New("set failed")
	}

	err := s.publish(shared.EventType_GitCompleteConnection, shared.GitCompleteConnectionEvent{
		InstallationId: "install-1",
		Token:          []byte("secret-token"),
	})

	s.Error(err)
	s.ErrorContains(err, "set failed")
	s.Empty(s.gitRepo.upsertedConnections)
}

func (s *ControllerSuite) TestOnGitCompleteConnection_UpsertError() {
	s.initController()
	s.gitRepo.upsertConnectionFn = func(ctx context.Context, connection *types.GitConnection) error {
		return errors.New("upsert failed")
	}

	err := s.publish(shared.EventType_GitCompleteConnection, shared.GitCompleteConnectionEvent{
		InstallationId: "install-1",
		Repos:          []string{"workdock/repo"},
	})

	s.Error(err)
	s.ErrorContains(err, "upsert failed")
}

func (s *ControllerSuite) TestOnGitCompleteConnection_RepublishesResumeEvent() {
	s.initController()
	resumeEvents := []shared.AgentSessionResumeEvent{}
	s.eventBus.Subscribe(shared.EventType_AgentSessionResume, func(ctx context.Context, event shared.DomainEvent) error {
		resumeEvents = append(resumeEvents, event.(shared.AgentSessionResumeEvent))
		return nil
	})

	sessionEventId := "evt-paused-1"
	s.gitRepo.upsertConnectionFn = func(ctx context.Context, connection *types.GitConnection) error {
		// Simulate the repository persisting the paused session event identifier.
		connection.SessionEventIdentifier = &sessionEventId
		return nil
	}

	err := s.publish(shared.EventType_GitCompleteConnection, shared.GitCompleteConnectionEvent{
		InstallationId: "install-1",
		Repos:          []string{"workdock/repo"},
	})

	s.Require().NoError(err)
	s.Require().Len(resumeEvents, 1, "a connection with a paused event should republish resume")
	s.Equal("evt-paused-1", resumeEvents[0].SessionEventIdentifier)
}

func (s *ControllerSuite) TestOnGitCompleteConnection_MultipleRepos() {
	s.initController()

	err := s.publish(shared.EventType_GitCompleteConnection, shared.GitCompleteConnectionEvent{
		InstallationId: "install-1",
		Repos:          []string{"workdock/repo-1", "workdock/repo-2"},
	})

	s.Require().NoError(err)
	s.Len(s.gitRepo.upsertedConnections, 2)
}

// ---------------------------------------------------------------------------
// getHandlers
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestGetHandlers_Success() {
	agent, git, sandbox, harness, err := s.c.getHandlers(newTestSession())

	s.Require().NoError(err)
	s.Same(s.agentHdl, agent)
	s.Same(s.gitHdl, git)
	s.Same(s.sandboxHdl, sandbox)
	s.Same(s.harnessHdl, harness)
}

func (s *ControllerSuite) TestGetHandlers_MissingAgentHandler() {
	delete(s.c.agentHandlerRegistry, "linear")

	_, _, _, _, err := s.c.getHandlers(newTestSession())

	s.Error(err)
	s.ErrorContains(err, "provider linear not configured for agent session run")
}

func (s *ControllerSuite) TestGetHandlers_MissingGitHandler() {
	delete(s.c.gitHostingHandlerRegistry, "github")

	_, _, _, _, err := s.c.getHandlers(newTestSession())

	s.Error(err)
	s.ErrorContains(err, "provider github not configured for git hosting handler")
}

func (s *ControllerSuite) TestGetHandlers_MissingSandboxHandler() {
	delete(s.c.sandboxHandlerRegistry, "daytona")

	_, _, _, _, err := s.c.getHandlers(newTestSession())

	s.Error(err)
	s.ErrorContains(err, "provider daytona not configured for sandbox handler")
}

func (s *ControllerSuite) TestGetHandlers_MissingHarnessHandler() {
	delete(s.c.harnessHandlerRegistry, "opencode")

	_, _, _, _, err := s.c.getHandlers(newTestSession())

	s.Error(err)
	s.ErrorContains(err, "provider opencode not configured for harness handler")
}

// ---------------------------------------------------------------------------
// getPrompt / createPrompt
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestGetPromptContextError() {
	s.agentHdl.getPromptContextFn = func(sessionEvent *types.SessionEvent) (*interfaces.PromptContext, error) {
		return nil, errors.New("prompt context failed")
	}

	_, err := s.c.getPrompt(context.Background(), s.agentHdl, newTestSession(), testSessionEvent)

	s.Error(err)
	s.ErrorContains(err, "prompt context failed")
}

func (s *ControllerSuite) TestCreatePrompt_Base() {
	session := &types.Session{RepoFullName: repoName("workdock/repo")}

	p := s.c.createPrompt(session, nil, testPromptContext)

	s.Contains(p, "**Title:** Title")
	s.Contains(p, "**Identifier:** issue-1")
	s.Contains(p, "**Repository:** workdock/repo")
	s.Contains(p, "Description")
	s.Contains(p, "do the work")
	s.False(strings.Contains(p, "### Latest User Comment"))
}

func (s *ControllerSuite) TestCreatePrompt_NilSession() {
	p := s.c.createPrompt(nil, nil, testPromptContext)

	s.Contains(p, "**Repository:** ")
}

func (s *ControllerSuite) TestCreatePrompt_NilRepo() {
	session := &types.Session{RepoFullName: nil}

	p := s.c.createPrompt(session, nil, testPromptContext)

	s.Contains(p, "**Repository:** ")
}

func (s *ControllerSuite) TestCreatePrompt_PRComment() {
	seed := "seed-evt-1"
	ref := "workdock/main"
	event := &types.SessionEvent{
		Identifier: "evt-2",
		Seed:       &seed,
		GitRef:     &ref,
		Reason:     types.AgentSessionEventReason_PRComment,
	}

	p := s.c.createPrompt(newTestSession(), event, testPromptContext)

	s.True(strings.Contains(p, "### Latest User Comment"))
	s.True(strings.Contains(p, "There are review comments on the pull request"))
}

func (s *ControllerSuite) TestCreatePrompt_CheckRun() {
	seed := "seed-evt-1"
	ref := "workdock/main"
	event := &types.SessionEvent{
		Identifier: "evt-2",
		Seed:       &seed,
		GitRef:     &ref,
		Reason:     types.AgentSessionEventReason_CheckRun,
	}

	p := s.c.createPrompt(newTestSession(), event, testPromptContext)

	s.True(strings.Contains(p, "The pull request checks have failed"))
}

func (s *ControllerSuite) TestCreatePrompt_WithContext() {
	ctx := "additional user context"
	promptContext := &interfaces.PromptContext{
		Prompt:  "do the work",
		Context: &ctx,
		Issue:   types.Issue{Title: "Title", Identifier: "issue-1", Description: "Description"},
	}

	p := s.c.createPrompt(newTestSession(), nil, promptContext)

	s.True(strings.Contains(p, "### Latest User Comment"))
	s.True(strings.Contains(p, "additional user context"))
}

func (s *ControllerSuite) TestGetPrompt_AssemblesPrompt() {
	session := &types.Session{RepoFullName: repoName("workdock/repo")}
	s.agentHdl.getPromptContextFn = func(sessionEvent *types.SessionEvent) (*interfaces.PromptContext, error) {
		return testPromptContext, nil
	}

	prompt, err := s.c.getPrompt(context.Background(), s.agentHdl, session, testSessionEvent)

	s.Require().NoError(err)
	s.Contains(prompt, "do the work")
	s.Contains(prompt, "Title")
}

// ---------------------------------------------------------------------------
// verifyGitAccess
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestVerifyGitAccess_NoRepo() {
	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl,
		&types.Session{RepoFullName: nil}, testSessionEvent,
	)

	s.Require().NoError(err)
	s.Nil(access)
	s.Equal(0, s.gitRepo.upsertCount(), "no repo means no git connection writes")
	s.Nil(s.gitRepo.lastUpserted(), "no connection should be upserted")
}

func (s *ControllerSuite) TestVerifyGitAccess_ConnectionLookupError() {
	session := &types.Session{RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return nil, errors.New("lookup failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "lookup failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_RequiresConnection_RequestsAccess() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return nil, nil // not connected yet
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Require().NoError(err)
	s.Nil(access, "access is not granted until the user connects the repo")

	s.Require().Len(s.gitRepo.upsertedConnections, 1)
	upserted := s.gitRepo.upsertedConnections[0]
	s.Equal("workdock/repo", upserted.RepoFullName)
	s.False(upserted.Connected)
	s.Require().NotNil(upserted.SessionEventIdentifier)
	s.Equal("evt-1", *upserted.SessionEventIdentifier)

	s.Require().Len(s.agentHdl.gitRequests, 1)
	s.Equal("sess-1", s.agentHdl.gitRequests[0].sessionId)
	s.Equal("github", s.agentHdl.gitRequests[0].gitProvider)
}

func (s *ControllerSuite) TestVerifyGitAccess_UpsertError() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.gitRepo.upsertConnectionFn = func(ctx context.Context, connection *types.GitConnection) error {
		return errors.New("upsert failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "upsert failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_SendGitConnectionRequestError() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.agentHdl.sendGitConnectionRqFn = func(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error {
		return errors.New("send failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "send failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_GetGitAccessError() {
	session := &types.Session{RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return &types.GitConnection{
			RepoFullName:   "workdock/repo",
			Connected:      true,
			InstallationId: repoName("install-1"),
		}, nil
	}
	s.gitHdl.getGitAccessFn = func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
		return nil, errors.New("git access failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "git access failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_InstallationUnavailable_ResetsAndReRequests() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return &types.GitConnection{
			RepoFullName:   "workdock/repo",
			Connected:      true,
			InstallationId: repoName("install-1"),
		}, nil
	}
	s.gitHdl.getGitAccessFn = func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
		return nil, shared.ErrGitHubInstallationUnavailable
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Require().NoError(err)
	s.Nil(access, "re-connection flow ends with awaiting user action")

	s.Equal([]string{"install-1"}, s.gitRepo.resetCalls)
	s.Require().Equal(1, s.secretMgr.deleteCalls)
	s.Equal("/github/installations", s.secretMgr.deletedPath)
	s.Require().Len(s.agentHdl.gitRequests, 1)
}

func (s *ControllerSuite) TestVerifyGitAccess_ResetError() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return &types.GitConnection{
			RepoFullName:   "workdock/repo",
			Connected:      true,
			InstallationId: repoName("install-1"),
		}, nil
	}
	s.gitHdl.getGitAccessFn = func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
		return nil, shared.ErrGitHubInstallationUnavailable
	}
	s.gitRepo.resetConnectionFn = func(ctx context.Context, installationId string, repos []string) error {
		return errors.New("reset failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "reset failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_SecretDeleteError() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return &types.GitConnection{
			RepoFullName:   "workdock/repo",
			Connected:      true,
			InstallationId: repoName("install-1"),
		}, nil
	}
	s.gitHdl.getGitAccessFn = func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
		return nil, shared.ErrGitHubInstallationUnavailable
	}
	s.secretMgr.deleteFn = func(ctx context.Context, secretPath, secretName string) error {
		return errors.New("delete failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "delete failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_InstallationUnavailable_ReRequestSendError() {
	session := &types.Session{Identifier: "sess-1", RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return &types.GitConnection{
			RepoFullName:   "workdock/repo",
			Connected:      true,
			InstallationId: repoName("install-1"),
		}, nil
	}
	s.gitHdl.getGitAccessFn = func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
		return nil, shared.ErrGitHubInstallationUnavailable
	}
	s.agentHdl.sendGitConnectionRqFn = func(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error {
		return errors.New("re-request send failed")
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "re-request send failed")
	s.Nil(access)
}

func (s *ControllerSuite) TestVerifyGitAccess_Success() {
	session := &types.Session{RepoFullName: repoName("workdock/repo")}
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return &types.GitConnection{
			RepoFullName:   "workdock/repo",
			Connected:      true,
			InstallationId: repoName("install-1"),
		}, nil
	}
	expectedAccess := &interfaces.GitAccess{Granted: true, Secret: "git-secret"}
	s.gitHdl.getGitAccessFn = func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
		return expectedAccess, nil
	}

	access, err := s.c.verifyGitAccess(
		context.Background(), s.agentHdl, "token", s.gitHdl, session, testSessionEvent,
	)

	s.Require().NoError(err)
	s.Same(expectedAccess, access)
}

// ---------------------------------------------------------------------------
// sandbox
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestSandbox_NoMcpNoGitAccess() {
	stdout, stderr, shutdown, err := s.c.sandbox(
		context.Background(), s.gitHdl, s.harnessHdl, s.sandboxHdl,
		nil, "prompt text", newTestSession(), testSessionEvent,
	)

	s.Require().NoError(err)
	s.NotNil(stdout)
	s.NotNil(stderr)
	s.NotNil(shutdown)

	config := s.sandboxHdl.runConfig
	s.Require().NotNil(config)
	s.Equal(5, config.AutoStopInterval)
	s.NotNil(config.Session)
	s.Same(testSessionEvent, config.SessionEvent)
	s.Empty(config.Secrets)
	s.Equal([]string{"harness-config-cmd"}, config.CommandsWhenCreated[len(config.CommandsWhenCreated)-1:])
	s.Equal("opencode run", config.HarnessCommand)
	s.Len(config.FileUploads, 2)
}

func (s *ControllerSuite) TestSandbox_WithMcpAndGitAccess() {
	s.mcpHdl.getMCPListFn = func() []interfaces.MCPConfig {
		return []interfaces.MCPConfig{
			{Name: "linear", AuthKey: "LINEAR_KEY", AuthSecret: "linear-secret", Hosts: []string{"api.linear.app"}},
		}
	}
	gitAccess := &interfaces.GitAccess{
		EnvVarName: "GITHUB_TOKEN",
		Secret:     "git-secret",
		Hosts:      []string{"github.com"},
		Granted:    true,
	}

	_, _, _, err := s.c.sandbox(
		context.Background(), s.gitHdl, s.harnessHdl, s.sandboxHdl,
		gitAccess, "prompt", newTestSession(), testSessionEvent,
	)

	s.Require().NoError(err)
	config := s.sandboxHdl.runConfig
	s.Require().Len(config.Secrets, 2)
	s.Equal("LINEAR_KEY", config.Secrets[0].Name)
	s.Equal("linear-secret", config.Secrets[0].Value)
	s.Equal("GITHUB_TOKEN", config.Secrets[1].Name)
	s.Equal("git-secret", config.Secrets[1].Value)
}

func (s *ControllerSuite) TestSandbox_GitAccessNotGranted_NotInSecrets() {
	gitAccess := &interfaces.GitAccess{Granted: false, Secret: "git-secret"}

	_, _, _, err := s.c.sandbox(
		context.Background(), s.gitHdl, s.harnessHdl, s.sandboxHdl,
		gitAccess, "prompt", newTestSession(), testSessionEvent,
	)

	s.Require().NoError(err)
	s.Empty(s.sandboxHdl.runConfig.Secrets)
}

func (s *ControllerSuite) TestSandbox_NilMcpHandler() {
	s.c.mcpHandler = nil

	_, _, _, err := s.c.sandbox(
		context.Background(), s.gitHdl, s.harnessHdl, s.sandboxHdl,
		nil, "prompt", newTestSession(), testSessionEvent,
	)

	s.Require().NoError(err)
	s.Empty(s.sandboxHdl.runConfig.Secrets)
}

func (s *ControllerSuite) TestSandbox_GetConfigFileError() {
	s.harnessHdl.getConfigFileFn = func(config interfaces.HarnessConfig) (string, []byte, error) {
		return "", nil, errors.New("config file failed")
	}

	stdout, stderr, shutdown, err := s.c.sandbox(
		context.Background(), s.gitHdl, s.harnessHdl, s.sandboxHdl,
		nil, "prompt", newTestSession(), testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "config file failed")
	s.Nil(stdout)
	s.Nil(stderr)
	s.Nil(shutdown)
}

func (s *ControllerSuite) TestSandbox_RunError() {
	s.sandboxHdl.runFn = func(ctx context.Context, config *interfaces.SandboxConfig, stdout chan<- string, stderr chan<- string) (interfaces.SandboxShutdown, error) {
		return nil, errors.New("run failed")
	}

	_, _, shutdown, err := s.c.sandbox(
		context.Background(), s.gitHdl, s.harnessHdl, s.sandboxHdl,
		nil, "prompt", newTestSession(), testSessionEvent,
	)

	s.Error(err)
	s.ErrorContains(err, "run failed")
	s.Nil(shutdown)
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

// executeFixture builds a controller and pre-loads the repositories so
// execute() can reach the sandbox/harness stage.
type executeFixture struct {
	controller *controller
}

func (s *ControllerSuite) prepareExecutable() {
	// Session event + session lookups succeed.
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return testSessionEvent, nil
	}
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		session := *newTestSession()
		return &session, nil
	}
}

func (s *ControllerSuite) TestExecute_SessionEventLookupError() {
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, errors.New("event lookup failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "event lookup failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_SessionEventNotFound() {
	s.sessionRep.getAgentSessionEventFn = func(ctx context.Context, identifier string) (*types.SessionEvent, error) {
		return nil, nil
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.NoError(err)
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_SessionLookupError() {
	s.prepareExecutable()
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return nil, errors.New("session lookup failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "session lookup failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_SessionNotFound() {
	s.prepareExecutable()
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		return nil, nil
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.NoError(err)
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_HandlerMissing() {
	s.prepareExecutable()
	delete(s.c.agentHandlerRegistry, "linear")

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_CredentialsError() {
	s.prepareExecutable()
	s.agentHdl.getCredentialsFn = func(ctx context.Context, orgId string) (string, error) {
		return "", errors.New("credentials failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "credentials failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_PromptError() {
	s.prepareExecutable()
	s.agentHdl.getPromptContextFn = func(sessionEvent *types.SessionEvent) (*interfaces.PromptContext, error) {
		return nil, errors.New("prompt failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "prompt failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_GitAccessError() {
	s.prepareExecutable()
	s.sessionWithRepo()
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return nil, errors.New("git lookup failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "git lookup failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_AwaitingAction() {
	s.prepareExecutable()
	s.sessionWithRepo()
	// Repo has no connection: the flow must stop with AwaitingAction.
	s.gitRepo.getConnectionFn = func(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
		return nil, nil
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Require().NoError(err)
	s.Equal(types.EventJobStatus_AwaitingAction, status)
	s.Len(s.agentHdl.gitRequests, 1, "the user should have been asked to grant git access")
}

func (s *ControllerSuite) TestExecute_SandboxError() {
	s.prepareExecutable()
	s.sandboxHdl.runFn = func(ctx context.Context, config *interfaces.SandboxConfig, stdout chan<- string, stderr chan<- string) (interfaces.SandboxShutdown, error) {
		return nil, errors.New("sandbox failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "sandbox failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_HarnessError() {
	s.prepareExecutable()
	s.harnessHdl.parseFn = func(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,
		sendThought func(ctx context.Context, text string) error,
		sendResponse func(ctx context.Context, text string) error,
		sendAction func(ctx context.Context, action types.AgentAction) error,
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
		sendServerInternalError func(ctx context.Context) error,
	) error {
		// Fail immediately without draining `part`: the returned error cancels
		// the errgroup context, which unblocks the collector goroutine.
		return errors.New("harness failed")
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Error(err)
	s.ErrorContains(err, "harness failed")
	s.Equal(types.EventJobStatus_Failed, status)
}

func (s *ControllerSuite) TestExecute_Success_WithPullRequestResult() {
	s.prepareExecutable()

	var shutdownResult string
	s.sandboxHdl.runFn = func(ctx context.Context, config *interfaces.SandboxConfig, stdout chan<- string, stderr chan<- string) (interfaces.SandboxShutdown, error) {
		go func() {
			// The harness emits one valid JSON message, then closes the channels.
			stdout <- `{"type":"text","text":"working"}`
			close(stdout)
			close(stderr)
		}()

		return func(ctx context.Context) string {
			shutdownResult = "pr created"
			return shutdownResult
		}, nil
	}
	pr := &types.PullRequest{HeadRefName: "workdock/main", Number: 1, URL: "https://github.com/workdock/repo/pull/1"}
	s.gitHdl.parseLatestResultFn = func(changes string) *types.PullRequest {
		return pr
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Require().NoError(err)
	s.Equal(types.EventJobStatus_Succeeded, status)

	s.Require().Len(s.sessionRep.updatedResults, 1, "a parsed PR must be persisted as the session event result")
	updated := s.sessionRep.updatedResults[0]
	s.Require().NotNil(updated.Result)
	s.Same(pr, updated.Result.PullRequest)
	s.Require().NotNil(updated.GitRef)
	s.Equal("workdock/main", *updated.GitRef)
}

func (s *ControllerSuite) TestExecute_Success_NoPullRequest() {
	s.prepareExecutable()

	s.sandboxHdl.runFn = func(ctx context.Context, config *interfaces.SandboxConfig, stdout chan<- string, stderr chan<- string) (interfaces.SandboxShutdown, error) {
		go func() {
			close(stdout)
			close(stderr)
		}()

		return func(ctx context.Context) string {
			return "no pr"
		}, nil
	}
	s.gitHdl.parseLatestResultFn = func(changes string) *types.PullRequest {
		return nil
	}

	status, err := s.c.execute(context.Background(), &types.EventJob{SessionEventIdentifier: "evt-1"})

	s.Require().NoError(err)
	s.Equal(types.EventJobStatus_Succeeded, status)
	s.Empty(s.sessionRep.updatedResults, "no PR means no result update")
}

// ---------------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------------

func (s *ControllerSuite) TestHarness_ForwardsValidJsonLines() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	stdout <- `{"type":"text"}`
	close(stdout)
	close(stderr)

	err := <-done
	s.Require().NoError(err)
	s.Require().True(s.harnessHdl.parseReturned)
	s.Require().Len(s.harnessHdl.parsedParts, 1)
	s.Equal([]byte(`{"type":"text"}`), s.harnessHdl.parsedParts[0])
}

func (s *ControllerSuite) TestHarness_SkipsInvalidJsonLines() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	stdout <- "not json"
	close(stdout)
	close(stderr)

	err := <-done
	s.Require().NoError(err)
	s.Empty(s.harnessHdl.parsedParts)
}

func (s *ControllerSuite) TestHarness_FlushesPartialBufferOnClose() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	// No trailing newline: the message is buffered until the channel closes.
	stdout <- `{"partial":true}`
	close(stdout)
	close(stderr)

	err := <-done
	s.Require().NoError(err)
	s.Require().Len(s.harnessHdl.parsedParts, 1)
	s.Equal([]byte(`{"partial":true}`), s.harnessHdl.parsedParts[0])
}

func (s *ControllerSuite) TestHarness_MixedLines_OnlyValidForwarded() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	stdout <- "{\"id\":1}\n"
	stdout <- "garbage\n"
	stdout <- "{\"id\":2}\n"
	close(stdout)
	close(stderr)

	err := <-done
	s.Require().NoError(err)
	s.Require().Len(s.harnessHdl.parsedParts, 2)
}

func (s *ControllerSuite) TestHarness_ForwardsStderrAfterCompletion() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	stderr <- "something went wrong"
	close(stderr)
	close(stdout)

	err := <-done
	s.Require().NoError(err)
	// stderr is reported back to the user through SendResponse.
	s.Contains(s.agentHdl.responses, "something went wrong")
}

func (s *ControllerSuite) TestHarness_ParseError() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	s.harnessHdl.parseFn = func(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,
		sendThought func(ctx context.Context, text string) error,
		sendResponse func(ctx context.Context, text string) error,
		sendAction func(ctx context.Context, action types.AgentAction) error,
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
		sendServerInternalError func(ctx context.Context) error,
	) error {
		// Fail immediately: the error cancels the errgroup context, which
		// unblocks the collector goroutine still reading stdout/stderr.
		return errors.New("parse failed")
	}

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	stdout <- `{"type":"text"}`
	close(stdout)
	close(stderr)

	err := <-done
	s.Error(err)
	s.ErrorContains(err, "parse failed")
}

func (s *ControllerSuite) TestHarness_LivenessUnhealthy() {
	// Shrink the probe so the miss threshold triggers quickly.
	s.c.livenessProbeConfig = types.HarnessLivenessProbeConfig{MaxMisses: 1, PeriodSeconds: 1}

	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	// Simulate a hung harness: channels stay open and emit nothing.
	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	select {
	case err := <-done:
		s.ErrorIs(err, shared.ErrHarnessUnhealthy)
	case <-time.After(5 * time.Second):
		s.Fail("harness did not return after the liveness probe declared it unhealthy")
	}
}

func (s *ControllerSuite) TestHarness_LivenessDisabled() {
	// With the probe disabled (MaxMisses/PeriodSeconds = 0) a silent harness
	// keeps waiting on channels; closing them ends the run cleanly.
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	close(stdout)
	close(stderr)

	select {
	case err := <-done:
		s.NoError(err)
	case <-time.After(2 * time.Second):
		s.Fail("harness did not return after channels closed")
	}
}

func (s *ControllerSuite) TestHarness_ContextCancellation() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(ctx, stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	// Keep stdout open and cancel: the collector must return ctx.Err().
	stdout <- `{"type":"text"}`
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		s.Error(err)
		s.ErrorContains(err, "context canceled")
	case <-time.After(2 * time.Second):
		s.Fail("harness did not return after context cancellation")
	}
}

func (s *ControllerSuite) TestHarness_ParseCallbacksForwardsToAgentHandler() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	// The mock Parse invokes every callback it receives, then drains `part`.
	s.harnessHdl.parseFn = func(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,
		sendThought func(ctx context.Context, text string) error,
		sendResponse func(ctx context.Context, text string) error,
		sendAction func(ctx context.Context, action types.AgentAction) error,
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
		sendServerInternalError func(ctx context.Context) error,
	) error {
		s.Require().NoError(sendThought(ctx, "thinking"))
		s.Require().NoError(sendResponse(ctx, "response text"))
		s.Require().NoError(sendAction(ctx, types.AgentAction{Name: "approve"}))
		s.Require().NoError(sendElicitation(ctx, types.AgentElicitation{Question: "pick one"}))
		s.Require().NoError(sendServerInternalError(ctx))

		for range part {
		}
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	close(stdout)
	close(stderr)

	err := <-done
	s.Require().NoError(err)

	s.Contains(s.agentHdl.thoughts, "thinking")
	s.Contains(s.agentHdl.responses, "response text")
	s.Require().Len(s.agentHdl.actions, 1)
	s.Equal("approve", s.agentHdl.actions[0].Name)
	s.Require().Len(s.agentHdl.elicitations, 1)
	s.Equal("pick one", s.agentHdl.elicitations[0].Question)
	s.Equal(1, s.agentHdl.internalErrors)
}

func (s *ControllerSuite) TestHarness_UnhealthyForwardsStderrToUser() {
	s.c.livenessProbeConfig = types.HarnessLivenessProbeConfig{MaxMisses: 1, PeriodSeconds: 1}

	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	// Simulate a hung harness that logged an error before going silent.
	stderr <- "fatal: sandbox crashed"
	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	select {
	case err := <-done:
		s.ErrorIs(err, shared.ErrHarnessUnhealthy)
		// The captured stderr is reported back to the user on a live context
		// even though the run context is cancelled by the probe.
		s.Contains(s.agentHdl.responses, "fatal: sandbox crashed")
	case <-time.After(5 * time.Second):
		s.Fail("harness did not return after the liveness probe declared it unhealthy")
	}
}

func (s *ControllerSuite) TestHarness_LivenessProbeStopsOnContextCancel() {
	s.c.livenessProbeConfig = types.HarnessLivenessProbeConfig{MaxMisses: 100, PeriodSeconds: 1}

	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(ctx, stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	cancel()

	select {
	case err := <-done:
		s.Error(err)
		s.ErrorContains(err, "context canceled")
	case <-time.After(2 * time.Second):
		s.Fail("harness did not return after context cancellation")
	}
}

func (s *ControllerSuite) TestHarness_LivenessProbeStopsWhenChannelsClose() {
	// With the probe enabled, a healthy run that simply finishes must stop the
	// probe through the done channel and end cleanly.
	s.c.livenessProbeConfig = types.HarnessLivenessProbeConfig{MaxMisses: 1, PeriodSeconds: 1}

	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	stdout <- "{\"id\":1}\n"
	close(stdout)
	close(stderr)

	select {
	case err := <-done:
		s.NoError(err)
		s.True(s.harnessHdl.parseReturned)
	case <-time.After(2 * time.Second):
		s.Fail("harness did not return after channels closed")
	}
}

func (s *ControllerSuite) TestHarness_FlushAbortsOnContextCancel() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	// Parse never reads from part, so once part's buffer (100) is full the
	// flush send on stdout close blocks until the context is cancelled.
	s.harnessHdl.parseFn = func(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,
		sendThought func(ctx context.Context, text string) error,
		sendResponse func(ctx context.Context, text string) error,
		sendAction func(ctx context.Context, action types.AgentAction) error,
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
		sendServerInternalError func(ctx context.Context) error,
	) error {
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(ctx, stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	// Fill part's buffer with 100 complete newline-delimited messages.
	for i := 0; i < 100; i++ {
		stdout <- fmt.Sprintf(`{"n":%d}`, i) + "\n"
	}
	time.Sleep(100 * time.Millisecond)

	// A partial message without a newline: buffered, then flushed on close.
	// The flush send blocks (part is full) and must observe the cancellation.
	stdout <- `{"partial":true}`
	close(stdout)
	close(stderr)
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		s.Error(err)
		s.ErrorContains(err, "context canceled")
	case <-time.After(3 * time.Second):
		s.Fail("harness did not return after context cancellation")
	}
}

func (s *ControllerSuite) TestHarness_PartSendAbortsOnContextCancel() {
	// part has buffer 100; fill it so the collector blocks sending and the
	// ctx.Done branch of the select is taken on cancellation.
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	s.harnessHdl.parseFn = func(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,
		sendThought func(ctx context.Context, text string) error,
		sendResponse func(ctx context.Context, text string) error,
		sendAction func(ctx context.Context, action types.AgentAction) error,
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
		sendServerInternalError func(ctx context.Context) error,
	) error {
		// Never read from part.
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(ctx, stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	// Buffer is 100; 101 valid messages force the 101st send to block.
	for i := 0; i < 101; i++ {
		stdout <- fmt.Sprintf(`{"n":%d}`, i) + "\n"
	}
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		s.Error(err)
		s.ErrorContains(err, "context canceled")
	case <-time.After(3 * time.Second):
		s.Fail("harness did not return after context cancellation")
	}
}

func (s *ControllerSuite) TestHarness_BufferCarriesOverToNextMessage() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)

	done := make(chan error, 1)
	go func() {
		done <- s.c.harness(context.Background(), stdout, stderr, s.agentHdl, "token", s.harnessHdl, newTestSession(), testSessionEvent)
	}()

	// A single write containing two complete messages: after the first is
	// forwarded, remaining bytes must trigger the pending startMessage branch.
	stdout <- "{\"id\":1}\n{\"id\":2}\n"
	close(stdout)
	close(stderr)

	err := <-done
	s.Require().NoError(err)
	s.Require().Len(s.harnessHdl.parsedParts, 2)
}

// ---------------------------------------------------------------------------
// Helpers used by execute/verifyGitAccess tests
// ---------------------------------------------------------------------------

// sessionWithRepo makes the session looked up by execute carry a repo so the
// git access verification stage runs.
func (s *ControllerSuite) sessionWithRepo() {
	s.sessionRep.getAgentSessionFn = func(ctx context.Context, identifier string) (*types.Session, error) {
		session := *newTestSession()
		repo := "workdock/repo"
		session.RepoFullName = &repo
		return &session, nil
	}
}
