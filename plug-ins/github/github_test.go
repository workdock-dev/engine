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

package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/features/webhook"
	"github.com/workdock-dev/engine/plug-ins/github/interfaces"
	"github.com/workdock-dev/engine/plug-ins/github/types"
	"github.com/workdock-dev/engine/shared"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockClient struct {
	isPublicFn  func(ctx context.Context, repo string) (bool, error)
	createTokFn func(installationId int) (*types.InstallationAccessToken, error)

	createTokCalledWith []int
}

func (m *mockClient) IsRepositoryPublic(ctx context.Context, repo string) (bool, error) {
	if m.isPublicFn != nil {
		return m.isPublicFn(ctx, repo)
	}
	return false, nil
}

func (m *mockClient) CreateInstallationAccessToken(installationId int) (*types.InstallationAccessToken, error) {
	m.createTokCalledWith = append(m.createTokCalledWith, installationId)
	if m.createTokFn != nil {
		return m.createTokFn(installationId)
	}
	return &types.InstallationAccessToken{Token: "fresh-token"}, nil
}

type mockSecretManager struct {
	getFn func(ctx context.Context, secretPath, secretName string) (string, error)
	setFn func(ctx context.Context, secretPath, secretName, secretValue string) error

	gets []string
	sets []string
}

func (m *mockSecretManager) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	m.gets = append(m.gets, secretPath+"/"+secretName)
	if m.getFn != nil {
		return m.getFn(ctx, secretPath, secretName)
	}
	return "", nil
}

func (m *mockSecretManager) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	m.sets = append(m.sets, secretPath+"/"+secretName+"="+secretValue)
	if m.setFn != nil {
		return m.setFn(ctx, secretPath, secretName, secretValue)
	}
	return nil
}

func (m *mockSecretManager) Delete(ctx context.Context, secretPath, secretName string) error {
	return nil
}

// eventRecorder subscribes to every relevant event type on a real EventBus.
// Publish is synchronous, so events are available right after the handler
// under test returns.
type eventRecorder struct {
	complete []shared.GitCompleteConnectionEvent
	reset    []shared.GitResetConnectionEvent
	comment  []shared.PullRequestCommentedEvent
	checks   []shared.PullRequestChecksFailedEvent
}

func newRecordingEventBus(rec *eventRecorder) *shared.EventBus {
	bus := shared.NewEventBus()
	handle := func(ctx context.Context, event shared.DomainEvent) error {
		switch e := event.(type) {
		case shared.GitCompleteConnectionEvent:
			rec.complete = append(rec.complete, e)
		case shared.GitResetConnectionEvent:
			rec.reset = append(rec.reset, e)
		case shared.PullRequestCommentedEvent:
			rec.comment = append(rec.comment, e)
		case shared.PullRequestChecksFailedEvent:
			rec.checks = append(rec.checks, e)
		}
		return nil
	}
	bus.Subscribe(shared.EventType_GitCompleteConnection, handle)
	bus.Subscribe(shared.EventType_GitResetConnection, handle)
	bus.Subscribe(shared.EventType_PullRequestCommented, handle)
	bus.Subscribe(shared.EventType_PullRequestChecksFailed, handle)
	return bus
}

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

var (
	testConfig = types.Config{
		BotLoginId:    "workdock-bot",
		WebhookSecret: "whsec",
	}

	futureToken = types.InstallationAccessToken{
		Token:     "stored-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}
)

func marshalToken(t types.InstallationAccessToken) string {
	data, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// common.go — getGitHubAccessToken
// ---------------------------------------------------------------------------

type CommonSuite struct {
	suite.Suite
	secrets *mockSecretManager
	client  *mockClient
}

func TestCommonSuite(t *testing.T) {
	suite.Run(t, new(CommonSuite))
}

func (s *CommonSuite) SetupTest() {
	s.secrets = &mockSecretManager{}
	s.client = &mockClient{}
}

func (s *CommonSuite) TestGetGitHubAccessToken_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := getGitHubAccessToken(ctx, s.secrets, s.client, "123")

	s.ErrorIs(err, context.Canceled)
	s.Empty(s.secrets.gets)
}

func (s *CommonSuite) TestGetGitHubAccessToken_SecretGetError() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return "", fmt.Errorf("boom")
	}

	_, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Error(err)
	s.Contains(err.Error(), "failed to get github token")
}

