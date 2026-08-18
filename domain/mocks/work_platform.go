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

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
)

type WorkPlatform struct {
	mock.Mock
}

func (m *WorkPlatform) BeginOAuth(ctx context.Context) string {
	args := m.Called(ctx)
	return args.String(0)
}

func (m *WorkPlatform) CompleteOAuth(ctx context.Context, code, errorP string) (string, error) {
	args := m.Called(ctx, code, errorP)
	return args.String(0), args.Error(1)
}

func (m *WorkPlatform) Ingest(event any, seed *string, from *types.SessionEvent) (*types.Session, *types.SessionEvent, error) {
	args := m.Called(event, seed, from)
	var sess *types.Session
	var sessEvt *types.SessionEvent
	if v := args.Get(0); v != nil {
		sess = v.(*types.Session)
	}
	if v := args.Get(1); v != nil {
		sessEvt = v.(*types.SessionEvent)
	}
	return sess, sessEvt, args.Error(2)
}

func (m *WorkPlatform) Process(ctx context.Context, config ports.ProcessConfig) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *WorkPlatform) Cancel(ctx context.Context, session *types.Session) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *WorkPlatform) IsCancelSignal(ctx context.Context, any any) (bool, error) {
	args := m.Called(ctx, any)
	return args.Bool(0), args.Error(1)
}

func (m *WorkPlatform) Webhook(ctx context.Context, req types.WebhookRequest) (any, error) {
	args := m.Called(ctx, req)
	return args.Get(0), args.Error(1)
}
