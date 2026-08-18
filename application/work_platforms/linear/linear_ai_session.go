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

package linear

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/telemetry"
	"github.com/jazielguerrero/workdock/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	GitHubUrl           = "https://github.com/"
	GitHubInstallAppUrl = "https://github.com/apps/IamFoo-App-Dev/installations/new" // TODO: This MUST come from config
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
)

type linearAISessionConfig struct {
	HarnessRegistry    ports.HarnessPlatformRegistry
	GitHostingRegistry ports.GitHostingPlatformRegistry
	Client             LinearClientInterface
	ForSecrets         ports.ForSecrets
	Sessions           repositories.SessionRepository
	Job                *types.EventJob
	SessionEvent       *types.SessionEvent
	Session            *types.Session
	Payload            *AgentSessionEventData
}

type linearAISession struct {
	config            linearAISessionConfig
	accessToken       string
	githubAccessToken string
	tracer            trace.Tracer
}

func newLinearAISession(ctx context.Context, config linearAISessionConfig) (*linearAISession, error) {
	tokenHandler := newTokenHandler(tokenHandlerConfig{
		ForSecrets: config.ForSecrets,
		Client:     config.Client,
	})
	accessToken, err := tokenHandler.GetLinearAccessToken(ctx, config.Session.OrganizationIdentifier)

	if err != nil {
		return nil, err
	}

	return &linearAISession{
		config:      config,
		accessToken: accessToken,
		tracer:      otel.Tracer("workdock.linear"),
	}, nil
}

func newLinearAISessionForCancellation(ctx context.Context, config linearAISessionConfig) (*linearAISession, error) {
	tokenHandler := newTokenHandler(tokenHandlerConfig{
		ForSecrets: config.ForSecrets,
		Client:     config.Client,
	})
	accessToken, err := tokenHandler.GetLinearAccessToken(ctx, config.Session.OrganizationIdentifier)

	if err != nil {
		return nil, err
	}

	return &linearAISession{
		config:      config,
		accessToken: accessToken,
	}, nil
}

func (s *linearAISession) Cancel(ctx context.Context) {
	s.Response(ctx, "Request stopped")
}