func (s *CommonSuite) TestGetGitHubAccessToken_UnmarshalError() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return "{not-json", nil
	}

	_, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Error(err)
	s.Contains(err.Error(), "failed to unmarshal github token")
}

func (s *CommonSuite) TestGetGitHubAccessToken_ValidTokenNoRenewal() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		s.Equal(types.GitHub_SecretPath, secretPath)
		s.Equal("123", secretName)
		return marshalToken(futureToken), nil
	}

	token, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Require().NoError(err)
	s.Equal("stored-token", token)
	s.Empty(s.client.createTokCalledWith)
	s.Empty(s.secrets.sets)
}

func (s *CommonSuite) TestGetGitHubAccessToken_Renewal_InvalidInstallationId() {
	expiring := futureToken
	expiring.ExpiresAt = time.Now().Add(-time.Hour)
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return marshalToken(expiring), nil
	}

	_, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "not-a-number")

	s.Error(err)
	s.Contains(err.Error(), "failed to parse installation id")
}

func (s *CommonSuite) TestGetGitHubAccessToken_Renewal_ClientError() {
	expiring := futureToken
	expiring.ExpiresAt = time.Now().Add(-time.Hour)
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return marshalToken(expiring), nil
	}
	s.client.createTokFn = func(installationId int) (*types.InstallationAccessToken, error) {
		return nil, fmt.Errorf("api down")
	}

	_, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Error(err)
	s.Contains(err.Error(), "failed to renew github access token")
}

func (s *CommonSuite) TestGetGitHubAccessToken_Renewal_MarshalError() {
	expiring := futureToken
	expiring.ExpiresAt = time.Now().Add(-time.Hour)
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return marshalToken(expiring), nil
	}
	// Permissions is `any`, so an unmarshalable value makes the renewal
	// marshal step fail.
	s.client.createTokFn = func(installationId int) (*types.InstallationAccessToken, error) {
		return &types.InstallationAccessToken{Token: "t", Permissions: make(chan int)}, nil
	}

	_, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Error(err)
	s.Contains(err.Error(), "failed to marshal github token")
}

func (s *CommonSuite) TestGetGitHubAccessToken_Renewal_SecretSetError() {
	expiring := futureToken
	expiring.ExpiresAt = time.Now().Add(-time.Hour)
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return marshalToken(expiring), nil
	}
	s.secrets.setFn = func(ctx context.Context, secretPath, secretName, secretValue string) error {
		return fmt.Errorf("store down")
	}

	_, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Error(err)
	s.Contains(err.Error(), "failed to store github token")
}

