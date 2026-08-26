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
	"log/slog"

	"github.com/workdock-dev/engine/application/async"
	"github.com/workdock-dev/engine/application/interfaces"
	"github.com/workdock-dev/engine/domain/factories"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/repositories"
	domain_service "github.com/workdock-dev/engine/domain/service"
	"github.com/workdock-dev/engine/domain/types"
)

type Config struct {
	WorkPlatformRegistry       ports.WorkPlatformRegistry
	GitHostingPlatformRegistry ports.GitHostingPlatformRegistry
	WebhooksRegistry           ports.WebhooksRegistry
	HarnessRegistry            ports.HarnessPlatformRegistry

	Organizations repositories.OrganizationRepository
	Sessions      repositories.SessionRepository

	ForSecrets ports.ForSecrets
	EventBus   ports.ForEventBus

	ForQueue            interfaces.Queue
	TaskSchedulerConfig async.TaskSchedulerConfig
}

type App struct {
	config Config

	aiService  *domain_service.AIService
	gitService *domain_service.GitService

	taskScheduler  *async.TaskScheduler
	WebhookService *domain_service.WebhookService

	sessionConfigService    *domain_service.SessionConfigService
	gitEventFilterService   *domain_service.GitEventFilterService
	sessionResultService    *domain_service.SessionResultService
	promptFactory           *factories.PromptFactory
	agentConfigFactory      *factories.AgentConfigFactory
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

	app.sessionConfigService = &domain_service.SessionConfigService{}
	app.gitEventFilterService = &domain_service.GitEventFilterService{}
	app.sessionResultService = &domain_service.SessionResultService{}
	app.promptFactory = &factories.PromptFactory{}
	app.agentConfigFactory = &factories.AgentConfigFactory{}

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

func (app *App) GetSessionConfigService() *domain_service.SessionConfigService {
	return app.sessionConfigService
}

func (app *App) GetGitEventFilterService() *domain_service.GitEventFilterService {
	return app.gitEventFilterService
}

func (app *App) GetSessionResultService() *domain_service.SessionResultService {
	return app.sessionResultService
}

func (app *App) GetPromptFactory() *factories.PromptFactory {
	return app.promptFactory
}

func (app *App) GetAgentConfigFactory() *factories.AgentConfigFactory {
	return app.agentConfigFactory
}

func (app *App) GetHarnessRegistry() ports.HarnessPlatformRegistry {
	return app.config.HarnessRegistry
}

func (app *App) GetGitHostingPlatformRegistry() ports.GitHostingPlatformRegistry {
	return app.config.GitHostingPlatformRegistry
}

func (app *App) GetWebhooksRegistry() ports.WebhooksRegistry {
	return app.config.WebhooksRegistry
}

func (app *App) GetOrganizations() repositories.OrganizationRepository {
	return app.config.Organizations
}

func (app *App) GetSessions() repositories.SessionRepository {
	return app.config.Sessions
}

func (app *App) GetForSecrets() ports.ForSecrets {
	return app.config.ForSecrets
}

func (app *App) GetEventBus() ports.ForEventBus {
	return app.config.EventBus
}

func (app *App) GetForQueue() interfaces.Queue {
	return app.config.ForQueue
}

func (app *App) GetTaskSchedulerConfig() async.TaskSchedulerConfig {
	return app.config.TaskSchedulerConfig
}

func (app *App) SetWorkPlatformRegistry(registry ports.WorkPlatformRegistry) {
	app.config.WorkPlatformRegistry = registry
}

func (app *App) SetGitHostingPlatformRegistry(registry ports.GitHostingPlatformRegistry) {
	app.config.GitHostingPlatformRegistry = registry
}

func (app *App) SetWebhooksRegistry(registry ports.WebhooksRegistry) {
	app.config.WebhooksRegistry = registry
}

func (app *App) SetHarnessRegistry(registry ports.HarnessPlatformRegistry) {
	app.config.HarnessRegistry = registry
}
