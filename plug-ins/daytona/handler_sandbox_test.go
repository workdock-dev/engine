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

package daytona

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/daytona/types"
)

type SandboxSuite struct {
	suite.Suite
}

func TestSandboxSuite(t *testing.T) {
	suite.Run(t, new(SandboxSuite))
}

func (s *SandboxSuite) TestNewSandboxHandler() {
	handler := NewSandboxHandler(types.Config{Target: "eu", ApiKey: "key", ApiUrl: "https://api"}, http.NewServeMux())

	var iface agent_session_interfaces.HandlerSandbox = handler
	s.NotNil(iface)
	s.IsType(&SandboxHandler{}, iface)
	s.Equal(types.Config{Target: "eu", ApiKey: "key", ApiUrl: "https://api"}, handler.(*SandboxHandler).config)
}

// ---------------------------------------------------------------------------
// isContextCanceledOrDeadlineExceeded
// ---------------------------------------------------------------------------

func (s *SandboxSuite) TestIsContextCanceledOrDeadlineExceeded() {
	handler := &SandboxHandler{}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "canceled", err: context.Canceled, want: true},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped canceled", err: fmt.Errorf("call failed: %w", context.Canceled), want: true},
		{name: "wrapped deadline", err: fmt.Errorf("call failed: %w", context.DeadlineExceeded), want: true},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, handler.isContextCanceledOrDeadlineExceeded(tt.err))
		})
	}
}

// ---------------------------------------------------------------------------
// newUUIDStartingWithLetter
// ---------------------------------------------------------------------------

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *SandboxSuite) TestNewUUIDStartingWithLetter() {
	handler := &SandboxHandler{}

	seen := make(map[string]bool)

	for range 100 {
		name := handler.newUUIDStartingWithLetter()

		s.Len(name, 36, "must be a 36-char uuid")
		s.Regexp(uuidPattern, name)
		s.True(name[0] >= 'a' && name[0] <= 'f', "must start with a hex letter, got %q", name)

		s.False(seen[name], "uuid must be unique: %s", name)
		seen[name] = true
	}
}

func (s *SandboxSuite) TestNewUUIDStartingWithLetter_FormattedLikeProductionUsage() {
	// The secret name is sent to Daytona as-is; it must not contain
	// characters outside the canonical uuid shape.
	handler := &SandboxHandler{}

	for range 10 {
		name := handler.newUUIDStartingWithLetter()
		s.False(strings.ContainsAny(name, "{}: "), "no braces, colons or spaces allowed: %s", name)
	}
}

func (s *SandboxSuite) TestConstructorConfiguresAndRegistersGitMCP() {
	mux := http.NewServeMux()
	stored := map[string]string{"session": "token"}
	handler := NewSandboxHandler(types.Config{MCPApiKey: "api-key"}, mux).(*SandboxHandler)
	handler.ConfigureMCP(&agent_session_interfaces.SandboxMCPConfig{
		TokenLookup: func(_ context.Context, id string) (string, error) {
			return stored[id], nil
		},
		GitLookup: func(context.Context, string) (agent_session_interfaces.HandlerGit, *agent_session_types.GitConnection, error) {
			return nil, nil, nil
		},
	})
	s.Require().NotNil(handler.mcp)
	invocations := 0
	handler.mcp.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := handler.mcp.authorized(r.Context(), types.AgentSession{Id: "session", Token: "token"})

		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		invocations++
		w.WriteHeader(http.StatusNoContent)
	})

	for _, test := range []struct {
		name   string
		header string
		status int
	}{
		{name: "missing API key", status: http.StatusUnauthorized},
		{name: "invalid API key", header: "Bearer invalid", status: http.StatusUnauthorized},
		{name: "valid execution", header: "Bearer api-key", status: http.StatusNoContent},
	} {
		s.Run(test.name, func() {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/git", nil)
			request.Header.Set("Authorization", test.header)
			response := httptest.NewRecorder()

			mux.ServeHTTP(response, request)

			s.Equal(test.status, response.Code)
		})
	}

	s.Equal(1, invocations)
	delete(stored, "session")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/git", nil)
	request.Header.Set("Authorization", "Bearer api-key")
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	s.Equal(http.StatusForbidden, response.Code)
	s.Equal(1, invocations)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/mcp/daytona", nil))
	s.Equal(http.StatusNotFound, response.Code)
}