func (s *CommonSuite) TestGetGitHubAccessToken_Renewal_Success() {
	expiring := futureToken
	expiring.ExpiresAt = time.Now().Add(-time.Hour)
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return marshalToken(expiring), nil
	}
	s.client.createTokFn = func(installationId int) (*types.InstallationAccessToken, error) {
		s.Equal(123, installationId)
		return &types.InstallationAccessToken{Token: "renewed-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}

	token, err := getGitHubAccessToken(context.Background(), s.secrets, s.client, "123")

	s.Require().NoError(err)
	s.Equal("renewed-token", token)
	s.Require().Len(s.secrets.sets, 1)
	s.Contains(s.secrets.sets[0], types.GitHub_SecretPath)
	s.Contains(s.secrets.sets[0], "renewed-token")
}

// ---------------------------------------------------------------------------
// handler_git.go — GitHandler
// ---------------------------------------------------------------------------

type GitHandlerSuite struct {
	suite.Suite
	secrets *mockSecretManager
	client  *mockClient
	handler *GitHandler
}

func TestGitHandlerSuite(t *testing.T) {
	suite.Run(t, new(GitHandlerSuite))
}

func (s *GitHandlerSuite) SetupTest() {
	s.secrets = &mockSecretManager{}
	s.client = &mockClient{}
	s.handler = &GitHandler{
		installationUrl: "https://github.com/apps/workdock/installations/new",
		client:          s.client,
		secretManager:   s.secrets,
	}
}

func (s *GitHandlerSuite) TestNewGitHandler() {
	h := NewGitHandler(
		types.Config{AppInstallURL: "https://install-url"},
		s.client,
		s.secrets,
	)

	var iface agent_session_interfaces.HandlerGit = h
	s.IsType(&GitHandler{}, iface)
	gitH := h.(*GitHandler)
	s.Equal("https://install-url", gitH.installationUrl)
	s.Equal(s.client, gitH.client)
	s.Equal(s.secrets, gitH.secretManager)
}

func (s *GitHandlerSuite) TestGetInstallationUrl() {
	s.Equal("https://github.com/apps/workdock/installations/new", s.handler.GetInstallationUrl())
}

func (s *GitHandlerSuite) TestGetConfigurationCommands() {
	commands := s.handler.GetConfigurationCommands()

	s.Require().Len(commands, 1)
	s.Equal(GH_CLI_INSTALL, commands[0])
	s.NotEmpty(commands[0])
}

func (s *GitHandlerSuite) TestGetCommands() {
	commands := s.handler.GetCommands()

	s.Require().Len(commands, 1)
	s.Equal(GH_GIT_SETUP, commands[0])
	s.NotEmpty(commands[0])
}

func (s *GitHandlerSuite) TestGetLatestChangesCommand() {
	s.Equal(GET_CHANGES, s.handler.GetLatestChangesCommand())
	s.NotEmpty(GET_CHANGES)
}

func (s *GitHandlerSuite) TestGetGitAccess_Success() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return marshalToken(futureToken), nil
	}

	access, err := s.handler.GetGitAccess(context.Background(), &agent_session_types.GitConnection{
		InstallationId: strPtr("123"),
	})

	s.Require().NoError(err)
	s.Equal(GITHUB_ACCESS_TOKEN_ENV_VAR, access.EnvVarName)
	s.Equal("stored-token", access.Secret)
	s.Equal([]string{"api.github.com", "github.com"}, access.Hosts)
	s.True(access.Granted)
}

func (s *GitHandlerSuite) TestGetGitAccess_Error() {
	s.secrets.getFn = func(ctx context.Context, secretPath, secretName string) (string, error) {
		return "", fmt.Errorf("boom")
	}

	access, err := s.handler.GetGitAccess(context.Background(), &agent_session_types.GitConnection{
		InstallationId: strPtr("123"),
	})

	s.Error(err)
	s.Nil(access)
}

func (s *GitHandlerSuite) TestParseLatestChangesResult_Empty() {
	s.Nil(s.handler.ParseLatestChangesResult(""))
}

func (s *GitHandlerSuite) TestParseLatestChangesResult_InvalidJson() {
	s.Nil(s.handler.ParseLatestChangesResult("{not-json"))
}

func (s *GitHandlerSuite) TestParseLatestChangesResult_Success() {
	payload := `{
		"headRefName": "feature-branch",
		"headRefOid": "abc123",
		"number": 42,
		"url": "https://github.com/owner/repo/pull/42"
	}`

	pr := s.handler.ParseLatestChangesResult(payload)

	s.Require().NotNil(pr)
	s.Equal("feature-branch", pr.HeadRefName)
	s.Equal("abc123", pr.HeadRefOID)
	s.Equal(42, pr.Number)
	s.Equal("https://github.com/owner/repo/pull/42", pr.URL)
}

func strPtr(v string) *string { return &v }

// ---------------------------------------------------------------------------
// handler_webhook.go — WEventTransformer
// ---------------------------------------------------------------------------

type WebhookSuite struct {
	suite.Suite
	recorder *eventRecorder
	bus      *shared.EventBus
}

