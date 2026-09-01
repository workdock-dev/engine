package runners

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/repositories"
	"github.com/workdock-dev/engine/domain/telemetry"
	"github.com/workdock-dev/engine/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var (
	//go:embed prompt_templ_work_item.txt
	PromptTemplate_WorkItem string

	//go:embed prompt_templ_user_prompt.txt
	PromptTemplate_LatestUserComment string

	//go:embed prompt_templ_pr_review.txt
	PromptTemplate_PullRequestChecksFailed string
)

type PromptContext struct {
	Prompt  string
	Context *string // Optional context to provide
	Issue   types.Issue
}

type SandboxSecret struct {
	Name  string   `yaml:"name"`
	Value string   `yaml:"value"`
	Hosts []string `yaml:"hosts"`
}

type GitAccess struct {
	EnvVarName string
	Secret     string
	Hosts      []string
	Granted    bool
}

type SandboxConfig struct {
	AutoStopInterval    int
	Session             *types.Session
	SessionEvent        *types.SessionEvent
	Secrets             []SandboxSecret
	FileUploads         map[string][]byte
	CommandsWhenCreated []string
	Commands            []string
	ExitCommand         string
	HarnessCommand      string
	GitName             string
	GitEmail            string
}

type HarnessConfig struct {
	Provider struct {
		Name         string
		Model        string
		ModelOptions string
		AuthEnvVar   string
	}
	Mcps map[string]struct {
		Url        string
		AuthEnvVar string
	}
	Permissions map[string]any
}

// AgentSessionHandler is the interfaces required to be implemented
// by work platforms that provides agent assignment to tickets
type AgentSessionHandler interface {
	// Ingest parses the agent session domain event into into domain session
	// and session event.
	Ingest(event ports.DomainEvent) (*types.Session, *types.SessionEvent, error)

	// GetCredentials returns the access token required to send agent session updates
	GetCredentials(ctx context.Context, orgId string) (string, error)

	// GetPromptContext returns the data required to build the user prompt
	GetPromptContext(sessionEvent *types.SessionEvent) (*PromptContext, error)

	// SendThought sends the thinking state to the provider
	SendThought(ctx context.Context, sessionId, accessToken, text string) error

	// SendResponse sends text chunks/parts to the provider
	SendResponse(ctx context.Context, sessionId, accessToken, text string) error

	// SendACtion sends an action required to be executed by the user
	SendAction(ctx context.Context, sessionId, accessToken string, action types.AgentAction) error

	// SendElicitation sends a collection of questions to be answer by the user
	SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation types.AgentElicitation) error

	// SendGitConnectionRequest indicates the user to grant access to the git hosting provider
	SendGitConnectionRequest(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error

	// SendServerInternalError sends a geneeric server internal error
	SendServerInternalError(ctx context.Context, sessionId, accessToken string) error
}

// GitHandler is the interface to interact with the git hosting provider
// for the intial setup of the sandbox
type GitHandler interface {
	// GetInstallationUrl returns the installation URL where user can
	// grant access
	GetInstallationUrl() string

	// GetConfigurationCommands may return a list of commands the git handler
	// provider requires to be install in the sandbox
	GetConfigurationCommands() []string

	// GetCommands may return a list of commands the git handler provider
	// requires to be run on every sandbox execution
	GetCommands() []string

	// GetLatestChangesComand returns the command to verify if a pull request or commit with push
	// was created
	GetLatestChangesComand() string

	// ParseLatestChangesResult receives the changes procude by the latest changes command
	// and parse it to a concrete domain type
	ParseLatestChangesResult(changes string) *types.PullRequest

	// VerifyRepoAccess verifies whether the platform connection associated
	// with the session has access to the specified repository.
	VerifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repo *string) (*GitAccess, error)

	// RequestConnection requests access to the specified repository.
	RequestConnection(ctx context.Context, sessionEventIdentifier, repo string) error
}

// SandboxHandler is the interfaces through which the sandbox will be
// executed or Archive
type SandboxHandler interface {
	// Run the sandbox with all it's related configuration. Receives channel
	// to listen to the harness output. The sandbox implementation is responsible
	// of closing the channels
	//
	// returns the shutdown function which may return the exit command result
	Run(
		ctx context.Context,
		config *SandboxConfig,
		stdout chan<- string,
		stderr chan<- string,
	) (func(ctx context.Context) string, error)

	// Archive a given sandbox
	Archive(ctx context.Context, config *SandboxConfig) error
}

