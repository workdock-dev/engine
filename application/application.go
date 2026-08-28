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

type App struct {
	// Domain
	workPlatformRegistry       ports.WorkPlatformRegistry
	gitHostingPlatformRegistry ports.GitHostingPlatformRegistry
	webhooksRegistry           ports.WebhooksRegistry
	harnessRegistry            ports.HarnessPlatformRegistry
	forSecrets                 ports.ForSecrets
	eventBus                   ports.ForEventBus
	organizations              repositories.OrganizationRepository
	sessions                   repositories.SessionRepository
	gitHubConnections          repositories.GitHubConnectionRepository
	aiService                  *domain_service.AIService
	gitService                 *domain_service.GitService
	webhookService             *domain_service.WebhookService
	sessionConfigService       *domain_service.SessionConfigService
	gitEventFilterService     *domain_service.GitEventFilterService
	issueLifecycleService     *domain_service.IssueLifecycleService
	sessionResultService       *domain_service.SessionResultService
	repoAccessService         *domain_service.RepoAccessService
	promptFactory              *factories.PromptFactory
	agentConfigFactory        *factories.AgentConfigFactory

	// Application
	forQueue interfaces.Queue
}

func New() *App {
	slog.Debug("application created")
	return &App{}
}

func WithWorkPlatformRegistry(app *App, registry ports.WorkPlatformRegistry) {
	app.workPlatformRegistry = registry
}

func WithGitHostingPlatformRegistry(app *App, registry ports.GitHostingPlatformRegistry) {
	app.gitHostingPlatformRegistry = registry
}

func WithWebhooksRegistry(app *App, registry ports.WebhooksRegistry) {
	app.webhooksRegistry = registry
}

func WithHarnessRegistry(app *App, registry ports.HarnessPlatformRegistry) {
	app.harnessRegistry = registry
}

func WithOrganizationRepository(app *App, value repositories.OrganizationRepository) {
	app.organizations = value
}

func WithSessionRepository(app *App, value repositories.SessionRepository) {
	app.sessions = value
}

func WithGitHubRepository(app *App, value repositories.GitHubConnectionRepository) {
	app.gitHubConnections = value
}

func WithEventBus(app *App, value ports.ForEventBus) {
	app.eventBus = value
}

func WithSecretManager(app *App, value ports.ForSecrets) {
	app.forSecrets = value
}

func WithQueue(app *App, value interfaces.Queue) {
	app.forQueue = value
}

func (app *App) Init() {
	app.aiService = domain_service.NewAIService(domain_service.AIServiceConfig{
		WorkPlatformRegistry: app.workPlatformRegistry,
		ForEvent:             app.eventBus,
		Organizations:        app.organizations,
		Sessions:             app.sessions,
	})

	app.gitService = domain_service.NewGitService(domain_service.GitServiceConfig{
		GitHostingPlatformRegistry: app.gitHostingPlatformRegistry,
		ForEvent:                   app.eventBus,
	})

	app.webhookService = domain_service.NewWebhookService(domain_service.WebhookServiceConfig{
		WebhooksRegistry: app.webhooksRegistry,
		ForEventBus:      app.eventBus,
	})

	app.sessionConfigService = &domain_service.SessionConfigService{}
	app.gitEventFilterService = &domain_service.GitEventFilterService{}
	app.issueLifecycleService = domain_service.NewIssueLifecycleService()
	app.sessionResultService = &domain_service.SessionResultService{}
	app.repoAccessService = domain_service.NewRepoAccessService(domain_service.RepoAccessConfig{
		GitHubConnections: app.gitHubConnections,
		ForSecrets:        app.forSecrets,
		ForEvent:          app.eventBus,
	})
	app.promptFactory = &factories.PromptFactory{}
	app.agentConfigFactory = &factories.AgentConfigFactory{}
}

func (app *App) Run(ctx context.Context, config async.TaskSchedulerConfig) error {
	taskScheduler, err := async.NewTaskScheduler(
		app.forQueue,
		config,
		app.aiService.Process,
	)

	if err != nil {
		slog.Error("failed to create task scheduler", "err", err)
		return err
	}

	return taskScheduler.Run(ctx)
}

func (app *App) GetWebhookService() *domain_service.WebhookService {
	return app.webhookService
}

func (app *App) GetSessionConfigService() *domain_service.SessionConfigService {
	return app.sessionConfigService
}

func (app *App) GetGitEventFilterService() *domain_service.GitEventFilterService {
	return app.gitEventFilterService
}

func (app *App) GetIssueLifecycleService() *domain_service.IssueLifecycleService {
	return app.issueLifecycleService
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

func (app *App) GetWorkPlatform(name types.PlatformProvider) (ports.ForWorkPlatform, error) {
	registry, ok := app.workPlatformRegistry[name]

	if !ok {
		slog.Error("failed to load work platform from registry", "name", name)
		return nil, types.ErrInternalServerError
	}

	return registry, nil
}

func (app *App) GetHarnessRegistry() ports.HarnessPlatformRegistry {
	return app.harnessRegistry
}

func (app *App) GetForSecrets() ports.ForSecrets {
	return app.forSecrets
}

func (app *App) GetEventBus() ports.ForEventBus {
	return app.eventBus
}

func (app *App) GetGitHostingPlatformRegistry() ports.GitHostingPlatformRegistry {
	return app.gitHostingPlatformRegistry
}

func (app *App) GetWebhooksRegistry() ports.WebhooksRegistry {
	return app.webhooksRegistry
}

func (app *App) GetOrganizations() repositories.OrganizationRepository {
	return app.organizations
}

func (app *App) GetGitHubConnections() repositories.GitHubConnectionRepository {
	return app.gitHubConnections
}

func (app *App) GetRepoAccessService() *domain_service.RepoAccessService {
	return app.repoAccessService
}

func (app *App) GetSessions() repositories.SessionRepository {
	return app.sessions
}

func (app *App) GetForQueue() interfaces.Queue {
	return app.forQueue
}
