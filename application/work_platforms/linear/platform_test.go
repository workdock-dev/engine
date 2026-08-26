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
	"testing"
	"time"

	"github.com/workdock-dev/engine/application"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

func TestLinearPlatformSuite(t *testing.T) {
	suite.Run(t, new(LinearPlatformSuite))
}

type LinearPlatformSuite struct {
	suite.Suite
	client        *mockLinearClient
	secrets       *mockSecrets
	sessions      *mockSessions
	organizations *mockOrganizations
	platform      *linearPlatform
	mockApp       *application.App
}

func (s *LinearPlatformSuite) SetupTest() {
	s.client = new(mockLinearClient)
	s.secrets = new(mockSecrets)
	s.sessions = new(mockSessions)
	s.organizations = new(mockOrganizations)

	s.mockApp, _ = application.New(application.Config{
		ForSecrets: s.secrets,
		Sessions:   s.sessions,
		Organizations: s.organizations,
	})

	s.platform = &linearPlatform{
		app:   s.mockApp,
		config: Config{
			Client: s.client,
		},
	}
}

// ---------------------------------------------------------------------------
// New
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestNew() {
	p := New(Config{Client: s.client})
	s.NotNil(p)
}

// ---------------------------------------------------------------------------
// BeginOAuth
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestBeginOAuth() {
	s.client.On("OauthAuthorize", mock.Anything).Return("https://linear.app/oauth")

	url := s.platform.BeginOAuth(context.Background())
	s.Equal("https://linear.app/oauth", url)
}

// ---------------------------------------------------------------------------
// CompleteOAuth
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestCompleteOAuth_CallbackError() {
	s.client.On("OauthCallback", mock.Anything, "code-123", "").Return(nil, errors.New("callback failed"))

	name, err := s.platform.CompleteOAuth(context.Background(), "code-123", "")
	s.Error(err)
	s.Contains(err.Error(), "callback failed")
	s.Empty(name)
}

func (s *LinearPlatformSuite) TestCompleteOAuth_SecretsError() {
	event := &OAuthCallbackEvent{
		Token:     Token{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)},
		Workspace: WorkspaceInfo{ID: "ws-1", Name: "My Workspace"},
	}
	s.client.On("OauthCallback", mock.Anything, "code-123", "").Return(event, nil)
	s.secrets.On("Set", mock.Anything, SecretsPath, "ws-1", mock.Anything).Return(errors.New("secrets error"))

	name, err := s.platform.CompleteOAuth(context.Background(), "code-123", "")
	s.ErrorIs(err, types.ErrInternalServerError)
	s.Empty(name)
}

func (s *LinearPlatformSuite) TestCompleteOAuth_OrgError() {
	event := &OAuthCallbackEvent{
		Token:     Token{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)},
		Workspace: WorkspaceInfo{ID: "ws-1", Name: "My Workspace"},
	}
	s.client.On("OauthCallback", mock.Anything, "code-123", "").Return(event, nil)
	s.secrets.On("Set", mock.Anything, SecretsPath, "ws-1", mock.Anything).Return(nil)
	s.organizations.On("UpsertOrganization", mock.Anything, mock.Anything).Return(errors.New("org error"))

	name, err := s.platform.CompleteOAuth(context.Background(), "code-123", "")
	s.ErrorIs(err, types.ErrInternalServerError)
	s.Empty(name)
}

func (s *LinearPlatformSuite) TestCompleteOAuth_Success() {
	event := &OAuthCallbackEvent{
		Token:     Token{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)},
		Workspace: WorkspaceInfo{ID: "ws-1", Name: "My Workspace"},
	}
	s.client.On("OauthCallback", mock.Anything, "code-123", "").Return(event, nil)
	s.secrets.On("Set", mock.Anything, SecretsPath, "ws-1", mock.Anything).Return(nil)
	s.organizations.On("UpsertOrganization", mock.Anything, mock.Anything).Return(nil)

	name, err := s.platform.CompleteOAuth(context.Background(), "code-123", "")
	s.NoError(err)
	s.Equal("My Workspace", name)
	s.secrets.AssertCalled(s.T(), "Set", mock.Anything, SecretsPath, "ws-1", mock.Anything)
	s.organizations.AssertCalled(s.T(), "UpsertOrganization", mock.Anything, mock.MatchedBy(func(org *types.Organization) bool {
		return org.Identifier == "ws-1" && org.Name == "My Workspace" && org.Provider == types.PlatformProvider_Linear
	}))
}

