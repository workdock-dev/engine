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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lmittmann/tint"
	"github.com/workdock-dev/engine/features/agent_session"
	agent_session_infrastructure "github.com/workdock-dev/engine/features/agent_session/infrastructure"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	oauth20 "github.com/workdock-dev/engine/features/oauth2.0"
	"github.com/workdock-dev/engine/features/organization"
	organization_infrastructure "github.com/workdock-dev/engine/features/organization/infrastructure"
	"github.com/workdock-dev/engine/features/webhook"
	"github.com/workdock-dev/engine/infrastructure/event_bus"
	"github.com/workdock-dev/engine/infrastructure/in_memory_secrets"
	"github.com/workdock-dev/engine/infrastructure/infisical_client"
	"github.com/workdock-dev/engine/infrastructure/otlp_client"
	"github.com/workdock-dev/engine/infrastructure/server"
	"github.com/workdock-dev/engine/plug-ings/daytona"
	daytona_types "github.com/workdock-dev/engine/plug-ings/daytona/types"
	"github.com/workdock-dev/engine/plug-ings/github"
	github_infra "github.com/workdock-dev/engine/plug-ings/github/infrastructure"
	github_types "github.com/workdock-dev/engine/plug-ings/github/types"
	"github.com/workdock-dev/engine/plug-ings/linear"
	linear_infra "github.com/workdock-dev/engine/plug-ings/linear/infrastructure"
	linear_types "github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/plug-ings/opencode"
	opencode_types "github.com/workdock-dev/engine/plug-ings/opencode/types"
	"github.com/workdock-dev/engine/shared"
	"gopkg.in/yaml.v3"
)

type PostgresConfig struct {
	DatabaseUrl string `yaml:"database_url"`
}

type MCPConfig struct {
	Name       string   `yaml:"name"`
	Url        string   `yaml:"url"`
	AuthKey    string   `yaml:"auth_key"`
	AuthSecret string   `yaml:"auth_secret"`
	Hosts      []string `yaml:"hosts"`
}

type Config struct {
	ServiceName   string                                  `yaml:"service_name"`
	ServerAddress string                                  `yaml:"server_address"`
	TaskScheduler agent_session_types.TaskSchedulerConfig `yaml:"task_scheduler"`
	MCPs          []MCPConfig                             `yaml:"mcps"`

	// plug-ings configuration
	Linear   linear_types.Config   `yaml:"linear"`
	Daytona  daytona_types.Config  `yaml:"daytona"`
	Opencode opencode_types.Config `yaml:"opencode"`
	Github   github_types.Config   `yaml:"github"`

	// infrastructure configuration
	Postgres        PostgresConfig                           `yaml:"postgres"`
	Infisical       *infisical_client.InfisicalServiceConfig `yaml:"infisical"`
	InMemorySecrets *in_memory_secrets.Config                `yaml:"in_memory_secrets"`
	Otlp            *otlp_client.Config                      `yaml:"otlp"`
}

type MCPFromConfigFile struct {
	config *Config
}