func (s *linearAISession) Process(ctx context.Context) error {
	if err := telemetry.SpanErr(ctx, s.tracer, "linear.set_external_urls", func(ctx context.Context) error {
		return s.setAgentSessionExternalUrls(ctx)
	}); err != nil {
		s.reportServerInternalError(ctx)
		return err
	}

	// TODO: Fix/Get git hosting provider
	registry, ok := s.config.GitHostingRegistry[types.PlatformProvider_GitHub]

	if !ok {
		err := errors.New("github hosting provider not register")
		slog.Error("failed to process github webhook", "err", err)
		return err
	}

	accessGranted, accessToken, err := telemetry.Span2(ctx, s.tracer, "linear.verify_repo_access", func(ctx context.Context) (bool, string, error) {
		return registry.VerifyRepoAccess(ctx, s.config.Session.Identifier, s.config.Session.RepoFullName)
	})

	s.githubAccessToken = accessToken

	if err != nil {
		if errors.Is(err, types.ErrGitHubConnectionReRequested) {
			if err := s.notifyGitHubConnectionRequired(ctx); err != nil {
				s.reportServerInternalError(ctx)
				return err
			}

			// Cannot continue because it requires github access
			return nil
		}

		s.reportServerInternalError(ctx)
		return err
	}

	// TODO: Does this will apply for contributing on public repos?
	if s.config.Session.RepoFullName != nil && !accessGranted {
		if err := telemetry.SpanErr(ctx, s.tracer, "linear.request_repo_access", func(ctx context.Context) error {
			return registry.RequestConnection(ctx, s.config.SessionEvent.Identifier, *s.config.Session.RepoFullName)
		}); err != nil {
			return err
		}

		if err := s.notifyGitHubConnectionRequired(ctx); err != nil {
			s.reportServerInternalError(ctx)
			return err
		}

		// Cannot continue because it requires github access
		return nil
	}

	if s.config.Payload.Action == "created" {
		s.Thought(ctx, "")
	}

	// TODO: Implement a way of choosing harnesses
	harnessName := types.HarnessProvider_OpenCode
	createHarness, ok := s.config.HarnessRegistry[harnessName]

	if !ok {
		err := fmt.Errorf("harness %s not found", types.HarnessProvider_OpenCode)
		slog.Error("failed to select a harness", "err", err, "event_identifier", s.config.SessionEvent.Identifier)
		return err
	}

	s.Thought(ctx, "")

	harness, err := createHarness(ports.NewHarnessConstructor{
		Session:      s.config.Session,
		SessionEvent: s.config.SessionEvent,
		Prompt:       s.createPrompt(),
		Secrets: map[string]string{
			"linearAccessToken": s.accessToken,
			"githubAccessToken": s.githubAccessToken,
		},
		Parts: s, // this
	})

	if err != nil {
		if s.isContextCanceledOrDeadlineExceeded(err) {
			return nil
		}

		s.reportServerInternalError(ctx)
		return err
	}

	defer telemetry.SpanErr(ctx, s.tracer, "linear.harness.dispose", func(ctx context.Context) error {
		return harness.Dispose(context.Background())
	})

	result, err := telemetry.Span(ctx, s.tracer, "linear.harness.run", func(ctx context.Context) (*types.SessionEventResult, error) {
		return harness.Run(ctx)
	}, trace.WithAttributes(
		attribute.String("harness", string(harnessName)),
	))

	if err != nil {
		if s.isContextCanceledOrDeadlineExceeded(err) {
			return nil
		}

		if errors.Is(err, types.ErrHarnessUnhealthy) {
			slog.Error("harness declared unhealthy; job will be retried", "event_identifier", s.config.SessionEvent.Identifier)
			return err
		}

		s.reportServerInternalError(ctx)
		return err
	}

	if result != nil && result.PullRequest != nil {
		s.config.SessionEvent.Result = result
		s.config.SessionEvent.GitRef = &result.PullRequest.HeadRefName
		return telemetry.SpanErr(ctx, s.tracer, "linear.update_session_event", func(ctx context.Context) error {
			return s.config.Sessions.UpdateSessionEventResult(ctx, s.config.SessionEvent)
		})
	}

	return nil
}

func (s *linearAISession) createPrompt() string {
	repo := ""

	if s.config.Session.RepoFullName != nil {
		repo = *s.config.Session.RepoFullName
	}

	prompt := strings.TrimSpace(fmt.Sprintf(PromptTemplate_WorkItem,
		s.config.Payload.AgentSession.Issue.Title,
		s.config.Payload.AgentSession.Issue.Identifier,
		repo,
		s.config.Payload.AgentSession.Issue.Description,
		s.config.Payload.PromptContext,
	))

	// Hope this logic doesn't cause odd behavior, if this
	// condition is true, it means the event did not came from
	// linear, but from a pr comment event
	if s.config.SessionEvent.GitRef != nil && s.config.SessionEvent.Seed != nil {
		prompt += fmt.Sprintf(PromptTemplate_LatestUserComment, "There are review comments on the pull request. Retrieve all review comments and address each one that is applicable to the current implementation. Make the necessary code changes, verify the changes, and ensure the pull request is ready for review again.")
	} else if s.config.Payload.AgentActivity != nil {
		prompt += fmt.Sprintf(PromptTemplate_LatestUserComment, s.config.Payload.AgentActivity.Content.Body)
	}

	slog.Debug("Prompt prepared", "event_identifier", s.config.SessionEvent.Identifier)
	return prompt
}