// ---------------------------------------------------------------------------
// Ingest
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestIngest_NilEvent() {
	session, event, err := s.platform.Ingest(nil, nil, nil)
	s.Error(err)
	s.Nil(session)
	s.Nil(event)
}

func (s *LinearPlatformSuite) TestIngest_WrongType() {
	session, event, err := s.platform.Ingest("not-a-linear-event", nil, nil)
	s.Error(err)
	s.Nil(session)
	s.Nil(event)
}

func (s *LinearPlatformSuite) TestIngest_JsonRawMessage_Valid() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID:        "session-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   User{Name: "Alice"},
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}
	data, _ := json.Marshal(linearEvent)

	session, event, err := s.platform.Ingest(json.RawMessage(data), nil, nil)
	s.NoError(err)
	s.NotNil(session)
	s.NotNil(event)
	s.Equal("org-1", session.OrganizationIdentifier)
	s.Equal("session-1", session.Identifier)
}

func (s *LinearPlatformSuite) TestIngest_JsonRawMessage_Invalid() {
	session, event, err := s.platform.Ingest(json.RawMessage([]byte("not-json")), nil, nil)
	s.Error(err)
	s.Nil(session)
	s.Nil(event)
}

func (s *LinearPlatformSuite) TestIngest_ValidWithSeed() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID:        "session-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   User{Name: "Alice"},
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}
	seed := "seed-123"

	session, event, err := s.platform.Ingest(linearEvent, &seed, nil)
	s.NoError(err)
	s.NotNil(session)
	s.NotNil(event)
	s.Equal("seed-123", *event.Seed)
}

func (s *LinearPlatformSuite) TestIngest_ValidWithoutSeed() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID:        "session-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   User{Name: "Alice"},
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}

	session, event, err := s.platform.Ingest(linearEvent, nil, nil)
	s.NoError(err)
	s.NotNil(session)
	s.NotNil(event)
	s.Nil(event.Seed)
}

func (s *LinearPlatformSuite) TestIngest_ValidWithGitRef() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID:        "session-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   User{Name: "Alice"},
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}
	gitRef := "feature-branch"
	from := &types.SessionEvent{GitRef: &gitRef}

	session, event, err := s.platform.Ingest(linearEvent, nil, from)
	s.NoError(err)
	s.NotNil(session)
	s.NotNil(event)
	s.Equal("feature-branch", *event.GitRef)
}

func (s *LinearPlatformSuite) TestIngest_ValidWithoutGitRef() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID:        "session-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   User{Name: "Alice"},
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}
	from := &types.SessionEvent{GitRef: nil}

	session, event, err := s.platform.Ingest(linearEvent, nil, from)
	s.NoError(err)
	s.NotNil(session)
	s.NotNil(event)
	s.Nil(event.GitRef)
}

func (s *LinearPlatformSuite) TestIngest_ValidFromNil() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID:        "session-1",
			UpdatedAt: "2026-01-01T00:00:00Z",
			Creator:   User{Name: "Alice"},
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}

	session, event, err := s.platform.Ingest(linearEvent, nil, nil)
	s.NoError(err)
	s.NotNil(session)
	s.NotNil(event)
	s.Nil(event.GitRef)
}

// ---------------------------------------------------------------------------
// Process
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestProcess_UnmarshalError() {
	config := ports.ProcessConfig{
		SessionEvent: &types.SessionEvent{
			Payload: []byte("not-json"),
		},
	}

	err := s.platform.Process(context.Background(), config)
	s.Error(err)
}

func (s *LinearPlatformSuite) TestProcess_NilAgentSession() {
	linearEvent := &AgentSessionEventData{
		AgentSession: nil,
	}
	payload, _ := json.Marshal(linearEvent)

	config := ports.ProcessConfig{
		SessionEvent: &types.SessionEvent{
			Payload: payload,
		},
	}

	err := s.platform.Process(context.Background(), config)
	s.Error(err)
	s.Contains(err.Error(), "corrupted state")
}

