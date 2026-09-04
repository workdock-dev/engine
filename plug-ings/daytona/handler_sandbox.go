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

package daytona

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	sdkerrors "github.com/daytona/clients/sdk-go/pkg/errors"
	"github.com/daytona/clients/sdk-go/pkg/options"
	sdktypes "github.com/daytona/clients/sdk-go/pkg/types"
	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/plug-ings/daytona/helpers"
	"github.com/workdock-dev/engine/plug-ings/daytona/types"
)

const (
	USER_PLACERHOLDER = "${USER}"
	DAYTONA_USER      = "daytona"
)

type SandboxHandler struct {
	config types.Config
}

func NewSandboxHandler(config types.Config) agent_session_interfaces.HandlerSandbox {
	return &SandboxHandler{
		config: config,
	}
}

func (h *SandboxHandler) Run(
	ctx context.Context,
	config *agent_session_interfaces.SandboxConfig,
	stdout chan<- string,
	stderr chan<- string,
) (func(ctx context.Context) string, error) {
	target := h.config.Target

	if target == "" {
		target = "us"
	}

	// *-------------------------------------------------------------------------*
	// * Create daytona client                                                   *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] client created")
	client, err := daytona.NewClientWithConfig(&sdktypes.DaytonaConfig{
		APIKey:     h.config.ApiKey,
		APIUrl:     h.config.ApiUrl,
		Target:     target,
		HTTPClient: helpers.HTTPClient,
	})

	if err != nil {
		slog.Error("failed to create daytona client", "err", err)
		return nil, err
	}

	// Track created secrets
	secretIds := make([]string, 0)

	// *-------------------------------------------------------------------------*
	// * Shutdown function to clean up everything                                *
	// *-------------------------------------------------------------------------*
	var sandbox *daytona.Sandbox
	var created bool
	var deleting bool
	var listening bool
	var execSessionCreated bool

	shutdown := func(ctx context.Context) string {
		out := ""

		if sandbox != nil && !deleting {
			// *-------------------------------------------------------------------------*
			// * Run the provided exit command
			// *-------------------------------------------------------------------------*
			_, result, _ := h.ExecuteCommand(
				ctx,
				sandbox,
				config,
				config.ExitCommand,
				time.Minute*2,
			)

			if result != "" {
				out = result
			}

			if !listening {
				close(stdout)
				close(stderr)
			}

			if execSessionCreated {
				h.DeleteExecutionSession(ctx, sandbox, config)
			}

			h.Shutdown(context.Background(), sandbox, config)
		}

		for _, id := range secretIds {
			h.DeleteSecret(ctx, client, config, id)
		}

		if err := client.Close(ctx); err != nil {
			slog.Error("[sandbox][daytona] failed to close client", "err", err, "event_identifier", config.SessionEvent.Identifier)
		}

		return out
	}

	// *-------------------------------------------------------------------------*
	// * Create secrets using daytona's secret API these secrets are never       *
	// * written into the sandbox                                                *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] secrets configured")
	secrets := make(map[string]string)

	for _, secret := range config.Secrets {
		secretId, secretName, err := h.SetSecret(ctx, client, config, secret.Value, secret.Hosts)

		if err != nil {
			return shutdown, err
		}

		secrets[secret.Name] = secretName
		secretIds = append(secretIds, secretId)
	}

	for _, secret := range h.config.Secrets {
		secretId, secretName, err := h.SetSecret(ctx, client, config, secret.Value, secret.Hosts)

		if err != nil {
			return shutdown, err
		}

		secrets[secret.Name] = secretName
		secretIds = append(secretIds, secretId)
	}

	// *-------------------------------------------------------------------------*
	// * Create or returns the existent sandbox based on the config              *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] created")
	sandbox, created, err = h.GetOrCreateSandbox(ctx, client, config, secrets)

	if err != nil {
		return shutdown, err
	}

	// *-------------------------------------------------------------------------*
	// * Starts the sandbox                                                      *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] started")
	if err := h.Start(ctx, sandbox, config); err != nil {
		return shutdown, err
	}

	// *-------------------------------------------------------------------------*
	// * If this sandbox was just created, install any additional dependency     *
	// *-------------------------------------------------------------------------*
	if created {
		slog.Debug("[sandbox][daytona] installing dependencies")
		for _, cmd := range config.CommandsWhenCreated {
			if _, _, err := h.ExecuteCommand(ctx, sandbox, config, cmd, time.Minute*1); err != nil {
				deleting = true
				h.DeleteSandbox(context.Background(), sandbox, config)
				return shutdown, err
			}
		}
	}

	// *-------------------------------------------------------------------------*
	// * Configure the git user, we call it always in case user updated it       *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] configured git user")
	if err := h.ConfigureGitUser(ctx, sandbox, config); err != nil {
		return shutdown, err
	}

	// *-------------------------------------------------------------------------*
	// * Upload any file required for work                                       *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] files uploaded")
	for path, data := range config.FileUploads {
		if err := h.UploadFile(ctx, sandbox, config, data, strings.ReplaceAll(path, USER_PLACERHOLDER, DAYTONA_USER)); err != nil {
			return shutdown, err
		}
	}

	// *-------------------------------------------------------------------------*
	// * Create an execution process since interacting with the AI takes time    *
	// *-------------------------------------------------------------------------*
	slog.Debug("[sandbox][daytona] execution sesion created")
	if err := h.CreateExecutionSession(ctx, sandbox, config); err != nil {
		return shutdown, err
	}

	execSessionCreated = true
	slog.Debug("[sandbox][daytona] execution sesion running", "cmd", config.HarnessCommand)
	result, err := h.ExecuteSessionCommand(ctx, sandbox, config)

	if err != nil {
		return shutdown, err
	}

	cmdId, ok := result["id"].(string)

	if !ok {
		err := errors.New("invalid pid type")
		slog.Error("[sandbox][daytona] failed to execute session command", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return shutdown, err
	}

	go func() {
		slog.Debug("[sandbox][daytona] streaming command logs", "event_identifier", config.SessionEvent.Identifier)

		// Channel are closed internally
		listening = true
		if err := h.StreamSessionCommandLogs(ctx, sandbox, config, cmdId, stdout, stderr); err != nil {
			slog.Error("[sandbox][daytona] failed to stream session output", "err", err, "event_identifier", config.SessionEvent.Identifier)
			return
		}
	}()

	return shutdown, nil
}

func (h *SandboxHandler) Archive(ctx context.Context, config *agent_session_interfaces.SandboxConfig) error {
	target := h.config.Target

	if target == "" {
		target = "us"
	}

	client, err := daytona.NewClientWithConfig(&sdktypes.DaytonaConfig{
		APIKey:     h.config.ApiKey,
		APIUrl:     h.config.ApiUrl,
		Target:     target,
		HTTPClient: helpers.HTTPClient,
	})

	if err != nil {
		slog.Error("failed to create daytona client", "err", err)
		return err
	}

	defer client.Close(context.Background())

	sandbox, err := helpers.RetryRateLimited(ctx, helpers.ThrottlerAuthenticated, "get sandbox for archive", func() (*daytona.Sandbox, error) {
		return client.Get(ctx, config.Session.Identifier)
	})

	if err != nil {
		if errors.Is(err, sdkerrors.ErrNotFound) {
			slog.Debug("sandbox not found for archive, skipping", "session_identifier", config.Session.Identifier)
			return nil
		}

		slog.Error("failed to get daytona sandbox for archive", "err", err, "session_identifier", config.Session.Identifier)
		return err
	}

	if sandbox == nil {
		slog.Warn("failed to archive sandbox, sandbox is nil")
		return nil
	}

	state := sandbox.State

	if state == daytona.SandboxStateArchived || state == daytona.SandboxStateArchiving {
		slog.Debug("sandbox already archived, skipping", "session_identifier", config.Session.Identifier)
		return nil
	}

	if state != daytona.SandboxStateStopped && state != daytona.SandboxStateStopping {
		slog.Debug("stopping sandbox before archive", "session_identifier", config.Session.Identifier, "state", state)

		if err := sandbox.StopWithTimeout(ctx, 2*time.Minute, true); err != nil {
			return err
		}
	}

	if err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerSandboxLifecycle, "archive sandbox", func() error {
		if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxLifecycle, "archive sandbox"); err != nil {
			return err
		}

		return sandbox.Archive(ctx)
	}); err != nil {
		slog.Error("failed to archive daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	slog.Info("archived daytona sandbox for done issue", "session_identifier", config.Session.Identifier)
	return nil
}

func (h *SandboxHandler) GetOrCreateSandbox(ctx context.Context, client *daytona.Client, config *agent_session_interfaces.SandboxConfig, secrets map[string]string) (*daytona.Sandbox, bool, error) {
	sandbox, err := helpers.RetryRateLimited(ctx, helpers.ThrottlerAuthenticated, "get sandbox", func() (*daytona.Sandbox, error) {
		return client.Get(ctx, config.Session.Identifier)
	})

	if err != nil {
		if h.isContextCanceledOrDeadlineExceeded(err) {
			return nil, false, err
		}

		if !errors.Is(err, sdkerrors.ErrNotFound) {
			slog.Error("[sandbox][daytona] failed to get", "err", err, "event_identifier", config.SessionEvent.Identifier)
			return nil, false, err
		}
	}

	if sandbox == nil {
		// Sandboxes have 1 vCPU, 1GB RAM, and 3GiB disk by default
		sdb, err := func() (*daytona.Sandbox, error) {
			if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxCreate, "create sandbox"); err != nil {
				return nil, err
			}

			return helpers.RetryRateLimited(ctx, helpers.ThrottlerSandboxCreate, "create sandbox", func() (*daytona.Sandbox, error) {
				return client.Create(ctx, sdktypes.SnapshotParams{
					Snapshot:         "daytona-small",
					Name:             config.Session.Identifier,
					Public:           false,
					AutoStopInterval: &config.AutoStopInterval,
					Labels: map[string]string{
						"session_event_identifier": config.SessionEvent.Identifier,
					},
					Secrets: secrets,
				})
			})
		}()

		if err != nil {
			slog.Error("[sandbox][daytona] failed to create", "err", err, "event_identifier", config.SessionEvent.Identifier)
			return nil, false, err
		}

		return sdb, true, nil
	}

	return sandbox, false, nil
}

