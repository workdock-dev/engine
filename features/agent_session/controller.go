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

package agent_session

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/workdock-dev/engine/features/agent_session/infrastructure"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
	"github.com/workdock-dev/engine/shared/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

var (
	//go:embed prompts/prompt_templ_work_item.txt
	PromptTemplate_WorkItem string

	//go:embed prompts/prompt_templ_user_prompt.txt
	PromptTemplate_LatestUserComment string

	//go:embed prompts/prompt_templ_pr_review.txt
	PromptTemplate_PullRequestChecksFailed string
)

type AgentHandlerRegistry map[string]interfaces.HandlerAgentSession

type GitHandlerRegistry map[string]interfaces.HandlerGit

type SandboxHandlerRegistry map[string]interfaces.HandlerSandbox

type HarnessHandlerRegistry map[string]interfaces.HandlerHarness

// controller is the pipeline that coordinates the work platform,
// git hosting platform, harness, and sandbox
type controller struct {
	taskSchedulerConfig       types.TaskSchedulerConfig
	livenessProbeConfig       types.HarnessLivenessProbeConfig
	eventBus                  *shared.EventBus
	secretManager             shared.SecretManager
	agentHandlerRegistry      AgentHandlerRegistry
	gitHostingHandlerRegistry GitHandlerRegistry
	sandboxHandlerRegistry    SandboxHandlerRegistry
	harnessHandlerRegistry    HarnessHandlerRegistry
	mcpHandler                interfaces.HandlerMCP
	organization              interfaces.RepositoryOrg
	git                       interfaces.RepositoryGit
	session                   interfaces.Repository
	queue                     interfaces.Queue
	taskScheduler             *infrastructure.TaskScheduler
	tracer                    trace.Tracer
}

// New creates a new AgentSessionRunner controller
// from the given configuration. It subscribes to each one of the
// agent handlers provider to listen for new agent sessions
// and it will run a task scheduler to work the agent session
// asynchronously. Calling this function will block the goroutine
func New(
	ctx context.Context,
	taskSchedulerConfig types.TaskSchedulerConfig,
	livenessProbeConfig types.HarnessLivenessProbeConfig,
	agentHandlerRegistry AgentHandlerRegistry,
	gitHostingHandlerRegistry GitHandlerRegistry,
	sandboxHandlerRegistry SandboxHandlerRegistry,
	harnessHandlerRegistry HarnessHandlerRegistry,
	mcpHandler interfaces.HandlerMCP,
	eventBus *shared.EventBus,
	secretManager shared.SecretManager,
	organization interfaces.RepositoryOrg,
	session interfaces.Repository,
	git interfaces.RepositoryGit,
	queue interfaces.Queue,
) error {
	r := &controller{
		taskSchedulerConfig:       taskSchedulerConfig,
		livenessProbeConfig:       livenessProbeConfig,
		eventBus:                  eventBus,
		secretManager:             secretManager,
		agentHandlerRegistry:      agentHandlerRegistry,
		gitHostingHandlerRegistry: gitHostingHandlerRegistry,
		sandboxHandlerRegistry:    sandboxHandlerRegistry,
		harnessHandlerRegistry:    harnessHandlerRegistry,
		mcpHandler:                mcpHandler,
		organization:              organization,
		session:                   session,
		git:                       git,
		queue:                     queue,
		tracer:                    otel.Tracer("workdock.agent_session"),
	}

	if err := r.init(); err != nil {
		return err
	}

	return r.taskScheduler.Run(ctx)
}

func (c *controller) init() error {
	taskScheduler, err := infrastructure.NewTaskScheduler(
		c.queue,
		c.taskSchedulerConfig,
		c.execute,
	)

	if err != nil {
		return err
	}

	c.taskScheduler = taskScheduler
	c.onAgentSessionPrompt()
	c.onAgentSessionResume()
	c.onAgentSessionStop()
	c.onIssueChange()
	c.onPullRequestCommented()
	c.onGitResetConnection()
	c.onGitCompleteConnection()

	// TODO: Implement/check run failed domain subscription

	return nil
}

