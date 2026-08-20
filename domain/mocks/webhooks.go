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

type Webhooks struct {
	mock.Mock
}

func (m *Webhooks) Webhook(ctx context.Context, req types.WebhookRequest) (any, error) {
	args := m.Called(ctx, req)
	return args.Get(0), args.Error(1)
}

// WebhooksWithIssueStatusChanges implements both ForWebhooks and
// ForIssueStatusChanges for testing.
type WebhooksWithIssueStatusChanges struct {
	Webhooks
}

func (m *WebhooksWithIssueStatusChanges) ParseIssueStatusChange(ctx context.Context, req types.WebhookRequest) (*types.IssueStatusChangePayload, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.IssueStatusChangePayload), args.Error(1)
}
