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

package infrastructure

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/shared"
)

const (
	GraphqlEndpoint       = "https://api.linear.app/graphql"
	AuthorizeEndpoint     = "https://linear.app/oauth/authorize"
	ExchangeTokenEndpoint = "https://api.linear.app/oauth/token"
)

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type Client struct {
	config     types.Config
	httpClient *http.Client
	forSecrets shared.ForSecrets
}

// NewClient initializes the Linear service and its dependencies.
//
//   - Configures the service with the required clients and dependencies for
//     interacting with the Linear API.
//   - Prepares the service for authenticated API requests and secret retrieval.
//
// The service does not perform any connectivity or credential validation during
// initialization.
func NewClient(config types.Config, forSecrets shared.ForSecrets) (*Client, error) {
	if config.ApiUrl == "" {
		config.ApiUrl = GraphqlEndpoint
	}

	if config.TokenUrl == "" {
		config.TokenUrl = ExchangeTokenEndpoint
	}

	slog.Debug("linear service created", "api_url", config.ApiUrl, "token_url", config.TokenUrl)
	return &Client{
		config:     config,
		forSecrets: forSecrets,
		httpClient: &http.Client{},
	}, nil
}

// RefreshToken exchanges a Linear OAuth refresh token for a fresh access
// token.
//
//   - Submits the refresh token to Linear's token endpoint using the
//     refresh_token grant type.
//   - Returns the new access token, refresh token, and expiration time
//     required to authenticate future API requests.
//
// The returned token must replace the stored one so subsequent requests keep
// using valid credentials.
func (s *Client) RefreshToken(ctx context.Context, refreshToken string) (*types.Token, error) {
	tokenData, err := s.exchangeRefreshToken(ctx, refreshToken)

	if err != nil {
		return nil, err
	}

	newRefreshToken := tokenData.RefreshToken

	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	return &types.Token{
		AccessToken:  tokenData.AccessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second),
	}, nil
}

// getWorkspaceInfo retrieves the Linear workspace associated with an access
// token.
//
//   - Queries Linear for the organization linked to the authenticated user.
//   - Returns the workspace identifier and metadata required to associate the
//     OAuth credentials with the correct organization.
//
// The access token must represent a valid authorization for a Linear workspace.
func (s *Client) GetWorkspaceInfo(ctx context.Context, accessToken string) (*types.WorkspaceInfo, error) {
	query := `query { viewer { organization { id name } } }`

	body, err := s.doRequest(ctx, query, nil, accessToken)

	if err != nil {
		return nil, err
	}

	var result struct {
		Viewer struct {
			Organization *types.WorkspaceInfo `json:"organization"`
		} `json:"viewer"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("failed to unmarshal response", "err", err)
		return nil, shared.ErrInternalServerError
	}

	if result.Viewer.Organization == nil {
		slog.Error("no organization found in response")
		return nil, shared.ErrInternalServerError
	}

	return result.Viewer.Organization, nil
}

// CreateAgentActivity publishes an activity update to a Linear agent session.
//
//   - Creates an activity entry representing the agent's progress, output,
//     actions, prompts, or errors.
//   - Supports the different activity shared expected by Linear's Agent API while
//     allowing optional metadata to be attached to the activity.
//   - Records the activity as part of the agent session timeline visible to users.
//
// The operation succeeds only if Linear accepts and persists the activity.
func (s *Client) CreateAgentActivity(ctx context.Context, accessToken string, input types.CreateAgentActivityInput) error {
	contentMap := map[string]any{
		"type": input.Content.Type,
	}

	switch input.Content.Type {
	case "response", "thought":
		contentMap["body"] = input.Content.Body
	case "action":
		contentMap["action"] = input.Content.Action
		contentMap["parameter"] = input.Content.Parameter
		contentMap["result"] = input.Content.Result
	case "error":
		contentMap["body"] = input.Content.Body
	case "prompt":
		contentMap["body"] = input.Content.Body
	case "elicitation":
		contentMap["body"] = input.Content.Body
	}

	vars := map[string]any{
		"input": map[string]any{
			"agentSessionId": input.AgentSessionID,
			"content":        contentMap,
		},
	}

	if input.Signal != "" {
		vars["input"].(map[string]any)["signal"] = input.Signal
	}

	if input.SourceCommentID != "" {
		vars["input"].(map[string]any)["sourceCommentId"] = input.SourceCommentID
	}

	if input.SignalMetadata != nil {
		vars["input"].(map[string]any)["signalMetadata"] = input.SignalMetadata
	}

	query := `mutation CreateAgentActivity($input: AgentActivityCreateInput!) {
  agentActivityCreate(input: $input) {
    success
    lastSyncId
    agentActivity {
      id
    }
  }
}`

	body, err := s.doRequest(ctx, query, vars, accessToken)

	if err != nil {
		return err
	}

	var result struct {
		Payload types.CreateAgentActivityOutput `json:"agentActivityCreate"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("failed to unmarshall create agent activity result", "err", err)
		return err
	}

	slog.Debug("Agent activity sent", "success", result.Payload.Success)

	if !result.Payload.Success {
		return errors.New("linear create activity returned unsuccess")
	}

	return nil
}