// onAgentSessionPrompt Configured domain event for agent session prompt
func (c *controller) onAgentSessionPrompt() {
	c.eventBus.Subscribe(shared.EventType_AgentSessionPrompt, func(ctx context.Context, event shared.DomainEvent) error {
		e, ok := event.(shared.AgentSessionPromptEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_AgentSessionPrompt, event.EventType())
		}

		provider, ok := c.agentHandlerRegistry[e.Provider]

		if !ok {
			return fmt.Errorf("[agent-session] agent session handler not found in registry: %s", e.Provider)
		}

		session, sessionEvent, err := provider.Ingest(event)

		if err != nil {
			return err
		}

		if org, err := c.organization.GetOrganization(ctx, session.OrganizationIdentifier); err != nil {
			return err
		} else if org == nil {
			// TODO: Inform the user they need to initialize their account
			// Received an agent session event from an unknown organization
			return fmt.Errorf("[agent-session] failed to start session, organization %s not found. did you authenticated?", session.OrganizationIdentifier)
		}

		if sess, err := c.session.GetAgentSession(ctx, session.Identifier); err != nil {
			return err
		} else if sess == nil {
			// Session doesn't exist, create it
			if err := c.session.UpsertAgentSession(ctx, session); err != nil {
				return err
			}
		} else {
			// Verify if the agent session event is a duplicate
			sEvent, err := c.session.GetAgentSessionEvent(ctx, sessionEvent.Identifier)

			if err != nil {
				return err
			}

			if sEvent != nil {
				slog.Debug("[agent-session] received duplicated event", "event_identifier", sessionEvent.Identifier)
				return nil
			}

			session = sess
		}

		// Update the session based on the ticket's labels
		credentials, err := provider.GetCredentials(ctx, session.OrganizationIdentifier)

		if err != nil {
			return err
		}

		labels, err := provider.GetLabels(ctx, session.IssueId, credentials)

		if err != nil {
			return err
		}

		repo := ""

		for _, label := range labels {
			if after, ok := strings.CutPrefix(label, "repo="); ok {
				repo = after
				break
			}
		}

		if (session.RepoFullName != nil && *session.RepoFullName != repo) || (session.RepoFullName == nil && repo != "") {
			slog.Debug("[agent-session] update session repo", "event_identifier", sessionEvent.Identifier)
			session.RepoFullName = &repo
			if err := c.session.UpsertAgentSession(ctx, session); err != nil {
				return err
			}
		}

		slog.Debug("[agent-session] created session event for prompt", "event_identifier", sessionEvent.Identifier)
		sessionEvent.Reason = types.AgentSessionEventReason_Prompt
		if err := c.session.CreateSessionEvent(ctx, sessionEvent); err != nil {
			return err
		}

		return nil
	})
}

// onAgentSessionResume Configured domain event for agent session resume
func (c *controller) onAgentSessionResume() {
	c.eventBus.Subscribe(shared.EventType_AgentSessionResume, func(ctx context.Context, event shared.DomainEvent) error {
		e, ok := event.(shared.AgentSessionResumeEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_AgentSessionResume, event.EventType())
		}

		sessionEvent, err := c.session.GetAgentSessionEvent(ctx, e.SessionEventIdentifier)

		if err != nil {
			return err
		}

		if sessionEvent == nil {
			return fmt.Errorf("[agent-session] session event not found %s", e.SessionEventIdentifier)
		}

		return c.session.ResumeSessionEvent(ctx, sessionEvent)
	})
}

