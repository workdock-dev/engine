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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/workdock-dev/engine/application"
	domain_service "github.com/workdock-dev/engine/domain/service"
	"github.com/workdock-dev/engine/domain/factories"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
)

// Config holds the dependencies required to run Linear sessions.
type Config struct {
	Client              LinearClientInterface
	GitHubAppInstallURL string
}

// linearPlatform adapts AIService to the Linear provider. It normalizes Linear
// webhook events and executes or stops sessions on the linearAISession worker.
type linearPlatform struct {
	app    *application.App
	config Config
}

func New(config Config, app *application.App) ports.ForWorkPlatform {
	p := &linearPlatform{
		app:    app,
		config: config,
	}

	if app.GetEventBus() != nil {
		eventType := types.PlatformWebhookEvent(types.PlatformProvider_Linear)
		slog.Debug("linearPlatform subscribed for event", "event_type", eventType)

		app.GetEventBus().Subscribe(
			eventType,
			func(ctx context.Context, event ports.DomainEvent) error {
				e, ok := event.(types.WebhookEvent)

				if !ok {
					return fmt.Errorf("expected a webhook event, received %s", event.EventType())
				}

				if e.Type != types.WebhookEventType_IssueStateUpdated {
					return nil
				}

				issueChange, ok := e.Payload.(*IssueStatusChangePayload)
				if !ok {
					slog.Debug("issue-state-updated event payload is not an IssueStatusChangePayload")
					return nil
				}

				if issueChange.Action != "update" {
					return nil
				}

				return p.archiveSandboxForIssue(ctx, issueChange.Data.ID)
			},
		)
	}

	return p
}

// BeginOAuth initiates the OAuth flow and returns the redirect URL
// for Linear's authorization page.
func (p *linearPlatform) BeginOAuth(ctx context.Context) string {
	return p.config.Client.OauthAuthorize(ctx)
}

// CompleteOAuth finalises the OAuth callback, persists the
// access token and workspace metadata, and returns the workspace info for
// use in the response.
func (p *linearPlatform) CompleteOAuth(ctx context.Context, code, errorP string) (string, error) {
	event, err := p.config.Client.OauthCallback(ctx, code, errorP)

	if err != nil {
		return "", err
	}

	data, err := json.Marshal(&event.Token)

	if err != nil {
		slog.Error("failed to marshal token", "err", err)
		return "", types.ErrInternalServerError
	}

	if err := p.app.GetForSecrets().Set(ctx, SecretsPath, event.Workspace.ID, string(data)); err != nil {
		return "", types.ErrInternalServerError
	}

	if err := p.app.GetOrganizations().UpsertOrganization(ctx, &types.Organization{
		Identifier: event.Workspace.ID,
		Provider:   types.PlatformProvider_Linear,
		Name:       event.Workspace.Name,
	}); err != nil {
		return "", types.ErrInternalServerError
	}

	return event.Workspace.Name, nil
}

// Ingest converts a raw Linear webhook event into the normalized session and
// its associated session event, computing the idempotency key from the agent
// session identity. Seed alters the generated idempotency key, useful for when
// it's needed to process the same request without altering the request's payload
func (p *linearPlatform) Ingest(event any, seed *string, from *types.SessionEvent) (*types.Session, *types.SessionEvent, error) {
	linearEvent, err := p.castAnyToAgentSessionEventData(event)

	if err != nil {
		return nil, nil, err
	}

	var s string
	if seed != nil {
		s = *seed
	}

	slog.Debug("Generated linear agent session event idempotency key from", "id", linearEvent.AgentSession.ID, "timestamp", linearEvent.AgentSession.UpdatedAt, "seed", s)

	keyFactory := &factories.IdempotencyKeyFactory{}
	key, err := keyFactory.Build(factories.IdempotencyKeyInput{
		ID:        linearEvent.AgentSession.ID,
		Timestamp: linearEvent.AgentSession.UpdatedAt,
		Seed:      seed,
	})

	if err != nil {
		return nil, nil, err
	}

	payload, err := json.Marshal(linearEvent)

	if err != nil {
		slog.Error("failed to marshal agent session payload")
		return nil, nil, err
	}

	var gitRef *string

	if from != nil && from.GitRef != nil {
		gitRef = from.GitRef
	}

	session := &types.Session{
		OrganizationIdentifier: linearEvent.OrganizationID,
		Identifier:             linearEvent.AgentSession.ID,
		Provider:               types.PlatformProvider_Linear,
		IssueId:                linearEvent.AgentSession.Issue.ID,
		Creator:                linearEvent.AgentSession.Creator.Name,
		RepoFullName:           nil,
	}

	sessionEvent := &types.SessionEvent{
		SessionIdentifier: session.Identifier,
		Identifier:        key,
		Payload:           payload,
		GitRef:            gitRef, // When set, pull request is associated
		// Result not set, is that ok?
		Seed: seed,
	}

	return session, sessionEvent, nil
}