func TestWebhookSuite(t *testing.T) {
	suite.Run(t, new(WebhookSuite))
}

func (s *WebhookSuite) SetupTest() {
	s.recorder = &eventRecorder{}
	s.bus = newRecordingEventBus(s.recorder)
}

func (s *WebhookSuite) newConsumer() *WEventConsumer {
	return NewWEventConsumer(testConfig, &mockClient{}, s.bus).(*WEventConsumer)
}

// ---------------------------------------------------------------------------
// WEventTransformer
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestTransform() {
	t := NewWEventTransformer()
	var iface webhook.WEventTransformer = t
	s.NotNil(iface)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"a":1}`))
	req.Header.Set("X-GitHub-Event", "ping")
	req.RemoteAddr = "1.2.3.4:5678"

	event, err := t.Transform(context.Background(), req)

	s.Require().NoError(err)
	s.Require().NotNil(event)
	s.Equal("ping", event.Get("X-GitHub-Event"))
	s.Equal("1.2.3.4:5678", event.RemoteAddr)
	s.Equal(req.Body, event.Body)
}

// ---------------------------------------------------------------------------
// WEventVerifier
// ---------------------------------------------------------------------------

func (s *WebhookSuite) sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s *WebhookSuite) verifiedEvent(eventType string, payload []byte) *webhook.VerifiedWEvent {
	s.T().Helper()

	body := append([]byte(nil), payload...)
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", s.sign(body, testConfig.WebhookSecret))
	headers.Set("X-GitHub-Event", eventType)
	headers.Set("X-GitHub-Delivery", "delivery-1")

	event := &webhook.WEvent{
		Headers: headers,
		Body:    strings.NewReader(string(body)),
	}

	verifier := NewWEventVerifier(testConfig)
	verified, err := verifier.Verify(context.Background(), event)
	s.Require().NoError(err)
	return verified
}

func (s *WebhookSuite) TestVerify_BodyReadError() {
	verifier := NewWEventVerifier(testConfig)

	event := &webhook.WEvent{Body: &errorBodyReader{}}
	_, err := verifier.Verify(context.Background(), event)

	s.ErrorIs(err, webhook.ErrWBadRequest)
}

func (s *WebhookSuite) TestVerify_MissingSignature() {
	verifier := NewWEventVerifier(testConfig)

	event := &webhook.WEvent{
		Headers: map[string][]string{"X-GitHub-Event": {"ping"}},
		Body:    strings.NewReader("{}"),
	}
	_, err := verifier.Verify(context.Background(), event)

	s.ErrorIs(err, webhook.ErrWUnAuthorized)
}

func (s *WebhookSuite) TestVerify_MissingEventType() {
	verifier := NewWEventVerifier(testConfig)

	event := &webhook.WEvent{
		Headers: map[string][]string{
			"X-Hub-Signature-256": {s.sign([]byte("{}"), testConfig.WebhookSecret)},
		},
		Body: strings.NewReader("{}"),
	}
	_, err := verifier.Verify(context.Background(), event)

	s.ErrorIs(err, webhook.ErrWBadRequest)
}

func (s *WebhookSuite) TestVerify_Success() {
	payload := []byte(`{"action":"created"}`)

	verified := s.verifiedEvent("installation", payload)

	s.Equal("installation", verified.WEventType)
	s.Equal("delivery-1", verified.DeliveryID)
	s.Equal(payload, verified.Payload)
}

func (s *WebhookSuite) TestVerifyWebhookSignature() {
	verifier := NewWEventVerifier(testConfig).(*WEventVerifier)
	body := []byte(`{"hello":"world"}`)
	valid := s.sign(body, testConfig.WebhookSecret)

	tests := []struct {
		name      string
		signature string
		body      []byte
		want      bool
	}{
		{name: "valid", signature: valid, body: body, want: true},
		{name: "empty signature", signature: "", body: body, want: false},
		{name: "no sha256 prefix", signature: hex.EncodeToString(macSum(body, testConfig.WebhookSecret)), body: body, want: false},
		{name: "short signature", signature: "sha256=ab", body: body, want: false},
		{name: "invalid hex", signature: "sha256=zzzz", body: body, want: false},
		{name: "wrong secret", signature: s.sign(body, "other-secret"), body: body, want: false},
		{name: "tampered body", signature: valid, body: []byte(`{"hello":"tampered"}`), want: false},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, verifier.verifyWebhookSignature(tt.signature, tt.body))
		})
	}
}

func macSum(body []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}

// errorBodyReader is an io.Reader that fails, used to trigger the Verify body
// read error branch.
type errorBodyReader struct{}

func (e *errorBodyReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

// ---------------------------------------------------------------------------
// WEventConsumer — routing
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestConsume_Ping() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Ping,
		Payload:    []byte("{invalid"),
	})

	s.NoError(err)
}

func (s *WebhookSuite) TestConsume_InvalidPayload() {
	consumer := s.newConsumer()

	tests := []string{
		WEventType_Installation,
		WEventType_InstallationRepositories,
		WEventType_PullRequestReviewComment,
		WEventType_CheckSuite,
		"totally_unknown",
	}

	for _, eventType := range tests {
		s.Run(eventType, func() {
			err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
				WEventType: eventType,
				Payload:    []byte("{invalid"),
			})

			s.ErrorIs(err, webhook.ErrWBadRequest)
		})
	}
}

func (s *WebhookSuite) TestConsume_IssuesIsIgnored() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Issues,
		Payload:    []byte(`{"action":"opened"}`),
	})

	s.NoError(err)
}

func (s *WebhookSuite) TestConsume_UnknownEventType() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: "mystery_event",
		Payload:    []byte(`{"action":"opened"}`),
	})

	s.ErrorIs(err, webhook.ErrWBadRequest)
}

// ---------------------------------------------------------------------------
// WEventConsumer — installation
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestHandleInstallation_NilInstallation() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload:    []byte(`{"action":"created"}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.complete)
	s.Empty(s.recorder.reset)
}

