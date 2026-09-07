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
	"context"
	"time"

	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
)

// --- HandlerAgentSession mock ---

type mockAgentHandler struct {
	ingestFn              func(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error)
	getLabelsFn           func(ctx context.Context, issueId, accessToken string) ([]string, error)
	getCredentialsFn      func(ctx context.Context, orgId string) (string, error)
	getPromptContextFn    func(sessionEvent *types.SessionEvent) (*interfaces.PromptContext, error)
	sendThoughtFn         func(ctx context.Context, sessionId, accessToken, text string) error
	sendResponseFn        func(ctx context.Context, sessionId, accessToken, text string) error
	sendActionFn          func(ctx context.Context, sessionId, accessToken string, action types.AgentAction) error
	sendElicitationFn     func(ctx context.Context, sessionId, accessToken string, elicitation types.AgentElicitation) error
	sendGitConnectionRqFn func(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error
	sendInternalErrFn     func(ctx context.Context, sessionId, accessToken string) error

	thoughts       []string
	responses      []string
	gitRequests    []gitRequest
	actions        []types.AgentAction
	elicitations   []types.AgentElicitation
	internalErrors int
}

type gitRequest struct {
	sessionId     string
	accessToken   string
	gitProvider   string
	gitInstallURL string
}

func (m *mockAgentHandler) Ingest(event shared.DomainEvent) (*types.Session, *types.SessionEvent, error) {
	if m.ingestFn != nil {
		return m.ingestFn(event)
	}
	return &types.Session{}, &types.SessionEvent{}, nil
}

func (m *mockAgentHandler) GetLabels(ctx context.Context, issueId, accessToken string) ([]string, error) {
	if m.getLabelsFn != nil {
		return m.getLabelsFn(ctx, issueId, accessToken)
	}
	return nil, nil
}

func (m *mockAgentHandler) GetCredentials(ctx context.Context, orgId string) (string, error) {
	if m.getCredentialsFn != nil {
		return m.getCredentialsFn(ctx, orgId)
	}
	return "token", nil
}

func (m *mockAgentHandler) GetPromptContext(sessionEvent *types.SessionEvent) (*interfaces.PromptContext, error) {
	if m.getPromptContextFn != nil {
		return m.getPromptContextFn(sessionEvent)
	}
	return &interfaces.PromptContext{
		Prompt: "prompt",
		Issue:  types.Issue{Title: "Title", Identifier: "issue-1", Description: "Description"},
	}, nil
}

func (m *mockAgentHandler) SendThought(ctx context.Context, sessionId, accessToken, text string) error {
	m.thoughts = append(m.thoughts, text)
	if m.sendThoughtFn != nil {
		return m.sendThoughtFn(ctx, sessionId, accessToken, text)
	}
	return nil
}

func (m *mockAgentHandler) SendResponse(ctx context.Context, sessionId, accessToken, text string) error {
	m.responses = append(m.responses, text)
	if m.sendResponseFn != nil {
		return m.sendResponseFn(ctx, sessionId, accessToken, text)
	}
	return nil
}

func (m *mockAgentHandler) SendAction(ctx context.Context, sessionId, accessToken string, action types.AgentAction) error {
	m.actions = append(m.actions, action)
	if m.sendActionFn != nil {
		return m.sendActionFn(ctx, sessionId, accessToken, action)
	}
	return nil
}

func (m *mockAgentHandler) SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation types.AgentElicitation) error {
	m.elicitations = append(m.elicitations, elicitation)
	if m.sendElicitationFn != nil {
		return m.sendElicitationFn(ctx, sessionId, accessToken, elicitation)
	}
	return nil
}