// GetIssue retrieves the state information of a Linear issue.
//
//   - Queries Linear for the issue's state metadata including the state name
//     and type.
//   - Returns an IssueStateResult containing the issue ID, state name and state
//     type.
//
// An error is returned if the specified issue cannot be found or the request
// fails.
func (s *Client) GetIssue(ctx context.Context, accessToken string, issueId string) (*types.IssueStateResult, error) {
	query := `query GetIssue($id: String!) {
  issue(id: $id) {
    id
    state {
      name
      type
    }
  }
}`

	vars := map[string]any{
		"id": issueId,
	}

	body, err := s.doRequest(ctx, query, vars, accessToken)

	if err != nil {
		return nil, err
	}

	var result struct {
		Issue *struct {
			ID    string `json:"id"`
			State *struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"state"`
		} `json:"issue"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.Issue == nil {
		return nil, fmt.Errorf("issue not found: %s", issueId)
	}

	issueResult := &types.IssueStateResult{
		ID: result.Issue.ID,
	}

	if result.Issue.State != nil {
		issueResult.StateName = result.Issue.State.Name
		issueResult.StateType = result.Issue.State.Type
	}

	return issueResult, nil
}

// GetIssueLabels retrieves the labels currently assigned to a Linear issue.
//
//   - Queries Linear for the issue's label metadata.
//   - Returns the complete set of labels associated with the issue, including
//     their identifiers and display information.
//
// An error is returned if the specified issue cannot be found.
func (s *Client) GetIssueLabels(ctx context.Context, accessToken string, issueId string) ([]types.IssueLabel, error) {
	query := `query GetIssueLabels($id: String!) {
  issue(id: $id) {
    labelIds
    labels {
      nodes {
        id
        name
        color
        description
        isGroup
      }
      pageInfo {
        hasNextPage
        endCursor
      }
    }
  }
}`

	vars := map[string]any{
		"id": issueId,
	}

	body, err := s.doRequest(ctx, query, vars, accessToken)

	if err != nil {
		return nil, err
	}

	var result struct {
		Issue *struct {
			LabelIds []string `json:"labelIds"`
			Labels   struct {
				Nodes    []types.IssueLabel       `json:"nodes"`
				PageInfo types.IssueLabelPageInfo `json:"pageInfo"`
			} `json:"labels"`
		} `json:"issue"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.Issue == nil {
		return nil, fmt.Errorf("issue not found: %s", issueId)
	}

	return result.Issue.Labels.Nodes, nil
}

// Webhook validates and parses an incoming Linear webhook request.
//
//   - Verifies that the request originates from an authorized Linear IP address.
//   - Validates the webhook signature to ensure the payload is authentic and
//     untampered.
//   - Enforces Linear's timestamp window to protect against replay attacks.
//   - Deserializes the webhook payload into the application's domain model for
//     downstream processing.
//
// Requests that fail origin, signature, or timestamp validation are rejected
// before any business logic is executed.
func (s *Client) Webhook(ctx context.Context, req shared.WebhookRequest) (any, shared.WebhookEventType, error) {
	if !s.isAllowedIP(req) {
		slog.Error("received request from invalid IP", "ip", s.clientIP(req))
		return nil, shared.WebhookEventType_Unknown, shared.ErrForbidden
	}

	rawBody, err := io.ReadAll(req.Body)

	if err != nil {
		slog.Error("failed to parse request body", "err", err)
		return nil, shared.WebhookEventType_Unknown, shared.ErrBadRequest
	}

	if !s.verifyWebhookSignature(req.Get("Linear-Signature"), rawBody) {
		slog.Error("failed verifying request signature")
		return nil, shared.WebhookEventType_Unknown, shared.ErrUnAuthorized
	}

	eventType := req.Get("Linear-Event")

	if eventType == "Issue" {
		var issuePayload types.IssueStatusChangePayload

		if err := json.Unmarshal(rawBody, &issuePayload); err != nil {
			slog.Error("failed to unmarshal issue payload", "err", err)
			return nil, shared.WebhookEventType_Unknown, shared.ErrBadRequest
		}

		diff := time.Since(time.UnixMilli(issuePayload.WebhookTimestamp))

		if diff < -60*time.Second || diff > 60*time.Second {
			slog.Error("request is past the 60 seconds expectation from linear")
			return nil, shared.WebhookEventType_Unknown, shared.ErrUnAuthorized
		}

		return &issuePayload, shared.WebhookEventType_IssueStateUpdated, nil
	}

	var payload types.AgentSessionEventData

	if err := json.Unmarshal(rawBody, &payload); err != nil {
		slog.Error("failed unmarshing request bosy", "err", err)
		return nil, shared.WebhookEventType_Unknown, shared.ErrBadRequest
	}

	diff := time.Since(time.UnixMilli(payload.WebhookTimestamp))

	if diff < -60*time.Second || diff > 60*time.Second {
		slog.Error("request is past the 60 seconds expectation from linear")
		return nil, shared.WebhookEventType_Unknown, shared.ErrUnAuthorized
	}

	return &payload, shared.WebhookEventType_AIRequest, nil
}

// doRequest executes an authenticated GraphQL request against the Linear API.
//
//   - Sends a GraphQL operation using the provided access token.
//   - Validates the HTTP and GraphQL responses before returning the requested
//     data.
//   - Provides a common execution path for all Linear API operations, ensuring
//     consistent request handling and error reporting.
//
// Requests are considered successful only when both the HTTP request succeeds
// and the GraphQL response contains no errors.
func (s *Client) doRequest(ctx context.Context, query string, variables map[string]any, token string) (json.RawMessage, error) {
	reqBody, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})

	if err != nil {
		slog.Error("failed to marshal linear graphql request", "err", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.ApiUrl, bytes.NewReader(reqBody))

	if err != nil {
		slog.Error("failed to create the linear graphql request", "err", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.httpClient.Do(req)

	if err != nil {
		if !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			slog.Error("failed to send linear graphql request", "err", err)
		}
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		slog.Error("failed to parse linear graphql response", "err", err)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		err := errors.New("unexpected linear request status code")
		slog.Error("failed linear graphql request", "err", err, "statusCode", resp.StatusCode, "query", query)
		return nil, err
	}

	var gqlResp graphQLResponse

	if err := json.Unmarshal(body, &gqlResp); err != nil {
		slog.Error("failed to unmarshall linear graphql response", "err", err)
		return nil, err
	}

	if len(gqlResp.Errors) > 0 {
		err := errors.New("linear graphql returned errors")
		for _, err := range gqlResp.Errors {
			slog.Error("failed linear graphql request", "err", err, "query", query)
		}
		return nil, err
	}

	return gqlResp.Data, nil
}

// verifyWebhookSignature validates that a webhook request was signed by Linear.
//
//   - Computes the expected HMAC-SHA256 signature using the configured webhook
//     secret.
//   - Compares the computed signature with the signature provided by Linear using
//     a constant-time comparison to prevent timing attacks.
//
// A successful verification confirms the request originated from a trusted
// source and that the payload was not modified in transit.
func (s *Client) verifyWebhookSignature(headerSignature string, body []byte) bool {
	if headerSignature == "" {
		return false
	}

	expected, err := hex.DecodeString(headerSignature)

	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(s.config.WebhookSecret))
	mac.Write(body)

	return subtle.ConstantTimeCompare(mac.Sum(nil), expected) == 1
}

// isAllowedIP determines whether a webhook request originates from a trusted
// Linear IP address.
//
// Requests originating from untrusted IP addresses should be rejected before
// processing.
func (s *Client) isAllowedIP(req shared.WebhookRequest) bool {
	ip := s.clientIP(req)
	return slices.Contains(s.config.IPs, ip)
}

// clientIP extracts the originating client IP address from an HTTP request.
//
//   - Prefers proxy forwarding headers when the application is deployed behind a
//     reverse proxy or load balancer.
//   - Falls back to the remote connection address when no forwarding information
//     is available.
//
// The returned IP is intended for request validation and auditing.
func (s *Client) clientIP(req shared.WebhookRequest) string {
	if xff := req.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}

	if xri := req.Get("X-Real-IP"); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(req.RemoteAddr)

	if err != nil {
		return req.RemoteAddr
	}

	return host
}

// exchangeCode exchanges a Linear OAuth authorization code for an access token.
//
//   - Completes the OAuth authorization flow by submitting the authorization code
//     to Linear's token endpoint.
//   - Returns the access and refresh tokens required to authenticate future API
//     requests on behalf of the authorized workspace.
//
// The authorization code is single-use and must be exchanged before it expires.
func (s *Client) ExchangeCode(ctx context.Context, code string) (*types.TokenExchanged, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {s.config.ClientId},
		"client_secret": {s.config.ClientSecret},
		"redirect_uri":  {s.config.ServerUrl + "/linear/oauth/callback"},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenUrl, strings.NewReader(data.Encode()))

	if err != nil {
		slog.Error("failed to create token exchange request", "err", err)
		return nil, shared.ErrInternalServerError
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		slog.Error("failed exchanging linear oauth token", "err", err)
		return nil, shared.ErrInternalServerError
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		slog.Error("failed to read request body", "err", err)
		return nil, shared.ErrInternalServerError
	}

	if resp.StatusCode != http.StatusOK {
		err := errors.New(string(body))
		slog.Error("failed exchanging linear oauth token", "err", err, "statusCode", resp.StatusCode)
		return nil, shared.ErrInternalServerError
	}

	var result types.TokenExchanged

	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("failed to unmarshal token exchange response", "err", err)
		return nil, shared.ErrInternalServerError
	}

	return &result, nil
}

// exchangeRefreshToken submits a Linear OAuth refresh token to the token
// endpoint and returns the renewed token response.
//
//   - Uses the refresh_token grant type with the configured client credentials.
//   - Returns the new access token and, when Linear rotates it, the new refresh
//     token.
func (s *Client) exchangeRefreshToken(ctx context.Context, refreshToken string) (*types.TokenExchanged, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {s.config.ClientId},
		"client_secret": {s.config.ClientSecret},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenUrl, strings.NewReader(data.Encode()))

	if err != nil {
		slog.Error("failed to create token refresh request", "err", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		slog.Error("failed refreshing linear oauth token", "err", err)
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		slog.Error("failed to read request body", "err", err)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		err := errors.New(string(body))
		slog.Error("failed refreshing linear oauth token", "err", err, "statusCode", resp.StatusCode)
		return nil, err
	}

	var result types.TokenExchanged

	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("failed to unmarshal token refresh response", "err", err)
		return nil, err
	}

	return &result, nil
}
