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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lmittmann/tint"
	"github.com/workdock-dev/engine/features/agent_session"
	"github.com/workdock-dev/engine/features/webhook"
	"github.com/workdock-dev/engine/infrastructure/event_bus"
	"github.com/workdock-dev/engine/infrastructure/in_memory_secrets"
	"github.com/workdock-dev/engine/infrastructure/infisical_client"
	"github.com/workdock-dev/engine/infrastructure/otlp_client"
	"github.com/workdock-dev/engine/infrastructure/postgres_client"
	"github.com/workdock-dev/engine/infrastructure/server"
	async "github.com/workdock-dev/engine/infrastructure/task_scheduler"
	"github.com/workdock-dev/engine/shared"
	"gopkg.in/yaml.v3"

	"github.com/workdock-dev/engine/plug-ings/daytona"
	"github.com/workdock-dev/engine/plug-ings/github"
	github_infra "github.com/workdock-dev/engine/plug-ings/github/infrastructure"
	"github.com/workdock-dev/engine/plug-ings/linear"
	linear_infra "github.com/workdock-dev/engine/plug-ings/linear/infrastructure"
	"github.com/workdock-dev/engine/plug-ings/opencode"

	daytona_types "github.com/workdock-dev/engine/plug-ings/daytona/types"
	github_types "github.com/workdock-dev/engine/plug-ings/github/types"
	linear_types "github.com/workdock-dev/engine/plug-ings/linear/types"
	opencode_types "github.com/workdock-dev/engine/plug-ings/opencode/types"
)

type Config struct {
	ServiceName        string `yaml:"service_name"`
	ServerAddress      string `yaml:"server_address"`
	Workers            int    `yaml:"workers"`
	WorkerLeaseSeconds int    `yaml:"worker_lease_seconds"`

	// plug-ings configuration
	Daytona  daytona_types.Config  `yaml:"daytona"`
	Linear   linear_types.Config   `yaml:"linear"`
	Opencode opencode_types.Config `yaml:"opencode"`
	Github   github_types.Config   `yaml:"github"`

	// infrastructure configuration
	Infisical infisical_client.InfisicalServiceConfig `yaml:"infisical"`
	Secrets   SecretsConfig                           `yaml:"secrets"`
	Postgres  postgres_client.PostgresServiceConfig   `yaml:"postgres"`
	Otlp      *otlp_client.Config                     `yaml:"otlp"`
}