func (m *mockAgentHandler) SendGitConnectionRequest(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error {
	m.gitRequests = append(m.gitRequests, gitRequest{
		sessionId:     sessionId,
		accessToken:   accessToken,
		gitProvider:   gitProvider,
		gitInstallURL: gitInstallURL,
	})
	if m.sendGitConnectionRqFn != nil {
		return m.sendGitConnectionRqFn(ctx, sessionId, accessToken, gitProvider, gitInstallURL)
	}
	return nil
}

func (m *mockAgentHandler) SendServerInternalError(ctx context.Context, sessionId, accessToken string) error {
	m.internalErrors++
	if m.sendInternalErrFn != nil {
		return m.sendInternalErrFn(ctx, sessionId, accessToken)
	}
	return nil
}

// --- HandlerGit mock ---

type mockGitHandler struct {
	getInstallationUrlFn  func() string
	getConfigCommandsFn   func() []string
	getCommandsFn         func() []string
	getLatestChangesCmdFn func() string
	getGitAccessFn        func(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error)
	parseLatestResultFn   func(changes string) *types.PullRequest
}

func (m *mockGitHandler) GetInstallationUrl() string {
	if m.getInstallationUrlFn != nil {
		return m.getInstallationUrlFn()
	}
	return "https://github.com/install"
}

func (m *mockGitHandler) GetConfigurationCommands() []string {
	if m.getConfigCommandsFn != nil {
		return m.getConfigCommandsFn()
	}
	return nil
}

func (m *mockGitHandler) GetCommands() []string {
	if m.getCommandsFn != nil {
		return m.getCommandsFn()
	}
	return nil
}

func (m *mockGitHandler) GetLatestChangesCommand() string {
	if m.getLatestChangesCmdFn != nil {
		return m.getLatestChangesCmdFn()
	}
	return ""
}

func (m *mockGitHandler) GetGitAccess(ctx context.Context, connection *types.GitConnection) (*interfaces.GitAccess, error) {
	if m.getGitAccessFn != nil {
		return m.getGitAccessFn(ctx, connection)
	}
	return &interfaces.GitAccess{Granted: true}, nil
}

func (m *mockGitHandler) ParseLatestChangesResult(changes string) *types.PullRequest {
	if m.parseLatestResultFn != nil {
		return m.parseLatestResultFn(changes)
	}
	return nil
}

// --- HandlerSandbox mock ---

type mockSandboxHandler struct {
	runFn func(ctx context.Context, config *interfaces.SandboxConfig, stdout chan<- string, stderr chan<- string) (interfaces.SandboxShutdown, error)

	runConfig   *interfaces.SandboxConfig
	runStdout   chan<- string
	runStderr   chan<- string
	shutdownRan bool
}

func (m *mockSandboxHandler) Run(ctx context.Context, config *interfaces.SandboxConfig, stdout chan<- string, stderr chan<- string) (interfaces.SandboxShutdown, error) {
	m.runConfig = config
	m.runStdout = stdout
	m.runStderr = stderr
	if m.runFn != nil {
		return m.runFn(ctx, config, stdout, stderr)
	}
	return func(ctx context.Context) string {
		m.shutdownRan = true
		return "shutdown result"
	}, nil
}

func (m *mockSandboxHandler) Archive(ctx context.Context, config *interfaces.SandboxConfig) error {
	return nil
}

// --- HandlerHarness mock ---

type mockHarnessHandler struct {
	getConfigCommandsFn func() []string
	getCommandsFn       func() []string
	getPromptFileFn     func(prompt string) (string, []byte)
	getConfigFileFn     func(config *interfaces.HarnessConfig) (string, []byte, error)
	runCommandFn        func() string
	parseFn             func(
		ctx context.Context,
		harnessConfig *interfaces.HarnessConfig,
		part <-chan []byte,
		sessionEventIdentifier string,
		sendThought func(ctx context.Context, text string) error,
		sendResponse func(ctx context.Context, text string) error,
		sendAction func(ctx context.Context, action types.AgentAction) error,
		sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
		sendServerInternalError func(ctx context.Context) error,
	) error

	promptFile    string
	promptData    []byte
	parsedParts   [][]byte
	parseReturned bool
}

func (m *mockHarnessHandler) GetConfigurationCommands() []string {
	if m.getConfigCommandsFn != nil {
		return m.getConfigCommandsFn()
	}
	return []string{"harness-config-cmd"}
}

func (m *mockHarnessHandler) GetCommands() []string {
	if m.getCommandsFn != nil {
		return m.getCommandsFn()
	}
	return []string{"harness-cmd"}
}

func (m *mockHarnessHandler) GetPromptFile(prompt string) (string, []byte) {
	if m.getPromptFileFn != nil {
		return m.getPromptFileFn(prompt)
	}
	m.promptFile = "/tmp/prompt.txt"
	m.promptData = []byte(prompt)
	return m.promptFile, m.promptData
}

func (m *mockHarnessHandler) GetConfigFile(config *interfaces.HarnessConfig) (string, []byte, error) {
	if m.getConfigFileFn != nil {
		return m.getConfigFileFn(config)
	}
	return "/tmp/config.json", []byte("{}"), nil
}

func (m *mockHarnessHandler) RunCommand() string {
	if m.runCommandFn != nil {
		return m.runCommandFn()
	}
	return "opencode run"
}

func (m *mockHarnessHandler) Parse(
	ctx context.Context,
	harnessConfig *interfaces.HarnessConfig,
	part <-chan []byte,
	sessionEventIdentifier string,
	sendThought func(ctx context.Context, text string) error,
	sendResponse func(ctx context.Context, text string) error,
	sendAction func(ctx context.Context, action types.AgentAction) error,
	sendElicitation func(ctx context.Context, elicitation types.AgentElicitation) error,
	sendServerInternalError func(ctx context.Context) error,
) error {
	if m.parseFn != nil {
		return m.parseFn(ctx, harnessConfig, part, sessionEventIdentifier, sendThought, sendResponse, sendAction, sendElicitation, sendServerInternalError)
	}

	for p := range part {
		m.parsedParts = append(m.parsedParts, p)
	}
	m.parseReturned = true
	return nil
}

// --- HandlerMCP mock ---

type mockMcpHandler struct {
	getMCPListFn func() []interfaces.MCPConfig
}

func (m *mockMcpHandler) GetMCPList() []interfaces.MCPConfig {
	if m.getMCPListFn != nil {
		return m.getMCPListFn()
	}
	return nil
}

// --- Repository mock (session) ---

type mockSessionRepository struct {
	getAgentSessionFn              func(ctx context.Context, identifier string) (*types.Session, error)
	getAgentSessionsByIssueIdFn    func(ctx context.Context, issueId string) ([]*types.Session, error)
	getAgentSessionEventFn         func(ctx context.Context, identifier string) (*types.SessionEvent, error)
	getAgentSessionEventByGitRefFn func(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error)
	createSessionEventFn           func(ctx context.Context, event *types.SessionEvent) error
	resumeSessionEventFn           func(ctx context.Context, event *types.SessionEvent) error
	upsertAgentSessionFn           func(ctx context.Context, session *types.Session) error
	updateSessionEventResultFn     func(ctx context.Context, event *types.SessionEvent) error
	cancelSessionFn                func(ctx context.Context, queuedBy, reason string) (int, error)

	upsertedSessions []*types.Session
	createdEvents    []*types.SessionEvent
	resumedEvents    []*types.SessionEvent
	updatedResults   []*types.SessionEvent
	cancelSession    string
	cancelReason     string
}

func (m *mockSessionRepository) GetAgentSession(ctx context.Context, identifier string) (*types.Session, error) {
	if m.getAgentSessionFn != nil {
		return m.getAgentSessionFn(ctx, identifier)
	}
	return nil, nil
}

func (m *mockSessionRepository) GetAgentSessionsByIssueId(ctx context.Context, issueId string) ([]*types.Session, error) {
	if m.getAgentSessionsByIssueIdFn != nil {
		return m.getAgentSessionsByIssueIdFn(ctx, issueId)
	}
	return nil, nil
}

func (m *mockSessionRepository) GetAgentSessionEvent(ctx context.Context, identifier string) (*types.SessionEvent, error) {
	if m.getAgentSessionEventFn != nil {
		return m.getAgentSessionEventFn(ctx, identifier)
	}
	return nil, nil
}

func (m *mockSessionRepository) GetAgentSessionEventByGitRef(ctx context.Context, identifier, repoFullName string) (*types.SessionEvent, error) {
	if m.getAgentSessionEventByGitRefFn != nil {
		return m.getAgentSessionEventByGitRefFn(ctx, identifier, repoFullName)
	}
	return nil, nil
}

func (m *mockSessionRepository) CreateSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	m.createdEvents = append(m.createdEvents, event)
	if m.createSessionEventFn != nil {
		return m.createSessionEventFn(ctx, event)
	}
	return nil
}

