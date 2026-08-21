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

package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jazielguerrero/workdock/application/interfaces"
	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/telemetry"
	"github.com/jazielguerrero/workdock/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	OPENCODE_BIN_PATH           = "/home/daytona/.opencode/bin"
	OPENCODE_WORKSPACE_PATH     = "/home/daytona/workspace"
	OPENCODE_INSTALL_VERSION    = "1.18.11"
	OPENCODE_INSTALL_SCRIPT     = "curl -fsSL https://opencode.ai/install | bash -s -- --version"
	GITHUB_CLI_INSTALL_SCRIPT   = `(type -p wget >/dev/null || (sudo apt update && sudo apt install wget -y)) && sudo mkdir -p -m 755 /etc/apt/keyrings && out=$(mktemp) && wget -nv -O$out https://cli.github.com/packages/githubcli-archive-keyring.gpg && cat $out | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null && sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null && sudo apt update && sudo apt install gh -y`
	LINEAR_ACCESS_TOKEN_ENV_VAR = "LINEAR_ACCESS_TOKEN"
	GITHUB_ACCESS_TOKEN_ENV_VAR = "GH_TOKEN"
	GITHUB_GET_PR_META          = `repo=$(find /home/daytona/workspace -type d -name .git -print -quit)

[ -n "$repo" ] || exit 0

cd "$(dirname "$repo")" || exit 1

output=$(gh pr view --json number,url,headRefName,headRefOid 2>&1)
exit_code=$?

if [ "$exit_code" -eq 0 ]; then
    printf '%s\n' "$output"
elif [[ "$output" == *"no pull requests found for branch"* ]]; then
    exit 0
else
    printf '%s\n' "$output" >&2
    exit "$exit_code"
fi`

	MaxInstallRetries = 3
)

// SecretSpec describes a dynamic secret configured per deployment: its
// plaintext value and the host allowlist Daytona may substitute it into.
// The fields must be exported so yaml unmarshalling can populate them;
// unexported fields are silently skipped, which used to create secrets
// with an empty value and no host allowlist.
type SecretSpec struct {
	Value string   `yaml:"value"`
	Hosts []string `yaml:"hosts"`
}

type ConfigExternal struct {
	Model               string                `yaml:"model"`
	Permission          map[string]any        `yaml:"permission"`
	Secrets             map[string]SecretSpec `yaml:"secrets"`
	Provider            map[string]any        `yaml:"provider"`
	DestroyOnDispose    bool                  `yaml:"destroy_on_dispose"`
	LivenessTimeoutSecs int                   `yaml:"liveness_timeout_seconds"`
	MaxHealthMisses     int                   `yaml:"max_health_misses"`
}

type Config struct {
	ConfigExternal
	Parts        ports.ForHarnessParts
	Sandbox      interfaces.Sandbox
	Session      *types.Session
	SessionEvent *types.SessionEvent
	Prompt       string
	Secrets      map[string]string
}

// OpenCode implements ports.ForSandboxing on top of the daytona
// sandbox concrete. It owns the opencode specifics: install scripts, opencode
// configuration, the prompt upload, and parsing the opencode run output. Each
// instance has its own daytona sandbox, isolating users from each other.
type OpenCode struct {
	serviceName       string
	config            Config
	linearAccessToken string
	githubAccessToken string
	secretIds         []string
	sandboxCreated    bool
	sandboxSecrets    map[string]string
	tracer            trace.Tracer
}

