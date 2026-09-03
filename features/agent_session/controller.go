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
	"time"

	"github.com/workdock-dev/engine/features/agent_session/infrastructure"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
	"github.com/workdock-dev/engine/shared/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var (
	//go:embed prompts/prompt_templ_work_item.txt
	PromptTemplate_WorkItem string

	//go:embed prompts/prompt_templ_user_prompt.txt
	PromptTemplate_LatestUserComment string

	//go:embed prompts/prompt_templ_pr_review.txt
	PromptTemplate_PullRequestChecksFailed string
)

type AgentHandlerRegistry map[string]interfaces.AgentSessionHandler

type GitHandlerRegistry map[string]interfaces.GitHandler

type SandboxHandlerRegistry map[string]interfaces.SandboxHandler

type HarnessHandlerRegistry map[string]interfaces.HarnessHandler

// controller is the pipeline that coordinates the work platform,
// git hosting platform, harness, and sandbox
type controller struct {
	taskSchedulerConfig       types.TaskSchedulerConfig
	eventBus                  shared.ForEventBus
	secretManager             shared.ForSecrets
	agentHandlerRegistry      AgentHandlerRegistry
	gitHostingHandlerRegistry GitHandlerRegistry
	sandboxHandlerRegistry    SandboxHandlerRegistry
	harnessHandlerRegistry    HarnessHandlerRegistry
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
	agentHandlerRegistry AgentHandlerRegistry,
	gitHostingHandlerRegistry GitHandlerRegistry,
	sandboxHandlerRegistry SandboxHandlerRegistry,
	harnessHandlerRegistry HarnessHandlerRegistry,
	eventBus shared.ForEventBus,
	secretManager shared.ForSecrets,
	organization interfaces.RepositoryOrg,
	session interfaces.Repository,
	git interfaces.RepositoryGit,
	queue interfaces.Queue,
) error {
	r := &controller{
		taskSchedulerConfig:       taskSchedulerConfig,
		eventBus:                  eventBus,
		secretManager:             secretManager,
		agentHandlerRegistry:      agentHandlerRegistry,
		gitHostingHandlerRegistry: gitHostingHandlerRegistry,
		sandboxHandlerRegistry:    sandboxHandlerRegistry,
		harnessHandlerRegistry:    harnessHandlerRegistry,
		organization:              organization,
		session:                   session,
		queue:                     queue,
		tracer:                    otel.Tracer("workdock.agent_session_runner"),
	}

	if err := r.init(); err != nil {
		return err
	}

	return r.taskScheduler.Run(ctx)
}

func (r *controller) init() error {
	taskScheduler, err := infrastructure.NewTaskScheduler(
		r.queue,
		r.taskSchedulerConfig,
		r.execute,
	)

	if err != nil {
		return err
	}

	r.taskScheduler = taskScheduler

	for provider, ingestor := range r.agentHandlerRegistry {
		r.eventBus.Subscribe(provider, func(
			ctx context.Context,
			event shared.DomainEvent,
		) error {
			session, sessionEvent, err := ingestor.Ingest(event)

			if err != nil {
				return err
			}

			// TODO: Fix this
			// sessionEvent.Reason = reason

			if org, err := r.organization.GetOrganization(ctx, session.OrganizationIdentifier); err != nil {
				return err
			} else if org == nil {
				// TODO: Inform the user they need to initialize their account
				// Received an agent session event from an unknown organization
				return err
			}

			if sess, err := r.session.GetAgentSession(ctx, session.Identifier); err != nil {
				return err
			} else if sess == nil {
				// Session doesn't exist, create it
				if err := r.session.UpsertAgentSession(ctx, session); err != nil {
					return err
				}
			} else {
				sEvent, err := r.session.GetAgentSessionEvent(ctx, sessionEvent.Identifier)

				if err != nil {
					return err
				}

				if sEvent != nil {
					slog.Debug("received duplicated event", "event_identifier", sessionEvent.Identifier)
					return nil
				}

				session = sess
			}

			if err := r.session.CreateSessionEvent(ctx, sessionEvent); err != nil {
				return err
			}

			slog.Debug("Created session event", "event_identifier", sessionEvent.Identifier)
			return nil
		})
	}

	r.eventBus.Subscribe(shared.EventType_GitResetConnection, func(ctx context.Context, event shared.DomainEvent) error {
		payload, ok := event.(shared.GitResetConnectionEvent)

		if !ok {
			return fmt.Errorf("expected event type %s got %s", shared.EventType_GitResetConnection, event.EventType())
		}

		if err := r.secretManager.Delete(ctx, "/github/installations", payload.InstallationId); err != nil {
			return err
		}

		return r.git.ResetGitHubConnection(ctx, payload.InstallationId, payload.Repos)
	})

	r.eventBus.Subscribe(shared.EventType_GitCompleteConnection, func(ctx context.Context, event shared.DomainEvent) error {
		payload, ok := event.(shared.GitCompleteConnectionEvent)

		if !ok {
			return fmt.Errorf("expected event type %s got %s", shared.EventType_GitCompleteConnection, event.EventType())
		}

		if err := r.secretManager.Set(ctx, "/github/installations", payload.InstallationId, string(payload.Token)); err != nil {
			slog.Error("failed to store installation access token", "installation_id", payload.InstallationId, "err", err)
			return err
		}

		for _, repo := range payload.Repos {
			connection := &shared.GitHubConnection{
				RepoFullName:   repo,
				Connected:      true,
				InstallationId: &payload.InstallationId,
			}

			if err := r.git.UpsertGitHubConnection(ctx, connection); err != nil {
				return err
			}
		}

		return nil
	})

	return nil
}

