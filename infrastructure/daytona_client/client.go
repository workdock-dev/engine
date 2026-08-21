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

package daytona_client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	sdkerrors "github.com/daytona/clients/sdk-go/pkg/errors"
	"github.com/daytona/clients/sdk-go/pkg/options"
	"github.com/daytona/clients/sdk-go/pkg/types"
	"github.com/google/uuid"
)

type SandboxConfig struct {
	ApiUrl string `yaml:"api_url"`
	ApiKey string `yaml:"api_key"`
	Target string `yaml:"target"`
}

var errSandboxNotInitialized = errors.New("daytona sandbox not initialized")

const defaultAutoStopInterval = 5 // minutes

// Sandbox is a concrete wrapper around the daytona client. It is not
// based on any interface; it holds the sandbox associated to a session and
// exposes the operations needed to configure it.
type Sandbox struct {
	client         *daytona.Client
	sandbox        *daytona.Sandbox
	sessionId      string
	sessionEventId string
}

func NewSandbox(config SandboxConfig, sessionId, sessionEventId string) (*Sandbox, error) {
	target := config.Target

	if target == "" {
		target = "us"
	}

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey:     config.ApiKey,
		APIUrl:     config.ApiUrl,
		Target:     target,
		HTTPClient: daytonaHTTPClient,
	})

	if err != nil {
		slog.Error("failed to create daytona client", "err", err)
		return nil, err
	}

	return &Sandbox{
		client:         client,
		sessionId:      sessionId,
		sessionEventId: sessionEventId,
	}, nil
}

func (s *Sandbox) GetOrCreateSandbox(ctx context.Context, secrets, envVars map[string]string) (bool, error) {
	sandbox, err := retryRateLimited(ctx, throttlerAuthenticated, "get sandbox", func() (*daytona.Sandbox, error) {
		return s.client.Get(ctx, s.sessionId)
	})

	if err != nil {
		if s.isContextCanceledOrDeadlineExceeded(err) {
			return false, err
		}

		if !errors.Is(err, sdkerrors.ErrNotFound) {
			slog.Error("failed to get daytona sandbox for the given session", "err", err, "event_identifier", s.sessionEventId)
			return false, err
		}
	}

	if sandbox == nil {
		// Sandboxes have 1 vCPU, 1GB RAM, and 3GiB disk by default
		sdb, err := func() (*daytona.Sandbox, error) {
			if err := preflight(ctx, throttlerSandboxCreate, "create sandbox"); err != nil {
				return nil, err
			}

			return retryRateLimited(ctx, throttlerSandboxCreate, "create sandbox", func() (*daytona.Sandbox, error) {
				autoStopInterval := defaultAutoStopInterval
				return s.client.Create(ctx, types.SnapshotParams{
					Snapshot: "daytona-small",
					SandboxBaseParams: types.SandboxBaseParams{
						Name:             s.sessionId,
						Public:           false,
						AutoStopInterval: &autoStopInterval,
						Labels: map[string]string{
							"session_event_identifier": s.sessionEventId,
						},
						Secrets: secrets,
						EnvVars: envVars,
					},
				})
			})
		}()

		if err != nil {
			slog.Error("failed to create daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
			return false, err
		}

		s.sandbox = sdb
		slog.Debug("created daytona sandbox", "event_identifier", s.sessionEventId)
		return true, nil
	}

	s.sandbox = sandbox
	slog.Debug("reusing daytona sandbox", "event_identifier", s.sessionEventId)
	return false, nil
}

// SetSecret creates a secret and returns its id and name.
func (s *Sandbox) SetSecret(ctx context.Context, secretValue string, hosts []string) (string, string, error) {
	secretName := s.newUUIDStartingWithLetter()
	secret, err := retryRateLimited(ctx, throttlerAuthenticated, "create secret", func() (*types.Secret, error) {
		return s.client.Secret.Create(ctx, &types.CreateSecretParams{
			Name:  secretName,
			Value: secretValue,
			Hosts: hosts,
		})
	})

	if err != nil {
		slog.Error("failed to create secret", "err", err, "secret", secretName, "event_identifier", s.sessionEventId)
		return "", "", err
	}

	slog.Debug("Secret set", "name", secretName, "secret_id", secret.ID, "hosts", hosts, "event_identifier", s.sessionEventId)
	return secret.ID, secretName, nil
}

func (s *Sandbox) DeleteSecret(ctx context.Context, secretId string) error {
	err := retryRateLimitedVoid(ctx, throttlerAuthenticated, "delete secret", func() error {
		return s.client.Secret.Delete(ctx, secretId)
	})

	if err != nil {
		slog.Error("failed to delete secret", "secret_id", secretId, "err", err, "event_identifier", s.sessionEventId)
	} else {
		slog.Debug("Deleted secret", "secret_id", secretId, "event_identifier", s.sessionEventId)
	}

	return err
}

func (s *Sandbox) Start(ctx context.Context) error {
	if s.sandbox == nil {
		slog.Error("failed to start daytona sandbox", "err", errSandboxNotInitialized, "event_identifier", s.sessionEventId)
		return errSandboxNotInitialized
	}

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "start sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "start sandbox"); err != nil {
			return err
		}

		return s.sandbox.Start(ctx)
	}); err != nil {
		if s.isContextCanceledOrDeadlineExceeded(err) {
			return err
		}

		slog.Error("failed to start daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Debug("Started daytona sandbox", "event_identifier", s.sessionEventId)
	return nil
}

// UpdateExistingSandbox updates the secrets and environment variables of an
// existing, running sandbox. This must be called after Start because the
// Daytona API needs the container's IP address to apply updates, and the IP
// is only available once the sandbox is running.
func (s *Sandbox) UpdateExistingSandbox(ctx context.Context, secrets, envVars map[string]string) error {
	if s.sandbox == nil {
		return errSandboxNotInitialized
	}

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "update secrets", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "update secrets"); err != nil {
			return err
		}

		return s.sandbox.UpdateSecrets(ctx, secrets)
	}); err != nil {
		slog.Error("failed to update daytona sandbox secrets", "event_identifier", s.sessionEventId, "err", err)
		return err
	}

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "update env vars", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "update env vars"); err != nil {
			return err
		}

		return s.sandbox.UpdateEnv(ctx, envVars, nil)
	}); err != nil {
		slog.Error("failed to update daytona sandbox env vars", "event_identifier", s.sessionEventId, "err", err)
		return err
	}

	return nil
}

