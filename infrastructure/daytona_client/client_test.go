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

package daytona_client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ClientSuite struct {
	suite.Suite
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

func (s *ClientSuite) newTestSandbox() *Sandbox {
	return &Sandbox{
		sessionId:      "test-session",
		sessionEventId: "test-event",
	}
}

// --- NewSandbox tests ---

func (s *ClientSuite) TestNewSandbox_Success() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	h, err := NewSandbox(SandboxConfig{
		ApiUrl: server.URL,
		ApiKey: "test-key",
	}, "session-1", "event-1")

	s.NoError(err)
	s.NotNil(h)
	s.Equal("session-1", h.sessionId)
	s.Equal("event-1", h.sessionEventId)
}

func (s *ClientSuite) TestNewSandbox_EmptyConfig() {
	_, err := NewSandbox(SandboxConfig{}, "", "")
	s.Error(err)
}

// --- nil-guard tests ---

func (s *ClientSuite) TestStart_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.Start(context.Background())
	s.ErrorIs(err, errSandboxNotInitialized)
}

func (s *ClientSuite) TestShutdown_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.Shutdown(context.Background())
	s.NoError(err)
}

func (s *ClientSuite) TestUploadFile_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.UploadFile(context.Background(), []byte("data"), "/tmp/test.txt")
	s.ErrorIs(err, errSandboxNotInitialized)
}

func (s *ClientSuite) TestUpdateEnv_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.UpdateEnv(context.Background(), map[string]string{"KEY": "val"})
	s.ErrorIs(err, errSandboxNotInitialized)
}

func (s *ClientSuite) TestConfigureGitUser_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.ConfigureGitUser(context.Background(), "name", "email")
	s.ErrorIs(err, errSandboxNotInitialized)
}

func (s *ClientSuite) TestExecuteCommand_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	result, err := sandbox.ExecuteCommand(context.Background(), "ls", time.Minute)
	s.ErrorIs(err, errSandboxNotInitialized)
	s.Empty(result)
}

func (s *ClientSuite) TestCreateExecutionSession_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.CreateExecutionSession(context.Background())
	s.ErrorIs(err, errSandboxNotInitialized)
}

func (s *ClientSuite) TestDeleteExecutionSession_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.DeleteExecutionSession(context.Background())
	s.NoError(err)
}

func (s *ClientSuite) TestExecuteSessionCommand_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	result, err := sandbox.ExecuteSessionCommand(context.Background(), "cmd")
	s.ErrorIs(err, errSandboxNotInitialized)
	s.Nil(result)
}

func (s *ClientSuite) TestStreamSessionCommandLogs_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	stdout := make(chan<- string)
	stderr := make(chan<- string)
	err := sandbox.StreamSessionCommandLogs(context.Background(), "cmd-1", stdout, stderr)
	s.ErrorIs(err, errSandboxNotInitialized)
}

func (s *ClientSuite) TestDeleteSandbox_NilSandbox() {
	sandbox := &Sandbox{sessionId: "s1", sessionEventId: "e1"}
	err := sandbox.DeleteSandbox(context.Background())
	s.NoError(err)
}

func (s *ClientSuite) TestSetSecret_NilClient() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	c, _ := NewSandbox(SandboxConfig{ApiUrl: server.URL, ApiKey: "k"}, "s1", "e1")
	// Remove the sandbox so the SDK calls fail on the server
	c.sandbox = nil
	id, name, err := c.SetSecret(context.Background(), "val", []string{"example.com"})
	s.Error(err)
	s.Empty(id)
	s.Empty(name)
}

func (s *ClientSuite) TestDeleteSecret_NilClient() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	c, _ := NewSandbox(SandboxConfig{ApiUrl: server.URL, ApiKey: "k"}, "s1", "e1")
	c.sandbox = nil
	err := c.DeleteSecret(context.Background(), "secret-id")
	s.Error(err)
}

// --- newUUIDStartingWithLetter tests ---

func (s *ClientSuite) TestNewUUIDStartingWithLetter() {
	sandbox := &Sandbox{}
	for i := range 100 {
		uuid := sandbox.newUUIDStartingWithLetter()
		s.Len(uuid, 36, "iteration %d: UUID should be 36 chars", i)
		first := string(uuid[0])
		s.Contains("abcdef", first, "iteration %d: UUID must start with a-f, got %q", i, first)
	}
}

// --- isContextCanceledOrDeadlineExceeded tests ---

func (s *ClientSuite) TestIsContextCanceledOrDeadlineExceeded_Canceled() {
	sandbox := &Sandbox{}
	s.True(sandbox.isContextCanceledOrDeadlineExceeded(context.Canceled))
}

func (s *ClientSuite) TestIsContextCanceledOrDeadlineExceeded_Deadline() {
	sandbox := &Sandbox{}
	s.True(sandbox.isContextCanceledOrDeadlineExceeded(context.DeadlineExceeded))
}

func (s *ClientSuite) TestIsContextCanceledOrDeadlineExceeded_Other() {
	sandbox := &Sandbox{}
	s.False(sandbox.isContextCanceledOrDeadlineExceeded(errors.New("some error")))
}

func (s *ClientSuite) TestIsContextCanceledOrDeadlineExceeded_Wrapped() {
	sandbox := &Sandbox{}
	wrapped := fmt.Errorf("outer: %w", context.Canceled)
	s.True(sandbox.isContextCanceledOrDeadlineExceeded(wrapped))
}

func (s *ClientSuite) TestIsContextCanceledOrDeadlineExceeded_Nil() {
	sandbox := &Sandbox{}
	s.False(sandbox.isContextCanceledOrDeadlineExceeded(nil))
}
