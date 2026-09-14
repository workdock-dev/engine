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
	"regexp"
	"strings"
	"testing"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/stretchr/testify/suite"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/plug-ins/daytona/types"
)

type SandboxSuite struct {
	suite.Suite
}

func TestSandboxSuite(t *testing.T) {
	suite.Run(t, new(SandboxSuite))
}

func (s *SandboxSuite) TestNewSandboxHandler() {
	handler := NewSandboxHandler(types.Config{Target: "eu", ApiKey: "key", ApiUrl: "https://api"})

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
// isUnstartableSandboxState
// ---------------------------------------------------------------------------

func (s *SandboxSuite) TestIsUnstartableSandboxState() {
	tests := []struct {
		name  string
		state daytona.SandboxState
		want  bool
	}{
		{name: "error", state: daytona.SandboxStateError, want: true},
		{name: "build failed", state: daytona.SandboxStateBuildFailed, want: true},
		{name: "stopped", state: daytona.SandboxStateStopped, want: false},
		{name: "stopping", state: daytona.SandboxStateStopping, want: false},
		{name: "started", state: daytona.SandboxStateStarted, want: false},
		{name: "creating", state: daytona.SandboxStateCreating, want: false},
		{name: "starting", state: daytona.SandboxStateStarting, want: false},
		{name: "archived", state: daytona.SandboxStateArchived, want: false},
		{name: "destroyed", state: daytona.SandboxStateDestroyed, want: false},
		{name: "empty", state: "", want: false},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, isUnstartableSandboxState(tt.state))
		})
	}
}

// ---------------------------------------------------------------------------
// startError
// ---------------------------------------------------------------------------

func (s *SandboxSuite) TestStartError() {
	startFailed := errors.New("start failed")

	tests := []struct {
		name        string
		state       daytona.SandboxState
		err         error
		unstartable bool
	}{
		{name: "error state", state: daytona.SandboxStateError, err: startFailed, unstartable: true},
		{name: "build failed state", state: daytona.SandboxStateBuildFailed, err: startFailed, unstartable: true},
		{name: "stopped state", state: daytona.SandboxStateStopped, err: startFailed, unstartable: false},
		{name: "starting state", state: daytona.SandboxStateStarting, err: startFailed, unstartable: false},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			wrapped := startError(tt.err, tt.state)

			if !tt.unstartable {
				s.Equal(tt.err, wrapped, "a state that can start must keep the original error")
				return
			}

			s.ErrorIs(wrapped, agent_session_interfaces.ErrSandboxCannotStart, "an unstartable state must wrap the sandbox cannot start sentinel")
			s.ErrorIs(wrapped, tt.err, "the original error must still be matchable")
			s.ErrorContains(wrapped, string(tt.state))
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
