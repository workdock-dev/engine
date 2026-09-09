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

package linear

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	oauth20 "github.com/workdock-dev/engine/features/oauth2.0"
	"github.com/workdock-dev/engine/features/webhook"
	"github.com/workdock-dev/engine/plug-ins/linear/interfaces"
	"github.com/workdock-dev/engine/plug-ins/linear/types"
	"github.com/workdock-dev/engine/shared"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockClient struct {
	labelsFn      func(ctx context.Context, issueId, accessToken string) ([]string, error)
	exchangeFn    func(ctx context.Context, code string) (*types.TokenExchanged, error)
	workspaceFn   func(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error)
	refreshFn     func(ctx context.Context, refreshToken string) (*types.Token, error)
	activityFn    func(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error
	issueFn       func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error)
	workflowFn    func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error)
	updateFn      func(ctx context.Context, accessToken, issueId, stateId string) error
	credentialsFn func(ctx context.Context, organizationId string) (string, error)

	activities     []types.CreateAgentActivityInput
	activityTokens []string
	issueCalls     []string
	workflowCalls  []string
	issueUpdates   []issueUpdateCall
}

type issueUpdateCall struct {
	issueId string
	stateId string
}

func (m *mockClient) SendInitialThought(ctx context.Context, sessionId, organizationId string) error {
	m.activities = append(m.activities, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Thought, Body: ""},
	})
	m.activityTokens = append(m.activityTokens, "org-access-token")
	if m.activityFn != nil {
		return m.activityFn(ctx, "org-access-token", types.CreateAgentActivityInput{
			AgentSessionID: sessionId,
			Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Thought, Body: ""},
		})
	}
	return nil
}

func (m *mockClient) GetCredentials(ctx context.Context, organizationId string) (string, error) {
	if m.credentialsFn != nil {
		return m.credentialsFn(ctx, organizationId)
	}
	return "org-access-token", nil
}

func (m *mockClient) GetIssueLabels(ctx context.Context, issueId, accessToken string) ([]string, error) {
	if m.labelsFn != nil {
		return m.labelsFn(ctx, issueId, accessToken)
	}
	return []string{"bug", "backend"}, nil
}

func (m *mockClient) GetIssue(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
	m.issueCalls = append(m.issueCalls, issueId)
	if m.issueFn != nil {
		return m.issueFn(ctx, accessToken, issueId)
	}
	return &types.IssueStateResult{
		ID:        issueId,
		TeamID:    "team-1",
		StateName: "Todo",
		StateType: types.IssueStateType_Unstarted,
	}, nil
}

func (m *mockClient) GetTeamWorkflowStates(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
	m.workflowCalls = append(m.workflowCalls, teamId)
	if m.workflowFn != nil {
		return m.workflowFn(ctx, accessToken, teamId)
	}
	return []types.WorkflowState{
		{ID: "state-todo", Name: "Todo", Type: types.IssueStateType_Unstarted, Position: 0},
		{ID: "state-in-progress", Name: "In Progress", Type: types.IssueStateType_Started, Position: 1},
		{ID: "state-in-review", Name: "In Review", Type: types.IssueStateType_Started, Position: 2},
	}, nil
}

func (m *mockClient) UpdateIssueState(ctx context.Context, accessToken, issueId, stateId string) error {
	m.issueUpdates = append(m.issueUpdates, issueUpdateCall{issueId: issueId, stateId: stateId})
	if m.updateFn != nil {
		return m.updateFn(ctx, accessToken, issueId, stateId)
	}
	return nil
}

func (m *mockClient) ExchangeCode(ctx context.Context, code string) (*types.TokenExchanged, error) {
	if m.exchangeFn != nil {
		return m.exchangeFn(ctx, code)
	}
	return &types.TokenExchanged{AccessToken: "at", RefreshToken: "rt", ExpiresIn: 3600}, nil
}

func (m *mockClient) GetWorkspaceInfo(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error) {
	if m.workspaceFn != nil {
		return m.workspaceFn(ctx, accessToken)
	}
	return &types.WorkspaceInfo{ID: "ws-1", Name: "Acme"}, nil
}

func (m *mockClient) RefreshToken(ctx context.Context, refreshToken string) (*types.Token, error) {
	if m.refreshFn != nil {
		return m.refreshFn(ctx, refreshToken)
	}
	return &types.Token{AccessToken: "at", RefreshToken: "rt"}, nil
}

func (m *mockClient) CreateAgentActivity(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error {
	m.activities = append(m.activities, input)
	m.activityTokens = append(m.activityTokens, accessToken)
	if m.activityFn != nil {
		return m.activityFn(ctx, accessToken, input)
	}
	return nil
}

type mockSecretManager struct {
	getFn func(ctx context.Context, secretPath, secretName string) (string, error)
	setFn func(ctx context.Context, secretPath, secretName, secretValue string) error
}

func (m *mockSecretManager) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	if m.getFn != nil {
		return m.getFn(ctx, secretPath, secretName)
	}
	data, err := json.Marshal(types.Token{
		AccessToken:  "org-access-token",
		RefreshToken: "org-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	if err != nil {
		panic(err)
	}
	return string(data), nil
}

func (m *mockSecretManager) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	if m.setFn != nil {
		return m.setFn(ctx, secretPath, secretName, secretValue)
	}
	return nil
}

func (m *mockSecretManager) Delete(ctx context.Context, secretPath, secretName string) error {
	return nil
}