func (s *LinearPlatformSuite) TestProcess_TokenError() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		AgentSession:   &AgentSession{ID: "session-1"},
	}
	payload, _ := json.Marshal(linearEvent)

	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("secrets error"))

	config := ports.ProcessConfig{
		Session: &types.Session{
			OrganizationIdentifier: "org-1",
			Identifier:             "session-1",
		},
		SessionEvent: &types.SessionEvent{
			Payload: payload,
		},
	}

	err := s.platform.Process(context.Background(), config)
	s.Error(err)
}

func (s *LinearPlatformSuite) TestProcess_Success() {
	linearEvent := &AgentSessionEventData{
		OrganizationID: "org-1",
		Action:         "created",
		AgentSession: &AgentSession{
			ID: "session-1",
			Issue: Issue{
				ID:         "issue-1",
				Title:      "Fix bug",
				Identifier: "ENG-1",
			},
		},
	}
	payload, _ := json.Marshal(linearEvent)

	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("GetIssueLabels", mock.Anything, "at", "").Return([]IssueLabel{}, nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at", mock.Anything).Return(nil)

	gitHosting := new(mockGitHosting)
	s.mockApp.SetGitHostingPlatformRegistry(ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	})
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)

	harness := new(mockHarness)
	s.mockApp.SetHarnessRegistry(ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	})
	harness.On("Run", mock.Anything).Return(nil, nil)
	harness.On("Dispose", mock.Anything).Return(nil)

	config := ports.ProcessConfig{
		Job: &types.EventJob{},
		Session: &types.Session{
			OrganizationIdentifier: "org-1",
			Identifier:             "session-1",
		},
		SessionEvent: &types.SessionEvent{
			Identifier: "evt-1",
			Payload:    payload,
		},
	}

	err := s.platform.Process(context.Background(), config)
	s.NoError(err)
	harness.AssertCalled(s.T(), "Run", mock.Anything)
	harness.AssertCalled(s.T(), "Dispose", mock.Anything)
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestCancel_TokenError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("secrets error"))

	session := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "session-1",
	}

	err := s.platform.Cancel(context.Background(), session)
	s.Error(err)
}

func (s *LinearPlatformSuite) TestCancel_Success() {
	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at", mock.Anything).Return(nil)

	session := &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "session-1",
	}

	err := s.platform.Cancel(context.Background(), session)
	s.NoError(err)
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Response && input.Content.Body == "Request stopped"
	}))
}

// ---------------------------------------------------------------------------
// IsCancelSignal
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestIsCancelSignal_NilEvent() {
	result, err := s.platform.IsCancelSignal(context.Background(), nil)
	s.Error(err)
	s.False(result)
}

func (s *LinearPlatformSuite) TestIsCancelSignal_WrongType() {
	result, err := s.platform.IsCancelSignal(context.Background(), "not-linear")
	s.Error(err)
	s.False(result)
}

func (s *LinearPlatformSuite) TestIsCancelSignal_JsonRawMessageInvalid() {
	result, err := s.platform.IsCancelSignal(context.Background(), json.RawMessage([]byte("bad")))
	s.Error(err)
	s.False(result)
}

func (s *LinearPlatformSuite) TestIsCancelSignal_NilAgentActivity() {
	event := &AgentSessionEventData{
		AgentActivity: nil,
	}

	result, err := s.platform.IsCancelSignal(context.Background(), event)
	s.NoError(err)
	s.False(result)
}

func (s *LinearPlatformSuite) TestIsCancelSignal_StopSignal() {
	event := &AgentSessionEventData{
		AgentActivity: &AgentActivity{
			Signal: SignalType_Stop,
		},
	}

	result, err := s.platform.IsCancelSignal(context.Background(), event)
	s.NoError(err)
	s.True(result)
}

func (s *LinearPlatformSuite) TestIsCancelSignal_OtherSignal() {
	event := &AgentSessionEventData{
		AgentActivity: &AgentActivity{
			Signal: SignalType_Select,
		},
	}

	result, err := s.platform.IsCancelSignal(context.Background(), event)
	s.NoError(err)
	s.False(result)
}

