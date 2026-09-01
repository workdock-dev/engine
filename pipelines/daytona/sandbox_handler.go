package daytona

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	sdkerrors "github.com/daytona/clients/sdk-go/pkg/errors"
	"github.com/daytona/clients/sdk-go/pkg/options"
	"github.com/daytona/clients/sdk-go/pkg/types"
	"github.com/workdock-dev/engine/pipelines/runners"
)

type SandboxHandler struct {
	apiKey string
	apiUrl string
	target string
}

func NewSandboxHandler(
	apiKey,
	apiUrl,
	target string,
) runners.SandboxHandler {
	return &SandboxHandler{
		apiKey: apiKey,
		apiUrl: apiUrl,
		target: target,
	}
}

func (h *SandboxHandler) Run(
	ctx context.Context,
	config *runners.SandboxConfig,
	stdout chan<- string,
	stderr chan<- string,
) (func(ctx context.Context) string, error) {
	target := h.target

	if target == "" {
		target = "us"
	}

	// *-------------------------------------------------------------------------*
	// * Create daytona client                                                   *
	// *-------------------------------------------------------------------------*
	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey:     h.apiKey,
		APIUrl:     h.apiUrl,
		Target:     target,
		HTTPClient: daytonaHTTPClient,
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
			exitCode, result, err := h.ExecuteCommand(
				ctx,
				sandbox,
				config,
				config.ExitCommand,
				time.Minute*2,
			)

			if err != nil {
				slog.Error("failed to run exit command", "event_identifier", config.SessionEvent.Identifier, "err", err)
			} else if exitCode != 0 {
				slog.Error("failed to run exit command", "event_identifier", config.SessionEvent.Identifier, "err", errors.New("non-zero exit code"), "exitCode", exitCode)
			} else if result != "" {
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
			slog.Error("failed to close daytona client", "err", err, "event_identifier", config.SessionEvent.Identifier)
		}

		return out
	}

	// *-------------------------------------------------------------------------*
	// * Create secrets using daytona's secret API these secrets are never       *
	// * written into the sandbox                                                *
	// *-------------------------------------------------------------------------*
	secrets := make(map[string]string)

	for _, secret := range config.Secrets {
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
	sandbox, created, err = h.GetOrCreateSandbox(ctx, client, config, secrets)

	if err != nil {
		return shutdown, err
	}

	// *-------------------------------------------------------------------------*
	// * Starts the sandbox                                                      *
	// *-------------------------------------------------------------------------*
	if err := h.Start(ctx, sandbox, config); err != nil {
		return shutdown, err
	}

	// *-------------------------------------------------------------------------*
	// * If this sandbox was just created, install any additional dependency     *
	// *-------------------------------------------------------------------------*
	if created {
		for _, cmd := range config.CommandsWhenCreated {
			if exitCode, _, err := h.ExecuteCommand(ctx, sandbox, config, cmd, time.Minute*1); err != nil {
				deleting = true
				h.DeleteSandbox(context.Background(), sandbox, config)
				return shutdown, err
			} else if exitCode != 0 {
				deleting = true
				slog.Error("command execution return non-zero exit code", "cmd", cmd, "event_identifier", config.SessionEvent.Identifier)
				h.DeleteSandbox(context.Background(), sandbox, config)
				return shutdown, errors.New("command execution return non-zero exit code")
			}
		}
	}

	// *-------------------------------------------------------------------------*
	// * Configure the git user, we call it always in case user updated it       *
	// *-------------------------------------------------------------------------*
	if err := h.ConfigureGitUser(ctx, sandbox, config); err != nil {
		return shutdown, err
	}

	// *-------------------------------------------------------------------------*
	// * Upload any file required for work                                       *
	// *-------------------------------------------------------------------------*
	for path, data := range config.FileUploads {
		if err := h.UploadFile(ctx, sandbox, config, data, path); err != nil {
			return shutdown, err
		}
	}

	// *-------------------------------------------------------------------------*
	// * Create an execution process since interacting with the AI takes time    *
	// *-------------------------------------------------------------------------*
	if err := h.CreateExecutionSession(ctx, sandbox, config); err != nil {
		return shutdown, err
	}

	execSessionCreated = true
	result, err := h.ExecuteSessionCommand(ctx, sandbox, config)

	if err != nil {
		return shutdown, err
	}

	cmdId, ok := result["id"].(string)

	if !ok {
		err := errors.New("invalid pid type")
		slog.Error("failed to execute session command in daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return shutdown, err
	}

	go func() {
		slog.Debug("Streamming session output", "event_identifier", config.SessionEvent.Identifier)

		// Channel are closed internally
		listening = true
		if err := h.StreamSessionCommandLogs(ctx, sandbox, config, cmdId, stdout, stderr); err != nil {
			slog.Error("failed to stream session output", "err", err, "event_identifier", config.SessionEvent.Identifier)
			return
		}
	}()

	return shutdown, nil
}

func (h *SandboxHandler) Archive(ctx context.Context, config *runners.SandboxConfig) error {
	target := h.target

	if target == "" {
		target = "us"
	}

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey:     h.apiKey,
		APIUrl:     h.apiUrl,
		Target:     target,
		HTTPClient: daytonaHTTPClient,
	})

	if err != nil {
		slog.Error("failed to create daytona client", "err", err)
		return err
	}

	defer client.Close(context.Background())

	sandbox, err := retryRateLimited(ctx, throttlerAuthenticated, "get sandbox for archive", func() (*daytona.Sandbox, error) {
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

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "archive sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "archive sandbox"); err != nil {
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

func (h *SandboxHandler) GetOrCreateSandbox(ctx context.Context, client *daytona.Client, config *runners.SandboxConfig, secrets map[string]string) (*daytona.Sandbox, bool, error) {
	sandbox, err := retryRateLimited(ctx, throttlerAuthenticated, "get sandbox", func() (*daytona.Sandbox, error) {
		return client.Get(ctx, config.Session.Identifier)
	})

	if err != nil {
		if h.isContextCanceledOrDeadlineExceeded(err) {
			return nil, false, err
		}

		if !errors.Is(err, sdkerrors.ErrNotFound) {
			slog.Error("failed to get daytona sandbox for the given session", "err", err, "event_identifier", config.SessionEvent.Identifier)
			return nil, false, err
		}
	}

	if sandbox == nil {
		// Sandboxes have 1 vCPU, 1GB RAM, and 3GiB disk by default
		sdb, err := func() (*daytona.Sandbox, error) {
			if err := preflight(ctx, throttlerSandboxCreate, "create sandbox"); err != nil {
				return nil, err
			}

			return retryRateLimited(ctx, throttlerSandboxCreate, "create sandbox", func() (*daytona.Sandbox, error) {
				return client.Create(ctx, types.SnapshotParams{
					Snapshot:         "daytona-small",
					Name:             config.Session.Identifier,
					Public:           false,
					AutoStopInterval: new(config.AutoStopInterval),
					Labels: map[string]string{
						"session_event_identifier": config.SessionEvent.Identifier,
					},
					Secrets: secrets,
				})
			})
		}()

		if err != nil {
			slog.Error("failed to create daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
			return nil, false, err
		}

		slog.Debug("created daytona sandbox", "event_identifier", config.SessionEvent.Identifier)
		return sdb, true, nil
	}

	slog.Debug("reusing daytona sandbox", "event_identifier", config.SessionEvent.Identifier)
	return sandbox, false, nil
}

// SetSecret creates a secret and returns its id and name.
func (h *SandboxHandler) SetSecret(ctx context.Context, client *daytona.Client, config *runners.SandboxConfig, secretValue string, hosts []string) (string, string, error) {
	secretName := h.newUUIDStartingWithLetter()
	secret, err := retryRateLimited(ctx, throttlerAuthenticated, "create secret", func() (*types.Secret, error) {
		return client.Secret.Create(ctx, &types.CreateSecretParams{
			Name:  secretName,
			Value: secretValue,
			Hosts: hosts,
		})
	})

	if err != nil {
		slog.Error("failed to create secret", "err", err, "secret", secretName, "event_identifier", config.SessionEvent.Identifier)
		return "", "", err
	}

	slog.Debug("Secret set", "name", secretName, "secret_id", secret.ID, "hosts", hosts, "event_identifier", config.SessionEvent.Identifier)
	return secret.ID, secretName, nil
}

func (h *SandboxHandler) DeleteSecret(ctx context.Context, client *daytona.Client, config *runners.SandboxConfig, secretId string) error {
	err := retryRateLimitedVoid(ctx, throttlerAuthenticated, "delete secret", func() error {
		return client.Secret.Delete(ctx, secretId)
	})

	if err != nil {
		slog.Error("failed to delete secret", "secret_id", secretId, "err", err, "event_identifier", config.SessionEvent.Identifier)
	} else {
		slog.Debug("Deleted secret", "secret_id", secretId, "event_identifier", config.SessionEvent.Identifier)
	}

	return err
}

func (h *SandboxHandler) Start(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig) error {
	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "start sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "start sandbox"); err != nil {
			return err
		}

		return sandbox.Start(ctx)
	}); err != nil {
		if h.isContextCanceledOrDeadlineExceeded(err) {
			return err
		}

		slog.Error("failed to start daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	slog.Debug("Started daytona sandbox", "event_identifier", config.SessionEvent.Identifier)
	return nil
}

// UpdateExistingSandbox updates the secrets and environment variables of an
// existing, running sandbox. This must be called after Start because the
// Daytona API needs the container's IP address to apply updates, and the IP
// is only available once the sandbox is running.
func (h *SandboxHandler) UpdateExistingSandbox(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig, secrets, envVars map[string]string) error {
	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "update secrets", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "update secrets"); err != nil {
			return err
		}

		return sandbox.UpdateSecrets(ctx, secrets)
	}); err != nil {
		slog.Error("failed to update daytona sandbox secrets", "event_identifier", config.SessionEvent.Identifier, "err", err)
		return err
	}

	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "update env vars", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "update env vars"); err != nil {
			return err
		}

		return sandbox.UpdateEnv(ctx, envVars, nil)
	}); err != nil {
		slog.Error("failed to update daytona sandbox env vars", "event_identifier", config.SessionEvent.Identifier, "err", err)
		return err
	}

	return nil
}