// SecretsConfig selects the secrets provider the engine wires as
// ports.ForSecrets. An empty Mode (or "infisical") uses Infisical;
// Mode == in_memory_secrets.ModeMemory uses an in-process store seeded from
// MemorySecrets, which is keyed by secret path and then by secret name.
type SecretsConfig struct {
	Mode          string                       `yaml:"mode"`
	MemorySecrets map[string]map[string]string `yaml:"memory_secrets"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func main() {
	cfg, err := loadConfig("config.yaml")

	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	if cfg.Workers <= 0 {
		cfg.Workers = 3
	}

	serviceName := fmt.Sprintf("workdock-%s", uuid.NewString())

	if cfg.ServiceName != "" {
		serviceName = cfg.ServiceName
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Otlp != nil {
		shutdown, err := otlp_client.New(ctx, *cfg.Otlp, serviceName)
		exit(err)
		defer shutdown(ctx)
	}

	if cfg.Otlp == nil || cfg.Otlp.Slog == nil {
		w := os.Stderr
		logger := slog.New(tint.NewTextHandler(w, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.DateTime,
		}))
		slog.SetDefault(logger)
	}

	var secretManager shared.ForSecrets

	switch cfg.Secrets.Mode {
	case in_memory_secrets.ModeMemory:
		secretManager = in_memory_secrets.NewWithSeeds(cfg.Secrets.MemorySecrets)
		slog.Info("using in-memory secrets provider")
	default:
		infisicalClient, err := infisical_client.New(ctx, cfg.Infisical)
		exit(err)
		secretManager = infisicalClient
	}

	// Create infrastructure
	eventBus := event_bus.NewInMemoryEventBus()

	linearClient, err := linear_infra.NewClient(cfg.Linear, secretManager)
	exit(err)

	githubClient, err := github_infra.NewClient(cfg.Github)
	exit(err)

	postgresClient, err := postgres_client.New(ctx, cfg.Postgres)
	exit(err)

	postgresEventQueue, err := postgres_client.NewEventQueue(ctx, postgresClient)
	exit(err)

	server, err := server.New(cfg.ServerAddress)
	exit(err)

	// Create and configure features
	webhook.New(
		"POST /github/webhook",
		server.Mux(),
		github.NewWEventTransformer(),
		github.NewWEventVerifier(cfg.Github),
		github.NewWEventConsumer(
			githubClient,
			postgresClient,
			secretManager,
			eventBus,
		),
	)

	webhook.New(
		"POST /linear/webhook",
		server.Mux(),
		linear.NewWEventTransformer(),
		linear.NewWEventVerifier(cfg.Linear),
		linear.NewWEventConsumer(eventBus),
	)

	agentSessionPipeline := agent_session.New(
		agent_session.AgentHandlerRegistry{
			string(shared.PlatformProvider_Linear): linear.NewAgentSessionHandler(
				linearClient,
				secretManager,
			),
		},
		agent_session.GitHandlerRegistry{
			string(shared.PlatformProvider_GitHub): github.NewGitHandler(
				cfg.Github,
				postgresClient,
				githubClient,
				secretManager,
			),
		},
		agent_session.SandboxHandlerRegistry{
			string(shared.PlatformProvider_Daytona): daytona.NewSandboxHandler(cfg.Daytona),
		},
		agent_session.HarnessHandlerRegistry{
			string(shared.HarnessProvider_OpenCode): opencode.NewHarnessHandler(cfg.Opencode),
		},
		eventBus,
		postgresClient,
		postgresClient,
	)

	taskScheduler, err := async.NewTaskScheduler(
		postgresEventQueue,
		async.TaskSchedulerConfig{
			Workers:       cfg.Workers,
			LeaseDuration: time.Duration(cfg.WorkerLeaseSeconds) * time.Second,
		},
		agentSessionPipeline.Execute,
	)
	exit(err)

	// Create app

	// app := application.New()
	// application.WithEventBus(app, async.NewInMemoryEventBus())
	// application.WithSecretManager(app, secretManager)
	// application.WithQueue(app, postgresEventQueue)
	// application.WithOrganizationRepository(app, postgresClient)
	// application.WithSessionRepository(app, postgresClient)
	// application.WithGitHubRepository(app, postgresClient)

	// // Create application platforms
	// opencodeHarness := func(consturctor ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
	// 	sessionEventId := "not-set"
	// 	if consturctor.SessionEvent != nil {
	// 		sessionEventId = consturctor.SessionEvent.Identifier
	// 	}

	// 	sandbox, err := daytona_client.NewSandbox(
	// 		cfg.DaytonaConfig,
	// 		consturctor.Session.Identifier,
	// 		sessionEventId,
	// 	)

	// 	if err != nil {
	// 		return nil, err
	// 	}

	// 	return opencode.New(opencode.Config{
	// 		ConfigExternal: cfg.Opencode,
	// 		Sandbox:        sandbox,
	// 		Parts:          consturctor.Parts,
	// 		Session:        consturctor.Session,
	// 		SessionEvent:   consturctor.SessionEvent,
	// 		Prompt:         consturctor.Prompt,
	// 		Secrets:        consturctor.Secrets,
	// 	}, app)
	// }

	// githubPlatform := github.New(github.GitHubPlatformConfig{
	// 	Client:       githubClient,
	// 	BotLoginName: cfg.Github.BotLoginId,
	// }, app)

	// linearPlatform := linear.New(linear.Config{
	// 	Client:              linearClient,
	// 	GitHubAppInstallURL: cfg.Github.AppInstallURL,
	// }, app)

	// // Git hosting registry
	// application.WithGitHostingPlatformRegistry(app, ports.GitHostingPlatformRegistry{
	// 	types.PlatformProvider_GitHub: githubPlatform,
	// })

	// // Webhook registry
	// application.WithWebhooksRegistry(app, ports.WebhooksRegistry{
	// 	types.PlatformProvider_GitHub: githubPlatform,
	// 	types.PlatformProvider_Linear: linearPlatform,
	// })

	// // Harness registry
	// application.WithHarnessRegistry(app, ports.HarnessPlatformRegistry{
	// 	types.HarnessProvider_OpenCode: opencodeHarness,
	// })

	// // Work platform registry
	// application.WithWorkPlatformRegistry(app, ports.WorkPlatformRegistry{
	// 	types.PlatformProvider_Linear: linearPlatform,
	// })

	// // Complete the application initialization
	// app.Init()

	var wg sync.WaitGroup
	wg.Go(func() {
		taskScheduler.Run(ctx)
	})

	if cfg.Otlp != nil && cfg.Otlp.Slog != nil {
		fmt.Println("otlp slog enabled, all logs are routed to your otlp provider")
	}

	fmt.Printf("Server started, service.name: %s\n", serviceName)

	server.Run(ctx)
	slog.Info("http server stopped")

	wg.Wait()
	slog.Info("workers stopped, goodbye")
}

func exit(err error) {
	if err != nil {
		os.Exit(1)
	}
}