// execute provisions and coordinates all the components to successfully run the
// agent session's request based on a scheduled job
func (r *controller) execute(ctx context.Context, job *types.EventJob) error {
	sessionEvent, err := telemetry.Span(ctx, r.tracer, "session.get_event", func(ctx context.Context) (*shared.SessionEvent, error) {
		return r.session.GetAgentSessionEvent(ctx, job.SessionEventIdentifier)
	})

	if err != nil || sessionEvent == nil {
		return err
	}

	session, err := telemetry.Span(ctx, r.tracer, "session.get", func(ctx context.Context) (*shared.Session, error) {
		return r.session.GetAgentSession(ctx, sessionEvent.SessionIdentifier)
	})

	if err != nil || session == nil {
		return err
	}

	// *-------------------------------------------------------------------------*
	// * Get provider: work platform, git hosting, harness, sandbox              *
	// *-------------------------------------------------------------------------*
	agentHandler, gitHandler, sandboxHandler, harnessHandler, err := r.getHandlers(session)

	if err != nil {
		return err
	}

	// *-------------------------------------------------------------------------*
	// * Get providers credentials                                               *
	// *-------------------------------------------------------------------------*
	agentHandlerCredential, err := telemetry.Span(ctx, r.tracer, "session.get_agent_handler_credentials", func(ctx context.Context) (string, error) {
		return agentHandler.GetCredentials(ctx, session.OrganizationIdentifier)
	})

	if err != nil {
		return err
	}

	// DO NOT REMOVE!
	// Can start communicating, this let's the user know the request is being process.
	agentHandler.SendThought(ctx, session.Identifier, agentHandlerCredential, "")

	// *-------------------------------------------------------------------------*
	// * Create prompt                                                           *
	// *-------------------------------------------------------------------------*
	prompt, err := r.getPrompt(ctx, agentHandler, agentHandlerCredential, session, sessionEvent)

	if err != nil {
		return err
	}

	// *-------------------------------------------------------------------------*
	// * Verify git access                                                       *
	// *-------------------------------------------------------------------------*
	gitAccess, err := r.verifyGitAccess(ctx, agentHandler, agentHandlerCredential, gitHandler, session, sessionEvent)

	if err != nil {
		return err
	}

	// Cannot continue, requires git access
	if session.RepoFullName != nil && gitAccess == nil {
		return nil
	}

	// *-------------------------------------------------------------------------*
	// * Configure, start sandbox and run harness                                *
	// *-------------------------------------------------------------------------*
	stdout, stderr, shutdown, err := r.sandbox(
		ctx,
		agentHandler,
		agentHandlerCredential,
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
			result := shutdown(context.Background())

			// *-------------------------------------------------------------------------*
			// * Get exit command results
			// *-------------------------------------------------------------------------*
			pr := gitHandler.ParseLatestChangesResult(result)

			if pr != nil {
				// TODO: Implemet what to do with this
			}
		}
	}()

	if err != nil {
		return err
	}

	// *-------------------------------------------------------------------------*
	// * Process harness output, blocks until work is completed or an error      *
	// *-------------------------------------------------------------------------*
	r.harness(
		ctx,
		stdout,
		stderr,
		agentHandler,
		agentHandlerCredential,
		harnessHandler,
		session,
		sessionEvent,
	)

	return nil
}