func (m *mockSessionRepository) ResumeSessionEvent(ctx context.Context, event *types.SessionEvent) error {
	m.resumedEvents = append(m.resumedEvents, event)
	if m.resumeSessionEventFn != nil {
		return m.resumeSessionEventFn(ctx, event)
	}
	return nil
}

func (m *mockSessionRepository) UpsertAgentSession(ctx context.Context, session *types.Session) error {
	m.upsertedSessions = append(m.upsertedSessions, session)
	if m.upsertAgentSessionFn != nil {
		return m.upsertAgentSessionFn(ctx, session)
	}
	return nil
}

func (m *mockSessionRepository) UpdateSessionEventResult(ctx context.Context, event *types.SessionEvent) error {
	m.updatedResults = append(m.updatedResults, event)
	if m.updateSessionEventResultFn != nil {
		return m.updateSessionEventResultFn(ctx, event)
	}
	return nil
}

func (m *mockSessionRepository) CancelSession(ctx context.Context, queuedBy, reason string) (int, error) {
	m.cancelSession = queuedBy
	m.cancelReason = reason
	if m.cancelSessionFn != nil {
		return m.cancelSessionFn(ctx, queuedBy, reason)
	}
	return 1, nil
}

// --- RepositoryOrg mock ---

type mockOrganizationRepository struct {
	getOrganizationFn func(ctx context.Context, identifier string) (*shared.Organization, error)

	getOrganizationCalledWith string
}