func (s *WebhookSuite) TestHandleInstallation_Deleted() {
	payload := []byte(`{
		"action": "deleted",
		"installation": {"id": 7},
		"repositories": [{"full_name": "owner/repo-a"}]
	}`)

	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload:    payload,
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.reset, 1)
	s.Equal([]string{"owner/repo-a"}, s.recorder.reset[0].Repos)
	s.Equal("7", s.recorder.reset[0].InstallationId)
	s.True(s.recorder.reset[0].Delete)
	s.Empty(s.recorder.complete)
}

func (s *WebhookSuite) TestHandleInstallation_OtherActionIgnored() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload:    []byte(`{"action":"suspend","installation":{"id":7}}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.complete)
	s.Empty(s.recorder.reset)
}

func (s *WebhookSuite) TestHandleInstallation_NoReposGranted() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload:    []byte(`{"action":"created","installation":{"id":7}}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.complete)
}

func (s *WebhookSuite) TestHandleInstallation_TokenError() {
	client := &mockClient{}
	client.createTokFn = func(installationId int) (*types.InstallationAccessToken, error) {
		return nil, fmt.Errorf("api down")
	}
	consumer := NewWEventConsumer(testConfig, client, s.bus)

	err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload:    []byte(`{"action":"created","installation":{"id":7},"repositories":[{"full_name":"o/r"}]}`),
	})

	s.Error(err)
	s.Empty(s.recorder.complete)
}

func (s *WebhookSuite) TestHandleInstallation_TokenMarshalError() {
	client := &mockClient{}
	// Permissions is `any`, so an unmarshalable value makes the consumer's
	// token marshal step fail.
	client.createTokFn = func(installationId int) (*types.InstallationAccessToken, error) {
		return &types.InstallationAccessToken{Token: "ghs_x", Permissions: make(chan int)}, nil
	}
	consumer := NewWEventConsumer(testConfig, client, s.bus)

	err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload:    []byte(`{"action":"created","installation":{"id":7},"repositories":[{"full_name":"o/r"}]}`),
	})

	s.Error(err)
	s.Empty(s.recorder.complete)
}

