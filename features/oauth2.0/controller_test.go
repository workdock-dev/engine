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

package oauth20

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/workdock-dev/engine/shared"
)

const (
	testProvider   = "linear"
	testAuthURL    = "https://linear.app/oauth/authorize?client_id=test-client"
	testEntityId   = "org-123"
	testEntityName = "Acme"
	testMessage    = "OAuth completed for Acme"
)

type ControllerSuite struct {
	suite.Suite
	mux           *http.ServeMux
	handler       *mockOauthHandler
	secretManager *mockSecretManager
	eventBus      *shared.EventBus
}

func TestControllerSuite(t *testing.T) {
	suite.Run(t, new(ControllerSuite))
}

func (s *ControllerSuite) SetupTest() {
	s.mux = http.NewServeMux()
	s.handler = &mockOauthHandler{}
	s.secretManager = &mockSecretManager{}
	s.eventBus = shared.NewEventBus()
}

func (s *ControllerSuite) newController() {
	New(testProvider, s.mux, s.handler, s.secretManager, s.eventBus)
}

func (s *ControllerSuite) newCallbackRequest(code, errCode string) *http.Request {
	s.T().Helper()

	params := url.Values{}
	if code != "" {
		params.Set("code", code)
	}
	if errCode != "" {
		params.Set("error", errCode)
	}

	target := fmt.Sprintf("/%s/oauth/callback?%s", testProvider, params.Encode())
	r, err := http.NewRequest(http.MethodGet, target, nil)
	s.Require().NoError(err)
	return r
}

func (s *ControllerSuite) publishedEvents() *[]shared.DomainEvent {
	s.T().Helper()

	events := &[]shared.DomainEvent{}
	s.eventBus.Subscribe(shared.EventType_OrganizationCreate, func(ctx context.Context, event shared.DomainEvent) error {
		*events = append(*events, event)
		return nil
	})
	return events
}

func (s *ControllerSuite) TestNew_RegistersRoutes() {
	s.newController()

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%s/oauth/authorize", testProvider), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	s.Equal(http.StatusFound, w.Code, "authorize route should be registered")

	r = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%s/oauth/callback", testProvider), nil)
	w = httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	s.NotEqual(http.StatusNotFound, w.Code, "callback route should be registered")
}

func (s *ControllerSuite) TestAuthorizeRoute_Redirects() {
	s.handler.getAuthorizationURLFn = func() string {
		return testAuthURL
	}
	s.newController()

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%s/oauth/authorize", testProvider), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)

	s.Require().Equal(http.StatusFound, w.Code)
	s.Equal(testAuthURL, w.Header().Get("Location"))
	s.Equal(1, s.handler.getAuthorizationURLCalled)
}

func (s *ControllerSuite) TestCallbackRoute_Success() {
	s.handler.callbackFn = func(ctx context.Context, code, errCode string) (*CallbackResult, error) {
		return &CallbackResult{
			EntityId:   testEntityId,
			EntityName: testEntityName,
			Message:    testMessage,
		}, nil
	}
	s.newController()

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%s/oauth/callback?code=abc", testProvider), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)

	s.Require().Equal(http.StatusOK, w.Code)
	s.Equal("text/html", w.Header().Get("Content-Type"))
	s.Equal(fmt.Sprintf(HTMLResponse, testMessage), w.Body.String())
}

func (s *ControllerSuite) TestCallbackRoute_HandlerError() {
	s.handler.callbackFn = func(ctx context.Context, code, errCode string) (*CallbackResult, error) {
		return nil, errors.New("invalid code")
	}
	s.newController()

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%s/oauth/callback?code=abc", testProvider), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)

	s.Equal(http.StatusInternalServerError, w.Code)
	s.Empty(w.Body.String())
}

func (s *ControllerSuite) TestAuthorize_ReturnsHandlerURL() {
	s.handler.getAuthorizationURLFn = func() string {
		return testAuthURL
	}
	c := &controller{
		provider:      testProvider,
		handler:       s.handler,
		secretManager: s.secretManager,
		eventBus:      s.eventBus,
	}

	got := c.authorize()

	s.Equal(testAuthURL, got)
	s.Equal(1, s.handler.getAuthorizationURLCalled)
}