// SetSecret creates a secret and returns its id and name.
func (h *SandboxHandler) SetSecret(ctx context.Context, client *daytona.Client, config *agent_session_interfaces.SandboxConfig, secretValue string, hosts []string) (string, string, error) {
	secretName := h.newUUIDStartingWithLetter()
	secret, err := helpers.RetryRateLimited(ctx, helpers.ThrottlerAuthenticated, "create secret", func() (*sdktypes.Secret, error) {
		return client.Secret.Create(ctx, &sdktypes.CreateSecretParams{
			Name:  secretName,
			Value: secretValue,
			Hosts: hosts,
		})
	})

	if err != nil {
		slog.Error("[sandbox][daytona] failed to create secret", "err", err, "secret", secretName, "event_identifier", config.SessionEvent.Identifier)
		return "", "", err
	}

	return secret.ID, secretName, nil
}

func (h *SandboxHandler) DeleteSecret(ctx context.Context, client *daytona.Client, config *agent_session_interfaces.SandboxConfig, secretId string) error {
	err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerAuthenticated, "delete secret", func() error {
		return client.Secret.Delete(ctx, secretId)
	})

	if err != nil {
		slog.Error("[sandbox][daytona] failed to delete secret", "secret_id", secretId, "err", err, "event_identifier", config.SessionEvent.Identifier)
	}

	return err
}

