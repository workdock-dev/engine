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

package domain_service

import (
	"context"
	"log/slog"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/types"
)

type WebhookServiceConfig struct {
	WebhooksRegistry ports.WebhooksRegistry
	ForEventBus      ports.ForEventBus
}

type WebhookService struct {
	config WebhookServiceConfig
}

func NewWebhookService(config WebhookServiceConfig) *WebhookService {
	return &WebhookService{
		config: config,
	}
}

func (s *WebhookService) On(ctx context.Context, name types.PlatformProvider, req types.WebhookRequest) error {
	slog.Debug("Webhook event received", "from", name)
	workPlatform, err := s.platform(name)

	if err != nil {
		return err
	}

	event, err := workPlatform.Webhook(ctx, req)

	if err != nil {
		return err
	}

	s.config.ForEventBus.Publish(context.Background(), types.WebhookEvent{
		Provider: name,
		Payload:  event,
	})

	slog.Debug("Webhook event accepted", "from", name, "event", event)
	return nil
}

func (s *WebhookService) platform(name types.PlatformProvider) (ports.ForWebhooks, error) {
	registry, ok := s.config.WebhooksRegistry[name]

	if !ok {
		slog.Error("failed to load platform from registry", "name", name)
		return nil, types.ErrInternalServerError
	}

	return registry, nil
}