func (m *mockOrganizationRepository) GetOrganization(ctx context.Context, identifier string) (*shared.Organization, error) {
	m.getOrganizationCalledWith = identifier
	if m.getOrganizationFn != nil {
		return m.getOrganizationFn(ctx, identifier)
	}
	return &shared.Organization{Identifier: identifier}, nil
}

// --- RepositoryGit mock ---

type mockGitRepository struct {
	getConnectionFn    func(ctx context.Context, repoFullName string) (*types.GitConnection, error)
	upsertConnectionFn func(ctx context.Context, connection *types.GitConnection) error
	resetConnectionFn  func(ctx context.Context, installationId string, repos []string) error

	upsertedConnections []*types.GitConnection
	resetCalls          []string
}

func (m *mockGitRepository) GetConnection(ctx context.Context, repoFullName string) (*types.GitConnection, error) {
	if m.getConnectionFn != nil {
		return m.getConnectionFn(ctx, repoFullName)
	}
	return nil, nil
}

func (m *mockGitRepository) UpsertConnection(ctx context.Context, connection *types.GitConnection) error {
	m.upsertedConnections = append(m.upsertedConnections, connection)
	if m.upsertConnectionFn != nil {
		return m.upsertConnectionFn(ctx, connection)
	}
	return nil
}

func (m *mockGitRepository) ResetConnection(ctx context.Context, installationId string, repos []string) error {
	m.resetCalls = append(m.resetCalls, installationId)
	if m.resetConnectionFn != nil {
		return m.resetConnectionFn(ctx, installationId, repos)
	}
	return nil
}

