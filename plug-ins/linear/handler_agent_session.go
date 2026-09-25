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
	"strings"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/linear/helpers"
	"github.com/workdock-dev/engine/plug-ins/linear/interfaces"
	"github.com/workdock-dev/engine/plug-ins/linear/types"
	"github.com/workdock-dev/engine/shared"
)

type AgentSessionHandler struct {
	client       interfaces.Client
	tokenHandler *helpers.TokenHandler
}

func NewAgentSessionHandler(
	client interfaces.Client,
	secretManager shared.SecretManager,
) agent_session_interfaces.HandlerAgentSession {
	return &AgentSessionHandler{
		client:       client,
		tokenHandler: helpers.NewTokenHandler(secretManager, client),
	}
}

func (h *AgentSessionHandler) Ingest(event shared.DomainEvent) (*agent_session_types.Session, *agent_session_types.SessionEvent, error) {
	agentSessionEvent, ok := event.(shared.AgentSessionPromptEvent)

	if !ok {
		return nil, nil, fmt.Errorf("[agent-session][linear] failed to ingest, expected %s got %s", shared.EventType_AgentSessionPrompt, event.EventType())
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

	session := &agent_session_types.Session{
		OrganizationIdentifier: linearEvent.OrganizationID,
		Identifier:             linearEvent.AgentSession.ID,
		Provider:               shared.PlatformProvider_Linear,
		IssueId:                linearEvent.AgentSession.Issue.ID,
		Creator:                linearEvent.AgentSession.Creator.Name,
		RepoFullName:           nil,
	}

	sessionEvent := &agent_session_types.SessionEvent{
		SessionIdentifier: session.Identifier,
		Identifier:        key,
		Payload:           payload,
	}

	return session, sessionEvent, nil
}

func (h *AgentSessionHandler) GetLabels(ctx context.Context, issueId, accessToken string) ([]string, error) {
	return h.client.GetIssueLabels(ctx, issueId, accessToken)
}

func (h *AgentSessionHandler) GetCredentials(ctx context.Context, orgId string) (string, error) {
	return h.tokenHandler.GetLinearAccessToken(ctx, orgId)
}

func (h *AgentSessionHandler) GetPromptContext(sessionEvent *agent_session_types.SessionEvent) (*agent_session_interfaces.PromptContext, error) {
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

	var contextFile *agent_session_interfaces.ContextFile

	if linearEvent.PromptContext != "" {
		contextFile = &agent_session_interfaces.ContextFile{
			Content: linearEvent.PromptContext,
			Summary: summarizePromptContext(linearEvent.PromptContext, linearEvent.AgentSession.Issue.Identifier),
		}
	}

	return &agent_session_interfaces.PromptContext{
		Context:     context,
		ContextFile: contextFile,
		Issue: agent_session_types.Issue{
			Title:       linearEvent.AgentSession.Issue.Title,
			Identifier:  linearEvent.AgentSession.Issue.Identifier,
			Description: linearEvent.AgentSession.Issue.Description,
		},
	}, nil
}

// summarizePromptContext describes Linear's prompt context document so the
// agent can decide whether and where reading it is worth the cost instead of
// loading a document that routinely exceeds a megabyte.
//
// Linear nests the work item inside its parent issue and sub-issues, and each
// comment thread carries its replies inline, so the counts are what tells the
// agent how to navigate: it greps for these tags to jump to the part it needs.
func summarizePromptContext(document, issueIdentifier string) string {
	issues := strings.Count(document, "<issue ")
	threads := strings.Count(document, "<comments>")
	replies := strings.Count(document, "<replies>")

	size := documentSize(document)

	summary := fmt.Sprintf("It is a %s XML document of about %s covering %s", size, issueIdentifier, pluralize(issues, "issue", "issues"))

	if threads > 0 || replies > 0 {
		summary += fmt.Sprintf(" with %s and %s", pluralize(threads, "comment thread", "comment threads"), pluralize(replies, "reply", "replies"))
	}

	if strings.Contains(document, "<parent-issue>") {
		summary += ", including the parent issue"
	}

	if strings.Contains(document, "<sub-issues>") {
		summary += " and its sub-issues"
	}

	return summary + "."
}

func documentSize(document string) string {
	const unit = 1024

	if kb := len(document) / unit; kb > 0 {
		return fmt.Sprintf("%d KB", kb)
	}

	return fmt.Sprintf("%d bytes", len(document))
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}

	return fmt.Sprintf("%d %s", n, plural)
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

func (h *AgentSessionHandler) SendAction(ctx context.Context, sessionId, accessToken string, action agent_session_types.AgentAction) error {
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

func (h *AgentSessionHandler) SendElicitation(ctx context.Context, sessionId, accessToken string, elicitation agent_session_types.AgentElicitation) error {
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

// SendError notifies the user about an error via a best-effort Linear
// activity containing the error's message.
func (h *AgentSessionHandler) SendError(ctx context.Context, sessionId, accessToken string, err error) error {
	return h.client.CreateAgentActivity(ctx, accessToken, types.CreateAgentActivityInput{
		AgentSessionID: sessionId,
		Content: types.AgentActivityContent{
			Type: types.AgentActivityContentType_Error,
			Body: err.Error(),
		},
	})
}

// GetIssueState returns the workflow state metadata of a Linear issue.
func (h *AgentSessionHandler) GetIssueState(ctx context.Context, issueId, accessToken string) (*agent_session_interfaces.IssueState, error) {
	issue, err := h.client.GetIssue(ctx, accessToken, issueId)

	if err != nil {
		return nil, err
	}

	return &agent_session_interfaces.IssueState{
		Name: issue.StateName,
		Type: issue.StateType,
	}, nil
}

// TransitionIssueToStarted moves the issue to the team's first started
// workflow state (lowest position) when the agent session begins executing,
// regardless of the issue's current state.
func (h *AgentSessionHandler) TransitionIssueToStarted(ctx context.Context, issueId, accessToken string) error {
	issue, err := h.client.GetIssue(ctx, accessToken, issueId)

	if err != nil {
		return err
	}

	states, err := h.client.GetTeamWorkflowStates(ctx, accessToken, issue.TeamID)

	if err != nil {
		return err
	}

	var startedState *types.WorkflowState

	for i := range states {
		if states[i].Type != types.IssueStateType_Started {
			continue
		}

		if startedState == nil || states[i].Position < startedState.Position {
			startedState = &states[i]
		}
	}

	if startedState == nil {
		return fmt.Errorf("[agent-session][linear] no started workflow state found for team %s", issue.TeamID)
	}

	slog.Debug(
		"[agent-session][linear] transitioning issue to started state",
		"issue_id", issueId,
		"state_name", startedState.Name,
	)

	return h.client.UpdateIssueState(ctx, accessToken, issueId, startedState.ID)
}

// TransitionIssueToInReview moves the issue to the team's "In Review"
// workflow state when the agent session completes its work, regardless
// of the issue's current state.
func (h *AgentSessionHandler) TransitionIssueToInReview(ctx context.Context, issueId, accessToken string) error {
	issue, err := h.client.GetIssue(ctx, accessToken, issueId)

	if err != nil {
		return err
	}

	states, err := h.client.GetTeamWorkflowStates(ctx, accessToken, issue.TeamID)

	if err != nil {
		return err
	}

	for _, state := range states {
		if state.Name == types.IssueStateName_InReview {
			slog.Debug(
				"[agent-session][linear] transitioning issue to in review state",
				"issue_id", issueId,
				"state_name", state.Name,
			)
			return h.client.UpdateIssueState(ctx, accessToken, issueId, state.ID)
		}
	}

	return fmt.Errorf("[agent-session][linear] no %q workflow state found for team %s", types.IssueStateName_InReview, issue.TeamID)
}