func (m *MCPFromConfigFile) GetMCPList() []agent_session_interfaces.MCPConfig {
	if m.config.MCPs == nil {
		return nil
	}

	list := make([]agent_session_interfaces.MCPConfig, len(m.config.MCPs))

	for i, mcp := range m.config.MCPs {
		list[i] = agent_session_interfaces.MCPConfig{
			Name:       mcp.Name,
			Url:        mcp.Url,
			AuthKey:    mcp.AuthKey,
			AuthSecret: mcp.AuthSecret,
			Hosts:      mcp.Hosts,
		}
	}

	return list
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
	// *-------------------------------------------------------------------------*
	// * Load config                                                             *
	// *-------------------------------------------------------------------------*

	cfg, err := loadConfig("config.yaml")

	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	serviceName := fmt.Sprintf("workdock-%s", uuid.NewString())

	if cfg.ServiceName != "" {
		serviceName = cfg.ServiceName
	}

	// *-------------------------------------------------------------------------*
	// * Setup logging                                                           *
	// *-------------------------------------------------------------------------*

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

	// *-------------------------------------------------------------------------*
	// * Setup secrets manager                                                   *
	// *-------------------------------------------------------------------------*

	var secretManager shared.ForSecrets

	if cfg.Infisical != nil {
		infisicalClient, err := infisical_client.New(ctx, *cfg.Infisical)
		exit(err)
		secretManager = infisicalClient
	} else if cfg.InMemorySecrets != nil {
		secretManager = in_memory_secrets.NewWithSeeds(cfg.InMemorySecrets.Secrets)
		slog.Info("using in-memory secrets provider")
	}

	// *-------------------------------------------------------------------------*
	// * Setup infrastructure                                                    *
	// *-------------------------------------------------------------------------*

	eventBus := event_bus.NewInMemoryEventBus()

	linearClient, err := linear_infra.NewClient(cfg.Linear, secretManager)
	exit(err)

	githubClient, err := github_infra.NewClient(cfg.Github)
	exit(err)

	postgres, err := pgxpool.New(context.Background(), cfg.Postgres.DatabaseUrl)
	exit(err)

	postgresRawConn, err := pgx.Connect(ctx, cfg.Postgres.DatabaseUrl)
	exit(err)

	server, err := server.New(cfg.ServerAddress)
	exit(err)

	// *-------------------------------------------------------------------------*
	// * Setup plug-ings                                                         *
	// *-------------------------------------------------------------------------*

	linearAgentSessionHandler := linear.NewAgentSessionHandler(linearClient, secretManager)
	githubGitHandler := github.NewGitHandler(cfg.Github, githubClient, secretManager)
	daytonaSandboxHandler := daytona.NewSandboxHandler(cfg.Daytona)
	opencodeHarnessHandler := opencode.NewHarnessHandler(cfg.Opencode)

	// *-------------------------------------------------------------------------*
	// * Setup application                                                       *
	// *-------------------------------------------------------------------------*

	webhook.New(
		"POST /github/webhook",
		server.Mux(),
		github.NewWEventTransformer(),
		github.NewWEventVerifier(cfg.Github),
		github.NewWEventConsumer(cfg.Github, githubClient, eventBus),
	)

	oauth20.New(
		"linear",
		server.Mux(),
		linear.NewOAuth20Handler(cfg.Linear, linearClient),
		secretManager,
		eventBus,
	)
	webhook.New(
		"POST /linear/webhook",
		server.Mux(),
		linear.NewWEventTransformer(),
		linear.NewWEventVerifier(cfg.Linear),
		linear.NewWEventConsumer(eventBus),
	)

	organization.New(
		eventBus,
		organization_infrastructure.NewPostgres(postgres),
	)

	// *-------------------------------------------------------------------------*
	// * Start application                                                       *
	// *-------------------------------------------------------------------------*

	var wg sync.WaitGroup
	wg.Go(func() {

		// *-------------------------------------------------------------------------*
		// * Setup core application feature                                          *
		// *-------------------------------------------------------------------------*

		agentSessionPostgres := agent_session_infrastructure.NewPostgres(postgres)
		agentSessionPostgresQueue := agent_session_infrastructure.NewEventQueue(postgres, postgresRawConn)

		err := agent_session.New(
			ctx,
			cfg.TaskScheduler,
			agent_session.AgentHandlerRegistry{
				string(shared.PlatformProvider_Linear): linearAgentSessionHandler,
			},
			agent_session.GitHandlerRegistry{
				string(shared.PlatformProvider_GitHub): githubGitHandler,
			},
			agent_session.SandboxHandlerRegistry{
				string(shared.PlatformProvider_Daytona): daytonaSandboxHandler,
			},
			agent_session.HarnessHandlerRegistry{
				string(shared.HarnessProvider_OpenCode): opencodeHarnessHandler,
			},
			&MCPFromConfigFile{config: cfg},
			eventBus,
			secretManager,
			agentSessionPostgres,
			agentSessionPostgres,
			agentSessionPostgres,
			agentSessionPostgresQueue,
		)
		exit(err)
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
