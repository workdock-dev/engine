package runners

import (
	"bytes"
	"context"
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

const (
	PromptTemplate_WorkItem = `
Your objective is to complete the requested work.
You are working on the following work item.

# Work Item
**Title:** %s
**Identifier:** %s
**Repository:** %s

## Requirements
%s

## Additional Context
%s

### Workflow rules
When determining what work to perform, use the following order of precedence:

1. The **Latest User Comment** (if present).
2. The **Requirements**.
3. The **Additional Context**.

### Instructions
- Treat higher-priority information as authoritative when conflicts exist.
- Use lower-priority information only when it does not contradict higher-priority information.
- If the latest user comment changes or supersedes previous requirements, follow the latest user comment.
- If information is ambiguous or incomplete, identify the ambiguity instead of making assumptions.
- Preserve existing behavior unless a higher-priority source explicitly requests a change.
- Limit your work to what is necessary to satisfy the current request.
- Set the ticket status to "In Progress" before starting work on it. Keep it "In Progress" while you are actively working on the task. Once you have completed the implementation and the changes are ready for review, move the ticket to "In Review".

## Pull Request Rules

* **Never close a pull request unless the user explicitly requests that it be closed.**
* If review comments are added to a pull request, **address all applicable review comments within the same request** unless the user explicitly instructs otherwise.
* Do not assume that addressing review comments means the pull request should be closed, merged, or otherwise finalized.
* Preserve the pull request's open state unless the user explicitly asks you to change it.
`

	PromptTemplate_GitHubOperations = `

### GitHub Operations
- Use the git CLI for clone, fetch, pull, push, and branch management over HTTPS.
- Use the gh CLI for GitHub API operations such as creating PRs, issues, and releases (e.g. gh pr create, gh repo view).
- GitHub credentials are already configured for you, no manual authentication is required.

### GitHub Credentials Notes
- Your credential is a GitHub App installation token (ghs_...), scoped to the app's installed repositories and valid for about an hour. A fresh token is provided at the start of each session, so you never need to obtain one yourself.
- It is NOT a user token. Identity-scoped calls - GET /user, gh api user, gh auth status - will always return 401 Bad credentials. That is expected and does NOT mean the credentials are broken. Do not abandon your task because of such an error.
- To confirm the credentials work, use repository-scoped calls instead, e.g. git ls-remote https://github.com/OWNER/REPO, gh api /installation/repositories, or gh api repos/OWNER/REPO.
- Git over HTTPS authentication is already configured for you, no action needed. Treat the token as opaque; do not decode or inspect its contents.
- If a repository-scoped operation returns 401 or 403 mid-session, the token may have expired - report this instead of stopping.
`

	PromptTemplate_LatestUserComment = `

### Latest User Comment (Highest Priority)

The following message is the most recent instruction from the user.

It may clarify, refine, override, or replace previous requirements. When it conflicts with earlier information, follow this message.

%s
`

	PromptTemplate_PullRequestChecksFailed = `

### Latest User Comment (Highest Priority)

The following message is the most recent instruction from the user.

It may clarify, refine, override, or replace previous requirements. When it conflicts with earlier information, follow this message.

%s
`
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
	EnvVars             map[string]string
	FileUploads         map[string][]byte
	CommandsWhenCreated []string
	Commands            []string
	HarnessCommand      string
	GitName             string
	GitEmail            string
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
	// returns the shutdown function
	Run(
		ctx context.Context,
		config *SandboxConfig,
		stdout chan<- string,
		stderr chan<- string,
	) (func(ctx context.Context), error)

	// Archive a given sandbox
	Archive(ctx context.Context, config *SandboxConfig) error
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
		part <-chan string,

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
	// * TODO: 1) Get provider: work platform, git hosting, harness, sandbox     *
	// *-------------------------------------------------------------------------*
	agentHandler, ok := r.agentHandlers[string(session.Provider)]

	if !ok {
		return fmt.Errorf("provider %s not configured for agent session run", session.Provider)
	}

	// TODO: Make this dynamic
	gitHandler, ok := r.gitHostingHandlers[string(types.PlatformProvider_GitHub)]

	if !ok {
		return fmt.Errorf("provider %s not configured for git hosting handler", types.PlatformProvider_GitHub)
	}

	// TODO: Make this dynamic
	sandboxHandler, ok := r.sandboxHandlers[string(types.PlatformProvider_Daytona)]

	if !ok {
		return fmt.Errorf("provider %s not configured for sandbox handler", types.PlatformProvider_Daytona)
	}

	// TODO: Make this dynamic
	harnessHandler, ok := r.harnessHandlers[string(types.HarnessProvider_OpenCode)]

	if !ok {
		return fmt.Errorf("provider %s not configured for harness handler", types.HarnessProvider_OpenCode)
	}

	// *-------------------------------------------------------------------------*
	// * 2) Get providers credentials                                            *
	// *-------------------------------------------------------------------------*
	agentHandlerCrendential, err := telemetry.Span(ctx, r.tracer, "session.get_agent_handler_credentials", func(ctx context.Context) (string, error) {
		return agentHandler.GetCredentials(ctx, session.OrganizationIdentifier)
	})

	if err != nil {
		return err
	}

	// Can start communicating, this let's the user know the request is being
	// process. Do not remove!
	agentHandler.SendThought(ctx, session.Identifier, agentHandlerCrendential, "")

	// *-------------------------------------------------------------------------*
	// * 3) Create prompt                                                        *
	// *-------------------------------------------------------------------------*
	promptContext, err := telemetry.Span(ctx, r.tracer, "session.get_prompt_context", func(ctx context.Context) (*PromptContext, error) {
		return agentHandler.GetPromptContext(sessionEvent)
	})

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
		return err
	}

	prompt, err := telemetry.Span(ctx, r.tracer, "session.create_prompt", func(ctx context.Context) (string, error) {
		return r.createPrompt(session, sessionEvent, promptContext)
	})

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
		return err
	}

	// *-------------------------------------------------------------------------*
	// * 4) Verify git access                                                    *
	// *-------------------------------------------------------------------------*
	gitAccess, err := telemetry.Span(ctx, r.tracer, "session.verify_repo_access", func(ctx context.Context) (*GitAccess, error) {
		return gitHandler.VerifyRepoAccess(ctx, session.Identifier, session.RepoFullName)
	})

	if err != nil {
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
		return err
	}

	// Request Git installation when repo access is not yet granted.
	// This applies to both public and private repositories — write operations
	// (push branches, create PRs) require an authenticated Git.
	if session.RepoFullName != nil && gitAccess != nil && !gitAccess.Granted {
		if err := telemetry.SpanErr(ctx, r.tracer, "session.request_repo_access", func(ctx context.Context) error {
			return gitHandler.RequestConnection(ctx, sessionEvent.Identifier, *session.RepoFullName)
		}); err != nil {
			return err
		}

		if err := agentHandler.SendGitConnectionRequest(
			ctx,
			session.Identifier,
			agentHandlerCrendential,
			string(types.PlatformProvider_GitHub),
			gitHandler.GetInstallationUrl(),
		); err != nil {
			agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
			return err
		}

		// Cannot continue because it requires github access
		return nil
	}

	// *-------------------------------------------------------------------------*
	// * 5) Configure, start sandbox and run harness                             *
	// *-------------------------------------------------------------------------*
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
		agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
		return err
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
			FileUploads:    fileUploads,
			Secrets:        secrets,
			GitName:        "workdock[bot]",
			GitEmail:       "no-reply@workdock.dev",
			HarnessCommand: harnessHandler.RunCommand(),
		},
		stdout,
		stderr,
	)

	defer func() {
		if shutdown != nil {
			shutdown(context.Background())
		}
	}()

	// *-------------------------------------------------------------------------*
	// * 6) Process harness output                                               *
	// *-------------------------------------------------------------------------*
	var out []byte
	var messageSpan trace.Span
	var stdErrBuilder strings.Builder
	part := make(chan string, 100)

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

		// sendThought sends the thinking state to the provider
		func(ctx context.Context, text string) error {
			return agentHandler.SendThought(ctx, session.Identifier, agentHandlerCrendential, text)
		},

		// sendResponse sends text chunks/parts to the provider
		func(ctx context.Context, text string) error {
			return agentHandler.SendResponse(ctx, session.Identifier, agentHandlerCrendential, text)
		},

		// sendACtion sends an action required to be executed by the user
		func(ctx context.Context, action types.AgentAction) error {
			return agentHandler.SendAction(ctx, session.Identifier, agentHandlerCrendential, action)
		},

		// sendElicitation sends a collection of questions to be answer by the user
		func(ctx context.Context, elicitation types.AgentElicitation) error {
			return agentHandler.SendElicitation(ctx, session.Identifier, agentHandlerCrendential, elicitation)
		},

		// sendServerInternalError sends a geneeric server internal error
		func(ctx context.Context) error {
			return agentHandler.SendServerInternalError(ctx, session.Identifier, agentHandlerCrendential)
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
						part <- string(out)
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
					part <- string(out)
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
		// o.stderrError = fmt.Errorf("%s", str)

		// span.AddEvent(
		// 	"output.stderr",
		// 	trace.WithAttributes(
		// 		attribute.String("error", str),
		// 	),
		// )

		// slog.Error(
		// 	"opencode stderr",
		// 	"event_identifier", o.sessionId,
		// 	"err", str,
		// )
	}

	// TODO: Run harness
	//
	// TODO: Get harness session results

	// return telemetry.SpanErr(ctx, r.tracer, "session.process", func(ctx context.Context) error {
	// 	return provider.Process(ctx, ports.ProcessConfig{
	// 		Job:          job,
	// 		SessionEvent: sessionEvent,
	// 		Session:      session,
	// 	})
	// }, trace.WithAttributes(
	// 	attribute.String("session.organization.id", session.OrganizationIdentifier),
	// 	attribute.String("session.identifier", session.Identifier),
	// 	attribute.String("session_event.identifier", sessionEvent.Identifier),
	// 	attribute.String("session.provider", string(session.Provider)),
	// ))
	return nil
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