func (s *Sandbox) Shutdown(ctx context.Context) error {
	if s.sandbox == nil {
		return nil
	}

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "stop sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "stop sandbox"); err != nil {
			return err
		}

		return s.sandbox.Stop(ctx)
	}); err != nil {
		slog.Error("failed to stop daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Debug("Stopped daytona sandbox", "event_identifier", s.sessionEventId)
	return nil
}

func (s *Sandbox) Archive(ctx context.Context) error {
	if s.client == nil {
		return errSandboxNotInitialized
	}

	if s.sandbox == nil {
		sandbox, err := retryRateLimited(ctx, throttlerAuthenticated, "get sandbox for archive", func() (*daytona.Sandbox, error) {
			return s.client.Get(ctx, s.sessionId)
		})

		if err != nil {
			if errors.Is(err, sdkerrors.ErrNotFound) {
				slog.Debug("sandbox not found for archive, skipping", "session_identifier", s.sessionId)
				return nil
			}

			slog.Error("failed to get daytona sandbox for archive", "err", err, "session_identifier", s.sessionId)
			return err
		}

		s.sandbox = sandbox
	}

	state := s.sandbox.State

	if state == daytona.SandboxStateArchived || state == daytona.SandboxStateArchiving {
		slog.Debug("sandbox already archived, skipping", "session_identifier", s.sessionId)
		return nil
	}

	if state != daytona.SandboxStateStopped && state != daytona.SandboxStateStopping {
		slog.Debug("stopping sandbox before archive", "session_identifier", s.sessionId, "state", state)

		if err := s.sandbox.StopWithTimeout(ctx, 2*time.Minute, true); err != nil {
			return err
		}
	}

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "archive sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "archive sandbox"); err != nil {
			return err
		}

		return s.sandbox.Archive(ctx)
	}); err != nil {
		slog.Error("failed to archive daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Info("archived daytona sandbox for done issue", "session_identifier", s.sessionId)
	return nil
}

func (s *Sandbox) UploadFile(ctx context.Context, data []byte, path string) error {
	if s.sandbox == nil {
		return errSandboxNotInitialized
	}

	if err := s.sandbox.FileSystem.UploadFile(ctx, data, path); err != nil {
		slog.Error("failed to upload file to daytona sandbox", "err", err, "path", path, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Debug("Uploaded file to daytona sandbox", "path", path, "event_identifier", s.sessionEventId)
	return nil
}

func (s *Sandbox) UpdateEnv(ctx context.Context, envVars map[string]string) error {
	if s.sandbox == nil {
		return errSandboxNotInitialized
	}

	if err := s.sandbox.UpdateEnv(ctx, envVars, nil); err != nil {
		slog.Error("failed to update env vars in daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Debug("Updated env vars in daytona sandbox", "event_identifier", s.sessionEventId)
	return nil
}

func (s *Sandbox) ConfigureGitUser(ctx context.Context, name, email string) error {
	if s.sandbox == nil {
		return errSandboxNotInitialized
	}

	if err := s.sandbox.Git.ConfigureUser(ctx, name, email); err != nil {
		slog.Error("failed to configure git user in daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Debug("Configured git user in daytona sandbox", "name", name, "event_identifier", s.sessionEventId)
	return nil
}

// ExecuteCommand runs a command in the sandbox and fails when the execution
// result is missing or its exit code is non-zero.
func (s *Sandbox) ExecuteCommand(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if s.sandbox == nil {
		return "", errSandboxNotInitialized
	}

	exec, err := s.sandbox.Process.ExecuteCommand(ctx, command, options.WithExecuteTimeout(timeout))

	if err != nil {
		slog.Error("failed to execute command in daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return "", err
	}

	if exec == nil {
		err := errors.New("exec result is nil")
		slog.Error("failed to execute command in daytona sandbox", "err", err, "event_identifier", s.sessionEventId)
		return "", err
	}

	if exec.ExitCode != 0 {
		err := fmt.Errorf("exec result is non-zero: exit code %d", exec.ExitCode)
		slog.Error("failed to execute command in daytona sandbox", "err", err, "event_identifier", s.sessionEventId, "result", exec.Result)
		return "", err
	}

	return exec.Result, nil
}

func (s *Sandbox) CreateExecutionSession(ctx context.Context) error {
	if s.sandbox == nil {
		return errSandboxNotInitialized
	}

	if err := s.sandbox.Process.CreateSession(ctx, s.sessionId); err != nil {
		slog.Error("failed to create daytona sandbox session", "err", err)
		return err
	}

	slog.Debug("Created execution session", "session_id", s.sessionId)
	return nil
}

func (s *Sandbox) DeleteExecutionSession(ctx context.Context) error {
	if s.sandbox == nil {
		return nil
	}

	if err := s.sandbox.Process.DeleteSession(ctx, s.sessionId); err != nil {
		slog.Error("failed to delete daytona sandbox session", "err", err, "event_identifier", s.sessionEventId)
		return err
	}

	slog.Debug("Deleted execution session", "event_identifier", s.sessionEventId)
	return nil
}

func (s *Sandbox) ExecuteSessionCommand(
	ctx context.Context,
	command string,
) (map[string]any, error) {
	if s.sandbox == nil {
		return nil, errSandboxNotInitialized
	}

	result, err := s.sandbox.Process.ExecuteSessionCommand(ctx, s.sessionId, command, true, false)

	if err != nil {
		slog.Error("failed to execute session command in daytona sandbox", "err", err, "cmd", command, "event_identifier", s.sessionEventId)
		return nil, err
	}

	return result, nil
}

func (s *Sandbox) StreamSessionCommandLogs(
	ctx context.Context,
	cmdId string,
	stdout chan<- string,
	stderr chan<- string,
) error {
	if s.sandbox == nil {
		return errSandboxNotInitialized
	}

	return s.sandbox.Process.GetSessionCommandLogsStream(ctx, s.sessionId, cmdId, stdout, stderr)
}

func (s *Sandbox) DeleteSandbox(ctx context.Context) error {
	if s.sandbox == nil {
		return nil
	}

	err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "delete sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "delete sandbox"); err != nil {
			return err
		}

		return s.sandbox.DeleteAndWait(ctx, time.Minute*1)
	})

	if err != nil {
		slog.Error("failed to delete sandbox", "err", err, "event_identifier", s.sessionEventId)
	} else {
		s.sandbox = nil
	}

	return err
}

// newUUIDStartingWithLetter generates a secret name that starts with a letter.
// Daytona returns 500 internal server error when creating a secret whose name
// starts with a number, so this is a patch for that behaviour.
func (s *Sandbox) newUUIDStartingWithLetter() string {
	count := 0

	for {
		count++
		u := uuid.New()

		switch u.String()[0] {
		case 'a', 'b', 'c', 'd', 'e', 'f':
			slog.Debug("Deterministic UUID that starts with a letter; patch for daytona ran", "count", count)
			return u.String()
		}
	}
}

func (s *Sandbox) isContextCanceledOrDeadlineExceeded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