// eventRecorder subscribes to the Linear-relevant events on a real EventBus.
// Publish is synchronous, so recorded events are available immediately.
type eventRecorder struct {
	archiveEvents []shared.AgentSessionArchiveEvent
	prompt        []shared.AgentSessionPromptEvent
	stop          []shared.AgentSessionStopEvent
}

func newRecordingEventBus(rec *eventRecorder) *shared.EventBus {
	bus := shared.NewEventBus()
	handle := func(ctx context.Context, event shared.DomainEvent) error {
		switch e := event.(type) {
		case shared.AgentSessionArchiveEvent:
			rec.archiveEvents = append(rec.archiveEvents, e)
		case shared.AgentSessionPromptEvent:
			rec.prompt = append(rec.prompt, e)
		case shared.AgentSessionStopEvent:
			rec.stop = append(rec.stop, e)
		}
		return nil
	}
	bus.Subscribe(shared.EventType_AgentSessionArchive, handle)
	bus.Subscribe(shared.EventType_AgentSessionPrompt, handle)
	bus.Subscribe(shared.EventType_AgentSessionStop, handle)
	return bus
}

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

var (
	testConfig = types.Config{
		WebhookSecret: "whsec",
		IPs:           []string{"10.0.0.1", "10.0.0.2"},
		ClientId:      "client-1",
		ServerUrl:     "https://server.example.com",
	}
)

