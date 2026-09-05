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

package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type WEventSuite struct {
	suite.Suite
}

func TestWEventSuite(t *testing.T) {
	suite.Run(t, new(WEventSuite))
}

func (s *WEventSuite) TestGet_ReturnsFirstValue() {
	wevent := WEvent{Headers: map[string][]string{
		"X-Github-Event": {"push", "ping"},
	}}

	s.Equal("push", wevent.Get("X-GitHub-Event"))
}

func (s *WEventSuite) TestGet_IsCaseInsensitive() {
	wevent := WEvent{Headers: map[string][]string{
		"Content-Type": {"application/json"},
	}}

	s.Equal("application/json", wevent.Get("content-type"))
	s.Equal("application/json", wevent.Get("CONTENT-TYPE"))
}

func (s *WEventSuite) TestGet_MissingHeader_ReturnsEmpty() {
	wevent := WEvent{Headers: map[string][]string{}}

	s.Empty(wevent.Get("X-Missing"))
}

func (s *WEventSuite) TestGet_EmptyValues_ReturnsEmpty() {
	wevent := WEvent{Headers: map[string][]string{
		"X-Empty": {},
	}}

	s.Empty(wevent.Get("X-Empty"))
}

type ControllerSuite struct {
	suite.Suite
	mux         *http.ServeMux
	transformer *mockTransformer
	verifier    *mockVerifier
	consumer    *mockConsumer
}

func TestControllerSuite(t *testing.T) {
	suite.Run(t, new(ControllerSuite))
}

func (s *ControllerSuite) SetupTest() {
	s.mux = http.NewServeMux()
	s.transformer = &mockTransformer{}
	s.verifier = &mockVerifier{}
	s.consumer = &mockConsumer{}
}

func (s *ControllerSuite) newController() {
	New("/linear/webhook", s.mux, s.transformer, s.verifier, s.consumer)
}

func (s *ControllerSuite) newRequest() *http.Request {
	s.T().Helper()

	r, err := http.NewRequest(http.MethodPost, "/linear/webhook", strings.NewReader(`{"event":"test"}`))
	s.Require().NoError(err)
	return r
}

func (s *ControllerSuite) TestNew_RegistersEndpoint() {
	s.newController()

	r := s.newRequest()
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)

	s.Equal(http.StatusAccepted, w.Code)
}

func (s *ControllerSuite) TestExecute_Success() {
	wevent := &WEvent{RemoteAddr: "10.0.0.1:8000"}
	verified := &VerifiedWEvent{WEventType: "issue.updated", DeliveryID: "delivery-1", Payload: []byte(`{"id":1}`)}
	s.transformer.transformFn = func(ctx context.Context, r *http.Request) (*WEvent, error) {
		return wevent, nil
	}
	s.verifier.verifyFn = func(ctx context.Context, event *WEvent) (*VerifiedWEvent, error) {
		return verified, nil
	}
	c := &controller{
		transformer: s.transformer,
		verifier:    s.verifier,
		consumer:    s.consumer,
	}
	r := s.newRequest()

	err := c.execute(r)

	s.Require().NoError(err)

	s.Require().Equal(1, s.transformer.transformCalled)
	s.Equal(r.Context(), s.transformer.transformCtx)
	s.Same(r, s.transformer.transformReq)

	s.Require().Equal(1, s.verifier.verifyCalled)
	s.Equal(r.Context(), s.verifier.verifyCtx)
	s.Same(wevent, s.verifier.verifyEvent)

	s.Require().Equal(1, s.consumer.consumeCalled)
	s.Equal(r.Context(), s.consumer.consumeCtx)
	s.Same(verified, s.consumer.consumeEvent)
}