// upsertCount returns how many times UpsertConnection was called.
func (m *mockGitRepository) upsertCount() int {
	return len(m.upsertedConnections)
}

// lastUpserted returns the most recently upserted connection, or nil.
func (m *mockGitRepository) lastUpserted() *types.GitConnection {
	if len(m.upsertedConnections) == 0 {
		return nil
	}
	return m.upsertedConnections[len(m.upsertedConnections)-1]
}

// --- SecretManager mock ---

type mockSecretManager struct {
	getFn    func(ctx context.Context, secretPath, secretName string) (string, error)
	setFn    func(ctx context.Context, secretPath, secretName, secretValue string) error
	deleteFn func(ctx context.Context, secretPath, secretName string) error

	setPath  string
	setName  string
	setValue string
	setCalls int

	deletedPath string
	deletedName string
	deleteCalls int
}

func (m *mockSecretManager) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	if m.getFn != nil {
		return m.getFn(ctx, secretPath, secretName)
	}
	return "", nil
}

func (m *mockSecretManager) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	m.setCalls++
	m.setPath = secretPath
	m.setName = secretName
	m.setValue = secretValue
	if m.setFn != nil {
		return m.setFn(ctx, secretPath, secretName, secretValue)
	}
	return nil
}

func (m *mockSecretManager) Delete(ctx context.Context, secretPath, secretName string) error {
	m.deleteCalls++
	m.deletedPath = secretPath
	m.deletedName = secretName
	if m.deleteFn != nil {
		return m.deleteFn(ctx, secretPath, secretName)
	}
	return nil
}

// --- Queue mock (minimal: New only needs a non-nil Queue) ---

type mockQueue struct {
	listenFn func(ctx context.Context) (<-chan struct{}, <-chan string, error)
}

func (m *mockQueue) Claim(ctx context.Context, owner string, nextAttemptAt time.Time) (*types.EventJob, error) {
	// New() starts a live scheduler whose workers attempt an initial claim;
	// there is never any queued work in tests, so return no job.
	return nil, nil
}

func (m *mockQueue) Heartbeat(ctx context.Context, id string, leaseDuration time.Duration) error {
	panic("unexpected call to Heartbeat")
}

func (m *mockQueue) Complete(ctx context.Context, id string, status types.EventJobStatus) error {
	panic("unexpected call to Complete")
}

func (m *mockQueue) Retry(ctx context.Context, id string, cause error, retryGracePeriod time.Duration) error {
	panic("unexpected call to Retry")
}

func (m *mockQueue) Fail(ctx context.Context, id string, cause error) error {
	panic("unexpected call to Fail")
}

func (m *mockQueue) Listen(ctx context.Context) (<-chan struct{}, <-chan string, error) {
	if m.listenFn != nil {
		return m.listenFn(ctx)
	}
	runnable := make(chan struct{})
	cancellable := make(chan string)
	return runnable, cancellable, nil
}

// Compile-time interface conformance checks.
var (
	_ interfaces.HandlerAgentSession = (*mockAgentHandler)(nil)
	_ interfaces.HandlerGit          = (*mockGitHandler)(nil)
	_ interfaces.HandlerSandbox      = (*mockSandboxHandler)(nil)
	_ interfaces.HandlerHarness      = (*mockHarnessHandler)(nil)
	_ interfaces.HandlerMCP          = (*mockMcpHandler)(nil)
	_ interfaces.Repository          = (*mockSessionRepository)(nil)
	_ interfaces.RepositoryOrg       = (*mockOrganizationRepository)(nil)
	_ interfaces.RepositoryGit       = (*mockGitRepository)(nil)
	_ shared.SecretManager           = (*mockSecretManager)(nil)
	_ interfaces.Queue               = (*mockQueue)(nil)
)
