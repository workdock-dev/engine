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
	"errors"
	"sync"
	"testing"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/stretchr/testify/suite"
)

type event struct {
	eventType string
}

func (e event) EventType() string { return e.eventType }

type EventBusSuite struct {
	suite.Suite
	bus *InMemoryEventBus
}

func TestEventBusSuite(t *testing.T) {
	suite.Run(t, new(EventBusSuite))
}

func (s *EventBusSuite) SetupTest() {
	s.bus = NewInMemoryEventBus()
}

func (s *EventBusSuite) TestNewInMemoryEventBus_InitializesEmpty() {
	bus := NewInMemoryEventBus()
	s.NotNil(bus)
	s.Empty(bus.handlers)
}

func (s *EventBusSuite) TestSubscribe_RegistersHandler() {
	called := false
	s.bus.Subscribe("test.event", func(ctx context.Context, e ports.DomainEvent) error {
		called = true
		return nil
	})

	s.Len(s.bus.handlers["test.event"], 1)

	err := s.bus.Publish(context.Background(), event{eventType: "test.event"})
	s.NoError(err)
	s.True(called)
}

func (s *EventBusSuite) TestSubscribe_MultipleHandlersSameType() {
	var order []int

	s.bus.Subscribe("test.event", func(ctx context.Context, e ports.DomainEvent) error {
		order = append(order, 1)
		return nil
	})
	s.bus.Subscribe("test.event", func(ctx context.Context, e ports.DomainEvent) error {
		order = append(order, 2)
		return nil
	})
	s.bus.Subscribe("test.event", func(ctx context.Context, e ports.DomainEvent) error {
		order = append(order, 3)
		return nil
	})

	s.Len(s.bus.handlers["test.event"], 3)

	err := s.bus.Publish(context.Background(), event{eventType: "test.event"})
	s.NoError(err)
	s.Equal([]int{1, 2, 3}, order)
}

func (s *EventBusSuite) TestPublish_InvokesSubscribedHandler() {
	var received ports.DomainEvent
	e := event{eventType: "msg"}

	s.bus.Subscribe("msg", func(ctx context.Context, evt ports.DomainEvent) error {
		received = evt
		return nil
	})

	err := s.bus.Publish(context.Background(), e)
	s.NoError(err)
	s.Equal(e, received)
}

func (s *EventBusSuite) TestPublish_NoSubscribers() {
	err := s.bus.Publish(context.Background(), event{eventType: "no.subscribers"})
	s.NoError(err)
}

func (s *EventBusSuite) TestPublish_HandlerErrorDoesNotStopOthers() {
	var secondCalled bool
	handlerErr := errors.New("handler failed")

	s.bus.Subscribe("test.event", func(ctx context.Context, e ports.DomainEvent) error {
		return handlerErr
	})
	s.bus.Subscribe("test.event", func(ctx context.Context, e ports.DomainEvent) error {
		secondCalled = true
		return nil
	})

	err := s.bus.Publish(context.Background(), event{eventType: "test.event"})

	s.NoError(err)
	s.True(secondCalled)
}

func (s *EventBusSuite) TestPublish_OnlyMatchingEventTypes() {
	calledA := false
	calledB := false

	s.bus.Subscribe("type.a", func(ctx context.Context, e ports.DomainEvent) error {
		calledA = true
		return nil
	})
	s.bus.Subscribe("type.b", func(ctx context.Context, e ports.DomainEvent) error {
		calledB = true
		return nil
	})

	err := s.bus.Publish(context.Background(), event{eventType: "type.a"})
	s.NoError(err)
	s.True(calledA)
	s.False(calledB)
}

func (s *EventBusSuite) TestPublish_MultipleHandlersOrdering() {
	var order []string
	var mu sync.Mutex

	s.bus.Subscribe("e", func(ctx context.Context, e ports.DomainEvent) error {
		mu.Lock()
		order = append(order, "first")
		mu.Unlock()
		return nil
	})
	s.bus.Subscribe("e", func(ctx context.Context, e ports.DomainEvent) error {
		mu.Lock()
		order = append(order, "second")
		mu.Unlock()
		return nil
	})

	err := s.bus.Publish(context.Background(), event{eventType: "e"})
	s.NoError(err)
	s.Equal([]string{"first", "second"}, order)
}
