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
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/shared"
)

type LinearServiceSuite struct {
	suite.Suite
	service *Client
	server  *httptest.Server
}

func TestLinearServiceSuite(t *testing.T) {
	suite.Run(t, new(LinearServiceSuite))
}

func (s *LinearServiceSuite) SetupTest() {
	s.service = nil
	if s.server != nil {
		s.server.Close()
		s.server = nil
	}
}

func (s *LinearServiceSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Close()
	}
}

// newService creates a LinearService backed by the given handler.
func (s *LinearServiceSuite) newService(handler http.Handler) *Client {
	s.server = httptest.NewServer(handler)
	svc, err := NewClient(types.Config{
		WebhookSecret: "test-secret",
		ClientId:      "client-id",
		ClientSecret:  "client-secret",
		ServerUrl:     "http://localhost:8080",
		ApiUrl:        s.server.URL + "/graphql",
		TokenUrl:      s.server.URL + "/token",
		IPs:           []string{"10.0.0.1", "192.168.1.100"},
	}, nil)
	s.Require().NoError(err)
	return svc
}

// newServiceNoServer creates a LinearService with custom URLs (no test server).
func (s *LinearServiceSuite) newServiceNoServer(apiURL, tokenURL string) *Client {
	svc, err := NewClient(types.Config{
		WebhookSecret: "test-secret",
		ClientId:      "client-id",
		ClientSecret:  "client-secret",
		ServerUrl:     "http://localhost:8080",
		ApiUrl:        apiURL,
		TokenUrl:      tokenURL,
		IPs:           []string{"10.0.0.1"},
	}, nil)
	s.Require().NoError(err)
	return svc
}

func computeHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *LinearServiceSuite) newWebhookRequest(body string, signature string, remoteAddr string, extraHeaders map[string]string) shared.WebhookRequest {
	headers := map[string][]string{}
	for k, v := range extraHeaders {
		headers[textproto.CanonicalMIMEHeaderKey(k)] = []string{v}
	}
	if signature != "" {
		headers[textproto.CanonicalMIMEHeaderKey("Linear-Signature")] = []string{signature}
	}
	return shared.WebhookRequest{
		Body:       io.NopCloser(strings.NewReader(body)),
		RemoteAddr: remoteAddr,
		Headers:    headers,
	}
}

// okJSONHandler returns a handler that responds with 200 and the given JSON body.
func okJSONHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	})
}

// --- New() constructor ---

func (s *LinearServiceSuite) TestNew_DefaultURLs() {
	svc, err := NewClient(types.Config{}, nil)
	s.Require().NoError(err)
	s.Equal(GraphqlEndpoint, svc.config.ApiUrl)
	s.Equal(ExchangeTokenEndpoint, svc.config.TokenUrl)
}

func (s *LinearServiceSuite) TestNew_CustomURLs() {
	svc, err := NewClient(types.Config{
		ApiUrl:   "https://custom.api/graphql",
		TokenUrl: "https://custom.api/token",
	}, nil)
	s.Require().NoError(err)
	s.Equal("https://custom.api/graphql", svc.config.ApiUrl)
	s.Equal("https://custom.api/token", svc.config.TokenUrl)
}

func (s *LinearServiceSuite) TestNew_NonNilService() {
	svc, err := NewClient(types.Config{}, nil)
	s.Require().NoError(err)
	s.NotNil(svc)
	s.NotNil(svc.httpClient)
}

func (s *LinearServiceSuite) TestNew_ConfigPreserved() {
	svc, err := NewClient(types.Config{
		WebhookSecret: "ws",
		ClientId:      "cid",
		ClientSecret:  "cs",
		ServerUrl:     "http://srv",
		IPs:           []string{"1.2.3.4"},
	}, nil)
	s.Require().NoError(err)
	s.Equal("ws", svc.config.WebhookSecret)
	s.Equal("cid", svc.config.ClientId)
	s.Equal("cs", svc.config.ClientSecret)
	s.Equal("http://srv", svc.config.ServerUrl)
	s.Equal([]string{"1.2.3.4"}, svc.config.IPs)
}

