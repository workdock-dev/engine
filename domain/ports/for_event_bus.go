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

package ports

import "context"

// DomainEvent is the base interface every domain event must implement. The
// event type is used by the event bus to route events to their subscribers.
type DomainEvent interface {
	EventType() string
}

// EventHandler processes a published domain event.
type EventHandler func(ctx context.Context, event DomainEvent) error

// ForEventBus is the port used by domain services to publish domain events and
// by the application layer to register subscribers. The concrete implementation
// lives in the application layer.
type ForEventBus interface {
	Publish(ctx context.Context, event DomainEvent) error
	Subscribe(eventType string, handler EventHandler)
}