type HarnessHandler interface {
	// GetConfigurationCommands may return a list of commands the harness handler
	// provider requires to be install in the sandbox
	GetConfigurationCommands() []string

	// GetCommands may return a list of commands the harness handler provider
	// requires to be run on every sandbox execution
	GetCommands() []string

	// GetPromptFile pass in the user's prompt
	// return file path+data
	//
	// Upload the prompt, we do it this way because of https://github.com/anomalyco/opencode/issues/38723
	// opencode run hangs waiting for an input (stdin) when running for the first time, but this never
	// happens. Thus, opencode run stays stuck. The work arround is to pipe stdin. This can happen in other
	// harnesses; thus, we standarized this form.
	GetPromptFile(prompt string) (string, []byte)

	// GetConfigFile pass in custom configuration
	// return file path+data
	GetConfigFile(config HarnessConfig) (string, []byte, error)

	// RunCommand returns the harness command for execution
	RunCommand() string

	// Parse parses the harness part output, which is expected to be JSON.
	// It dispatches to the appropriate parser based on the LLM part type and
	// invokes the corresponding callback to update the agent handler.
	Parse(
		ctx context.Context,
		part <-chan []byte,
		sessionEventIdentifier string,

		// sendThought sends the thinking state to the provider
		sendThought func(ctx context.Context, text string) error,

		// sendResponse sends text chunks/parts to the provider
		sendResponse func(ctx context.Context, text string) error,

		// sendACtion sends an action required to be executed by the user
		sendAction func(ctx context.Context, action types.AgentAction) error,

		// sendElicitation sends a collection of questions to be answer by the user
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,

		// sendServerInternalError sends a geneeric server internal error
		sendServerInternalError func(ctx context.Context) error,
	)
}

// AgentSessionRunner is the pipeline that coordinates the work platform,
// git hosting platform, harness, and sandbox
type AgentSessionRunner struct {
	eventBus           ports.ForEventBus
	agentHandlers      map[string]AgentSessionHandler
	gitHostingHandlers map[string]GitHandler
	sandboxHandlers    map[string]SandboxHandler
	harnessHandlers    map[string]HarnessHandler
	organization       repositories.OrganizationRepository
	session            repositories.SessionRepository
	tracer             trace.Tracer
}