func (h *SandboxHandler) Start(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig) error {
	if err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerSandboxLifecycle, "start sandbox", func() error {
		if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxLifecycle, "start sandbox"); err != nil {
			return err
		}

		return sandbox.Start(ctx)
	}); err != nil {
		if h.isContextCanceledOrDeadlineExceeded(err) {
			return err
		}

		slog.Error("[sandbox][daytona] failed to start", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	return nil
}

// UpdateExistingSandbox updates the secrets and environment variables of an
// existing, running sandbox. This must be called after Start because the
// Daytona API needs the container's IP address to apply updates, and the IP
// is only available once the sandbox is running.
func (h *SandboxHandler) UpdateExistingSandbox(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig, secrets, envVars map[string]string) error {
	if err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerSandboxLifecycle, "update secrets", func() error {
		if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxLifecycle, "update secrets"); err != nil {
			return err
		}

		return sandbox.UpdateSecrets(ctx, secrets)
	}); err != nil {
		slog.Error("[sandbox][daytona] failed to update secrets", "event_identifier", config.SessionEvent.Identifier, "err", err)
		return err
	}

	if err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerSandboxLifecycle, "update env vars", func() error {
		if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxLifecycle, "update env vars"); err != nil {
			return err
		}

		return sandbox.UpdateEnv(ctx, envVars, nil)
	}); err != nil {
		slog.Error("[sandbox][daytona] failed to update env vars", "event_identifier", config.SessionEvent.Identifier, "err", err)
		return err
	}

	return nil
}

