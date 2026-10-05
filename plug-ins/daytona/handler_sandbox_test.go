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
	handler, err := NewSandboxHandler(types.Config{Target: "eu", ApiKey: "key", ApiUrl: "https://api"})
	s.Require().NoError(err)

	var iface agent_session_interfaces.HandlerSandbox = handler
	s.NotNil(iface)
	s.IsType(&SandboxHandler{}, iface)
	s.Equal(types.Config{Target: "eu", ApiKey: "key", ApiUrl: "https://api"}, handler.config)
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

func (s *SandboxSuite) TestGitURLValidation() {
	handler := &SandboxHandler{}
	s.NoError(handler.validateGitURL("https://github.com/workdock-dev/engine.git", "session"))

	for _, rawURL := range []string{
		"http://github.com/workdock-dev/engine.git",
		"https://example.com/workdock-dev/engine.git",
		"https://github.com.attacker.example/workdock-dev/engine.git",
		"https://credential@github.com/workdock-dev/engine.git",
		"https://github.com/workdock-dev/engine.git?token=secret",
		"https://github.com/workdock-dev/engine.git#secret",
		"https://github.com:443/workdock-dev/engine.git",
		"https://github.com",
		"git@github.com:workdock-dev/engine.git",
		"https://%invalid",
	} {
		s.Error(handler.validateGitURL(rawURL, "session"), rawURL)
	}
}

func (s *SandboxSuite) TestCloneRejectsUntrustedURLBeforeAccessingClient() {
	handler := &SandboxHandler{}

	err := handler.GitClone(context.Background(), agent_session_interfaces.GitCloneInput{
		SessionId:   "session",
		AccessToken: "private",
		Url:         "https://attacker.example/repo.git",
		Path:        "/workspace",
	})

	s.ErrorContains(err, "GitHub installation authentication is restricted to github.com remotes")
}

func (s *SandboxSuite) TestCloseSharedClient() {
	client := &daytona.Client{}
	handler := &SandboxHandler{client: client}

	s.NoError(handler.Close(context.Background()))
	s.Same(client, handler.client)
}
