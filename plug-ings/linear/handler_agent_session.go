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

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ings/linear/helpers"
	"github.com/workdock-dev/engine/plug-ings/linear/interfaces"
	"github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/shared"
)

type AgentSessionHandler struct {
	client       interfaces.Client
	tokenHandler *helpers.TokenHandler
}

func NewAgentSessionHandler(
	client interfaces.Client,
	secretManager shared.ForSecrets,
) agent_session_interfaces.HandlerAgentSession {
	return &AgentSessionHandler{
		client:       client,
		tokenHandler: helpers.NewTokenHandler(secretManager, client),
	}
}

func (h *AgentSessionHandler) Ingest(event shared.DomainEvent) (*shared.Session, *shared.SessionEvent, error) {
	agentSessionEvent, ok := event.(shared.AgentSessionEvent)

	if !ok {
		return nil, nil, fmt.Errorf("[agent-session][linear] failed to ingest, expected %s got %s", shared.EventType_AgentSession, event.EventType())
	}

	linearEvent, ok := agentSessionEvent.Payload.(types.AgentSessionEventData)

	if !ok {
		return nil, nil, fmt.Errorf("[agent-session][linear] failed to cast payload")
	}

	slog.Debug(
		"[agent-session][linear] generated idempotency key from",
		"id", linearEvent.AgentSession.ID,
		"timestamp", linearEvent.AgentSession.UpdatedAt,
		"seed", nil,
	)

	key, err := agent_session_types.GenerateIdempotencyKey(map[string]any{
		"id":        linearEvent.AgentSession.ID,
		"timestamp": linearEvent.AgentSession.UpdatedAt,
		"seed":      nil,
	})

	if err != nil {
		return nil, nil, err
	}

	payload, err := json.Marshal(linearEvent)

	if err != nil {
		slog.Error("failed to marshal agent session payload")
		return nil, nil, err
	}

	session := &shared.Session{
		OrganizationIdentifier: linearEvent.OrganizationID,
		Identifier:             linearEvent.AgentSession.ID,
		Provider:               shared.PlatformProvider_Linear,
		IssueId:                linearEvent.AgentSession.Issue.ID,
		Creator:                linearEvent.AgentSession.Creator.Name,
		RepoFullName:           nil,
	}

	sessionEvent := &shared.SessionEvent{
		SessionIdentifier: session.Identifier,
		Identifier:        key,
		Payload:           payload,
	}

	return session, sessionEvent, nil
}

func (h *AgentSessionHandler) GetCredentials(ctx context.Context, orgId string) (string, error) {
	return h.tokenHandler.GetLinearAccessToken(ctx, orgId)
}

func (h *AgentSessionHandler) GetPromptContext(sessionEvent *shared.SessionEvent) (*agent_session_interfaces.PromptContext, error) {
	var linearEvent types.AgentSessionEventData

	if err := json.Unmarshal(sessionEvent.Payload, &linearEvent); err != nil {
		slog.Error("[agent-session][linear] failed to unmarshal payload", "err", err, "event_identifier", sessionEvent.Identifier)
		return nil, err
	}

	if linearEvent.AgentSession == nil {
		err := errors.New("[agent-session][linear] event in corrupted state")
		return nil, err
	}

	var context *string

	if linearEvent.AgentActivity != nil {
		context = &linearEvent.AgentActivity.Content.Body
	}

	return &agent_session_interfaces.PromptContext{
		Prompt:  linearEvent.PromptContext,
		Context: context,
		Issue: shared.Issue{
			Title:       linearEvent.AgentSession.Issue.Title,
			Identifier:  linearEvent.AgentSession.Issue.Identifier,
			Description: linearEvent.AgentSession.Issue.Description,
		},
	}, nil
}

func (h *AgentSessionHandler) SendThought(ctx context.Context, sessionId, accessToken, text string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type: types.AgentActivityContentType_Thought,
			Body: text,
		},
	})
}

func (h *AgentSessionHandler) SendResponse(ctx context.Context, sessionId, accessToken, text string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type: types.AgentActivityContentType_Response,
			Body: text,
		},
	})
}

func (h *AgentSessionHandler) SendAction(ctx context.Context, sessionId, accessToken string, action shared.AgentAction) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type:      types.AgentActivityContentType_Action,
			Action:    action.Name,
			Parameter: action.Input,
			Result:    action.Output,
		},
	})
}

func (h *AgentSessionHandler) SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation shared.AgentElicitation) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type: types.AgentActivityContentType_Elicitation,
			Body: elicitation.Question,
		},
		Signal: types.SignalType_Select,
		SignalMetadata: map[string]any{
			"options": elicitation.Options,
		},
	})
}

func (h *AgentSessionHandler) SendGitConnectionRequest(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type: types.AgentActivityContentType_Elicitation,
			Body: "Git connection required",
		},
		Signal: types.SignalType_Auth,
		SignalMetadata: map[string]any{
			"url":          gitInstallURL,
			"providerName": gitProvider,
		},
	})
}

// reportServerInternalError notifies the user about an unexpected
// server-side error via a best-effort Linear activity.
func (h *AgentSessionHandler) SendServerInternalError(ctx context.Context, sessionId, accessToken string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type: types.AgentActivityContentType_Error,
			Body: "Internal Server Error 500",
		},
	})
}
