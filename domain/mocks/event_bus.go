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

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/stretchr/testify/mock"
)

type EventBus struct {
	mock.Mock
	Handlers map[string]ports.EventHandler
}

func NewEventBus() *EventBus {
	return &EventBus{
		Handlers: make(map[string]ports.EventHandler),
	}
}

func (m *EventBus) Publish(ctx context.Context, event ports.DomainEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *EventBus) Subscribe(eventType string, handler ports.EventHandler) {
	m.Handlers[eventType] = handler
	m.Called(eventType, handler)
}

func (m *EventBus) Invoke(ctx context.Context, eventType string, event ports.DomainEvent) error {
	handler, ok := m.Handlers[eventType]
	if !ok {
		panic("no handler registered for event type: " + eventType)
	}
	return handler(ctx, event)
}
