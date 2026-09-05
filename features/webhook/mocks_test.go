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

package webhook

import (
	"context"
	"net/http"
)

type mockTransformer struct {
	transformFn func(ctx context.Context, r *http.Request) (*WEvent, error)

	transformCalled int
	transformCtx    context.Context
	transformReq    *http.Request
}

func (m *mockTransformer) Transform(ctx context.Context, r *http.Request) (*WEvent, error) {
	m.transformCalled++
	m.transformCtx = ctx
	m.transformReq = r
	if m.transformFn != nil {
		return m.transformFn(ctx, r)
	}
	return &WEvent{}, nil
}

type mockVerifier struct {
	verifyFn func(ctx context.Context, event *WEvent) (*VerifiedWEvent, error)

	verifyCalled int
	verifyCtx    context.Context
	verifyEvent  *WEvent
}

func (m *mockVerifier) Verify(ctx context.Context, event *WEvent) (*VerifiedWEvent, error) {
	m.verifyCalled++
	m.verifyCtx = ctx
	m.verifyEvent = event
	if m.verifyFn != nil {
		return m.verifyFn(ctx, event)
	}
	return &VerifiedWEvent{}, nil
}

type mockConsumer struct {
	consumeFn func(ctx context.Context, event *VerifiedWEvent) error

	consumeCalled int
	consumeCtx    context.Context
	consumeEvent  *VerifiedWEvent
}

func (m *mockConsumer) Consume(ctx context.Context, event *VerifiedWEvent) error {
	m.consumeCalled++
	m.consumeCtx = ctx
	m.consumeEvent = event
	if m.consumeFn != nil {
		return m.consumeFn(ctx, event)
	}
	return nil
}