// onAgentSessionStop Configured domain event for agent session stop
func (c *controller) onAgentSessionStop() {
	c.eventBus.Subscribe(shared.EventType_AgentSessionStop, func(ctx context.Context, event shared.DomainEvent) error {
		e, ok := event.(shared.AgentSessionStopEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_AgentSessionStop, event.EventType())
		}

		provider, ok := c.agentHandlerRegistry[e.Provider]

		if !ok {
			return fmt.Errorf("[agent-session] agent session handler not found in registry: %s", e.Provider)
		}

		credentials, err := provider.GetCredentials(ctx, e.OrganizationIdentifier)

		if err != nil {
			return err
		}

		slog.Debug("[agent-session] stopped")
		provider.SendResponse(ctx, e.SessionIdentifier, credentials, "Request stopped")
		_, err = c.session.CancelSession(ctx, e.SessionIdentifier, "cancelled by user")

		return err
	})
}

// onIssueChange Configured domain event for agent session
func (c *controller) onIssueChange() {
	c.eventBus.Subscribe(shared.EventType_IssueChange, func(ctx context.Context, event shared.DomainEvent) error {
		_, ok := event.(shared.IssueChangedEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_IssueChange, event.EventType())
		}

		// TODO Implement/Handle issue status changed

		return nil
	})
}

// onPullRequestCommented Configured domain event for pr review comment
func (c *controller) onPullRequestCommented() {
	c.eventBus.Subscribe(shared.EventType_PullRequestCommented, func(ctx context.Context, event shared.DomainEvent) error {
		e, ok := event.(shared.PullRequestCommentedEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_PullRequestCommented, event.EventType())
		}

		sessionEvent, err := c.session.GetAgentSessionEventByGitRef(ctx, e.GitRef, e.RepoFullName)

		if err != nil {
			return err
		}

		if sessionEvent == nil {
			return fmt.Errorf("[agent-session] session event not found: %s@%s", e.GitRef, e.RepoFullName)
		}

		session, err := c.session.GetAgentSession(ctx, sessionEvent.SessionIdentifier)

		if err != nil {
			return err
		}

		if session == nil {
			return fmt.Errorf("[agent-session] session not found: %s", sessionEvent.SessionIdentifier)
		}

		slog.Debug("[agent-session] created session event for pull request comment review")
		if err := c.session.CreateSessionEvent(ctx, &types.SessionEvent{
			SessionIdentifier: session.Identifier,
			Identifier:        uuid.NewV7().String(),
			Payload:           sessionEvent.Payload,
			Seed:              &sessionEvent.Identifier,
			GitRef:            &e.GitRef,
			Reason:            types.AgentSessionEventReason_PRComment,
		}); err != nil {
			return err
		}

		return nil
	})
}

// onGitResetConnection Configured domain event for removing git access
func (c *controller) onGitResetConnection() {
	c.eventBus.Subscribe(shared.EventType_GitResetConnection, func(ctx context.Context, event shared.DomainEvent) error {
		payload, ok := event.(shared.GitResetConnectionEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_GitResetConnection, event.EventType())
		}

		if payload.Delete {
			slog.Debug("[agent-session] deleted git access secret")
			// TODO: Remove this hardcoded value
			if err := c.secretManager.Delete(ctx, "/github/installations", payload.InstallationId); err != nil {
				return err
			}
		}

		slog.Debug("[agent-session] deleted git connection")
		return c.git.ResetConnection(ctx, payload.InstallationId, payload.Repos)
	})
}

