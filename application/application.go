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

package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jazielguerrero/workdock/application/async"
	"github.com/jazielguerrero/workdock/application/interfaces"
	"github.com/jazielguerrero/workdock/application/work_platforms/linear"
	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	domain_service "github.com/jazielguerrero/workdock/domain/service"
	"github.com/jazielguerrero/workdock/domain/types"
)

type Config struct {
	WorkPlatformRegistry       ports.WorkPlatformRegistry
	GitHostingPlatformRegistry ports.GitHostingPlatformRegistry
	WebhooksRegistry           ports.WebhooksRegistry

	Organizations repositories.OrganizationRepository
	Sessions      repositories.SessionRepository

	ForSecrets     ports.ForSecrets
	EventBus       ports.ForEventBus
	LinearPlatform *linear.Platform

	ForQueue            interfaces.Queue
	TaskSchedulerConfig async.TaskSchedulerConfig
}

type App struct {
	config Config

	aiService      *domain_service.AIService
	gitService     *domain_service.GitService
	linearPlatform *linear.Platform

	taskScheduler  *async.TaskScheduler
	WebhookService *domain_service.WebhookService
}

func New(config Config) (*App, error) {
	app := &App{
		config: config,
	}

	aiService := domain_service.NewAIService(domain_service.AIServiceConfig{
		WorkPlatformRegistry: config.WorkPlatformRegistry,
		ForEvent:             config.EventBus,
		Organizations:        config.Organizations,
		Sessions:             config.Sessions,
	})

	app.gitService = domain_service.NewGitService(domain_service.GitServiceConfig{
		GitHostingPlatformRegistry: config.GitHostingPlatformRegistry,
		ForEvent:                   config.EventBus,
	})

	// Subscribe to issue-state-updated webhook events to archive sandboxes
	// when a ticket is marked as done.
	if app.linearPlatform != nil {
		config.EventBus.Subscribe(
			types.EventType_Webhook+"."+string(types.PlatformProvider_Linear),
			func(ctx context.Context, event ports.DomainEvent) error {
				e, ok := event.(types.WebhookEvent)

				if !ok {
					return fmt.Errorf("expected a webhook event, received %s", event.EventType())
				}

				if e.Type != types.WebhookEventType_IssueStateUpdated {
					return nil
				}

				issueChange, ok := e.Payload.(*linear.IssueStatusChangePayload)
				if !ok {
					slog.Debug("issue-state-updated event payload is not an IssueStatusChangePayload")
					return nil
				}

				return app.linearPlatform.ArchiveSandboxForIssue(ctx, issueChange.Data.ID)
			},
		)
	}

	taskScheduler, err := async.NewTaskScheduler(
		config.ForQueue,
		config.TaskSchedulerConfig,
		aiService.Process,
	)

	if err != nil {
		slog.Error("failed to create task scheduler", "err", err)
		return nil, err
	}

	app.taskScheduler = taskScheduler

	app.WebhookService = domain_service.NewWebhookService(domain_service.WebhookServiceConfig{
		WebhooksRegistry: config.WebhooksRegistry,
		ForEventBus:      config.EventBus,
	})

	slog.Debug("application created")
	return app, nil
}

// RunWorkers starts the TaskScheduler worker pool. It blocks until the
// provided context is cancelled.
func (app *App) RunWorkers(ctx context.Context) error {
	return app.taskScheduler.Run(ctx)
}

func (app *App) GetWorkPlatform(name types.PlatformProvider) (ports.ForWorkPlatform, error) {
	registry, ok := app.config.WorkPlatformRegistry[name]

	if !ok {
		slog.Error("failed to load work platform from registry", "name", name)
		return nil, types.ErrInternalServerError
	}

	return registry, nil
}
