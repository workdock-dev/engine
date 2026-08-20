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

package mocks

import (
	"context"

	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
)

type SessionRepository struct {
	mock.Mock
}

func (m *SessionRepository) GetAgentSession(ctx context.Context, identifier string) (*types.Session, error) {
	args := m.Called(ctx, identifier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Session), args.Error(1)
}

func (m *SessionRepository) GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*types.Session, error) {
	args := m.Called(ctx, issueId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*types.Session), args.Error(1)
}

func (m *SessionRepository) GetAgentSessionEvent(ctx context.Context, identifier string) (*types.SessionEvent, error) {
	args := m.Called(ctx, identifier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.SessionEvent), args.Error(1)
}

func (m *SessionRepository) GetAgentSessionEventByGitRef(ctx context.Context, identifier string, repoFullName string) (*types.SessionEvent, error) {
	args := m.Called(ctx, identifier, repoFullName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.SessionEvent), args.Error(1)
}

func (m *SessionRepository) CreateSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *SessionRepository) UpsertAgentSession(ctx context.Context, session *types.Session) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *SessionRepository) UpdateSessionEventResult(ctx context.Context, event *types.SessionEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *SessionRepository) CancelSession(ctx context.Context, queuedBy, reason string) (int, error) {
	args := m.Called(ctx, queuedBy, reason)
	return args.Int(0), args.Error(1)
}