// --- RefreshToken() ---

func (s *LinearServiceSuite) TestRefreshToken_ExchangeFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"server_error"}`)
	})
	svc := s.newService(handler)
	_, err := svc.RefreshToken(context.Background(), "old-refresh")
	s.Error(err)
}

func (s *LinearServiceSuite) TestRefreshToken_FallbackOldRefresh() {
	handler := okJSONHandler(`{"access_token":"new-at","refresh_token":"","expires_in":3600}`)
	svc := s.newService(handler)
	result, err := svc.RefreshToken(context.Background(), "old-refresh")
	s.NoError(err)
	s.Equal("new-at", result.AccessToken)
	s.Equal("old-refresh", result.RefreshToken)
}

func (s *LinearServiceSuite) TestRefreshToken_NewRefreshToken() {
	handler := okJSONHandler(`{"access_token":"new-at","refresh_token":"new-refresh","expires_in":3600}`)
	svc := s.newService(handler)
	result, err := svc.RefreshToken(context.Background(), "old-refresh")
	s.NoError(err)
	s.Equal("new-at", result.AccessToken)
	s.Equal("new-refresh", result.RefreshToken)
}

func (s *LinearServiceSuite) TestRefreshToken_Success() {
	handler := okJSONHandler(`{"access_token":"new-at","refresh_token":"new-refresh","expires_in":7200}`)
	svc := s.newService(handler)
	before := time.Now()
	result, err := svc.RefreshToken(context.Background(), "old-refresh")
	after := time.Now()
	s.NoError(err)
	s.Equal("new-at", result.AccessToken)
	s.Equal("new-refresh", result.RefreshToken)
	s.False(result.ExpiresAt.Before(before.Add(7199 * time.Second)))
	s.False(result.ExpiresAt.After(after.Add(7201 * time.Second)))
}

// --- GetWorkspaceInfo() ---

func (s *LinearServiceSuite) TestGetWorkspaceInfo_DoRequestFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
}

func (s *LinearServiceSuite) TestGetWorkspaceInfo_BadJSON() {
	handler := okJSONHandler(`{"data": "not-an-object"}`)
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestGetWorkspaceInfo_NilOrganization() {
	handler := okJSONHandler(`{"data":{"viewer":{"organization":null}}}`)
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestGetWorkspaceInfo_Success() {
	handler := okJSONHandler(`{"data":{"viewer":{"organization":{"id":"ws-1","name":"My Org"}}}}`)
	svc := s.newService(handler)
	info, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.NoError(err)
	s.Equal("ws-1", info.ID)
	s.Equal("My Org", info.Name)
}

// --- CreateAgentActivity() ---

func (s *LinearServiceSuite) TestCreateAgentActivity_ResponseType() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "response", Body: "Hello"},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	content := input["content"].(map[string]any)
	s.Equal("response", content["type"])
	s.Equal("Hello", content["body"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_ThoughtType() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "thought", Body: "Thinking..."},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	content := input["content"].(map[string]any)
	s.Equal("thought", content["type"])
	s.Equal("Thinking...", content["body"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_ActionType() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content: types.AgentActivityContent{
			Type:      "action",
			Action:    "run_command",
			Parameter: "ls -la",
			Result:    "file1.txt",
		},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	content := input["content"].(map[string]any)
	s.Equal("action", content["type"])
	s.Equal("run_command", content["action"])
	s.Equal("ls -la", content["parameter"])
	s.Equal("file1.txt", content["result"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_ErrorType() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "error", Body: "something broke"},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	content := input["content"].(map[string]any)
	s.Equal("error", content["type"])
	s.Equal("something broke", content["body"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_PromptType() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "prompt", Body: "Enter name:"},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	content := input["content"].(map[string]any)
	s.Equal("prompt", content["type"])
	s.Equal("Enter name:", content["body"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_ElicitationType() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "elicitation", Body: "Choose option:"},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	content := input["content"].(map[string]any)
	s.Equal("elicitation", content["type"])
	s.Equal("Choose option:", content["body"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_WithOptionalFields() {
	var receivedVars map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req graphQLRequest
		json.Unmarshal(body, &req)
		receivedVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"agentActivityCreate":{"success":true,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID:  "session-1",
		Content:         types.AgentActivityContent{Type: "response", Body: "done"},
		Signal:          "select",
		SourceCommentID: "comment-1",
		SignalMetadata:  map[string]any{"options": []string{"a", "b"}},
	})
	s.NoError(err)
	input := receivedVars["input"].(map[string]any)
	s.Equal("select", input["signal"])
	s.Equal("comment-1", input["sourceCommentId"])
	s.NotNil(input["signalMetadata"])
}

func (s *LinearServiceSuite) TestCreateAgentActivity_SuccessFalse() {
	handler := okJSONHandler(`{"data":{"agentActivityCreate":{"success":false,"lastSyncId":1,"agentActivity":{"id":"act-1"}}}}`)
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "response", Body: "done"},
	})
	s.Error(err)
	s.Contains(err.Error(), "unsuccess")
}

func (s *LinearServiceSuite) TestCreateAgentActivity_DoRequestFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "response", Body: "done"},
	})
	s.Error(err)
}

// --- GetIssue() ---

func (s *LinearServiceSuite) TestGetIssue_DoRequestFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	svc := s.newService(handler)
	_, err := svc.GetIssue(context.Background(), "token", "issue-1")
	s.Error(err)
}

func (s *LinearServiceSuite) TestGetIssue_BadJSON() {
	handler := okJSONHandler(`{"data": "not-an-object"}`)
	svc := s.newService(handler)
	_, err := svc.GetIssue(context.Background(), "token", "issue-1")
	s.Error(err)
	s.Contains(err.Error(), "failed to unmarshal response")
}

func (s *LinearServiceSuite) TestGetIssue_NilIssue() {
	handler := okJSONHandler(`{"data":{"issue":null}}`)
	svc := s.newService(handler)
	_, err := svc.GetIssue(context.Background(), "token", "issue-1")
	s.Error(err)
	s.Contains(err.Error(), "issue not found")
}

func (s *LinearServiceSuite) TestGetIssue_Success() {
	issueJSON := `{"data":{"issue":{"id":"issue-1","state":{"name":"Done","type":"completed"}}}}`
	handler := okJSONHandler(issueJSON)
	svc := s.newService(handler)
	issue, err := svc.GetIssue(context.Background(), "token", "issue-1")
	s.NoError(err)
	s.Equal("issue-1", issue.ID)
	s.Equal("Done", issue.StateName)
	s.Equal("completed", issue.StateType)
}

func (s *LinearServiceSuite) TestGetIssue_Success_NilState() {
	issueJSON := `{"data":{"issue":{"id":"issue-1","state":null}}}`
	handler := okJSONHandler(issueJSON)
	svc := s.newService(handler)
	issue, err := svc.GetIssue(context.Background(), "token", "issue-1")
	s.NoError(err)
	s.Equal("issue-1", issue.ID)
	s.Empty(issue.StateName)
	s.Empty(issue.StateType)
}

func (s *LinearServiceSuite) TestGetIssueLabels_DoRequestFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	svc := s.newService(handler)
	_, err := svc.GetIssueLabels(context.Background(), "token", "issue-1")
	s.Error(err)
}

func (s *LinearServiceSuite) TestGetIssueLabels_BadJSON() {
	handler := okJSONHandler(`{"data": "not-an-object"}`)
	svc := s.newService(handler)
	_, err := svc.GetIssueLabels(context.Background(), "token", "issue-1")
	s.Error(err)
	s.Contains(err.Error(), "failed to unmarshal response")
}

func (s *LinearServiceSuite) TestGetIssueLabels_NilIssue() {
	handler := okJSONHandler(`{"data":{"issue":null}}`)
	svc := s.newService(handler)
	_, err := svc.GetIssueLabels(context.Background(), "token", "issue-1")
	s.Error(err)
	s.Contains(err.Error(), "issue not found")
}

func (s *LinearServiceSuite) TestGetIssueLabels_Success() {
	labelsJSON := `repo=workdock-dev/engine`
	handler := okJSONHandler(labelsJSON)
	svc := s.newService(handler)
	labels, err := svc.GetIssueLabels(context.Background(), "token", "issue-1")
	s.NoError(err)
	s.Len(labels, 1)
	s.Equal("repo=workdock-dev/engine", labels[0])
}

// --- doRequest() (tested via GetWorkspaceInfo which calls doRequest) ---

func (s *LinearServiceSuite) TestDoRequest_ContextCanceled() {
	svc := s.newServiceNoServer("", "")
	ctx := contextWithCancel()
	ctx.cancel()
	_, err := svc.GetWorkspaceInfo(ctx.ctx, "token")
	s.Error(err)
}

func (s *LinearServiceSuite) TestDoRequest_BodyReadFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	})
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
}

func (s *LinearServiceSuite) TestDoRequest_Non200Status() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"unauthorized"}`)
	})
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
	s.Contains(err.Error(), "unexpected linear request status code")
}