func (s *WebhookSuite) TestHandleInstallation_Success() {
	client := &mockClient{}
	client.createTokFn = func(installationId int) (*types.InstallationAccessToken, error) {
		s.Equal(7, installationId)
		return &types.InstallationAccessToken{Token: "ghs_x", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	consumer := NewWEventConsumer(testConfig, client, s.bus)

	err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_Installation,
		Payload: []byte(`{
			"action": "added",
			"installation": {"id": 7},
			"repositories": [{"full_name": "owner/kept"}],
			"repositories_added": [{"full_name": "owner/new"}]
		}`),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.complete, 1)

	event := s.recorder.complete[0]
	s.Equal([]string{"owner/kept", "owner/new"}, event.Repos)
	s.Equal("7", event.InstallationId)

	var token types.InstallationAccessToken
	s.Require().NoError(json.Unmarshal(event.Token, &token))
	s.Equal("ghs_x", token.Token)
}

// ---------------------------------------------------------------------------
// WEventConsumer — installation_repositories
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestHandleInstallationRepositories_NilInstallation() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_InstallationRepositories,
		Payload:    []byte(`{"action":"added"}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.complete)
	s.Empty(s.recorder.reset)
}

func (s *WebhookSuite) TestHandleInstallationRepositories_Added() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_InstallationRepositories,
		Payload: []byte(`{
			"action": "added",
			"installation": {"id": 9},
			"repositories_added": [{"full_name": "owner/new-1"}, {"full_name": "owner/new-2"}]
		}`),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.complete, 1)
	s.Equal([]string{"owner/new-1", "owner/new-2"}, s.recorder.complete[0].Repos)
	s.Equal("9", s.recorder.complete[0].InstallationId)
}

func (s *WebhookSuite) TestHandleInstallationRepositories_AddedNoRepos() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_InstallationRepositories,
		Payload:    []byte(`{"action":"added","installation":{"id":9}}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.complete)
}

func (s *WebhookSuite) TestHandleInstallationRepositories_Removed() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_InstallationRepositories,
		Payload: []byte(`{
			"action": "removed",
			"installation": {"id": 9},
			"repositories_removed": [{"full_name": "owner/gone"}]
		}`),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.reset, 1)
	s.Equal([]string{"owner/gone"}, s.recorder.reset[0].Repos)
	s.Equal("9", s.recorder.reset[0].InstallationId)
	s.False(s.recorder.reset[0].Delete)
}

