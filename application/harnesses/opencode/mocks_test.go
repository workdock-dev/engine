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

package opencode

import (
	"context"
	"time"

	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
)

type mockSandbox struct {
	mock.Mock
}

func (m *mockSandbox) GetOrCreateSandbox(ctx context.Context, secrets, envVars map[string]string) (bool, error) {
	args := m.Called(ctx, secrets, envVars)
	return args.Bool(0), args.Error(1)
}

func (m *mockSandbox) UpdateExistingSandbox(ctx context.Context, secrets, envVars map[string]string) error {
	args := m.Called(ctx, secrets, envVars)
	return args.Error(0)
}

func (m *mockSandbox) SetSecret(ctx context.Context, secretValue string, hosts []string) (string, string, error) {
	args := m.Called(ctx, secretValue, hosts)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *mockSandbox) DeleteSecret(ctx context.Context, secretId string) error {
	args := m.Called(ctx, secretId)
	return args.Error(0)
}

func (m *mockSandbox) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockSandbox) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockSandbox) UploadFile(ctx context.Context, data []byte, path string) error {
	args := m.Called(ctx, data, path)
	return args.Error(0)
}

func (m *mockSandbox) UpdateEnv(ctx context.Context, envVars map[string]string) error {
	args := m.Called(ctx, envVars)
	return args.Error(0)
}

func (m *mockSandbox) ConfigureGitUser(ctx context.Context, name, email string) error {
	args := m.Called(ctx, name, email)
	return args.Error(0)
}

func (m *mockSandbox) ExecuteCommand(ctx context.Context, command string, timeout time.Duration) (string, error) {
	args := m.Called(ctx, command, timeout)
	return args.String(0), args.Error(1)
}

func (m *mockSandbox) CreateExecutionSession(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockSandbox) DeleteExecutionSession(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockSandbox) ExecuteSessionCommand(ctx context.Context, command string) (map[string]any, error) {
	args := m.Called(ctx, command)
	return args.Get(0).(map[string]any), args.Error(1)
}

func (m *mockSandbox) StreamSessionCommandLogs(ctx context.Context, cmdId string, stdout chan<- string, stderr chan<- string) error {
	args := m.Called(ctx, cmdId, stdout, stderr)
	return args.Error(0)
}

func (m *mockSandbox) DeleteSandbox(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockSandbox) Archive(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type mockParts struct {
	mock.Mock
}

func (m *mockParts) Thought(ctx context.Context, text string) {
	m.Called(ctx, text)
}

func (m *mockParts) Response(ctx context.Context, text string) {
	m.Called(ctx, text)
}

func (m *mockParts) Action(ctx context.Context, action types.AgentAction) {
	m.Called(ctx, action)
}

func (m *mockParts) Elicitation(ctx context.Context, elicitation types.AgentElicitation) {
	m.Called(ctx, elicitation)
}

func defaultSession() *types.Session {
	return &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "session-1",
		Provider:               "linear",
	}
}

func defaultSessionEvent() *types.SessionEvent {
	return &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "event-1",
	}
}
