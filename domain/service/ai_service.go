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

package domain_service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/telemetry"
	"github.com/jazielguerrero/workdock/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type AIServiceConfig struct {
	WorkPlatformRegistry ports.WorkPlatformRegistry
	ForEvent             ports.ForEventBus
	Organizations        repositories.OrganizationRepository
	Sessions             repositories.SessionRepository
}

// AIService owns the business logic for agent sessions across providers.
//
// Session ingests and persists a provider-specific session event, Process
// orchestrates the execution of a claimed event job by dispatching to the
// provider-specific handler, and Cancel stops an in-flight session.
type AIService struct {
	config AIServiceConfig
	tracer trace.Tracer
}

func NewAIService(config AIServiceConfig) *AIService {
	s := &AIService{
		config: config,
		tracer: otel.Tracer("workdock.ai_service"),
	}

	for key := range config.WorkPlatformRegistry {
		eventType := types.PlatformWebhookEvent(key)

		slog.Debug("AIService subscribed for event", "event_type", eventType)
		s.config.ForEvent.Subscribe(
			eventType,
			func(ctx context.Context, event ports.DomainEvent) error {
				e, ok := event.(types.WebhookEvent)

				if !ok {
					return fmt.Errorf("expected a webhook event, received %s", event.EventType())
				}

				if e.Type != types.WebhookEventType_AIRequest {
					slog.Debug("AIService skipping non-AI-request webhook event", "type", e.Type)
					return nil
				}

				workPlatform, err := s.platform(e.Provider)

				if err != nil {
					return err
				}

				isCancel, err := workPlatform.IsCancelSignal(ctx, e.Payload)

				if err != nil {
					return err
				}

				if isCancel {
					return s.Cancel(ctx, e.Provider, e.Payload)
				}

				return s.Session(ctx, e.Provider, e.Payload, nil, nil)
			},
		)
	}

	slog.Debug("AIService subscribed for event", "event_type", types.EventType_GitHubConnected)
	s.config.ForEvent.Subscribe(
		types.EventType_GitHubConnected,
		func(ctx context.Context, event ports.DomainEvent) error {
			connection, ok := event.(types.GitHubConnectedEvent)

			if !ok {
				return fmt.Errorf("expected a github connection event, received %s", event.EventType())
			}

			if connection.Connection.SessionEventIdentifier == nil {
				slog.Debug("GitHubConnectedEvent received but session_event_identifier is nil, skipping AI session resumption")
				return nil
			}

			sessionEvent, err := s.config.Sessions.GetAgentSessionEvent(ctx, *connection.Connection.SessionEventIdentifier)

			if err != nil {
				return err
			}

			if sessionEvent == nil {
				slog.Debug("session event not found for GitHubConnectedEvent, skipping AI session resumption", "session_event_identifier", *connection.Connection.SessionEventIdentifier)
				return nil
			}

			session, err := s.config.Sessions.GetAgentSession(ctx, sessionEvent.SessionIdentifier)

			if err != nil {
				return err
			}

			if session == nil {
				return fmt.Errorf("session not found: %s", sessionEvent.SessionIdentifier)
			}

			return s.Session(ctx, session.Provider, sessionEvent.Payload, new(uuid.NewString()), sessionEvent)
		},
	)

	slog.Debug("AIService subscribed for event", "event_type", types.EventType_PullRequestCommented)
	s.config.ForEvent.
		Subscribe(
			types.EventType_PullRequestCommented,
			func(ctx context.Context, event ports.DomainEvent) error {
				e, ok := event.(types.PullRequestCommentedEvent)

				if !ok {
					return fmt.Errorf("expected a pull request commented event, received %s", event.EventType())
				}

				sessionEvent, err := s.config.Sessions.GetAgentSessionEventByGitRef(ctx, e.GitRef, e.RepoFullName)

				if err != nil {
					return err
				}

				if sessionEvent == nil {
					return fmt.Errorf("session event not found: %s@%s", e.GitRef, e.RepoFullName)
				}

				session, err := s.config.Sessions.GetAgentSession(ctx, sessionEvent.SessionIdentifier)

				if err != nil {
					return err
				}

				if session == nil {
					return fmt.Errorf("session not found: %s", sessionEvent.SessionIdentifier)
				}

				return s.Session(ctx, session.Provider, sessionEvent.Payload, new(uuid.NewString()), sessionEvent)
			},
		)

	return s
}