func (r *controller) getHandlers(session *shared.Session) (
	interfaces.AgentSessionHandler,
	interfaces.GitHandler,
	interfaces.SandboxHandler,
	interfaces.HarnessHandler,
	error,
) {
	agentHandler, ok := r.agentHandlerRegistry[string(session.Provider)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for agent session run", session.Provider)
	}

	// TODO: Make this dynamic
	gitHandler, ok := r.gitHostingHandlerRegistry[string(shared.PlatformProvider_GitHub)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for git hosting handler", shared.PlatformProvider_GitHub)
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := r.sandboxHandlerRegistry[string(shared.PlatformProvider_Daytona)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for sandbox handler", shared.PlatformProvider_Daytona)
	}

	// TODO: Make this dynamic
	harnessHandler, ok := r.harnessHandlerRegistry[string(shared.HarnessProvider_OpenCode)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for harness handler", shared.HarnessProvider_OpenCode)
	}

	return agentHandler, gitHandler, sandboxHandler, harnessHandler, nil
}

func (r *controller) getPrompt(
	ctx context.Context,
	agentHandler interfaces.AgentSessionHandler,
	agentHandlerCrendential string,
	session *shared.Session,
	sessionEvent *shared.SessionEvent,
) (string, error) {
	promptContext, err := telemetry.Span(ctx, r.tracer, "session.get_prompt_context", func(ctx context.Context) (*interfaces.PromptContext, error) {
		return agentHandler.GetPromptContext(sessionEvent)
	})

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
		return "", err
	}

	prompt, err := telemetry.Span(ctx, r.tracer, "session.create_prompt", func(ctx context.Context) (string, error) {
		return r.createPrompt(session, sessionEvent, promptContext)
	})

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
		return "", err
	}

	return prompt, nil
}

