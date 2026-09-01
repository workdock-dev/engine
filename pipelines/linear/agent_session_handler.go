package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/workdock-dev/engine/pipelines/runners"
)

type AgentSessionHandler struct {
	client       LinearClientInterface
	tokenHandler *tokenHandler
}

func NewAgentSessionHandler(client LinearClientInterface, secretManager ports.ForSecrets) runners.AgentSessionHandler {
	return &AgentSessionHandler{
		client:       client,
		tokenHandler: newTokenHandler(secretManager, client),
	}
}

func (h *AgentSessionHandler) Ingest(event ports.DomainEvent) (*types.Session, *types.SessionEvent, error) {
	agentSessionEvent, ok := event.(types.LinearAgentSession[AgentSessionEventData])

	if !ok {
		return nil, nil, fmt.Errorf("failed to ingest linear agent, expected %s got %s", types.EventType_LinearAgentSession, event.EventType())
	}

	linearEvent := agentSessionEvent.Payload

	slog.Debug(
		"Generated linear agent session event idempotency key from",
		"id", linearEvent.AgentSession.ID,
		"timestamp", linearEvent.AgentSession.UpdatedAt,
		"seed", nil,
	)

	key, err := types.GenerateIdempotencyKey(map[string]any{
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

	// var gitRef *string

	// if from != nil && from.GitRef != nil {
	// 	gitRef = from.GitRef
	// }

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
		// GitRef:            gitRef, // When set, pull request is associated
		// Result not set, is that ok?
		// Seed: seed,
	}

	return session, sessionEvent, nil
}

func (h *AgentSessionHandler) GetCredentials(ctx context.Context, orgId string) (string, error) {
	return h.tokenHandler.GetLinearAccessToken(ctx, orgId)
}

func (h *AgentSessionHandler) GetPromptContext(sessionEvent *types.SessionEvent) (*runners.PromptContext, error) {
	var linearEvent AgentSessionEventData

	if err := json.Unmarshal(sessionEvent.Payload, &linearEvent); err != nil {
		slog.Error("failed to unmarshal agent session payload", "err", err, "event_identifier", sessionEvent.Identifier)
		return nil, err
	}

	if linearEvent.AgentSession == nil {
		err := errors.New("linear event in corrupted state")
		return nil, err
	}

	var context *string

	if linearEvent.AgentActivity != nil {
		context = &linearEvent.AgentActivity.Content.Body
	}

	return &runners.PromptContext{
		Prompt:  linearEvent.PromptContext,
		Context: context,
		Issue: types.Issue{
			Title:       linearEvent.AgentSession.Issue.Title,
			Identifier:  linearEvent.AgentSession.Issue.Identifier,
			Description: linearEvent.AgentSession.Issue.Description,
		},
	}, nil
}

func (h *AgentSessionHandler) SendThought(ctx context.Context, sessionId, accessToken, text string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Thought,
			Body: text,
		},
	})
}

func (h *AgentSessionHandler) SendResponse(ctx context.Context, sessionId, accessToken, text string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Response,
			Body: text,
		},
	})
}

func (h *AgentSessionHandler) SendAction(ctx context.Context, sessionId, accessToken string, action types.AgentAction) error {
	return h.client.CreateAgentActivity(ctx, accessToken, CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: AgentActivityContent{
			Type:      AgentActivityContentType_Action,
			Action:    action.Name,
			Parameter: action.Input,
			Result:    action.Output,
		},
	})
}

func (h *AgentSessionHandler) SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation types.AgentElicitation) error {
	return h.client.CreateAgentActivity(ctx, accessToken, CreateAgentActivityInput{
		AgentSessionID: sessionId,
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

func (h *AgentSessionHandler) SendGitConnectionRequest(ctx context.Context, sessionId, accessToken, gitProvider, gitInstallURL string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Elicitation,
			Body: "GitHub connection required",
		},
		Signal: SignalType_Auth,
		SignalMetadata: map[string]any{
			"url":          gitInstallURL,
			"providerName": gitProvider,
		},
	})
}

// reportServerInternalError notifies the user about an unexpected
// server-side error via a best-effort Linear activity.
func (h *AgentSessionHandler) SendServerInternalError(ctx context.Context, sessionId, accessToken string) error {
	return h.client.CreateAgentActivity(ctx, accessToken, CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: AgentActivityContent{
			Type: AgentActivityContentType_Error,
			Body: "Internal Server Error 500",
		},
	})
}