func (s *LinearServiceSuite) TestDoRequest_GraphQLErrors() {
	handler := okJSONHandler(`{"data":null,"errors":[{"message":"Not authenticated"}]}`)
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
	s.Contains(err.Error(), "linear graphql returned errors")
}

func (s *LinearServiceSuite) TestDoRequest_BadResponseJSON() {
	handler := okJSONHandler(`{not json`)
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
}

func (s *LinearServiceSuite) TestDoRequest_Success() {
	handler := okJSONHandler(`{"data":{"viewer":{"organization":{"id":"ws-1","name":"Org"}}}}`)
	svc := s.newService(handler)
	info, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.NoError(err)
	s.Equal("ws-1", info.ID)
}

// --- ExchangeCode() ---

func (s *LinearServiceSuite) TestExchangeCode_HTTPFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	})
	svc := s.newService(handler)
	_, err := svc.ExchangeCode(context.Background(), "bad-code")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestExchangeCode_Non200Status() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid_client"}`)
	})
	svc := s.newService(handler)
	_, err := svc.ExchangeCode(context.Background(), "code")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestExchangeCode_BadJSON() {
	handler := okJSONHandler(`{not json`)
	svc := s.newService(handler)
	_, err := svc.ExchangeCode(context.Background(), "code")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestExchangeCode_Success() {
	handler := okJSONHandler(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
	svc := s.newService(handler)
	result, err := svc.ExchangeCode(context.Background(), "valid-code")
	s.NoError(err)
	s.Equal("at", result.AccessToken)
	s.Equal("rt", result.RefreshToken)
	s.Equal(3600, result.ExpiresIn)
}

// --- exchangeRefreshToken() ---

func (s *LinearServiceSuite) TestExchangeRefreshToken_HTTPFailure() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	})
	svc := s.newService(handler)
	_, err := svc.exchangeRefreshToken(context.Background(), "bad-refresh")
	s.Error(err)
}