func (r *controller) createPrompt(
	session *shared.Session,
	sessionEvent *shared.SessionEvent,
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
		if sessionEvent.Reason == shared.SessionEventTriggerReason_CheckRun {
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

	slog.Debug("Prompt prepared", "event_identifier", sessionEvent.Identifier)
	return p, nil
}

func (r *controller) verifyGitAccess(
	ctx context.Context,
	agentHandler interfaces.AgentSessionHandler,
	agentHandlerCredential string,
	gitHandler interfaces.GitHandler,
	session *shared.Session,
	sessionEvent *shared.SessionEvent,
) (*interfaces.GitAccess, error) {
	// no repo, no access required
	if session.RepoFullName == nil {
		return nil, nil
	}

	connection, err := telemetry.Span(ctx, r.tracer, "session.get_git_connection", func(ctx context.Context) (*shared.GitHubConnection, error) {
		return r.git.GetGitHubConnection(ctx, *session.RepoFullName)
	})

	if err != nil {
		return nil, err
	}

	// Request Git installation when repo access is not yet granted.
	// This applies to both public and private repositories — write operations
	// (push branches, create PRs) require an authenticated Git.
	if connection == nil || !connection.Connected || connection.InstallationId == nil {
		if err := telemetry.SpanErr(ctx, r.tracer, "session.upsert_git_connection", func(ctx context.Context) error {
			return r.git.UpsertGitHubConnection(
				ctx, &shared.GitHubConnection{
					SessionEventIdentifier: &session.Identifier,
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
			agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
			return nil, err
		}

		// Cannot continue because it requires github access
		return nil, nil
	}

	access, err := gitHandler.GetGitAccess(ctx, connection)

	if err != nil {
		if errors.Is(err, shared.ErrGitHubInstallationUnavailable) {
			if err := r.git.ResetGitHubConnection(ctx, *connection.InstallationId, []string{*session.RepoFullName}); err != nil {
				return nil, err
			}

			// TODO: Fix/hardcoded secret path
			if err := r.secretManager.Delete(ctx, "/github/installations", *connection.InstallationId); err != nil {
				return nil, err
			}

			if err := agentHandler.SendGitConnectionRequest(
				ctx,
				session.Identifier,
				agentHandlerCredential,
				string(shared.PlatformProvider_GitHub),
				gitHandler.GetInstallationUrl(),
			); err != nil {
				agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
				return nil, err
			}

			return nil, nil
		}

		return nil, err
	}

	return access, nil
}

func (r *controller) sandbox(
	ctx context.Context,
	agentHandler interfaces.AgentSessionHandler,
	agentHandlerCredential string,
	gitHandler interfaces.GitHandler,
	harnessHandler interfaces.HarnessHandler,
	sandboxHandler interfaces.SandboxHandler,
	gitAccess *interfaces.GitAccess,
	prompt string,
	session *shared.Session,
	sessionEvent *shared.SessionEvent,
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
	if file, data, err := harnessHandler.GetConfigFile(interfaces.HarnessConfig{}); err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		return nil, nil, nil, err
	} else {
		fileUploads[file] = data
	}

	shutdown, err := sandboxHandler.Run(
		ctx,
		&interfaces.SandboxConfig{
			AutoStopInterval: int(time.Minute * 5),
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

func (r *controller) harness(
	ctx context.Context,
	stdout <-chan string,
	stderr <-chan string,
	agentHandler interfaces.AgentSessionHandler,
	agentHandlerCredential string,
	harnessHandler interfaces.HarnessHandler,
	session *shared.Session,
	sessionEvent *shared.SessionEvent,
) {
	var out []byte
	var messageSpan trace.Span
	var stdErrBuilder strings.Builder

	part := make(chan []byte, 100)
	defer close(part)

	// Start and end messages tracks the span of each harness message, allow us
	// to detect when the time it took the harness to generate each message. Helpful
	// to verify slow reponses
	startMessage := func() {
		if messageSpan != nil {
			return
		}

		_, messageSpan = r.tracer.Start(ctx, "opencode.output.message")
	}

	endMessage := func() {
		if messageSpan == nil {
			return
		}

		messageSpan.End()
		messageSpan = nil
	}

	defer endMessage()

	// configure the harness parser and connects it to the agent handler
	harnessHandler.Parse(
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
		func(ctx context.Context, action shared.AgentAction) error {
			return agentHandler.SendAction(ctx, session.Identifier, agentHandlerCredential, action)
		},

		// sendElicitation sends a collection of questions to be answer by the user
		func(ctx context.Context, elicitation shared.AgentElicitation) error {
			return agentHandler.SendElicitation(ctx, session.Identifier, agentHandlerCredential, elicitation)
		},

		// sendServerInternalError sends a geneeric server internal error
		func(ctx context.Context) error {
			return agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		},
	)

outer:
	for stdout != nil || stderr != nil {
		select {
		case chunk, ok := <-stdout:
			if !ok {
				stdout = nil

				if len(out) > 0 {
					startMessage()

					if json.Valid(out) {
						part <- out
					}

					out = nil
					endMessage()
				}

				continue
			}

			// o.lastEvent.Store(time.Now().UnixNano())

			// We received the first chunk of a new message.
			startMessage()
			out = append(out, chunk...)

			for {
				i := bytes.IndexByte(out, '\n')

				if i == -1 {
					break
				}

				// The complete message has now been received.
				if json.Valid(out) {
					part <- out
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

			// o.lastEvent.Store(time.Now().UnixNano())

			if _, err := stdErrBuilder.Write([]byte(chunk)); err != nil {
				slog.Error("failed to write to stderr builder", "err", err, "event_identifier", sessionEvent.Identifier)
			}

		case <-ctx.Done():
			break outer
		}
	}

	if str := stdErrBuilder.String(); str != "" {
		agentHandler.SendResponse(ctx, session.Identifier, agentHandlerCredential, str)
		// o.stderrError = fmt.Errorf("%s", str)

		// span.AddEvent(
		// 	"output.stderr",
		// 	trace.WithAttributes(
		// 		attribute.String("error", str),
		// 	),
		// )

		slog.Error(
			"opencode stderr",
			"event_identifier", sessionEvent.Identifier,
			"err", str,
		)
	}

}