// onGitCompleteConnection Configured domain event to complete the git access connection
func (c *controller) onGitCompleteConnection() {
	c.eventBus.Subscribe(shared.EventType_GitCompleteConnection, func(ctx context.Context, event shared.DomainEvent) error {
		payload, ok := event.(shared.GitCompleteConnectionEvent)

		if !ok {
			return fmt.Errorf("[agent-session] expected event type %s got %s", shared.EventType_GitCompleteConnection, event.EventType())
		}

		// this case happens when a repo is being connected to an existent connection
		if payload.Token != nil {
			if err := c.secretManager.Set(ctx, "/github/installations", payload.InstallationId, string(payload.Token)); err != nil {
				slog.Error("[agent-session] failed to store installation access token", "installation_id", payload.InstallationId, "err", err)
				return err
			}
		}

		for _, repo := range payload.Repos {
			connection := &types.GitConnection{
				RepoFullName:   repo,
				Connected:      true,
				InstallationId: &payload.InstallationId,
			}

			slog.Debug("[agent-session] git connection completed", "repo", repo)
			if err := c.git.UpsertConnection(ctx, connection); err != nil {
				return err
			}

			// When set, it means this session event was paused until the user
			// granted git access. Now we need to continue it
			if connection.SessionEventIdentifier != nil {
				c.eventBus.Publish(ctx, shared.AgentSessionResumeEvent{
					SessionEventIdentifier: *connection.SessionEventIdentifier,
				})
			}
		}

		return nil
	})
}

// execute provisions and coordinates all the components to successfully run the
// agent session's request based on a scheduled job
func (c *controller) execute(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
	sessionEvent, err := telemetry.Span(ctx, c.tracer, "session.get_event", func(ctx context.Context) (*types.SessionEvent, error) {
		return c.session.GetAgentSessionEvent(ctx, job.SessionEventIdentifier)
	})

	if err != nil || sessionEvent == nil {
		return types.EventJobStatus_Failed, err
	}

	session, err := telemetry.Span(ctx, c.tracer, "session.get", func(ctx context.Context) (*types.Session, error) {
		return c.session.GetAgentSession(ctx, sessionEvent.SessionIdentifier)
	})

	if err != nil || session == nil {
		return types.EventJobStatus_Failed, err
	}

	// *-------------------------------------------------------------------------*
	// * Get provider: work platform, git hosting, harness, sandbox              *
	// *-------------------------------------------------------------------------*
	slog.Debug("[agent-session] get handlers")
	agentHandler, gitHandler, sandboxHandler, harnessHandler, err := c.getHandlers(session)

	if err != nil {
		return types.EventJobStatus_Failed, err
	}

	// *-------------------------------------------------------------------------*
	// * Get providers credentials                                               *
	// *-------------------------------------------------------------------------*
	slog.Debug("[agent-session] get agent handler credentials")
	agentHandlerCredential, err := telemetry.Span(ctx, c.tracer, "session.get_agent_handler_credentials", func(ctx context.Context) (string, error) {
		return agentHandler.GetCredentials(ctx, session.OrganizationIdentifier)
	})

	if err != nil {
		return types.EventJobStatus_Failed, err
	}

	// DO NOT REMOVE!
	// Can start communicating, this let's the user know the request is being process.
	slog.Debug("[agent-session] send thought event")
	agentHandler.SendThought(ctx, session.Identifier, agentHandlerCredential, "")

	// *-------------------------------------------------------------------------*
	// * Create prompt                                                           *
	// *-------------------------------------------------------------------------*
	slog.Debug("[agent-session] get prompt")
	prompt, err := c.getPrompt(ctx, agentHandler, session, sessionEvent)

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		return types.EventJobStatus_Failed, err
	}

	// *-------------------------------------------------------------------------*
	// * Verify git access                                                       *
	// *-------------------------------------------------------------------------*
	slog.Debug("[agent-session] verify git access")
	gitAccess, err := c.verifyGitAccess(ctx, agentHandler, agentHandlerCredential, gitHandler, session, sessionEvent)

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		return types.EventJobStatus_Failed, err
	}

	// Cannot continue, requires git access
	if session.RepoFullName != nil && gitAccess == nil {
		slog.Debug("[agent-session] git acess required")
		return types.EventJobStatus_AwaitingAction, nil
	}

	// *-------------------------------------------------------------------------*
	// * Configure, start sandbox and run harness                                *
	// *-------------------------------------------------------------------------*
	slog.Debug("[agent-session] sandbox start")
	stdout, stderr, shutdown, err := c.sandbox(
		ctx,
		gitHandler,
		harnessHandler,
		sandboxHandler,
		gitAccess,
		prompt,
		session,
		sessionEvent,
	)

	defer func() {
		if shutdown != nil {
			slog.Debug("[agent-session] sandbox shutdown")
			result := shutdown(context.Background())

			// *-------------------------------------------------------------------------*
			// * Parse exit command
			// *-------------------------------------------------------------------------*
			pr := gitHandler.ParseLatestChangesResult(result)

			if pr != nil {
				slog.Debug("[agent-session] update session result")
				sessionEvent.Result = &types.SessionEventResult{
					PullRequest: pr,
				}
				sessionEvent.GitRef = &pr.HeadRefName
				c.session.UpdateSessionEventResult(ctx, sessionEvent)
			}

			// DO NOT REMOVE!
			// Some times the harness bug out and doesn't send the response, causing the
			// UI/UX Chat to stay in a thinking state; thus, with this, we guranteed to
			// send the finish signal
			slog.Debug("[agent-session] send response event")
			agentHandler.SendResponse(ctx, session.Identifier, agentHandlerCredential, "")
		}
	}()

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		return types.EventJobStatus_Failed, err
	}

	// *-------------------------------------------------------------------------*
	// * Process harness output, blocks until work is completed or an error      *
	// *-------------------------------------------------------------------------*
	slog.Debug("[agent-session] harness running")
	if err := c.harness(
		ctx,
		stdout,
		stderr,
		agentHandler,
		agentHandlerCredential,
		harnessHandler,
		session,
		sessionEvent,
	); err != nil {
		return types.EventJobStatus_Failed, err
	}

	return types.EventJobStatus_Succeeded, nil
}

