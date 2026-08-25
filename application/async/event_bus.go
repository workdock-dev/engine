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

package async

import (
	"context"
	"log/slog"
	"sync"

	"github.com/workdock-dev/engine/domain/ports"
)

// InMemoryEventBus is the application-layer implementation of ports.ForEventBus.
// It delivers events synchronously to every handler subscribed to the event's
// type, in subscription order.
//
// A failing handler does not stop the delivery of the event to the remaining
// handlers: the error is logged and delivery continues.
type InMemoryEventBus struct {
	mu       sync.RWMutex
	handlers map[string][]ports.EventHandler
}

func NewInMemoryEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{
		handlers: make(map[string][]ports.EventHandler),
	}
}

// Subscribe registers a handler for the given event type.
func (b *InMemoryEventBus) Subscribe(eventType string, handler ports.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish synchronously invokes every handler subscribed to the event's type.
func (b *InMemoryEventBus) Publish(ctx context.Context, event ports.DomainEvent) error {
	b.mu.RLock()
	handlers := make([]ports.EventHandler, len(b.handlers[event.EventType()]))
	copy(handlers, b.handlers[event.EventType()])
	b.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			slog.Error("event handler failed", "event_type", event.EventType(), "err", err)
		}
	}

	return nil
}
