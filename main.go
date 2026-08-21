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
	"github.com/jazielguerrero/workdock/api"
	"github.com/jazielguerrero/workdock/application"
	"github.com/jazielguerrero/workdock/application/async"
	"github.com/jazielguerrero/workdock/application/git_hosting_platforms/github"
	"github.com/jazielguerrero/workdock/application/harnesses/opencode"
	"github.com/jazielguerrero/workdock/application/work_platforms/linear"
	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/jazielguerrero/workdock/infrastructure/daytona_client"
	"github.com/jazielguerrero/workdock/infrastructure/github_client"
	"github.com/jazielguerrero/workdock/infrastructure/in_memory_secrets"
	"github.com/jazielguerrero/workdock/infrastructure/infisical_client"
	"github.com/jazielguerrero/workdock/infrastructure/linear_client"
	"github.com/jazielguerrero/workdock/infrastructure/otlp_client"
	"github.com/jazielguerrero/workdock/infrastructure/postgres_client"
	"github.com/lmittmann/tint"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ServiceName        string                                  `yaml:"service_name"`
	ServerAddress      string                                  `yaml:"server_address"`
	Workers            int                                     `yaml:"workers"`
	WorkerLeaseSeconds int                                     `yaml:"worker_lease_seconds"`
	DaytonaConfig      daytona_client.SandboxConfig            `yaml:"daytona"`
	Linear             linear_client.LinearServiceConfig       `yaml:"linear"`
	Opencode           opencode.ConfigExternal                 `yaml:"opencode"`
	Infisical          infisical_client.InfisicalServiceConfig `yaml:"infisical"`
	Secrets            SecretsConfig                           `yaml:"secrets"`
	Postgres           postgres_client.PostgresServiceConfig   `yaml:"postgres"`
	Github             github_client.GitHubClientConfig        `yaml:"github"`
	Otlp               *otlp_client.Config                     `yaml:"otlp"`
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

		if err != nil {
			os.Exit(1)
		}

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

	var forSecrets ports.ForSecrets

	switch cfg.Secrets.Mode {
	case in_memory_secrets.ModeMemory:
		forSecrets = in_memory_secrets.NewWithSeeds(cfg.Secrets.MemorySecrets)
		slog.Info("using in-memory secrets provider")

	default:
		infisicalClient, err := infisical_client.New(ctx, cfg.Infisical)

		if err != nil {
			os.Exit(1)
		}

		forSecrets = infisicalClient
	}

	linearClient, err := linear_client.New(cfg.Linear, forSecrets)

	if err != nil {
		os.Exit(1)
	}

	githubClient, err := github_client.New(cfg.Github)

	if err != nil {
		os.Exit(1)
	}

	postgresClient, err := postgres_client.New(ctx, cfg.Postgres)

	if err != nil {
		os.Exit(1)
	}

	postgresEventQueue, err := postgres_client.NewEventQueue(ctx, postgresClient)

	if err != nil {
		os.Exit(1)
	}

	eventBus := async.NewInMemoryEventBus()

	// Registries definition
	harnessRegistry := make(ports.HarnessPlatformRegistry)
	workPlatformRegistry := make(ports.WorkPlatformRegistry)
	gitHostingPlatformRegistry := make(ports.GitHostingPlatformRegistry)
	webhookRegistry := make(ports.WebhooksRegistry)

	// Registries configuration
	opencodeHarness := func(consturctor ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
		sessionEventId := "not-set"
		if consturctor.SessionEvent != nil {
			sessionEventId = consturctor.SessionEvent.Identifier
		}

		sandbox, err := daytona_client.NewSandbox(
			cfg.DaytonaConfig,
			consturctor.Session.Identifier,
			sessionEventId,
		)

		if err != nil {
			return nil, err
		}

		return opencode.New(opencode.Config{
			ConfigExternal: cfg.Opencode,
			Sandbox:        sandbox,
			Parts:          consturctor.Parts,
			Session:        consturctor.Session,
			SessionEvent:   consturctor.SessionEvent,
			Prompt:         consturctor.Prompt,
			Secrets:        consturctor.Secrets,
		}, serviceName)
	}
	harnessRegistry[types.HarnessProvider_OpenCode] = opencodeHarness

	githubPlatform := github.New(github.GitHubPlatformConfig{
		Client:            githubClient,
		ForSecrets:        forSecrets,
		ForEvents:         eventBus,
		GitHubConnections: postgresClient,
		BotLoginName:      cfg.Github.BotLoginId,
	})
	gitHostingPlatformRegistry[types.PlatformProvider_GitHub] = githubPlatform
	webhookRegistry[types.PlatformProvider_GitHub] = gitHostingPlatformRegistry[types.PlatformProvider_GitHub]

	linearPlatform := linear.New(linear.Config{
		HarnessRegistry:     harnessRegistry,
		GitHostingRegistry:  gitHostingPlatformRegistry,
		Client:              linearClient,
		ForSecrets:          forSecrets,
		ForEvent:            eventBus,
		Sessions:            postgresClient,
		Organizations:       postgresClient,
		GitHubAppInstallURL: cfg.Github.AppInstallURL,
	})
	workPlatformRegistry[types.PlatformProvider_Linear] = linearPlatform
	webhookRegistry[types.PlatformProvider_Linear] = workPlatformRegistry[types.PlatformProvider_Linear]

	app, err := application.New(application.Config{
		WorkPlatformRegistry:       workPlatformRegistry,
		GitHostingPlatformRegistry: gitHostingPlatformRegistry,
		WebhooksRegistry:           webhookRegistry,
		Organizations:              postgresClient,
		Sessions:                   postgresClient,
		ForSecrets:                 forSecrets,
		ForQueue:                   postgresEventQueue,
		EventBus:                   eventBus,
		TaskSchedulerConfig: async.TaskSchedulerConfig{
			Workers:       cfg.Workers,
			LeaseDuration: time.Duration(cfg.WorkerLeaseSeconds) * time.Second,
		},
	})

	if err != nil {
		os.Exit(1)
	}

	server, err := api.NewHTTPServer(cfg.ServerAddress, *app)

	if err != nil {
		os.Exit(1)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := app.RunWorkers(ctx); err != nil {
			return
		}
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
