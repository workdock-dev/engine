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
	"fmt"
	"log/slog"

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
)

type GitServiceConfig struct {
	GitHostingPlatformRegistry ports.GitHostingPlatformRegistry
	ForEvent                   ports.ForEventBus
}

type GitService struct {
	config GitServiceConfig
}

func NewGitService(config GitServiceConfig) *GitService {
	s := &GitService{
		config: config,
	}

	for key := range config.GitHostingPlatformRegistry {
		eventType := types.PlatformWebhookEvent(key)

		slog.Debug("GitService subscribed for event", "event_type", eventType)
		s.config.ForEvent.Subscribe(
			eventType,
			func(ctx context.Context, event ports.DomainEvent) error {
				e, ok := event.(types.WebhookEvent)

				if !ok {
					return fmt.Errorf("expected a github connection event, received %s", event.EventType())
				}

				workPlatform, err := s.platform(e.Provider)

				if err != nil {
					return err
				}

				return workPlatform.Ingest(ctx, e.Payload)
			},
		)
	}

	return s
}

func (s *GitService) platform(name types.PlatformProvider) (ports.ForGitHostingPlatform, error) {
	registry, ok := s.config.GitHostingPlatformRegistry[name]

	if !ok {
		err := fmt.Errorf("failed to load git hosting platform from registry %s", name)
		return nil, err
	}

	return registry, nil
}