// NewAgentSessionRunner creates a new AgentSessionRunner pipeline
// from the given configuration. It subscribes to each one of the
// agent handlers provider to listen for new agent sessions
func NewAgentSessionRunner(
	agentHandlers map[string]AgentSessionHandler,
	gitHostingHandlers map[string]GitHandler,
	sandboxHandlers map[string]SandboxHandler,
	harnessHandlers map[string]HarnessHandler,
	eventBus ports.ForEventBus,
	organization repositories.OrganizationRepository,
	session repositories.SessionRepository,
) *AgentSessionRunner {
	r := &AgentSessionRunner{
		eventBus:           eventBus,
		agentHandlers:      agentHandlers,
		gitHostingHandlers: gitHostingHandlers,
		sandboxHandlers:    sandboxHandlers,
		harnessHandlers:    harnessHandlers,
		organization:       organization,
		session:            session,
		tracer:             otel.Tracer("workdock.agent_session_runner"),
	}

	for provider, ingestor := range r.agentHandlers {
		r.eventBus.Subscribe(provider, func(
			ctx context.Context,
			event ports.DomainEvent,
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

	return r
}

// Execute provisions and coordinates all the components to successfully run the
// agent session's request based on a scheduled job
func (r *AgentSessionRunner) Execute(ctx context.Context, job *types.EventJob) error {
	sessionEvent, err := telemetry.Span(ctx, r.tracer, "session.get_event", func(ctx context.Context) (*types.SessionEvent, error) {
		return r.session.GetAgentSessionEvent(ctx, job.SessionEventIdentifier)
	})

	if err != nil || sessionEvent == nil {
		return err
	}

	session, err := telemetry.Span(ctx, r.tracer, "session.get", func(ctx context.Context) (*types.Session, error) {
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

func (r *AgentSessionRunner) getHandlers(session *types.Session) (
	AgentSessionHandler,
	GitHandler,
	SandboxHandler,
	HarnessHandler,
	error,
) {
	agentHandler, ok := r.agentHandlers[string(session.Provider)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for agent session run", session.Provider)
	}

	// TODO: Make this dynamic
	gitHandler, ok := r.gitHostingHandlers[string(types.PlatformProvider_GitHub)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for git hosting handler", types.PlatformProvider_GitHub)
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := r.sandboxHandlers[string(types.PlatformProvider_Daytona)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for sandbox handler", types.PlatformProvider_Daytona)
	}

	// TODO: Make this dynamic
	harnessHandler, ok := r.harnessHandlers[string(types.HarnessProvider_OpenCode)]

	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("provider %s not configured for harness handler", types.HarnessProvider_OpenCode)
	}

	return agentHandler, gitHandler, sandboxHandler, harnessHandler, nil
}

func (r *AgentSessionRunner) getPrompt(
	ctx context.Context,
	agentHandler AgentSessionHandler,
	agentHandlerCrendential string,
	session *types.Session,
	sessionEvent *types.SessionEvent,
) (string, error) {
	promptContext, err := telemetry.Span(ctx, r.tracer, "session.get_prompt_context", func(ctx context.Context) (*PromptContext, error) {
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

func (r *AgentSessionRunner) createPrompt(
	session *types.Session,
	sessionEvent *types.SessionEvent,
	promptContext *PromptContext,
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
		if sessionEvent.Reason == types.SessionEventTriggerReason_CheckRun {
			p += fmt.Sprintf(
				types.PromptTemplate_PullRequestChecksFailed,
				"The pull request checks have failed. Review the check failures, fix the issues, and ensure all checks pass before the pull request can be merged.",
			)
		} else {
			p += fmt.Sprintf(
				types.PromptTemplate_LatestUserComment,
				"There are review comments on the pull request. Retrieve all review comments and address each one that is applicable to the current implementation. Make the necessary code changes, verify the changes, and ensure the pull request is ready for review again.",
			)
		}
	} else if promptContext.Context != nil {
		p += fmt.Sprintf(types.PromptTemplate_LatestUserComment, *promptContext.Context)
	}

	slog.Debug("Prompt prepared", "event_identifier", sessionEvent.Identifier)
	return p, nil
}

func (r *AgentSessionRunner) verifyGitAccess(
	ctx context.Context,
	agentHandler AgentSessionHandler,
	agentHandlerCredential string,
	gitHandler GitHandler,
	session *types.Session,
	sessionEvent *types.SessionEvent,
) (*GitAccess, error) {
	gitAccess, err := telemetry.Span(ctx, r.tracer, "session.verify_repo_access", func(ctx context.Context) (*GitAccess, error) {
		return gitHandler.VerifyRepoAccess(ctx, session.Identifier, session.RepoFullName)
	})

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		return nil, err
	}

	// Request Git installation when repo access is not yet granted.
	// This applies to both public and private repositories — write operations
	// (push branches, create PRs) require an authenticated Git.
	if session.RepoFullName != nil && gitAccess != nil && !gitAccess.Granted {
		if err := telemetry.SpanErr(ctx, r.tracer, "session.request_repo_access", func(ctx context.Context) error {
			return gitHandler.RequestConnection(ctx, sessionEvent.Identifier, *session.RepoFullName)
		}); err != nil {
			return nil, err
		}

		if err := agentHandler.SendGitConnectionRequest(
			ctx,
			session.Identifier,
			agentHandlerCredential,
			string(types.PlatformProvider_GitHub),
			gitHandler.GetInstallationUrl(),
		); err != nil {
			agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
			return nil, err
		}

		// Cannot continue because it requires github access
		return nil, nil
	}

	return gitAccess, nil
}

func (r *AgentSessionRunner) sandbox(
	ctx context.Context,
	agentHandler AgentSessionHandler,
	agentHandlerCredential string,
	gitHandler GitHandler,
	harnessHandler HarnessHandler,
	sandboxHandler SandboxHandler,
	gitAccess *GitAccess,
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
	secrets := make([]SandboxSecret, 0)
	fileUploads := make(map[string][]byte)

	if gitAccess != nil && gitAccess.Granted {
		secrets = append(secrets, SandboxSecret{
			Name:  gitAccess.EnvVarName,
			Value: gitAccess.Secret,
			Hosts: gitAccess.Hosts,
		})
	}

	// Get prompt file and prepare it for upload
	promptFilePath, promptData := harnessHandler.GetPromptFile(prompt)
	fileUploads[promptFilePath] = promptData

	// Get harness configuration and prepare it for upload
	if file, data, err := harnessHandler.GetConfigFile(HarnessConfig{}); err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCredential)
		return nil, nil, nil, err
	} else {
		fileUploads[file] = data
	}

	shutdown, err := sandboxHandler.Run(
		ctx,
		&SandboxConfig{
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

func (r *AgentSessionRunner) harness(
	ctx context.Context,
	stdout <-chan string,
	stderr <-chan string,
	agentHandler AgentSessionHandler,
	agentHandlerCredential string,
	harnessHandler HarnessHandler,
	session *types.Session,
	sessionEvent *types.SessionEvent,
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