func signHmac(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// handler_webhook.go — WEventTransformer
// ---------------------------------------------------------------------------

type WebhookSuite struct {
	suite.Suite
	recorder *eventRecorder
	bus      *shared.EventBus
	client   *mockClient
}

func TestWebhookSuite(t *testing.T) {
	suite.Run(t, new(WebhookSuite))
}

func (s *WebhookSuite) SetupTest() {
	s.recorder = &eventRecorder{}
	s.bus = newRecordingEventBus(s.recorder)
	s.client = &mockClient{}
}

func (s *WebhookSuite) newConsumer() *WEventConsumer {
	return NewWEventConsumer(s.bus, s.client).(*WEventConsumer)
}

func (s *WebhookSuite) TestTransform() {
	t := NewWEventTransformer()
	var iface webhook.WEventTransformer = t
	s.NotNil(iface)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"a":1}`))
	req.Header.Set("Linear-Event", "Issue")
	req.RemoteAddr = "10.0.0.1:1234"

	event, err := t.Transform(context.Background(), req)

	s.Require().NoError(err)
	s.Require().NotNil(event)
	s.Equal("Issue", event.Get("Linear-Event"))
	s.Equal("10.0.0.1:1234", event.RemoteAddr)
	s.Equal(req.Body, event.Body)
}

func (s *WebhookSuite) newVerifier() *WEventVerifier {
	return NewWEventVerifier(testConfig).(*WEventVerifier)
}

// ---------------------------------------------------------------------------
// clientIP / isAllowedIP
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestClientIP() {
	verifier := s.newVerifier()

	tests := []struct {
		name       string
		headers    map[string][]string
		remoteAddr string
		want       string
	}{
		{
			name:       "xff first entry trimmed",
			headers:    map[string][]string{"X-Forwarded-For": {" 10.0.0.9 , 10.0.0.10"}},
			remoteAddr: "192.168.0.1:5678",
			want:       "10.0.0.9",
		},
		{
			name:       "xff blank first entry falls back to x-real-ip",
			headers:    map[string][]string{"X-Forwarded-For": {" ,10.0.0.10"}, "X-Real-Ip": {"10.0.0.5"}},
			remoteAddr: "192.168.0.1:5678",
			want:       "10.0.0.5",
		},
		{
			name:       "x-real-ip",
			headers:    map[string][]string{"X-Real-Ip": {"10.0.0.5"}},
			remoteAddr: "192.168.0.1:5678",
			want:       "10.0.0.5",
		},
		{
			name:       "remote addr host split",
			remoteAddr: "192.168.0.1:5678",
			want:       "192.168.0.1",
		},
		{
			name:       "remote addr without port",
			remoteAddr: "192.168.0.1",
			want:       "192.168.0.1",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			event := &webhook.WEvent{
				Headers:    map[string][]string{},
				RemoteAddr: tt.remoteAddr,
			}
			for k, v := range tt.headers {
				event.Headers[k] = v
			}

			s.Equal(tt.want, verifier.clientIP(event))
		})
	}
}

func (s *WebhookSuite) TestIsAllowedIP() {
	verifier := s.newVerifier()

	allowed := &webhook.WEvent{
		Headers:    map[string][]string{"X-Forwarded-For": {"10.0.0.2"}},
		RemoteAddr: "192.168.0.1:5678",
	}
	s.True(verifier.isAllowedIP(allowed))

	denied := &webhook.WEvent{
		Headers:    map[string][]string{},
		RemoteAddr: "192.168.0.1:5678",
	}
	s.False(verifier.isAllowedIP(denied))
}

// ---------------------------------------------------------------------------
// verifyWebhookSignature
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestVerifyWebhookSignature() {
	verifier := s.newVerifier()
	body := []byte(`{"hello":"world"}`)
	valid := signHmac(body, testConfig.WebhookSecret)

	tests := []struct {
		name      string
		signature string
		body      []byte
		want      bool
	}{
		{name: "valid", signature: valid, body: body, want: true},
		{name: "empty signature", signature: "", body: body, want: false},
		{name: "invalid hex", signature: "zzzz", body: body, want: false},
		{name: "wrong secret", signature: signHmac(body, "other"), body: body, want: false},
		{name: "tampered body", signature: valid, body: []byte(`{"hello":"tampered"}`), want: false},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, verifier.verifyWebhookSignature(tt.signature, tt.body))
		})
	}
}

// ---------------------------------------------------------------------------
// Verify
// ---------------------------------------------------------------------------

func (s *WebhookSuite) newWEvent(headers map[string]string, body io.Reader) *webhook.WEvent {
	s.T().Helper()

	parsed := make(map[string][]string, len(headers))
	for k, v := range headers {
		parsed[k] = []string{v}
	}
	return &webhook.WEvent{
		Headers:    parsed,
		RemoteAddr: "10.0.0.1:1234",
		Body:       body,
	}
}

func (s *WebhookSuite) TestVerify_BodyReadError() {
	verifier := s.newVerifier()

	_, err := verifier.Verify(context.Background(), &webhook.WEvent{
		Headers: map[string][]string{"X-Forwarded-For": {"10.0.0.1"}},
		Body:    &errorReader{},
	})

	s.ErrorIs(err, webhook.ErrWBadRequest)
}

func (s *WebhookSuite) TestVerify_UntrustedIP() {
	verifier := s.newVerifier()

	_, err := verifier.Verify(context.Background(), &webhook.WEvent{
		Headers:    map[string][]string{},
		RemoteAddr: "8.8.8.8:9999",
		Body:       strings.NewReader(`{}`),
	})

	s.ErrorIs(err, webhook.ErrWForBidden)
}

func (s *WebhookSuite) TestVerify_BadSignature() {
	verifier := s.newVerifier()

	_, err := verifier.Verify(context.Background(), s.newWEvent(
		map[string]string{"Linear-Signature": "0000"},
		strings.NewReader(`{}`),
	))

	s.ErrorIs(err, webhook.ErrWUnAuthorized)
}

func (s *WebhookSuite) TestVerify_UnhandledEventType() {
	verifier := s.newVerifier()
	body := []byte(`{}`)

	_, err := verifier.Verify(context.Background(), s.newWEvent(
		map[string]string{
			"Linear-Signature": signHmac(body, testConfig.WebhookSecret),
			"Linear-Event":     "Comment",
		},
		strings.NewReader(string(body)),
	))

	s.ErrorIs(err, webhook.ErrWBadRequest)
}

func (s *WebhookSuite) TestVerify_Success() {
	verifier := s.newVerifier()
	body := []byte(`{"action":"update"}`)

	verified, err := verifier.Verify(context.Background(), s.newWEvent(
		map[string]string{
			"Linear-Signature": signHmac(body, testConfig.WebhookSecret),
			"Linear-Event":     "Issue",
		},
		strings.NewReader(string(body)),
	))

	s.Require().NoError(err)
	s.Equal(WEventType_Issue, verified.WEventType)
	s.Equal(body, verified.Payload)

	verified, err = verifier.Verify(context.Background(), s.newWEvent(
		map[string]string{
			"Linear-Signature": signHmac(body, testConfig.WebhookSecret),
			"Linear-Event":     "AgentSessionEvent",
		},
		strings.NewReader(string(body)),
	))

	s.Require().NoError(err)
	s.Equal(WEventType_AgentSession, verified.WEventType)
	s.Equal(body, verified.Payload)
}

// ---------------------------------------------------------------------------
// verifyTimestampRecency
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestVerifyTimestampRecency() {
	consumer := s.newConsumer()

	s.NoError(consumer.verifyTimestampRecency(time.Now().UnixMilli()))
	s.NoError(consumer.verifyTimestampRecency(time.Now().Add(59 * time.Second).UnixMilli()))

	s.ErrorIs(consumer.verifyTimestampRecency(time.Now().Add(-time.Hour).UnixMilli()), webhook.ErrWUnAuthorized)
	s.ErrorIs(consumer.verifyTimestampRecency(time.Now().Add(-2*time.Minute).UnixMilli()), webhook.ErrWUnAuthorized)
	s.ErrorIs(consumer.verifyTimestampRecency(time.Now().Add(2*time.Minute).UnixMilli()), webhook.ErrWUnAuthorized)
}

// ---------------------------------------------------------------------------
// Consume
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestConsume_UnhandledEventType() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: "mystery",
		Payload:    []byte(`{}`),
	})

	s.ErrorIs(err, webhook.ErrWBadRequest)
	s.Empty(s.recorder.archiveEvents)
	s.Empty(s.recorder.prompt)
}

func (s *WebhookSuite) TestConsume_Issue_InvalidJson() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte("{not-json"),
	})

	s.ErrorIs(err, webhook.ErrWBadRequest)
	s.Empty(s.recorder.archiveEvents)
}

func (s *WebhookSuite) TestConsume_Issue_StaleTimestamp() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte(`{"webhookTimestamp": 1}`),
	})

	s.ErrorIs(err, webhook.ErrWUnAuthorized)
	s.Empty(s.recorder.archiveEvents)
}

func (s *WebhookSuite) TestConsume_Issue_Success() {
	payload := fmt.Sprintf(`{
		"action": "update",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"data": {"id": "issue-1", "title": "Title", "identifier": "ENG-1"},
		"updatedFrom": {"stateName": "Todo"}
	}`, time.Now().UnixMilli())

	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		s.Equal("org-access-token", accessToken)
		s.Equal("issue-1", issueId)
		return &types.IssueStateResult{
			ID:        issueId,
			TeamID:    "team-1",
			StateName: "Done",
			StateType: types.IssueStateType_Completed,
		}, nil
	}

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.archiveEvents, 1)

	event := s.recorder.archiveEvents[0]
	s.Equal(string(shared.PlatformProvider_Linear), event.Provider)
	s.Equal("issue-1", event.IssueId)
}

func (s *WebhookSuite) TestConsume_Issue_NotCompletedState_NoEvent() {
	payload := fmt.Sprintf(`{
		"action": "update",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"data": {"id": "issue-1"},
		"updatedFrom": {"stateName": "Done"}
	}`, time.Now().UnixMilli())

	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{
			ID:        issueId,
			TeamID:    "team-1",
			StateName: "Todo",
			StateType: types.IssueStateType_Unstarted,
		}, nil
	}

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Empty(s.recorder.archiveEvents, "non-done issues must not emit the agent session archive event")
}

func (s *WebhookSuite) TestConsume_Issue_CreationAction_NoEvent() {
	payload := fmt.Sprintf(`{
		"action": "create",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"data": {"id": "issue-1"}
	}`, time.Now().UnixMilli())

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Empty(s.recorder.archiveEvents, "creation events must not emit the agent session archive event")
	s.Empty(s.client.issueCalls, "creation events must not query the issue state")
}

func (s *WebhookSuite) TestConsume_Issue_CredentialsError_NoEvent() {
	payload := fmt.Sprintf(`{
		"action": "update",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"data": {"id": "issue-1"}
	}`, time.Now().UnixMilli())

	consumer := NewWEventConsumer(s.bus, &mockClient{credentialsFn: func(ctx context.Context, organizationId string) (string, error) {
		return "", fmt.Errorf("boom")
	}}).(*WEventConsumer)

	err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err, "issue verification failures must not fail webhook ingestion")
	s.Empty(s.recorder.archiveEvents)
}

func (s *WebhookSuite) TestConsume_Issue_GetIssueError_NoEvent() {
	payload := fmt.Sprintf(`{
		"action": "update",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"data": {"id": "issue-1"}
	}`, time.Now().UnixMilli())

	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return nil, fmt.Errorf("linear api down")
	}

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issue,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err, "issue verification failures must not fail webhook ingestion")
	s.Empty(s.recorder.archiveEvents)
}

func (s *WebhookSuite) TestConsume_AgentSession_InvalidJson() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte("{not-json"),
	})

	s.ErrorIs(err, webhook.ErrWBadRequest)
	s.Empty(s.recorder.prompt)
	s.Empty(s.recorder.stop)
}

func (s *WebhookSuite) TestConsume_AgentSession_StaleTimestamp() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(`{"webhookTimestamp": 1}`),
	})

	s.ErrorIs(err, webhook.ErrWUnAuthorized)
	s.Empty(s.recorder.prompt)
	s.Empty(s.recorder.stop)
}

func (s *WebhookSuite) TestConsume_AgentSession_StopSignal() {
	payload := fmt.Sprintf(`{
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"},
		"agentActivity": {"signal": "%s"}
	}`, time.Now().UnixMilli(), types.SignalType_Stop)

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Empty(s.recorder.prompt)
	s.Require().Len(s.recorder.stop, 1)

	event := s.recorder.stop[0]
	s.Equal(string(shared.PlatformProvider_Linear), event.Provider)
	s.Equal("org-1", event.OrganizationIdentifier)
	s.Equal("sess-1", event.SessionIdentifier)
}

func (s *WebhookSuite) TestConsume_AgentSession_Prompt() {
	payload := fmt.Sprintf(`{
		"promptContext": "do the work",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"},
		"agentActivity": {"signal": "", "content": {"type": "thought", "body": "hello"}}
	}`, time.Now().UnixMilli())

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Empty(s.recorder.stop)
	s.Require().Len(s.recorder.prompt, 1)

	event := s.recorder.prompt[0]
	s.Equal(string(shared.PlatformProvider_Linear), event.Provider)
	data, ok := event.Payload.(types.AgentSessionEventData)
	s.Require().True(ok)
	s.Equal("do the work", data.PromptContext)
	s.Equal("sess-1", data.AgentSession.ID)
	s.Equal("hello", data.AgentActivity.Content.Body)
}

func (s *WebhookSuite) TestConsume_AgentSession_Created_SendsInitialThoughtBeforePromptEvent() {
	payload := fmt.Sprintf(`{
		"action": "%s",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"}
	}`, types.AgentSessionAction_Created, time.Now().UnixMilli())

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.prompt, 1)

	// The acknowledgement must reach Linear before the prompt event is
	// processed (which performs all the DB work and job insertion).
	s.Require().Len(s.client.activities, 1)
	s.Equal(types.CreateAgentActivityInput{
		AgentSessionID: "sess-1",
		Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Thought, Body: ""},
	}, s.client.activities[0])
}

func (s *WebhookSuite) TestConsume_AgentSession_Prompted_DoesNotSendInitialThought() {
	payload := fmt.Sprintf(`{
		"action": "%s",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"},
		"agentActivity": {"signal": "", "content": {"type": "prompt", "body": "do more"}}
	}`, types.AgentSessionAction_Prompted, time.Now().UnixMilli())

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.prompt, 1)
	s.Empty(s.client.activities)
}

func (s *WebhookSuite) TestConsume_AgentSession_StopSignal_DoesNotSendInitialThought() {
	payload := fmt.Sprintf(`{
		"action": "%s",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"},
		"agentActivity": {"signal": "%s"}
	}`, types.AgentSessionAction_Created, time.Now().UnixMilli(), types.SignalType_Stop)

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Empty(s.recorder.prompt)
	s.Require().Len(s.recorder.stop, 1)
	s.Empty(s.client.activities)
}

func (s *WebhookSuite) TestConsume_AgentSession_Created_ThoughtFailureDoesNotBlockIngestion() {
	s.client.activityFn = func(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error {
		return fmt.Errorf("linear api down")
	}
	payload := fmt.Sprintf(`{
		"action": "%s",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"}
	}`, types.AgentSessionAction_Created, time.Now().UnixMilli())

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.prompt, 1, "prompt event must still be published when the acknowledgement fails")
	s.Require().Len(s.client.activities, 1)
}

func (s *WebhookSuite) TestConsume_AgentSession_Created_MissingSessionData() {
	payload := fmt.Sprintf(`{
		"action": "%s",
		"organizationId": "org-1",
		"webhookTimestamp": %d
	}`, types.AgentSessionAction_Created, time.Now().UnixMilli())

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.prompt, 1)
	s.Empty(s.client.activities)
}

func (s *WebhookSuite) TestConsume_AgentSession_Created_NilClientStillIngests() {
	consumer := NewWEventConsumer(s.bus, nil).(*WEventConsumer)
	payload := fmt.Sprintf(`{
		"action": "%s",
		"organizationId": "org-1",
		"webhookTimestamp": %d,
		"agentSession": {"id": "sess-1"}
	}`, types.AgentSessionAction_Created, time.Now().UnixMilli())

	err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_AgentSession,
		Payload:    []byte(payload),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.prompt, 1)
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

// ---------------------------------------------------------------------------
// handler_agent_session.go — AgentSessionHandler
// ---------------------------------------------------------------------------

type AgentSessionSuite struct {
	suite.Suite
	client  *mockClient
	secrets *mockSecretManager
	handler *AgentSessionHandler
}

func TestAgentSessionSuite(t *testing.T) {
	suite.Run(t, new(AgentSessionSuite))
}

func (s *AgentSessionSuite) SetupTest() {
	s.client = &mockClient{}
	s.secrets = &mockSecretManager{}
	s.handler = NewAgentSessionHandler(s.client, s.secrets).(*AgentSessionHandler)
}

func (s *AgentSessionSuite) TestNewAgentSessionHandler() {
	var iface agent_session_interfaces.HandlerAgentSession = NewAgentSessionHandler(s.client, s.secrets)
	s.NotNil(iface)
	s.IsType(&AgentSessionHandler{}, iface)
}

func (s *AgentSessionSuite) TestIngest_WrongEventType() {
	_, _, err := s.handler.Ingest(mismatchedDomainEvent{})

	s.Error(err)
	s.Contains(err.Error(), "failed to ingest")
}

type mismatchedDomainEvent struct{}

func (m mismatchedDomainEvent) EventType() string { return "agent_session.prompt" }

func (s *AgentSessionSuite) TestIngest_WrongPayload() {
	_, _, err := s.handler.Ingest(shared.AgentSessionPromptEvent{Payload: "not-a-struct"})

	s.Error(err)
	s.Contains(err.Error(), "failed to cast payload")
}

func (s *AgentSessionSuite) TestIngest_Success() {
	event := shared.AgentSessionPromptEvent{
		Payload: agentSessionEventData(),
	}

	session, sessionEvent, err := s.handler.Ingest(event)

	s.Require().NoError(err)
	s.Require().NotNil(session)
	s.Require().NotNil(sessionEvent)

	s.Equal("org-1", session.OrganizationIdentifier)
	s.Equal("sess-1", session.Identifier)
	s.Equal(shared.PlatformProvider_Linear, session.Provider)
	s.Equal("issue-1", session.IssueId)
	s.Equal("Alice", session.Creator)
	s.Nil(session.RepoFullName)

	s.Equal("sess-1", sessionEvent.SessionIdentifier)
	s.Len(sessionEvent.Identifier, 36, "idempotency key must be 36 hex chars")
	s.NotEmpty(sessionEvent.Payload)

	var unmarshaled types.AgentSessionEventData
	s.Require().NoError(json.Unmarshal(sessionEvent.Payload, &unmarshaled))
	s.Equal("sess-1", unmarshaled.AgentSession.ID)
}

func (s *AgentSessionSuite) TestIngest_IdempotencyKeyDeterministic() {
	event := shared.AgentSessionPromptEvent{Payload: agentSessionEventData()}

	_, first, err := s.handler.Ingest(event)
	s.Require().NoError(err)

	_, second, err := s.handler.Ingest(event)
	s.Require().NoError(err)

	s.Equal(first.Identifier, second.Identifier)
}

func agentSessionEventData() types.AgentSessionEventData {
	return types.AgentSessionEventData{
		PromptContext:  "do the work",
		OrganizationID: "org-1",
		AgentSession: &types.AgentSession{
			ID:        "sess-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   types.User{Name: "Alice"},
			Issue:     types.Issue{ID: "issue-1", Title: "Title", Identifier: "ENG-1", Description: "desc"},
		},
	}
}

func (s *AgentSessionSuite) TestGetLabels() {
	s.client.labelsFn = func(ctx context.Context, issueId, accessToken string) ([]string, error) {
		s.Equal("issue-1", issueId)
		s.Equal("token-1", accessToken)
		return []string{"bug"}, nil
	}

	labels, err := s.handler.GetLabels(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Equal([]string{"bug"}, labels)
}

func (s *AgentSessionSuite) TestGetCredentials() {
	token, err := s.handler.GetCredentials(context.Background(), "org-1")

	s.Require().NoError(err)
	s.Equal("org-access-token", token)
}

func (s *AgentSessionSuite) TestGetCredentials_SecretError() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return "", fmt.Errorf("boom")
	}

	_, err := s.handler.GetCredentials(context.Background(), "org-1")

	s.Error(err)
}

func (s *AgentSessionSuite) TestGetPromptContext_InvalidJson() {
	_, err := s.handler.GetPromptContext(&agent_session_types.SessionEvent{
		Identifier: "evt-1",
		Payload:    []byte("{not-json"),
	})

	s.Error(err)
}

func (s *AgentSessionSuite) TestGetPromptContext_NilAgentSession() {
	_, err := s.handler.GetPromptContext(&agent_session_types.SessionEvent{
		Identifier: "evt-1",
		Payload:    []byte(`{"promptContext":"work"}`),
	})

	s.Error(err)
	s.Contains(err.Error(), "corrupted state")
}

func (s *AgentSessionSuite) TestGetPromptContext_WithActivity() {
	data := agentSessionEventData()
	data.AgentActivity = &types.AgentActivity{
		Content: types.ActivityContent{Type: "thought", Body: "recent thoughts"},
	}
	payload, err := json.Marshal(data)
	s.Require().NoError(err)

	context, err := s.handler.GetPromptContext(&agent_session_types.SessionEvent{
		Identifier: "evt-1",
		Payload:    payload,
	})

	s.Require().NoError(err)
	s.Equal("do the work", context.Prompt)
	s.Require().NotNil(context.Context)
	s.Equal("recent thoughts", *context.Context)
	s.Equal("Title", context.Issue.Title)
	s.Equal("ENG-1", context.Issue.Identifier)
	s.Equal("desc", context.Issue.Description)
}

func (s *AgentSessionSuite) TestGetPromptContext_WithoutActivity() {
	data := agentSessionEventData()
	data.AgentActivity = nil
	payload, err := json.Marshal(data)
	s.Require().NoError(err)

	context, err := s.handler.GetPromptContext(&agent_session_types.SessionEvent{
		Identifier: "evt-1",
		Payload:    payload,
	})

	s.Require().NoError(err)
	s.Equal("do the work", context.Prompt)
	s.Nil(context.Context)
	s.Equal("Title", context.Issue.Title)
}

// ---------------------------------------------------------------------------
// Send* methods
// ---------------------------------------------------------------------------

func (s *AgentSessionSuite) TestSendThought() {
	err := s.handler.SendThought(context.Background(), "sess-1", "token-1", "thinking")

	s.Require().NoError(err)
	s.Require().Len(s.client.activities, 1)
	s.Equal(types.CreateAgentActivityInput{
		AgentSessionID: "sess-1",
		Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Thought, Body: "thinking"},
	}, s.client.activities[0])
	s.Equal("token-1", s.client.activityTokens[0])
}

func (s *AgentSessionSuite) TestSendResponse() {
	err := s.handler.SendResponse(context.Background(), "sess-1", "token-1", "here you go")

	s.Require().NoError(err)
	s.Require().Len(s.client.activities, 1)
	s.Equal(types.CreateAgentActivityInput{
		AgentSessionID: "sess-1",
		Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Response, Body: "here you go"},
	}, s.client.activities[0])
}

func (s *AgentSessionSuite) TestSendAction() {
	err := s.handler.SendAction(context.Background(), "sess-1", "token-1", agent_session_types.AgentAction{
		Name:   "bash",
		Input:  "ls",
		Output: "files",
	})

	s.Require().NoError(err)
	s.Require().Len(s.client.activities, 1)
	s.Equal(types.CreateAgentActivityInput{
		AgentSessionID: "sess-1",
		Content: types.AgentActivityContent{
			Type:      types.AgentActivityContentType_Action,
			Action:    "bash",
			Parameter: "ls",
			Result:    "files",
		},
	}, s.client.activities[0])
}

func (s *AgentSessionSuite) TestSendElicitation() {
	err := s.handler.SendElicitation(context.Background(), "sess-1", "token-1", agent_session_types.AgentElicitation{
		Question: "Which DB?",
		Options:  []agent_session_types.AgentOption{{Label: "Postgres", Description: "relational"}},
	})

	s.Require().NoError(err)
	s.Require().Len(s.client.activities, 1)
	input := s.client.activities[0]
	s.Equal(types.CreateAgentActivityInput{
		AgentSessionID: "sess-1",
		Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Elicitation, Body: "Which DB?"},
		Signal:         types.SignalType_Select,
	}, types.CreateAgentActivityInput{
		AgentSessionID: input.AgentSessionID,
		Content:        input.Content,
		Signal:         input.Signal,
	})
	s.NotNil(input.SignalMetadata)
}

func (s *AgentSessionSuite) TestSendGitConnectionRequest() {
	err := s.handler.SendGitConnectionRequest(context.Background(), "sess-1", "token-1", "github", "https://install")

	s.Require().NoError(err)
	s.Require().Len(s.client.activities, 1)
	input := s.client.activities[0]
	s.Equal("sess-1", input.AgentSessionID)
	s.Equal(types.AgentActivityContentType_Elicitation, input.Content.Type)
	s.Equal("Git connection required", input.Content.Body)
	s.Equal(types.SignalType_Auth, input.Signal)
	s.Equal("https://install", input.SignalMetadata["url"])
	s.Equal("github", input.SignalMetadata["providerName"])
}

func (s *AgentSessionSuite) TestSendServerInternalError() {
	err := s.handler.SendServerInternalError(context.Background(), "sess-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.activities, 1)
	s.Equal(types.CreateAgentActivityInput{
		AgentSessionID: "sess-1",
		Content:        types.AgentActivityContent{Type: types.AgentActivityContentType_Error, Body: "Internal Server Error 500"},
	}, s.client.activities[0])
}

func (s *AgentSessionSuite) TestSend_ActivityErrorPropagates() {
	s.client.activityFn = func(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error {
		return fmt.Errorf("api down")
	}

	err := s.handler.SendThought(context.Background(), "sess-1", "token-1", "thinking")

	s.Error(err)
}

// ---------------------------------------------------------------------------
// Issue state transitions
// ---------------------------------------------------------------------------

func (s *AgentSessionSuite) TestGetIssueState() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		s.Equal("token-1", accessToken)
		s.Equal("issue-1", issueId)
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "Todo", StateType: types.IssueStateType_Unstarted}, nil
	}

	state, err := s.handler.GetIssueState(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Equal("Todo", state.Name)
	s.Equal(types.IssueStateType_Unstarted, state.Type)
}

func (s *AgentSessionSuite) TestGetIssueState_Error() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return nil, fmt.Errorf("api down")
	}

	_, err := s.handler.GetIssueState(context.Background(), "issue-1", "token-1")

	s.Error(err)
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_UnstartedIssue() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		s.Equal("team-1", teamId)
		return []types.WorkflowState{
			{ID: "state-todo", Name: "Todo", Type: types.IssueStateType_Unstarted, Position: 0},
			{ID: "state-in-progress", Name: "In Progress", Type: types.IssueStateType_Started, Position: 1},
			{ID: "state-in-review", Name: "In Review", Type: types.IssueStateType_Started, Position: 2},
		}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("issue-1", s.client.issueUpdates[0].issueId)
	s.Equal("state-in-progress", s.client.issueUpdates[0].stateId)
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_SelectsLowestPosition() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		return []types.WorkflowState{
			{ID: "state-in-review", Name: "In Review", Type: types.IssueStateType_Started, Position: 5},
			{ID: "state-in-progress", Name: "In Progress", Type: types.IssueStateType_Started, Position: 2},
		}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("state-in-progress", s.client.issueUpdates[0].stateId)
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_InReviewIssue() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "In Review", StateType: types.IssueStateType_Started}, nil
	}
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		return []types.WorkflowState{
			{ID: "state-in-progress", Name: "In Progress", Type: types.IssueStateType_Started, Position: 1},
			{ID: "state-in-review", Name: "In Review", Type: types.IssueStateType_Started, Position: 2},
		}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("issue-1", s.client.issueUpdates[0].issueId)
	s.Equal("state-in-progress", s.client.issueUpdates[0].stateId, "an issue in In Review must move back to the first started state")
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_AlreadyStarted_Transitions() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "In Progress", StateType: types.IssueStateType_Started}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("issue-1", s.client.issueUpdates[0].issueId)
	s.Equal("state-in-progress", s.client.issueUpdates[0].stateId, "an issue already in the first started state must still be transitioned")
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_AlreadyCompleted_Transitions() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "Done", StateType: types.IssueStateType_Completed}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("state-in-progress", s.client.issueUpdates[0].stateId, "a completed issue must still be transitioned to the first started state")
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_AlreadyCanceled_Transitions() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "Canceled", StateType: types.IssueStateType_Canceled}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("state-in-progress", s.client.issueUpdates[0].stateId, "a canceled issue must still be transitioned to the first started state")
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_GetIssueError() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return nil, fmt.Errorf("api down")
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Error(err)
	s.Empty(s.client.issueUpdates)
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_WorkflowError() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		return nil, fmt.Errorf("api down")
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Error(err)
	s.Empty(s.client.issueUpdates)
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_NoStartedState() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		return []types.WorkflowState{
			{ID: "state-todo", Name: "Todo", Type: types.IssueStateType_Unstarted, Position: 0},
		}, nil
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Error(err)
	s.Contains(err.Error(), "no started workflow state")
	s.Empty(s.client.issueUpdates)
}

func (s *AgentSessionSuite) TestTransitionIssueToStarted_UpdateError() {
	s.client.updateFn = func(ctx context.Context, accessToken, issueId, stateId string) error {
		return fmt.Errorf("api down")
	}

	err := s.handler.TransitionIssueToStarted(context.Background(), "issue-1", "token-1")

	s.Error(err)
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		s.Equal("team-1", teamId)
		return []types.WorkflowState{
			{ID: "state-todo", Name: "Todo", Type: types.IssueStateType_Unstarted, Position: 0},
			{ID: "state-in-progress", Name: "In Progress", Type: types.IssueStateType_Started, Position: 1},
			{ID: "state-in-review", Name: "In Review", Type: types.IssueStateType_Started, Position: 2},
		}, nil
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("issue-1", s.client.issueUpdates[0].issueId)
	s.Equal("state-in-review", s.client.issueUpdates[0].stateId)
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_AlreadyInReview_Transitions() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "In Review", StateType: types.IssueStateType_Started}, nil
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("issue-1", s.client.issueUpdates[0].issueId)
	s.Equal("state-in-review", s.client.issueUpdates[0].stateId, "an issue already in In Review must still be transitioned")
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_AlreadyCompleted_Transitions() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "Done", StateType: types.IssueStateType_Completed}, nil
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("state-in-review", s.client.issueUpdates[0].stateId, "a completed issue must still be transitioned to In Review")
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_AlreadyCanceled_Transitions() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return &types.IssueStateResult{ID: issueId, TeamID: "team-1", StateName: "Canceled", StateType: types.IssueStateType_Canceled}, nil
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Require().NoError(err)
	s.Require().Len(s.client.issueUpdates, 1)
	s.Equal("state-in-review", s.client.issueUpdates[0].stateId, "a canceled issue must still be transitioned to In Review")
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_GetIssueError() {
	s.client.issueFn = func(ctx context.Context, accessToken, issueId string) (*types.IssueStateResult, error) {
		return nil, fmt.Errorf("api down")
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Error(err)
	s.Empty(s.client.issueUpdates)
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_WorkflowError() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		return nil, fmt.Errorf("api down")
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Error(err)
	s.Empty(s.client.issueUpdates)
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_NoInReviewState() {
	s.client.workflowFn = func(ctx context.Context, accessToken, teamId string) ([]types.WorkflowState, error) {
		return []types.WorkflowState{
			{ID: "state-todo", Name: "Todo", Type: types.IssueStateType_Unstarted, Position: 0},
			{ID: "state-in-progress", Name: "In Progress", Type: types.IssueStateType_Started, Position: 1},
		}, nil
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Error(err)
	s.Contains(err.Error(), "no \"In Review\" workflow state")
	s.Empty(s.client.issueUpdates)
}

func (s *AgentSessionSuite) TestTransitionIssueToInReview_UpdateError() {
	s.client.updateFn = func(ctx context.Context, accessToken, issueId, stateId string) error {
		return fmt.Errorf("api down")
	}

	err := s.handler.TransitionIssueToInReview(context.Background(), "issue-1", "token-1")

	s.Error(err)
}

// ---------------------------------------------------------------------------
// handler_oauth20.go — oauth20Handler
// ---------------------------------------------------------------------------

type OAuthSuite struct {
	suite.Suite
	client *mockClient
}

func TestOAuthSuite(t *testing.T) {
	suite.Run(t, new(OAuthSuite))
}

func (s *OAuthSuite) SetupTest() {
	s.client = &mockClient{}
}

func (s *OAuthSuite) newHandler() oauth20.OauthHandler {
	return NewOAuth20Handler(testConfig, s.client)
}

func (s *OAuthSuite) TestNewOAuth20Handler() {
	handler := s.newHandler()
	s.NotNil(handler)
	s.IsType(&oauth20Handler{}, handler)
}

func (s *OAuthSuite) TestGetAuthorizationURL() {
	url := s.newHandler().GetAuthorizationURL()

	s.Equal(
		"https://linear.app/oauth/authorize?client_id=client-1&redirect_uri=https://server.example.com/linear/oauth/callback&response_type=code&scope=read,write,app:assignable,app:mentionable&actor=app",
		url,
	)
}

func (s *OAuthSuite) TestCallback_ErrorCode() {
	result, err := s.newHandler().Callback(context.Background(), "code-1", "user_denied")

	s.ErrorIs(err, shared.ErrBadRequest)
	s.Nil(result)
}

func (s *OAuthSuite) TestCallback_MissingCode() {
	result, err := s.newHandler().Callback(context.Background(), "", "")

	s.ErrorIs(err, shared.ErrBadRequest)
	s.Nil(result)
}

func (s *OAuthSuite) TestCallback_ExchangeError() {
	s.client.exchangeFn = func(ctx context.Context, code string) (*types.TokenExchanged, error) {
		return nil, fmt.Errorf("bad code")
	}

	result, err := s.newHandler().Callback(context.Background(), "code-1", "")

	s.Error(err)
	s.Nil(result)
}

func (s *OAuthSuite) TestCallback_WorkspaceInfoError() {
	s.client.workspaceFn = func(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error) {
		return nil, fmt.Errorf("api down")
	}

	result, err := s.newHandler().Callback(context.Background(), "code-1", "")

	s.Error(err)
	s.Nil(result)
}

func (s *OAuthSuite) TestCallback_Success() {
	before := time.Now()
	var usedCode string
	s.client.exchangeFn = func(ctx context.Context, code string) (*types.TokenExchanged, error) {
		usedCode = code
		return &types.TokenExchanged{AccessToken: "at-1", RefreshToken: "rt-1", ExpiresIn: 600}, nil
	}
	s.client.workspaceFn = func(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error) {
		s.Equal("at-1", accessToken)
		return &types.WorkspaceInfo{ID: "ws-9", Name: "Acme Inc"}, nil
	}

	result, err := s.newHandler().Callback(context.Background(), "code-42", "")

	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Equal("code-42", usedCode)

	s.Equal("ws-9", result.EntityId)
	s.Equal("Acme Inc", result.EntityName)
	s.Equal("Acme Inc", result.Message)
	s.Equal("at-1", result.Token.AccessToken)
	s.Equal("rt-1", result.Token.RefreshToken)
	s.WithinDuration(before.Add(600*time.Second), result.Token.ExpiresAt, 5*time.Second)
}

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

var (
	_ interfaces.Client                            = (*mockClient)(nil)
	_ shared.SecretManager                         = (*mockSecretManager)(nil)
	_ shared.DomainEvent                           = mismatchedDomainEvent{}
	_ agent_session_interfaces.HandlerAgentSession = (*AgentSessionHandler)(nil)
	_ oauth20.OauthHandler                         = (*oauth20Handler)(nil)
	_ webhook.WEventConsumer                       = (*WEventConsumer)(nil)
)