func (s *WebhookSuite) TestHandleInstallationRepositories_RemovedNoRepos() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_InstallationRepositories,
		Payload:    []byte(`{"action":"removed","installation":{"id":9}}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.reset)
}

func (s *WebhookSuite) TestHandleInstallationRepositories_OtherActionIgnored() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_InstallationRepositories,
		Payload:    []byte(`{"action":"created","installation":{"id":9}}`),
	})

	s.NoError(err)
	s.Empty(s.recorder.complete)
	s.Empty(s.recorder.reset)
}

// ---------------------------------------------------------------------------
// WEventConsumer — pull request comment
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestHandlePullRequestComment() {
	consumer := s.newConsumer()

	tests := []struct {
		name    string
		payload string
	}{
		{name: "nil sender", payload: `{"action":"created"}`},
		{name: "bot login", payload: `{"action":"created","sender":{"login":"workdock-bot"}}`},
		{name: "deleted action", payload: `{"action":"deleted","sender":{"login":"alice"}}`},
		{name: "nil pull request", payload: `{"action":"created","sender":{"login":"alice"}}`},
		{name: "nil installation", payload: `{"action":"created","sender":{"login":"alice"},"pull_request":{"head":{"ref":"main"}}}`},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
				WEventType: WEventType_PullRequestReviewComment,
				Payload:    []byte(tt.payload),
			})

			s.NoError(err)
		})
	}

	s.Empty(s.recorder.comment)
}

func (s *WebhookSuite) TestHandlePullRequestComment_Success() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_PullRequestReviewComment,
		Payload: []byte(`{
			"action": "created",
			"sender": {"login": "alice"},
			"installation": {"id": 11},
			"pull_request": {
				"head": {"ref": "feature", "repo": {"full_name": "owner/repo"}}
			}
		}`),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.comment, 1)

	event := s.recorder.comment[0]
	s.Equal(shared.PlatformProvider_GitHub, event.Provider)
	s.Equal("feature", event.GitRef)
	s.Equal("owner/repo", event.RepoFullName)
	s.Equal("11", event.InstallationId)
}

// ---------------------------------------------------------------------------
// WEventConsumer — check_suite
// ---------------------------------------------------------------------------

func (s *WebhookSuite) TestHandleCheckSuite() {
	consumer := s.newConsumer()

	tests := []struct {
		name    string
		payload string
	}{
		{name: "nil check suite", payload: `{"action":"completed"}`},
		{name: "nil sender", payload: `{"action":"completed","check_suite":{"conclusion":"failure"}}`},
		{name: "bot login", payload: `{"action":"completed","sender":{"login":"workdock-bot"},"check_suite":{"conclusion":"failure"}}`},
		{name: "nil conclusion", payload: `{"action":"completed","sender":{"login":"alice"},"check_suite":{}}`},
		{name: "not completed", payload: `{"action":"requested","sender":{"login":"alice"},"check_suite":{"conclusion":"failure"}}`},
		{name: "passing conclusion", payload: `{"action":"completed","sender":{"login":"alice"},"check_suite":{"conclusion":"success"}}`},
		{name: "nil installation", payload: `{"action":"completed","sender":{"login":"alice"},"check_suite":{"conclusion":"failure"}}`},
		{name: "no pull requests", payload: `{"action":"completed","sender":{"login":"alice"},"installation":{"id":5},"check_suite":{"conclusion":"failure"}}`},
		{name: "nil repository", payload: `{"action":"completed","sender":{"login":"alice"},"installation":{"id":5},"check_suite":{"conclusion":"failure","pull_requests":[{"head":{"ref":"r1"},"url":"https://github.com/pull/1"}]}}`},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := consumer.Consume(context.Background(), &webhook.VerifiedWEvent{
				WEventType: WEventType_CheckSuite,
				Payload:    []byte(tt.payload),
			})

			s.NoError(err)
		})
	}

	s.Empty(s.recorder.checks)
}

func (s *WebhookSuite) TestHandleCheckSuite_Success() {
	err := s.newConsumer().Consume(context.Background(), &webhook.VerifiedWEvent{
		WEventType: WEventType_CheckSuite,
		Payload: []byte(`{
			"action": "completed",
			"sender": {"login": "alice"},
			"installation": {"id": 5},
			"repository": {"full_name": "owner/repo"},
			"check_suite": {
				"conclusion": "failure",
				"pull_requests": [
					{"head": {"ref": "suite-1", "repo": {"full_name": "owner/repo"}}, "url": "https://github.com/pull/10"},
					{"head": {"ref": "suite-2", "repo": {"full_name": "owner/repo"}}, "url": "https://github.com/pull/11"}
				]
			}
		}`),
	})

	s.Require().NoError(err)
	s.Require().Len(s.recorder.checks, 2)

	for i, expected := range []struct{ ref, url string }{
		{"suite-1", "https://github.com/pull/10"},
		{"suite-2", "https://github.com/pull/11"},
	} {
		event := s.recorder.checks[i]
		s.Equal(shared.PlatformProvider_GitHub, event.Provider)
		s.Equal(expected.ref, event.GitRef)
		s.Equal("owner/repo", event.RepoFullName)
		s.Equal("5", event.InstallationId)
		s.Equal([]string{expected.url}, event.ChecksFailed)
	}
}

// compile-time interface checks
var (
	_ interfaces.Client      = (*mockClient)(nil)
	_ shared.SecretManager   = (*mockSecretManager)(nil)
	_ webhook.WEventConsumer = (*WEventConsumer)(nil)
)