func (c *controller) getHandlers(session *types.Session) (
	interfaces.HandlerAgentSession,
	interfaces.HandlerGit,
	interfaces.HandlerSandbox,
	interfaces.HandlerHarness,
	error,
) {
	agentHandler, ok := c.agentHandlerRegistry[string(session.Provider)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("[agent-session] provider %s not configured for agent session run", session.Provider)
	}

	// TODO: Make this dynamic
	gitHandler, ok := c.gitHostingHandlerRegistry[string(shared.PlatformProvider_GitHub)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("[agent-session] provider %s not configured for git hosting handler", shared.PlatformProvider_GitHub)
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := c.sandboxHandlerRegistry[string(shared.PlatformProvider_Daytona)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("[agent-session] provider %s not configured for sandbox handler", shared.PlatformProvider_Daytona)
	}

	// TODO: Make this dynamic
	harnessHandler, ok := c.harnessHandlerRegistry[string(shared.HarnessProvider_OpenCode)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("[agent-session] provider %s not configured for harness handler", shared.HarnessProvider_OpenCode)
	}

	return agentHandler, gitHandler, sandboxHandler, harnessHandler, nil
}

func (c *controller) getPrompt(
	ctx context.Context,
	agentHandler interfaces.HandlerAgentSession,
	session *types.Session,
	sessionEvent *types.SessionEvent,
) (string, error) {
	promptContext, err := telemetry.Span(ctx, c.tracer, "session.get_prompt_context", func(ctx context.Context) (*interfaces.PromptContext, error) {
		return agentHandler.GetPromptContext(sessionEvent)
	})

	if err != nil {
		return "", err
	}

	prompt, err := telemetry.Span(ctx, c.tracer, "session.create_prompt", func(ctx context.Context) (string, error) {
		return c.createPrompt(session, sessionEvent, promptContext)
	})

	if err != nil {
		return "", err
	}

	return prompt, nil
}

func (c *controller) createPrompt(
	session *types.Session,
	sessionEvent *types.SessionEvent,
	promptContext *interfaces.PromptContext,
) (string, error) {
	repo := ""

	if session != nil && session.RepoFullName != nil {
		repo = *session.RepoFullName
	}

	p := strings.TrimSpace(fmt.Sprintf(PromptTemplate_WorkItem,
		promptContext.Issue.Title,
		promptContext.Issue.Identifier,
		repo,
		promptContext.Issue.Description,
		promptContext.Prompt,
	))

	if sessionEvent != nil && sessionEvent.GitRef != nil && sessionEvent.Seed != nil {
		if sessionEvent.Reason == types.AgentSessionEventReason_CheckRun {
			p += fmt.Sprintf(
				PromptTemplate_PullRequestChecksFailed,
				"The pull request checks have failed. Review the check failures, fix the issues, and ensure all checks pass before the pull request can be merged.",
			)
		} else {
			p += fmt.Sprintf(
				PromptTemplate_LatestUserComment,
				"There are review comments on the pull request. Retrieve all review comments and address each one that is applicable to the current implementation. Make the necessary code changes, verify the changes, and ensure the pull request is ready for review again.",
			)
		}
	} else if promptContext.Context != nil {
		p += fmt.Sprintf(PromptTemplate_LatestUserComment, *promptContext.Context)
	}

	return p, nil
}

func (c *controller) verifyGitAccess(
	ctx context.Context,
	agentHandler interfaces.HandlerAgentSession,
	agentHandlerCredential string,
	gitHandler interfaces.HandlerGit,
	session *types.Session,
	sessionEvent *types.SessionEvent,
) (*interfaces.GitAccess, error) {
	// no repo, no access required
	if session.RepoFullName == nil {
		return nil, nil
	}

	connection, err := telemetry.Span(ctx, c.tracer, "session.get_git_connection", func(ctx context.Context) (*types.GitConnection, error) {
		return c.git.GetConnection(ctx, *session.RepoFullName)
	})

	if err != nil {
		return nil, err
	}

	// Request Git installation when repo access is not yet granted.
	// This applies to both public and private repositories — write operations
	// (push branches, create PRs) require an authenticated Git.
	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		if err := telemetry.SpanErr(ctx, c.tracer, "session.upsert_git_connection", func(ctx context.Context) error {
			return c.git.UpsertConnection(
				ctx, &types.GitConnection{
					SessionEventIdentifier: &sessionEvent.Identifier,
					RepoFullName:           *session.RepoFullName,
					Connected:              false,
					InstallationId:         nil,
				},
			)
		}); err != nil {
			return nil, err
		}

		if err := agentHandler.SendGitConnectionRequest(
			ctx,
			session.Identifier,
			agentHandlerCredential,
			string(shared.PlatformProvider_GitHub),
			gitHandler.GetInstallationUrl(),
		); err != nil {
			return nil, err
		}

		// Cannot continue because it requires github access
		return nil, nil
	}

	access, err := gitHandler.GetGitAccess(ctx, connection)

	if err != nil {
		if errors.Is(err, shared.ErrGitHubInstallationUnavailable) {
			if err := c.git.ResetConnection(ctx, *connection.InstallationId, []string{*session.RepoFullName}); err != nil {
				return nil, err
			}

			// TODO: Fix/hardcoded secret path
			if err := c.secretManager.Delete(ctx, "/github/installations", *connection.InstallationId); err != nil {
				return nil, err
			}

			if err := agentHandler.SendGitConnectionRequest(
				ctx,
				session.Identifier,
				agentHandlerCredential,
				string(shared.PlatformProvider_GitHub),
				gitHandler.GetInstallationUrl(),
			); err != nil {
				return nil, err
			}

			return nil, nil
		}

		return nil, err
	}

	return access, nil
}