func (s *LinearPlatformSuite) TestIsCancelSignal_StopSignal_ViaRawMessage() {
	event := &AgentSessionEventData{
		AgentActivity: &AgentActivity{
			Signal: SignalType_Stop,
		},
	}
	data, _ := json.Marshal(event)

	result, err := s.platform.IsCancelSignal(context.Background(), json.RawMessage(data))
	s.NoError(err)
	s.True(result)
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestWebhook() {
	req := types.WebhookRequest{
		Headers: map[string][]string{"X-Linear-Signature": {"xxx"}},
	}
	expected := &AgentSessionEventData{Type: "webhook"}

	s.client.On("Webhook", mock.Anything, req).Return(expected, types.WebhookEventType_AIRequest, nil)

	result, eventType, err := s.platform.Webhook(context.Background(), req)
	s.NoError(err)
	s.Equal(types.WebhookEventType_AIRequest, eventType)
	s.Equal(expected, result)
}

// ---------------------------------------------------------------------------
// archiveSandboxForIssue
// ---------------------------------------------------------------------------

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_SessionsError() {
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(nil, errors.New("db error"))

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.Error(err)
	s.Contains(err.Error(), "db error")
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_NoSessions() {
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return([]*types.Session{}, nil)

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.NoError(err)
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_TokenError() {
	sessions := []*types.Session{
		{OrganizationIdentifier: "org-1", Identifier: "session-1", IssueId: "issue-1"},
	}
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("secrets error"))

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.Error(err)
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_GetIssueError() {
	sessions := []*types.Session{
		{OrganizationIdentifier: "org-1", Identifier: "session-1", IssueId: "issue-1"},
	}
	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("GetIssue", mock.Anything, "at", "issue-1").Return(nil, errors.New("api error"))

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.Error(err)
	s.Contains(err.Error(), "api error")
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_StateNotDone() {
	sessions := []*types.Session{
		{OrganizationIdentifier: "org-1", Identifier: "session-1", IssueId: "issue-1"},
	}
	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("GetIssue", mock.Anything, "at", "issue-1").Return(&IssueStateResult{
		ID:        "issue-1",
		StateName: "In Progress",
		StateType: "started",
	}, nil)

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.NoError(err)
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_DoneState_Archives() {
	sessions := []*types.Session{
		{OrganizationIdentifier: "org-1", Identifier: "session-1", IssueId: "issue-1"},
	}
	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("GetIssue", mock.Anything, "at", "issue-1").Return(&IssueStateResult{
		ID:        "issue-1",
		StateName: "Done",
		StateType: "completed",
	}, nil)

	harness := new(mockHarness)
	s.mockApp.SetHarnessRegistry(ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	})
	harness.On("Archive", mock.Anything).Return(nil)

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.NoError(err)
	harness.AssertCalled(s.T(), "Archive", mock.Anything)
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_DoneState_HarnessConstructorError() {
	sessions := []*types.Session{
		{OrganizationIdentifier: "org-1", Identifier: "session-1", IssueId: "issue-1"},
	}
	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("GetIssue", mock.Anything, "at", "issue-1").Return(&IssueStateResult{
		ID:        "issue-1",
		StateName: "Done",
		StateType: "completed",
	}, nil)

	s.mockApp.SetHarnessRegistry(ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return nil, errors.New("harness error")
		},
	})

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.NoError(err)
}

func (s *LinearPlatformSuite) TestArchiveSandboxForIssue_DoneState_ArchiveError_Continues() {
	sessions := []*types.Session{
		{OrganizationIdentifier: "org-1", Identifier: "session-1", IssueId: "issue-1"},
	}
	validToken := `{"access_token":"at","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(validToken, nil)
	s.client.On("GetIssue", mock.Anything, "at", "issue-1").Return(&IssueStateResult{
		ID:        "issue-1",
		StateName: "Done",
		StateType: "completed",
	}, nil)

	harness := new(mockHarness)
	s.mockApp.SetHarnessRegistry(ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	})
	harness.On("Archive", mock.Anything).Return(errors.New("archive failed"))

	err := s.platform.archiveSandboxForIssue(context.Background(), "issue-1")
	s.NoError(err)
	harness.AssertCalled(s.T(), "Archive", mock.Anything)
}