func (h *SandboxHandler) Shutdown(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig) error {
	if err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerSandboxLifecycle, "stop sandbox", func() error {
		if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxLifecycle, "stop sandbox"); err != nil {
			return err
		}

		return sandbox.Stop(ctx)
	}); err != nil {
		slog.Error("[sandbox][daytona] failed to stop", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	return nil
}

func (h *SandboxHandler) UploadFile(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig, data []byte, path string) error {
	if err := sandbox.FileSystem.UploadFile(ctx, data, strings.ReplaceAll(path, USER_PLACERHOLDER, DAYTONA_USER)); err != nil {
		slog.Error("[sandbox][daytona] failed to upload file", "err", err, "path", path, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	return nil
}

func (h *SandboxHandler) ConfigureGitUser(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig) error {
	if err := sandbox.Git.ConfigureUser(ctx, config.GitName, config.GitEmail); err != nil {
		slog.Error("[sandbox][daytona] failed to configure git user", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	return nil
}

// ExecuteCommand runs a command in the sandbox and fails when the execution
// result is missing or its exit code is non-zero.
func (h *SandboxHandler) ExecuteCommand(
	ctx context.Context,
	sandbox *daytona.Sandbox,
	config *agent_session_interfaces.SandboxConfig,
	command string,
	timeout time.Duration,
) (int, string, error) {
	exec, err := sandbox.Process.ExecuteCommand(ctx, command, options.WithExecuteTimeout(timeout))

	if err != nil {
		slog.Error("[sandbox][daytona] failed to execute command", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return -1, "", err
	}

	if exec == nil {
		err := errors.New("exec result is nil")
		slog.Error("[sandbox][daytona] failed to execute command", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return -1, "", err
	}

	if exec.ExitCode != 0 {
		err := fmt.Errorf("exec result is non-zero: exit code %d", exec.ExitCode)
		slog.Error("[sandbox][daytona] failed to execute command", "err", err, "event_identifier", config.SessionEvent.Identifier, "result", exec.Result)
		return exec.ExitCode, "", err
	}

	return exec.ExitCode, exec.Result, nil
}

func (h *SandboxHandler) CreateExecutionSession(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig) error {
	if err := sandbox.Process.CreateSession(ctx, config.Session.Identifier); err != nil {
		slog.Error("[sandbox][daytona] failed to create execution session", "err", err)
		return err
	}

	return nil
}

func (h *SandboxHandler) DeleteExecutionSession(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig) error {
	if err := sandbox.Process.DeleteSession(ctx, config.Session.Identifier); err != nil {
		slog.Error("[sandbox][daytona] failed to delete execution session", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	return nil
}

func (h *SandboxHandler) ExecuteSessionCommand(
	ctx context.Context,
	sandbox *daytona.Sandbox,
	config *agent_session_interfaces.SandboxConfig,
) (map[string]any, error) {
	result, err := sandbox.Process.ExecuteSessionCommand(ctx, config.Session.Identifier, config.HarnessCommand, true, false)

	if err != nil {
		slog.Error("[sandbox][daytona] failed to execute session command", "err", err, "cmd", config.HarnessCommand, "event_identifier", config.SessionEvent.Identifier)
		return nil, err
	}

	return result, nil
}

func (h *SandboxHandler) StreamSessionCommandLogs(
	ctx context.Context,
	sandbox *daytona.Sandbox,
	config *agent_session_interfaces.SandboxConfig,
	cmdId string,
	stdout chan<- string,
	stderr chan<- string,
) error {
	return sandbox.Process.GetSessionCommandLogsStream(ctx, config.Session.Identifier, cmdId, stdout, stderr)
}

func (h *SandboxHandler) DeleteSandbox(ctx context.Context, sandbox *daytona.Sandbox, config *agent_session_interfaces.SandboxConfig) error {
	err := helpers.RetryRateLimitedVoid(ctx, helpers.ThrottlerSandboxLifecycle, "delete sandbox", func() error {
		if err := helpers.Preflight(ctx, helpers.ThrottlerSandboxLifecycle, "delete sandbox"); err != nil {
			return err
		}

		return sandbox.DeleteAndWait(ctx, time.Minute*1)
	})

	if err != nil {
		slog.Error("[sandbox][daytona] failed to delete", "err", err, "event_identifier", config.SessionEvent.Identifier)
	}

	return err
}

// newUUIDStartingWithLetter generates a secret name that starts with a letter.
// Daytona returns 500 internal server error when creating a secret whose name
// starts with a number, so this is a patch for that behaviour.
func (h *SandboxHandler) newUUIDStartingWithLetter() string {
	count := 0

	for {
		count++
		u := uuid.New()

		switch u.String()[0] {
		case 'a', 'b', 'c', 'd', 'e', 'f':
			slog.Debug("[sandbox][daytona] deterministic UUID that starts with a letter; patch for daytona ran", "count", count)
			return u.String()
		}
	}
}

func (h *SandboxHandler) isContextCanceledOrDeadlineExceeded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