func (c *controller) sandbox(
	ctx context.Context,
	gitHandler interfaces.HandlerGit,
	harnessHandler interfaces.HandlerHarness,
	sandboxHandler interfaces.HandlerSandbox,
	gitAccess *interfaces.GitAccess,
	prompt string,
	session *types.Session,
	sessionEvent *types.SessionEvent,
) (
	<-chan string,
	<-chan string,
	func(ctx context.Context) string,
	error,
) {
	stdout := make(chan string, 100)
	stderr := make(chan string, 100)
	secrets := make([]interfaces.SandboxSecret, 0)
	fileUploads := make(map[string][]byte)
	harnessConfig := interfaces.HarnessConfig{}

	// TODO: dynamicly inject harness configuration

	if c.mcpHandler != nil {
		harnessConfig.Mcps = c.mcpHandler.GetMCPList()

		for _, mcp := range harnessConfig.Mcps {
			secrets = append(secrets, interfaces.SandboxSecret{
				Name:  mcp.AuthKey,
				Value: mcp.AuthSecret,
				Hosts: mcp.Hosts,
			})
		}
	}

	if gitAccess != nil && gitAccess.Granted {
		secrets = append(secrets, interfaces.SandboxSecret{
			Name:  gitAccess.EnvVarName,
			Value: gitAccess.Secret,
			Hosts: gitAccess.Hosts,
		})
	}

	// Get prompt file and prepare it for upload
	promptFilePath, promptData := harnessHandler.GetPromptFile(prompt)
	fileUploads[promptFilePath] = promptData

	// Get harness configuration and prepare it for upload
	if file, data, err := harnessHandler.GetConfigFile(harnessConfig); err != nil {
		return nil, nil, nil, err
	} else {
		fileUploads[file] = data
	}

	shutdown, err := sandboxHandler.Run(
		ctx,
		&interfaces.SandboxConfig{
			AutoStopInterval: 5, // 5 minutes
			Session:          session,
			SessionEvent:     sessionEvent,
			CommandsWhenCreated: slices.Concat(
				gitHandler.GetConfigurationCommands(),
				harnessHandler.GetConfigurationCommands(),
			),
			Commands: slices.Concat(
				gitHandler.GetCommands(),
				harnessHandler.GetCommands(),
			),
			ExitCommand:    gitHandler.GetLatestChangesComand(),
			FileUploads:    fileUploads,
			Secrets:        secrets,
			GitName:        "workdock[bot]",
			GitEmail:       "no-reply@workdock.dev",
			HarnessCommand: harnessHandler.RunCommand(),
		},
		stdout,
		stderr,
	)

	return stdout, stderr, shutdown, err
}