func (s *ControllerSuite) TestExecute_TransformError() {
	transformErr := errors.New("malformed request")
	s.transformer.transformFn = func(ctx context.Context, r *http.Request) (*WEvent, error) {
		return nil, transformErr
	}
	c := &controller{
		transformer: s.transformer,
		verifier:    s.verifier,
		consumer:    s.consumer,
	}
	r := s.newRequest()

	err := c.execute(r)

	s.Require().ErrorIs(err, transformErr)
	s.Equal(1, s.transformer.transformCalled)
	s.Equal(0, s.verifier.verifyCalled, "verification should not run when transformation fails")
	s.Equal(0, s.consumer.consumeCalled, "consumption should not run when transformation fails")
}

func (s *ControllerSuite) TestExecute_VerifyError() {
	verifyErr := ErrWUnAuthorized
	s.transformer.transformFn = func(ctx context.Context, r *http.Request) (*WEvent, error) {
		return &WEvent{}, nil
	}
	s.verifier.verifyFn = func(ctx context.Context, event *WEvent) (*VerifiedWEvent, error) {
		return nil, verifyErr
	}
	c := &controller{
		transformer: s.transformer,
		verifier:    s.verifier,
		consumer:    s.consumer,
	}
	r := s.newRequest()

	err := c.execute(r)

	s.Require().ErrorIs(err, verifyErr)
	s.Equal(1, s.transformer.transformCalled)
	s.Equal(1, s.verifier.verifyCalled)
	s.Equal(0, s.consumer.consumeCalled, "consumption should not run when verification fails")
}

func (s *ControllerSuite) TestExecute_ConsumeError() {
	consumeErr := ErrWServerInternalError
	s.transformer.transformFn = func(ctx context.Context, r *http.Request) (*WEvent, error) {
		return &WEvent{}, nil
	}
	s.verifier.verifyFn = func(ctx context.Context, event *WEvent) (*VerifiedWEvent, error) {
		return &VerifiedWEvent{}, nil
	}
	s.consumer.consumeFn = func(ctx context.Context, event *VerifiedWEvent) error {
		return consumeErr
	}
	c := &controller{
		transformer: s.transformer,
		verifier:    s.verifier,
		consumer:    s.consumer,
	}
	r := s.newRequest()

	err := c.execute(r)

	s.Require().ErrorIs(err, consumeErr)
	s.Equal(1, s.transformer.transformCalled)
	s.Equal(1, s.verifier.verifyCalled)
	s.Equal(1, s.consumer.consumeCalled)
}

func (s *ControllerSuite) TestRoute_StatusCodeMapping() {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{name: "success", err: nil, expectedStatus: http.StatusAccepted},
		{name: "bad request", err: ErrWBadRequest, expectedStatus: http.StatusBadRequest},
		{name: "unauthorized", err: ErrWUnAuthorized, expectedStatus: http.StatusUnauthorized},
		{name: "forbidden", err: ErrWForBidden, expectedStatus: http.StatusForbidden},
		{name: "internal error", err: ErrWServerInternalError, expectedStatus: http.StatusInternalServerError},
		{name: "unknown error", err: errors.New("unexpected failure"), expectedStatus: http.StatusInternalServerError},
		{name: "wrapped bad request", err: fmt.Errorf("pipeline: %w", ErrWBadRequest), expectedStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			mux := http.NewServeMux()
			transformer := &mockTransformer{}
			verifier := &mockVerifier{}
			consumer := &mockConsumer{}

			if tt.err == nil {
				verifier.verifyFn = func(ctx context.Context, event *WEvent) (*VerifiedWEvent, error) {
					return &VerifiedWEvent{}, nil
				}
			} else {
				transformer.transformFn = func(ctx context.Context, r *http.Request) (*WEvent, error) {
					return nil, tt.err
				}
			}

			New("/linear/webhook", mux, transformer, verifier, consumer)

			r, err := http.NewRequest(http.MethodPost, "/linear/webhook", strings.NewReader(`{"event":"test"}`))
			s.Require().NoError(err)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)

			s.Equal(tt.expectedStatus, w.Code)
		})
	}
}