// Process executes the Linear workflow for a claimed event job.
func (p *linearPlatform) Process(ctx context.Context, config ports.ProcessConfig) error {
	var linearEvent AgentSessionEventData

	if err := json.Unmarshal(config.SessionEvent.Payload, &linearEvent); err != nil {
		slog.Error("failed to unmarshal agent session payload", "err", err, "event_identifier", config.SessionEvent.Identifier)
		return err
	}

	if linearEvent.AgentSession == nil {
		err := errors.New("linear event in corrupted state")
		return err
	}

	service, err := newLinearAISession(ctx, linearAISessionConfig{
		App:                 p.app,
		Client:              p.config.Client,
		Job:                 config.Job,
		SessionEvent:        config.SessionEvent,
		Session:             config.Session,
		Payload:             &linearEvent,
		GitHubAppInstallURL: p.config.GitHubAppInstallURL,
	})

	if err != nil {
		return err
	}

	return service.Process(ctx)
}

// Cancel stops an in-flight Linear session.
func (p *linearPlatform) Cancel(ctx context.Context, session *types.Session) error {
	service, err := newLinearAISessionForCancellation(ctx, linearAISessionConfig{
		App:     p.app,
		Client:  p.config.Client,
		Session: session,
	})

	if err != nil {
		return err
	}

	service.Cancel(ctx)
	return nil
}

func (p *linearPlatform) IsCancelSignal(ctx context.Context, event any) (bool, error) {
	linearEvent, err := p.castAnyToAgentSessionEventData(event)

	if err != nil {
		return false, err
	}

	classificationService := &domain_service.EventClassificationService{}
	domainEvent := &domain_service.LinearAgentSessionEvent{
		AgentActivity: &domain_service.LinearAgentActivity{
			Signal: linearEvent.AgentActivity.Signal,
		},
	}

	return classificationService.IsCancelSignal(domainEvent), nil
}

// Webhook handles an incoming webhook request from the any platform.
func (p *linearPlatform) Webhook(ctx context.Context, req types.WebhookRequest) (any, types.WebhookEventType, error) {
	return p.config.Client.Webhook(ctx, req)
}

func (p *linearPlatform) castAnyToAgentSessionEventData(event any) (*AgentSessionEventData, error) {
	if event == nil {
		err := errors.New("received event is nil")
		slog.Error("failed to cast any to AgentSessionEventData", "err", err)
		return nil, err
	}

	var linearEvent *AgentSessionEventData

	switch event := event.(type) {
	case *AgentSessionEventData:
		linearEvent = event

	case json.RawMessage:
		linearEvent = new(AgentSessionEventData)

		if err := json.Unmarshal(event, linearEvent); err != nil {
			slog.Error("failed to unmarshal linear event", "provider", types.PlatformProvider_Linear, "error", err)
			return nil, types.ErrInternalServerError
		}

	default:
		slog.Error("received an event of an unexpected type from the linear provider", "provider", types.PlatformProvider_Linear, "type", fmt.Sprintf("%T", event))
		return nil, types.ErrInternalServerError
	}

	return linearEvent, nil
}

// archiveSandboxForIssue archives the sandbox associated with a session when
// an issue transitions to a "done" status. It looks up all sessions for the
// given issue ID, queries Linear for the current issue state, and only proceeds
// with archiving when the issue state type is "completed".
func (p *linearPlatform) archiveSandboxForIssue(ctx context.Context, issueId string) error {
	sessions, err := p.app.GetSessions().GetAgentSessionsByIssueId(ctx, issueId)

	if err != nil {
		slog.Error("failed to get sessions for issue", "err", err, "issue_id", issueId)
		return err
	}

	if len(sessions) == 0 {
		slog.Debug("no sessions found for issue, nothing to archive", "issue_id", issueId)
		return nil
	}

	accessToken, err := newTokenHandler(tokenHandlerConfig{
		ForSecrets: p.app.GetForSecrets(),
		Client:     p.config.Client,
	}).GetLinearAccessToken(ctx, sessions[0].OrganizationIdentifier)

	if err != nil {
		slog.Error("failed to get linear access token for issue", "err", err, "issue_id", issueId)
		return err
	}

	issue, err := p.config.Client.GetIssue(ctx, accessToken, issueId)

	if err != nil {
		slog.Error("failed to get issue state from linear", "err", err, "issue_id", issueId)
		return err
	}

	if !p.app.GetIssueLifecycleService().ShouldArchiveForIssue(issue.StateType) {
		slog.Debug("issue state is not done, skipping archive", "issue_id", issueId, "state_type", issue.StateType)
		return nil
	}

	for _, session := range sessions {
		harnessConstructor, ok := p.app.GetHarnessRegistry()[types.HarnessProvider_OpenCode]

		if !ok {
			slog.Error("harness provider not found in registry", "provider", types.HarnessProvider_OpenCode)
			continue
		}

		harness, err := harnessConstructor(ports.NewHarnessConstructor{
			Session: session,
			Secrets: map[string]string{
				"linearAccessToken": "nop-secret",
			},
		})

		if err != nil {
			slog.Error("failed to create harness for archive", "err", err, "session_identifier", session.Identifier)
			continue
		}

		if err := harness.Archive(ctx); err != nil {
			slog.Error("failed to archive sandbox for session", "err", err, "session_identifier", session.Identifier, "issue_id", issueId)
			continue
		}
	}

	return nil
}