func (c *controller) harness(
	ctx context.Context,
	stdout <-chan string,
	stderr <-chan string,
	agentHandler interfaces.HandlerAgentSession,
	agentHandlerCredential string,
	harnessHandler interfaces.HandlerHarness,
	session *types.Session,
	sessionEvent *types.SessionEvent,
) error {
	var messageSpan trace.Span

	// Start and end messages tracks the span of each harness message, allow us
	// to detect when the time it took the harness to generate each message. Helpful
	// to verify slow reponses
	startMessage := func() {
		if messageSpan != nil {
			return
		}

		_, messageSpan = c.tracer.Start(ctx, "opencode.output.message")
	}

	endMessage := func() {
		if messageSpan == nil {
			return
		}

		messageSpan.End()
		messageSpan = nil
	}

	defer endMessage()

	var out []byte
	var stdErrBuilder strings.Builder
	var missed atomic.Int64

	wg, ctx := errgroup.WithContext(ctx)
	missed.Store(-1)
	part := make(chan []byte, 100)
	done := make(chan struct{})

	heartbeat := func() {
		missed.Store(-1)
	}

	// *-------------------------------------------------------------------------*
	// * Runs the harness liveness prove                                         *
	// *-------------------------------------------------------------------------*

	if c.livenessProbeConfig.MaxMisses > 0 && c.livenessProbeConfig.PeriodSeconds > 0 {
		wg.Go(func() error {
			ticker := time.NewTicker(time.Second * time.Duration(c.livenessProbeConfig.PeriodSeconds))
			defer ticker.Stop()

			for {
				select {
				case <-done:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					m := missed.Add(1)

					if m == 0 {
						continue
					}

					slog.Warn("harness health check missed",
						"event_identifier", sessionEvent.Identifier,
						"missed", m,
						"max", c.livenessProbeConfig.MaxMisses,
					)

					if m >= c.livenessProbeConfig.MaxMisses {
						slog.Error("harness declared unhealthy",
							"event_identifier", sessionEvent.Identifier,
							"missed_checks", m,
						)

						return shared.ErrHarnessUnhealthy
					}
				}
			}
		})
	}

	// *-------------------------------------------------------------------------*
	// * Parse the outputs base on the harness parse and forwards it to the      *
	// * agent session                                                           *
	// *-------------------------------------------------------------------------*

	wg.Go(func() error {
		return harnessHandler.Parse(
			ctx,
			part,
			sessionEvent.Identifier,

			// sendThought sends the thinking state to the provider
			func(ctx context.Context, text string) error {
				return agentHandler.SendThought(ctx, session.Identifier, agentHandlerCredential, text)
			},

			// sendResponse sends text chunks/parts to the provider
			func(ctx context.Context, text string) error {
				return agentHandler.SendResponse(ctx, session.Identifier, agentHandlerCredential, text)
			},

			// sendACtion sends an action required to be executed by the user
			func(ctx context.Context, action types.AgentAction) error {
				return agentHandler.SendAction(ctx, session.Identifier, agentHandlerCredential, action)
			},

			// sendElicitation sends a collection of questions to be answer by the user
			func(ctx context.Context, elicitation types.AgentElicitation) error {
				return agentHandler.SendElicitation(ctx, session.Identifier, agentHandlerCredential, elicitation)
			},

			// sendServerInternalError sends a geneeric server internal error
			func(ctx context.Context) error {
				return agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
			},
		)
	})

	// *-------------------------------------------------------------------------*
	// * Get the stdout and stderr from the running harness withing the sandbox  *
	// * and forwards it to the harness parser                                   *
	// *-------------------------------------------------------------------------*

	wg.Go(func() error {
		defer close(part)
		defer close(done)

		for stdout != nil || stderr != nil {
			select {
			case chunk, ok := <-stdout:
				if !ok {
					stdout = nil

					if len(out) > 0 {
						startMessage()

						if json.Valid(out) {
							select {
							case part <- out:
							case <-ctx.Done():
								return ctx.Err()
							}
						}

						out = nil
						endMessage()
					}

					continue
				}

				// We received the first chunk of a new message.
				heartbeat()
				startMessage()
				out = append(out, chunk...)

				for {
					i := bytes.IndexByte(out, '\n')

					if i == -1 {
						break
					}

					// The complete message has now been received.
					message := out[:i]

					if json.Valid(message) {
						select {
						case part <- message:
						case <-ctx.Done():
							return ctx.Err()
						}
					}

					out = out[i+1:]
					endMessage()

					// There may already be another complete/partial message
					// in pending, so start measuring the next one.
					if len(out) > 0 {
						startMessage()
					}
				}

			case chunk, ok := <-stderr:
				if !ok {
					stderr = nil
					continue
				}

				heartbeat()

				if _, err := stdErrBuilder.Write([]byte(chunk)); err != nil {
					slog.Error("[agent-session] failed to write to stderr builder", "err", err, "event_identifier", sessionEvent.Identifier)
				}

			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})

	if err := wg.Wait(); err != nil {
		if errors.Is(err, shared.ErrHarnessUnhealthy) {
			if str := stdErrBuilder.String(); str != "" {
				agentHandler.SendResponse(context.WithoutCancel(ctx), session.Identifier, agentHandlerCredential, str)
				slog.Error("[agent-session] harness stderr", "event_identifier", sessionEvent.Identifier, "err", str)
			}
		}

		return err
	}

	if str := stdErrBuilder.String(); str != "" {
		agentHandler.SendResponse(context.WithoutCancel(ctx), session.Identifier, agentHandlerCredential, str)
		slog.Error("[agent-session] harness stderr", "event_identifier", sessionEvent.Identifier, "err", str)
	}

	return nil
}