// New builds the harness bound to a session: it validates the
// secrets and creates the daytona sandbox instance.
func New(config Config, serviceName string) (*OpenCode, error) {
	h := &OpenCode{
		config:      config,
		tracer:      otel.Tracer("workdock.opencode"),
		serviceName: serviceName,
	}

	// Setup linear access token
	if h.linearAccessToken = config.Secrets["linearAccessToken"]; h.linearAccessToken == "" {
		err := errors.New("missing linear access token")
		slog.Error("failed to init opencode sandbox", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return nil, err
	}

	// Setup GitHub access token
	h.githubAccessToken = config.Secrets["githubAccessToken"]

	return h, nil
}

func (h *OpenCode) create(ctx context.Context) error {
	var err error

	// Setup linear access token secret
	secretId, secretName, err := h.config.Sandbox.SetSecret(ctx, h.linearAccessToken, []string{"mcp.linear.app"})

	if err != nil {
		return err
	}

	h.secretIds = append(h.secretIds, secretId)
	sandboxSecrets := map[string]string{
		LINEAR_ACCESS_TOKEN_ENV_VAR: secretName,
	}

	// Setup GitHub access token secret
	if h.githubAccessToken != "" {
		secretId, secretName, err := h.config.Sandbox.SetSecret(ctx, h.githubAccessToken, []string{"api.github.com", "github.com"})

		if err != nil {
			return err
		}

		h.secretIds = append(h.secretIds, secretId)
		sandboxSecrets[GITHUB_ACCESS_TOKEN_ENV_VAR] = secretName
	}

	// Setup dynamic secrets
	if h.config.ConfigExternal.Secrets != nil {
		for env, secret := range h.config.ConfigExternal.Secrets {
			secretId, secretName, err := h.config.Sandbox.SetSecret(ctx, secret.Value, secret.Hosts)

			if err != nil {
				return err
			}

			h.secretIds = append(h.secretIds, secretId)
			sandboxSecrets[env] = secretName
		}
	}

	created, err := h.config.Sandbox.GetOrCreateSandbox(ctx, sandboxSecrets, nil)

	if err != nil {
		return err
	}

	h.sandboxCreated = created
	h.sandboxSecrets = sandboxSecrets
	return nil
}

func (h *OpenCode) start(ctx context.Context) error {
	if err := h.config.Sandbox.Start(ctx); err != nil {
		return err
	}

	if !h.sandboxCreated {
		if err := h.config.Sandbox.UpdateExistingSandbox(ctx, h.sandboxSecrets, nil); err != nil {
			return err
		}
	}

	return nil
}

func (h *OpenCode) setup(ctx context.Context) error {
	if h.sandboxCreated {
		if err := h.installOpenCode(ctx); err != nil {
			h.config.Sandbox.DeleteSandbox(context.Background())
			return err
		}

		if err := h.installGitHubCLI(ctx); err != nil {
			h.config.Sandbox.DeleteSandbox(context.Background())
			return err
		}

		if err := h.config.Sandbox.ConfigureGitUser(ctx, "workdock[bot]", "no-reply@workdock.dev"); err != nil {
			h.config.Sandbox.DeleteSandbox(context.Background())
			return err
		}
	}

	if err := h.uploadOpenCodeConfig(ctx); err != nil {
		return err
	}

	if err := h.setupGitHubCredentials(ctx); err != nil {
		return err
	}

	// Upload the prompt, we do it this way because of https://github.com/anomalyco/opencode/issues/38723
	// opencode run hangs waiting for an input (stdin) when running for the first time, but this never
	// happens. Thus, opencode run stays stuck. The work arround is to pipe stdin
	if err := h.config.Sandbox.UploadFile(ctx, []byte(h.config.Prompt), "/tmp/prompt.txt"); err != nil {
		return err
	}

	slog.Debug("Uploaded prompt file", "event_identifier", h.config.SessionEvent.Identifier)

	// Create an execution process since interacting with the AI takes time
	if err := h.config.Sandbox.CreateExecutionSession(ctx); err != nil {
		return err
	}

	slog.Debug("Created execution session", "event_identifier", h.config.SessionEvent.Identifier)
	return nil
}

func (h *OpenCode) Run(ctx context.Context) (*types.SessionEventResult, error) {
	if err := telemetry.SpanErr(ctx, h.tracer, "opencode.create", func(ctx context.Context) error {
		return h.create(ctx)
	}); err != nil {
		return nil, err
	}

	if err := telemetry.SpanErr(ctx, h.tracer, "opencode.start", func(ctx context.Context) error {
		return h.start(ctx)
	}); err != nil {
		return nil, err
	}

	if err := telemetry.SpanErr(ctx, h.tracer, "opencode.setup", func(ctx context.Context) error {
		return h.setup(ctx)
	}); err != nil {
		return nil, err
	}

	// We are ready, happy prompting!
	provider := "unknown"
	model := "unknown"

	if h.config.ConfigExternal.Model != "" {
		before, after, found := strings.Cut(h.config.ConfigExternal.Model, "/")

		if found {
			provider = before
			model = after
		}
	}

	if err := telemetry.SpanErr(ctx, h.tracer, "gen_ai.operation.invoke_agent", func(ctx context.Context) error {
		runOpenCode, err := h.config.Sandbox.ExecuteSessionCommand(
			ctx,
			// --format           format: default (formatted) or json (raw JSON events) [string] [choices: "default", "json"] [default: "default"]
			// --thinking         show thinking blocks
			// -m, --model        model to use in the format of provider/model
			// --dir              directory to run in, path on remote server if attaching
			// -c, --continue     continue the last session
			fmt.Sprintf(`mkdir -p %[1]s && %[2]s/opencode run --format json --thinking --dir %[1]s -c < /tmp/prompt.txt`,
				OPENCODE_WORKSPACE_PATH, OPENCODE_BIN_PATH,
			),
		)

		if err != nil {
			return err
		}

		slog.Debug("Running opencode run", "event_identifier", h.config.SessionEvent.Identifier)

		stdout := make(chan string, 100)
		stderr := make(chan string, 100)
		cmdId, ok := runOpenCode["id"].(string)

		if !ok {
			err := errors.New("invalid pid type")
			slog.Error("failed to execute opencode run in daytona sandbox", "err", err, "event_identifier", h.config.SessionEvent.Identifier)
			return err
		}

		// Process opencode run output
		runCtx, runCancel := context.WithCancel(ctx)
		defer runCancel()

		var unhealthy atomic.Bool

		onUnhealthy := func() {
			if unhealthy.CompareAndSwap(false, true) {
				slog.Error("shutting down unhealthy harness", "event_identifier", h.config.SessionEvent.Identifier)
				runCancel()
			}
		}

		go func() {
			slog.Debug("Streamming opencode run output", "event_identifier", h.config.SessionEvent.Identifier)

			// Channel are closed internally
			if err := h.config.Sandbox.StreamSessionCommandLogs(runCtx, cmdId, stdout, stderr); err != nil {
				slog.Error("failed to stream opencode run output", "err", err, "event_identifier", h.config.SessionEvent.Identifier)
				return
			}
		}()

		output, err := NewOpenCodeOutput(
			h.config.Parts,
			provider,
			model,
			h.linearAccessToken,
			h.config.Session.Identifier,
			stdout,
			stderr,
			time.Duration(int(time.Second)*h.config.LivenessTimeoutSecs),
			h.config.MaxHealthMisses,
			onUnhealthy,
		)

		if err != nil {
			return err
		}

		// .Parse, Blocks until it stdout, stderr are close
		output.Parse(runCtx)

		if unhealthy.Load() {
			return types.ErrHarnessUnhealthy
		}

		return nil
	}, trace.WithAttributes(
		attribute.String("gen_ai.request.model", model),
		attribute.String("gen_ai.provider.name", provider),
		attribute.String("gen_ai.agent.version", OPENCODE_INSTALL_VERSION),
		attribute.Bool("gen_ai.request.stream", true),
	)); err != nil {
		return nil, err
	}

	result := &types.SessionEventResult{}

	if pr := telemetry.Span1(ctx, h.tracer, "opencode.get_pr_metadata", func(ctx context.Context) *types.PullRequest {
		return h.getPRMetadata(ctx)
	}); pr != nil {
		result.PullRequest = pr
	}

	slog.Debug("Terminated session", "event_identifier", h.config.SessionEvent.Identifier)
	return result, nil
}

func (h *OpenCode) Dispose(ctx context.Context) error {
	h.config.Sandbox.DeleteExecutionSession(ctx)

	if h.config.DestroyOnDispose {
		h.config.Sandbox.DeleteSandbox(ctx)
	} else {
		h.config.Sandbox.Shutdown(ctx)
	}

	for _, id := range h.secretIds {
		// Should we do this in parallel for all secretIds?
		h.config.Sandbox.DeleteSecret(ctx, id)
	}

	return nil
}

func (h *OpenCode) Archive(ctx context.Context) error {
	return h.config.Sandbox.Archive(ctx)
}

func (h *OpenCode) installOpenCode(ctx context.Context) error {
	var lastErr error
	for attempt := 1; attempt <= MaxInstallRetries; attempt++ {
		if attempt > 1 {
			slog.Debug("retrying opencode installation", "event_identifier", h.config.SessionEvent.Identifier, "attempt", attempt)
		}
		if _, err := h.config.Sandbox.ExecuteCommand(ctx, fmt.Sprintf("%s %s", OPENCODE_INSTALL_SCRIPT, OPENCODE_INSTALL_VERSION), time.Minute*2); err != nil {
			lastErr = err
			continue
		}
		slog.Debug("Installed opencode", "event_identifier", h.config.SessionEvent.Identifier, "version", OPENCODE_INSTALL_VERSION)
		return nil
	}
	return lastErr
}

func (h *OpenCode) installGitHubCLI(ctx context.Context) error {
	var lastErr error
	for attempt := 1; attempt <= MaxInstallRetries; attempt++ {
		if attempt > 1 {
			slog.Debug("retrying github cli installation", "event_identifier", h.config.SessionEvent.Identifier, "attempt", attempt)
		}
		if _, err := h.config.Sandbox.ExecuteCommand(ctx, GITHUB_CLI_INSTALL_SCRIPT, time.Minute*5); err != nil {
			lastErr = err
			continue
		}
		slog.Debug("Installed gh cli", "event_identifier", h.config.SessionEvent.Identifier)
		return nil
	}
	return lastErr
}

func (h *OpenCode) setupGitHubCredentials(ctx context.Context) error {
	if h.githubAccessToken == "" {
		return nil
	}

	if _, err := h.config.Sandbox.ExecuteCommand(ctx, "gh auth setup-git", time.Minute); err != nil {
		return err
	}

	if _, err := h.config.Sandbox.ExecuteCommand(ctx, "git config --global http.sslCAInfo /etc/daytona/netleash/ca.crt", time.Minute); err != nil {
		return err
	}

	slog.Debug("Configured git credentials", "event_identifier", h.config.SessionEvent.Identifier)
	return nil
}

func (h *OpenCode) uploadOpenCodeConfig(ctx context.Context) error {
	permission := h.config.Permission

	if permission == nil {
		permission = map[string]any{
			"*": "allow",
		}
	}

	permissionStr, err := json.Marshal(permission)

	if err != nil {
		slog.Error("failed to marshal opencode permissions", "err", err, "event_identifier", h.config.SessionEvent.Identifier)
		return err
	}

	providerStr := "{}"

	if h.config.ConfigExternal.Provider != nil {
		d, err := json.Marshal(h.config.ConfigExternal.Provider)

		if err != nil {
			slog.Error("failed to marshal opencode providers", "err", err, "event_identifier", h.config.SessionEvent.Identifier)
			return err
		}

		providerStr = string(d)
	}

	mcp := fmt.Sprintf(`"linear": {
      "type": "remote",
      "url": "https://mcp.linear.app/mcp",
      "enabled": true,
      "oauth": false,
      "headers": {
        "Authorization": "Bearer {env:%s}"
      }
    }`, LINEAR_ACCESS_TOKEN_ENV_VAR)

	openCodeConfig := fmt.Appendf(nil, `
{
  "$schema": "https://opencode.ai/config.json",
  "share": "disabled",
  "autoupdate": false,
  "permission": %s,
  "provider": %s,
  "mcp": {
    %s
  }
}		
	`, string(permissionStr), providerStr, mcp)

	if err := h.config.Sandbox.UploadFile(ctx, openCodeConfig, "/home/daytona/.config/opencode/opencode.json"); err != nil {
		return err
	}

	slog.Debug("Uploaded opencode configuration", "event_identifier", h.config.SessionEvent.Identifier, "config", openCodeConfig)
	return nil
}

func (h *OpenCode) getPRMetadata(ctx context.Context) *types.PullRequest {
	if ctx.Err() != nil {
		return nil
	}

	result, err := h.config.Sandbox.ExecuteCommand(ctx, GITHUB_GET_PR_META, time.Minute*2)

	if err != nil {
		slog.Error("failed to get pr details", "event_identifier", h.config.SessionEvent.Identifier, "err", err)
		return nil
	}

	if result == "" {
		return nil
	}

	slog.Debug("Session PR Metadata", "event_identifier", h.config.SessionEvent.Identifier, "details", result)
	var pr types.PullRequest

	if err := json.Unmarshal([]byte(result), &pr); err != nil {
		slog.Error("failed to unmarshal pull request metadata", "event_identifier", h.config.SessionEvent.Identifier, "err", err)
	}

	return &pr
}