func (s *LinearServiceSuite) TestExchangeRefreshToken_Non200Status() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid_client"}`)
	})
	svc := s.newService(handler)
	_, err := svc.exchangeRefreshToken(context.Background(), "refresh")
	s.Error(err)
}

func (s *LinearServiceSuite) TestExchangeRefreshToken_BadJSON() {
	handler := okJSONHandler(`{not json`)
	svc := s.newService(handler)
	_, err := svc.exchangeRefreshToken(context.Background(), "refresh")
	s.Error(err)
}

func (s *LinearServiceSuite) TestExchangeRefreshToken_Success() {
	handler := okJSONHandler(`{"access_token":"new-at","refresh_token":"new-rt","expires_in":7200}`)
	svc := s.newService(handler)
	result, err := svc.exchangeRefreshToken(context.Background(), "old-refresh")
	s.NoError(err)
	s.Equal("new-at", result.AccessToken)
	s.Equal("new-rt", result.RefreshToken)
	s.Equal(7200, result.ExpiresIn)
}

// --- Additional error path coverage ---

func (s *LinearServiceSuite) TestCreateAgentActivity_UnmarshalError() {
	handler := okJSONHandler(`{"data":{"agentActivityCreate":{"success":123,"lastSyncId":"bad","agentActivity":{"id":true}}}}`)
	svc := s.newService(handler)
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "response", Body: "done"},
	})
	s.Error(err)
}