func (h *SandboxHandler) Shutdown(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig) error {
	if err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "stop sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "stop sandbox"); err != nil {
			return err
		}

		return sandbox.Stop(ctx)
	}); err != nil {
		slog.Error("failed to stop daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	slog.Debug("Stopped daytona sandbox", "event_identifier", config.SessionEvent.Identifier)
	return nil
}

func (h *SandboxHandler) UploadFile(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig, data []byte, path string) error {
	if err := sandbox.FileSystem.UploadFile(ctx, data, path); err != nil {
		slog.Error("failed to upload file to daytona sandbox", "err", err, "path", path, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	slog.Debug("Uploaded file to daytona sandbox", "path", path, "event_identifier", config.SessionEvent.Identifier)
	return nil
}

func (h *SandboxHandler) ConfigureGitUser(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig) error {
	if err := sandbox.Git.ConfigureUser(ctx, config.GitName, config.GitEmail); err != nil {
		slog.Error("failed to configure git user in daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	slog.Debug("Configured git user in daytona sandbox", "name", config.GitName, "event_identifier", config.SessionEvent.Identifier)
	return nil
}

// ExecuteCommand runs a command in the sandbox and fails when the execution
// result is missing or its exit code is non-zero.
func (h *SandboxHandler) ExecuteCommand(
	ctx context.Context,
	sandbox *daytona.Sandbox,
	config *runners.SandboxConfig,
	command string,
	timeout time.Duration,
) (int, string, error) {
	exec, err := sandbox.Process.ExecuteCommand(ctx, command, options.WithExecuteTimeout(timeout))

	if err != nil {
		slog.Error("failed to execute command in daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return -1, "", err
	}

	if exec == nil {
		err := errors.New("exec result is nil")
		slog.Error("failed to execute command in daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return -1, "", err
	}

	if exec.ExitCode != 0 {
		err := fmt.Errorf("exec result is non-zero: exit code %d", exec.ExitCode)
		slog.Error("failed to execute command in daytona sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier, "result", exec.Result)
		return -1, "", err
	}

	return exec.ExitCode, exec.Result, nil
}

func (h *SandboxHandler) CreateExecutionSession(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig) error {
	if err := sandbox.Process.CreateSession(ctx, config.Session.Identifier); err != nil {
		slog.Error("failed to create daytona sandbox session", "err", err)
		return err
	}

	slog.Debug("Created execution session", "session_id", config.Session.Identifier)
	return nil
}

func (h *SandboxHandler) DeleteExecutionSession(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig) error {
	if err := sandbox.Process.DeleteSession(ctx, config.Session.Identifier); err != nil {
		slog.Error("failed to delete daytona sandbox session", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	slog.Debug("Deleted execution session", "event_identifier", config.SessionEvent.Identifier)
	return nil
}

func (h *SandboxHandler) ExecuteSessionCommand(
	ctx context.Context,
	sandbox *daytona.Sandbox,
	config *runners.SandboxConfig,
) (map[string]any, error) {
	result, err := sandbox.Process.ExecuteSessionCommand(ctx, config.Session.Identifier, config.HarnessCommand, true, false)

	if err != nil {
		slog.Error("failed to execute session command in daytona sandbox", "err", err, "cmd", config.HarnessCommand, "event_identifier", config.SessionEvent.Identifier)
		return nil, err
	}

	return result, nil
}

func (h *SandboxHandler) StreamSessionCommandLogs(
	ctx context.Context,
	sandbox *daytona.Sandbox,
	config *runners.SandboxConfig,
	cmdId string,
	stdout chan<- string,
	stderr chan<- string,
) error {
	return sandbox.Process.GetSessionCommandLogsStream(ctx, config.Session.Identifier, cmdId, stdout, stderr)
}

func (h *SandboxHandler) DeleteSandbox(ctx context.Context, sandbox *daytona.Sandbox, config *runners.SandboxConfig) error {
	err := retryRateLimitedVoid(ctx, throttlerSandboxLifecycle, "delete sandbox", func() error {
		if err := preflight(ctx, throttlerSandboxLifecycle, "delete sandbox"); err != nil {
			return err
		}

		return sandbox.DeleteAndWait(ctx, time.Minute*1)
	})

	if err != nil {
		slog.Error("failed to delete sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
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
			slog.Debug("Deterministic UUID that starts with a letter; patch for daytona ran", "count", count)
			return u.String()
		}
	}
}

func (h *SandboxHandler) isContextCanceledOrDeadlineExceeded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
