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

	"github.com/workdock-dev/engine/application"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/repositories"
	domain_service "github.com/workdock-dev/engine/domain/service"
	"github.com/workdock-dev/engine/domain/telemetry"
	"github.com/workdock-dev/engine/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	GitHubUrl = "https://github.com/"
)

type linearAISessionConfig struct {
	HarnessRegistry     ports.HarnessPlatformRegistry
	GitHostingRegistry  ports.GitHostingPlatformRegistry
	Client              LinearClientInterface
	ForSecrets          ports.ForSecrets
	Sessions            repositories.SessionRepository
	Job                 *types.EventJob
	SessionEvent        *types.SessionEvent
	Session             *types.Session
	Payload             *AgentSessionEventData
	GitHubAppInstallURL string
	App                *application.App
}

type linearAISession struct {
	config            linearAISessionConfig
	app               *application.App
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
		app:         config.App,
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
		app:         config.App,
		accessToken: accessToken,
	}, nil
}

func (s *linearAISession) Cancel(ctx context.Context) {
	s.Response(ctx, "Request stopped")
}

func (s *linearAISession) Process(ctx context.Context) error {
	defer func() {
		s.Response(ctx, "")
	}()

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

	// Request GitHub App installation when repo access is not yet granted.
	// This applies to both public and private repositories — write operations
	// (push branches, create PRs) require an authenticated GitHub App installation.
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

		if s.config.Job != nil && s.config.Job.WillRetry {
			s.notifyRetryScheduled(ctx)
		} else {
			s.reportServerInternalError(ctx)
		}

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
	promptFactory := s.app.GetPromptFactory()

	var latestComment *string
	if s.config.Payload.AgentActivity != nil {
		latestComment = &s.config.Payload.AgentActivity.Content.Body
	}

	prompt := promptFactory.BuildWorkItemPrompt(
		s.config.Session,
		s.config.SessionEvent,
		s.config.Payload.AgentSession.Issue.Title,
		s.config.Payload.AgentSession.Issue.Identifier,
		s.config.Payload.AgentSession.Issue.Description,
		s.config.Payload.PromptContext,
		latestComment,
	)

	slog.Debug("Prompt prepared", "event_identifier", s.config.SessionEvent.Identifier)
	return prompt
}

func (s *linearAISession) setAgentSessionExternalUrls(ctx context.Context) error {
	labels, err := s.config.Client.GetIssueLabels(ctx, s.accessToken, s.config.Session.IssueId)

	if err != nil {
		slog.Error("failed to get issue labels", "issue_id", s.config.Session.IssueId, "err", err)
		return err
	}

	domainLabels := make([]domain_service.Label, len(labels))
	for i, l := range labels {
		domainLabels[i] = domain_service.Label{Name: l.Name}
	}

	existingURLs := make([]types.ExternalURL, len(s.config.Payload.AgentSession.ExternalUrls))
	for i, u := range s.config.Payload.AgentSession.ExternalUrls {
		existingURLs[i] = types.ExternalURL{Label: u.Label, URL: u.URL}
	}

	originalRepoFullName := s.config.Session.RepoFullName

	sessionConfigService := s.app.GetSessionConfigService()
	result := sessionConfigService.ConfigureSessionRepo(domain_service.ConfigureSessionRepoInput{
		Session:      s.config.Session,
		Labels:       domainLabels,
		ExistingURLs: existingURLs,
	})

	if result.RepoFound && (originalRepoFullName == nil || *result.UpdatedSession.RepoFullName != *originalRepoFullName) {
		if err := telemetry.SpanErr(ctx, s.tracer, "linear.set_external_urls.upsert_session", func(ctx context.Context) error {
			return s.config.Sessions.UpsertAgentSession(ctx, result.UpdatedSession)
		}); err != nil {
			return err
		}
		s.config.Session = result.UpdatedSession
		slog.Debug("Configured session repo", "event_identifier", s.config.SessionEvent.Identifier)
	}

	if len(result.UpdatedURLs) > len(existingURLs) || hasURLChanged(existingURLs, result.UpdatedURLs) {
		updatedExtURLs := make([]ExternalURL, len(result.UpdatedURLs))
		for i, u := range result.UpdatedURLs {
			updatedExtURLs[i] = ExternalURL{Label: u.Label, URL: u.URL}
		}

		if _, err := telemetry.Span(ctx, s.tracer, "linear.set_external_urls.request", func(ctx context.Context) (any, error) {
			return s.config.Client.SetExternalURLs(ctx, s.accessToken, SetExternalURLsInput{
				SessionID:    s.config.Session.Identifier,
				ExternalURLs: updatedExtURLs,
			})
		}); err != nil {
			return err
		}

		slog.Debug("Configured session external url repo", "event_identifier", s.config.SessionEvent.Identifier)
	}

	return nil
}

func hasURLChanged(oldURLs, newURLs []types.ExternalURL) bool {
	if len(newURLs) != len(oldURLs) {
		return true
	}
	for i := range oldURLs {
		if oldURLs[i].URL != newURLs[i].URL {
			return true
		}
	}
	return false
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
			"url":          s.config.GitHubAppInstallURL,
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

func (s *linearAISession) notifyRetryScheduled(ctx context.Context) {
	slog.Debug("notifying user about retry", "event_identifier", s.config.SessionEvent.Identifier)
	s.config.Client.CreateAgentActivity(ctx, s.accessToken, CreateAgentActivityInput{
		AgentSessionID: s.config.Session.Identifier,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Error,
			Body: "Execution failed but will be retried automatically.",
		},
	})
}