func (s *linearAISession) setAgentSessionExternalUrls(ctx context.Context) error {
	labels, err := s.config.Client.GetIssueLabels(ctx, s.accessToken, s.config.Session.IssueId)

	if err != nil {
		slog.Error("failed to get issue labels", "issue_id", s.config.Session.IssueId, "err", err)
		return err
	}

	for _, label := range labels {
		if after, ok := strings.CutPrefix(label.Name, "repo="); ok {
			found := false
			repoFullName := after
			externalURLs := s.config.Payload.AgentSession.ExternalUrls
			updated := make([]ExternalURL, 0, len(externalURLs)+1)

			// Update the storage if needed
			if s.config.Session.RepoFullName == nil || *s.config.Session.RepoFullName != repoFullName {
				s.config.Session.RepoFullName = new(repoFullName)

				if err := telemetry.SpanErr(ctx, s.tracer, "linear.set_external_urls.upsert_session", func(ctx context.Context) error {
					return s.config.Sessions.UpsertAgentSession(ctx, s.config.Session)
				}); err != nil {
					return err
				}
				slog.Debug("Configured session repo", "event_identifier", s.config.SessionEvent.Identifier)
			}

			// If linear it's already updated, skip the linear update
			if slices.ContainsFunc(externalURLs, func(e ExternalURL) bool {
				return e.URL == GitHubUrl+repoFullName
			}) {
				continue
			}

			for _, ext := range externalURLs {
				if ext.Label == "repo" {
					ext.URL = GitHubUrl + repoFullName
					found = true
				}

				updated = append(updated, ext)
			}

			if !found {
				updated = append(updated, ExternalURL{
					Label: "repo",
					URL:   GitHubUrl + repoFullName,
				})
			}

			if _, err := telemetry.Span(ctx, s.tracer, "linear.set_external_urls.request", func(ctx context.Context) (any, error) {
				return s.config.Client.SetExternalURLs(ctx, s.accessToken, SetExternalURLsInput{
					SessionID:    s.config.Session.Identifier,
					ExternalURLs: updated,
				})
			}); err != nil {
				return err
			}

			slog.Debug("Configured session external url repo", "event_identifier", s.config.SessionEvent.Identifier)
		}
	}

	return nil
}

// notifyGitHubConnectionRequired notifies the user to authorize GitHub.
func (s *linearAISession) notifyGitHubConnectionRequired(ctx context.Context) error {
	slog.Debug("Request github connection")
	return s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Session.Identifier,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Elicitation,
			Body: "GitHub connection required",
		},
		Signal: SignalType_Auth,
		SignalMetadata: map[string]any{
			"url":          GitHubInstallAppUrl,
			"providerName": "GitHub",
		},
	})
}

func (s *linearAISession) Thought(ctx context.Context, text string) {
	s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Session.Identifier,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Thought,
			Body: text,
		},
	})
}

func (s *linearAISession) Response(ctx context.Context, text string) {
	s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Session.Identifier,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Response,
			Body: text,
		},
	})
}

func (s *linearAISession) Action(ctx context.Context, action types.AgentAction) {
	s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Session.Identifier,
		Content: AgentActivityContent{
			Type:      AgentActivityContentType_Action,
			Action:    action.Name,
			Parameter: action.Input,
			Result:    action.Output,
		},
	})
}

func (s *linearAISession) Elicitation(ctx context.Context, elicitation types.AgentElicitation) {
	s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Session.Identifier,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Elicitation,
			Body: elicitation.Question,
		},
		Signal: SignalType_Select,
		SignalMetadata: map[string]any{
			"options": elicitation.Options,
		},
	})
}

// reportServerInternalError notifies the user about an unexpected
// server-side error via a best-effort Linear activity.
func (s *linearAISession) reportServerInternalError(ctx context.Context) {
	s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Payload.AgentSession.ID,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Error,
			Body: "Internal Server Error 500",
		},
	})
}

func (s *linearAISession) isContextCanceledOrDeadlineExceeded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