func (s *ControllerSuite) TestCallback_Success() {
	token := Token{
		AccessToken:  "at-123",
		RefreshToken: "rt-123",
		ExpiresAt:    time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	s.handler.callbackFn = func(ctx context.Context, code, errCode string) (*CallbackResult, error) {
		return &CallbackResult{
			EntityId:   testEntityId,
			EntityName: testEntityName,
			Message:    testMessage,
			Token:      token,
		}, nil
	}
	events := s.publishedEvents()
	c := &controller{
		provider:      testProvider,
		handler:       s.handler,
		secretManager: s.secretManager,
		eventBus:      s.eventBus,
	}
	r := s.newCallbackRequest("abc", "")

	message, err := c.callback(r)

	s.Require().NoError(err)
	s.Equal(testMessage, message)

	s.Require().Equal(1, s.handler.callbackCalled)
	s.Equal(r.Context(), s.handler.callbackCtx)
	s.Equal("abc", s.handler.callbackCode)
	s.Empty(s.handler.callbackErrCode)

	s.Require().Equal(1, s.secretManager.setCalled)
	s.Equal(r.Context(), s.secretManager.setCtx)
	s.Equal(fmt.Sprintf("%s/oauth", testProvider), s.secretManager.setPath)
	s.Equal(testEntityId, s.secretManager.setName)

	expected, err := json.Marshal(token)
	s.Require().NoError(err)
	s.Equal(string(expected), s.secretManager.setValue)

	s.Require().Len(*events, 1)
	s.IsType(shared.OrganizationCreateEvent{}, (*events)[0])
	published := (*events)[0].(shared.OrganizationCreateEvent)
	s.Equal(testEntityId, published.Organization.Identifier)
	s.Equal(shared.PlatformProvider(testProvider), published.Organization.Provider)
	s.Equal(testEntityName, published.Organization.Name)
}

func (s *ControllerSuite) TestCallback_HandlerError() {
	callbackErr := errors.New("invalid code")
	s.handler.callbackFn = func(ctx context.Context, code, errCode string) (*CallbackResult, error) {
		return nil, callbackErr
	}
	events := s.publishedEvents()
	c := &controller{
		provider:      testProvider,
		handler:       s.handler,
		secretManager: s.secretManager,
		eventBus:      s.eventBus,
	}
	r := s.newCallbackRequest("", "access_denied")

	message, err := c.callback(r)

	s.Require().ErrorIs(err, callbackErr)
	s.Empty(message)
	s.Equal(1, s.handler.callbackCalled)
	s.Equal("access_denied", s.handler.callbackErrCode)
	s.Equal(0, s.secretManager.setCalled, "secret should not be stored when the handler fails")
	s.Empty(events, "no event should be published when the handler fails")
}

func (s *ControllerSuite) TestCallback_TokenMarshalError() {
	// time.Date(10000, ...) makes time.Time.MarshalJSON fail since the year
	// is outside of the representable range [0, 9999].
	s.handler.callbackFn = func(ctx context.Context, code, errCode string) (*CallbackResult, error) {
		return &CallbackResult{
			EntityId: testEntityId,
			Message:  testMessage,
			Token: Token{
				AccessToken: "at-123",
				ExpiresAt:   time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}, nil
	}
	events := s.publishedEvents()
	c := &controller{
		provider:      testProvider,
		handler:       s.handler,
		secretManager: s.secretManager,
		eventBus:      s.eventBus,
	}
	r := s.newCallbackRequest("abc", "")

	message, err := c.callback(r)

	s.Require().Error(err)
	s.Empty(message)
	s.ErrorContains(err, "year outside of range")
	s.Equal(1, s.handler.callbackCalled)
	s.Equal(0, s.secretManager.setCalled, "secret should not be stored when token marshaling fails")
	s.Empty(events, "no event should be published when token marshaling fails")
}

func (s *ControllerSuite) TestCallback_SecretSetError() {
	setErr := errors.New("vault unavailable")
	s.secretManager.setFn = func(ctx context.Context, secretPath, secretName, secretValue string) error {
		return setErr
	}
	s.handler.callbackFn = func(ctx context.Context, code, errCode string) (*CallbackResult, error) {
		return &CallbackResult{
			EntityId:   testEntityId,
			EntityName: testEntityName,
			Message:    testMessage,
		}, nil
	}
	events := s.publishedEvents()
	c := &controller{
		provider:      testProvider,
		handler:       s.handler,
		secretManager: s.secretManager,
		eventBus:      s.eventBus,
	}
	r := s.newCallbackRequest("abc", "")

	message, err := c.callback(r)

	s.Require().ErrorIs(err, setErr)
	s.Empty(message)
	s.Equal(1, s.handler.callbackCalled)
	s.Equal(1, s.secretManager.setCalled)
	s.Empty(events, "no event should be published when storing the secret fails")
}