// Session validates and transforms a provider-specific session event into the
// application's domain model. It is responsible for validation, normalization,
// idempotency, and persistence of the session and its associated event.
//
// This method does not perform AI interactions or communicate with the provider
// or any LLM. Its responsibility is limited to preparing and persisting the
// domain state required for subsequent processing.
//
// The seed is use by the providers to alters the generated idempotency key,
// useful for when it's needed to process the same request without altering
// the request's payload
func (s *AIService) Session(ctx context.Context, name types.PlatformProvider, event any, seed *string, from *types.SessionEvent) error {
	workPlatform, err := s.platform(name)

	if err != nil {
		return err
	}

	session, sessionEvent, err := workPlatform.Ingest(event, seed, from)

	if err != nil {
		return err
	}

	if org, err := s.config.Organizations.GetOrganization(ctx, session.OrganizationIdentifier); err != nil {
		return err
	} else if org == nil {
		// TODO: Inform the user they need to initialize their account
		// Received an agent session event from an unknown organization
		return err
	}

	if sess, err := s.config.Sessions.GetAgentSession(ctx, session.Identifier); err != nil {
		return err
	} else if sess == nil {
		// Session doesn't exist, create it
		if err := s.config.Sessions.UpsertAgentSession(ctx, session); err != nil {
			return err
		}
	} else {
		sEvent, err := s.config.Sessions.GetAgentSessionEvent(ctx, sessionEvent.Identifier)

		if err != nil {
			return err
		}

		if sEvent != nil {
			slog.Debug("received duplicated event", "event_identifier", sessionEvent.Identifier)
			return nil
		}

		session = sess
	}

	if err := s.config.Sessions.CreateSessionEvent(ctx, sessionEvent); err != nil {
		return err
	}

	slog.Debug("Created session event", "event_identifier", sessionEvent.Identifier)
	return nil
}

// Cancel validates and transforms a provider-specific cancellation request,
// persists the session's cancellation state, and dispatches the request to the
// corresponding provider-specific handler.
//
// This method performs provider-independent validation, normalization, state
// persistence, and routing. Provider-specific cancellation behavior is
// delegated to the selected handler. Depending on the provider
// implementation, cancellation may be performed synchronously or
// cooperatively.
func (s *AIService) Cancel(ctx context.Context, name types.PlatformProvider, event any) error {
	workPlatform, err := s.platform(name)

	if err != nil {
		return err
	}

	session, _, err := workPlatform.Ingest(event, nil, nil)

	if err != nil {
		return err
	}

	if err := workPlatform.Cancel(ctx, session); err != nil {
		return err
	}

	count, err := s.config.Sessions.CancelSession(ctx, session.Identifier, "cancelled by user")

	if err != nil {
		return err
	}

	slog.Debug("Cancelled session", "count", count, "queued_by", session.Identifier)
	return nil
}

// Process validates the state of a domain session wrapped by an event job and
// dispatches its execution to the appropriate provider-specific handler.
//
// This method is responsible for orchestrating the processing lifecycle of a
// session. It performs provider-independent validation and routing, while
// delegating provider-specific behavior and AI interactions to the selected
// handler.
//
// Processing is synchronous and may block for an unbounded period of time,
// depending on the provider implementation and the duration of any AI
// interactions performed while handling the request.
func (s *AIService) Process(ctx context.Context, job *types.EventJob) error {
	sessionEvent, err := telemetry.Span(ctx, s.tracer, "session.get_event", func(ctx context.Context) (*types.SessionEvent, error) {
		return s.config.Sessions.GetAgentSessionEvent(ctx, job.SessionEventIdentifier)
	})

	if err != nil || sessionEvent == nil {
		return err
	}

	session, err := telemetry.Span(ctx, s.tracer, "session.get", func(ctx context.Context) (*types.Session, error) {
		return s.config.Sessions.GetAgentSession(ctx, sessionEvent.SessionIdentifier)
	})

	if err != nil || session == nil {
		return err
	}

	sessionProvider, err := s.platform(session.Provider)

	if err != nil {
		return err
	}

	return telemetry.SpanErr(ctx, s.tracer, "session.process", func(ctx context.Context) error {
		return sessionProvider.Process(ctx, ports.ProcessConfig{
			Job:          job,
			SessionEvent: sessionEvent,
			Session:      session,
		})
	}, trace.WithAttributes(
		attribute.String("session.organization.id", session.OrganizationIdentifier),
		attribute.String("session.identifier", session.Identifier),
		attribute.String("session_event.identifier", sessionEvent.Identifier),
		attribute.String("session.provider", string(session.Provider)),
	))
}

func (s *AIService) platform(name types.PlatformProvider) (ports.ForWorkPlatform, error) {
	registry, ok := s.config.WorkPlatformRegistry[name]

	if !ok {
		err := fmt.Errorf("failed to load work platform from registry %s", name)
		return nil, err
	}

	return registry, nil
}