func (s *LinearServiceSuite) TestDoRequest_MarshalFailure() {
	svc := s.newServiceNoServer("http://localhost:1/graphql", "")
	// Channels cannot be marshaled to JSON; create an activity with
	// a channel in SignalMetadata to trigger json.Marshal error in doRequest
	err := svc.CreateAgentActivity(context.Background(), "token", types.CreateAgentActivityInput{
		AgentSessionID: "session-1",
		Content:        types.AgentActivityContent{Type: "response", Body: "done"},
		SignalMetadata: map[string]any{"ch": make(chan int)},
	})
	s.Error(err)
}

func (s *LinearServiceSuite) TestDoRequest_NewRequestFailure() {
	svc := s.newServiceNoServer("://bad-url", "")
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
}

func (s *LinearServiceSuite) TestDoRequest_TransportErrorNotContext() {
	// Use a server that hijacks and closes the connection
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	})
	svc := s.newService(handler)
	_, err := svc.GetWorkspaceInfo(context.Background(), "token")
	s.Error(err)
}

// hijackCloseAfterHeaders sends valid HTTP headers then closes before body,
// causing io.ReadAll to fail after the Do call succeeds.
func hijackCloseAfterHeaders() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, _ := hj.Hijack()
		buf.WriteString("HTTP/1.1 200 OK\r\n")
		buf.WriteString("Content-Type: application/json\r\n")
		buf.WriteString("Content-Length: 100\r\n")
		buf.WriteString("\r\n")
		buf.Flush()
		conn.Close()
	})
}

func (s *LinearServiceSuite) TestExchangeCode_NewRequestFailure() {
	svc := s.newServiceNoServer("", "://bad-url")
	_, err := svc.ExchangeCode(context.Background(), "code")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestExchangeCode_DoFailure() {
	svc := s.newServiceNoServer("", "http://127.0.0.1:1/token")
	_, err := svc.ExchangeCode(context.Background(), "code")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestExchangeCode_ReadBodyFailure() {
	svc := s.newService(hijackCloseAfterHeaders())
	_, err := svc.ExchangeCode(context.Background(), "code")
	s.ErrorIs(err, shared.ErrInternalServerError)
}

func (s *LinearServiceSuite) TestExchangeRefreshToken_NewRequestFailure() {
	svc := s.newServiceNoServer("", "://bad-url")
	_, err := svc.exchangeRefreshToken(context.Background(), "refresh")
	s.Error(err)
}

func (s *LinearServiceSuite) TestExchangeRefreshToken_DoFailure() {
	svc := s.newServiceNoServer("", "http://127.0.0.1:1/token")
	_, err := svc.exchangeRefreshToken(context.Background(), "refresh")
	s.Error(err)
}

func (s *LinearServiceSuite) TestExchangeRefreshToken_ReadBodyFailure() {
	svc := s.newService(hijackCloseAfterHeaders())
	_, err := svc.exchangeRefreshToken(context.Background(), "refresh")
	s.Error(err)
}

// --- errorReader helper ---

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

// --- contextWithCancel helper ---

type cancellableContext struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func contextWithCancel() *cancellableContext {
	ctx, cancel := context.WithCancel(context.Background())
	return &cancellableContext{ctx: ctx, cancel: cancel}
}
