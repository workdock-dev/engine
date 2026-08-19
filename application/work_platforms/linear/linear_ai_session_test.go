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
	"testing"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

func TestLinearAISessionSuite(t *testing.T) {
	suite.Run(t, new(LinearAISessionSuite))
}

type LinearAISessionSuite struct {
	suite.Suite
	client        *mockLinearClient
	secrets       *mockSecrets
	sessions      *mockSessions
	organizations *mockOrganizations
}

func (s *LinearAISessionSuite) SetupTest() {
	s.client = new(mockLinearClient)
	s.secrets = new(mockSecrets)
	s.sessions = new(mockSessions)
	s.organizations = new(mockOrganizations)
}

func (s *LinearAISessionSuite) validTokenRaw() string {
	return `{"access_token":"at_valid","refresh_token":"rt","expires_at":"2099-01-01T00:00:00Z"}`
}

func (s *LinearAISessionSuite) baseSession() *types.Session {
	return &types.Session{
		OrganizationIdentifier: "org-1",
		Identifier:             "session-1",
		Provider:               types.PlatformProvider_Linear,
		IssueId:                "issue-1",
		Creator:                "Alice",
	}
}

func (s *LinearAISessionSuite) baseSessionEvent() *types.SessionEvent {
	return &types.SessionEvent{
		SessionIdentifier: "session-1",
		Identifier:        "evt-1",
	}
}

func (s *LinearAISessionSuite) basePayload() *AgentSessionEventData {
	return &AgentSessionEventData{
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
				Description: "Fix the bug",
			},
			ExternalUrls: []ExternalURL{},
		},
		PromptContext: "some context",
	}
}

func (s *LinearAISessionSuite) defaultHarnessRegistry(h *mockHarness) ports.HarnessPlatformRegistry {
	return ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return h, nil
		},
	}
}

func (s *LinearAISessionSuite) defaultGitHostingRegistry(g *mockGitHosting) ports.GitHostingPlatformRegistry {
	return ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: g,
	}
}

// ---------------------------------------------------------------------------
// newLinearAISession
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestNewLinearAISession_TokenError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("secrets error"))

	sess, err := newLinearAISession(context.Background(), linearAISessionConfig{
		ForSecrets: s.secrets,
		Client:     s.client,
		Session:    &types.Session{OrganizationIdentifier: "org-1"},
	})
	s.Error(err)
	s.Nil(sess)
}

func (s *LinearAISessionSuite) TestNewLinearAISession_Success() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(s.validTokenRaw(), nil)

	sess, err := newLinearAISession(context.Background(), linearAISessionConfig{
		ForSecrets: s.secrets,
		Client:     s.client,
		Session:    &types.Session{OrganizationIdentifier: "org-1"},
	})
	s.NoError(err)
	s.NotNil(sess)
	s.Equal("at_valid", sess.accessToken)
	s.NotNil(sess.tracer)
}

// ---------------------------------------------------------------------------
// newLinearAISessionForCancellation
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestNewLinearAISessionForCancellation_TokenError() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return("", errors.New("secrets error"))

	sess, err := newLinearAISessionForCancellation(context.Background(), linearAISessionConfig{
		ForSecrets: s.secrets,
		Client:     s.client,
		Session:    &types.Session{OrganizationIdentifier: "org-1"},
	})
	s.Error(err)
	s.Nil(sess)
}

func (s *LinearAISessionSuite) TestNewLinearAISessionForCancellation_Success() {
	s.secrets.On("Get", mock.Anything, SecretsPath, "org-1").Return(s.validTokenRaw(), nil)

	sess, err := newLinearAISessionForCancellation(context.Background(), linearAISessionConfig{
		ForSecrets: s.secrets,
		Client:     s.client,
		Session:    &types.Session{OrganizationIdentifier: "org-1"},
	})
	s.NoError(err)
	s.NotNil(sess)
	s.Equal("at_valid", sess.accessToken)
	s.Nil(sess.tracer)
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestCancel() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Session: s.baseSession(),
		},
		accessToken: "at_valid",
	}

	sess.Cancel(context.Background())
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Response && input.Content.Body == "Request stopped"
	}))
}

// ---------------------------------------------------------------------------
// createPrompt
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestCreatePrompt_NilRepo() {
	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      &types.Session{RepoFullName: nil},
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					Issue: Issue{Title: "Fix", Identifier: "ENG-1", Description: "desc"},
				},
			},
		},
	}

	prompt := sess.createPrompt()
	s.Contains(prompt, "Fix")
	s.Contains(prompt, "ENG-1")
	s.NotContains(prompt, "https://github.com/")
}

func (s *LinearAISessionSuite) TestCreatePrompt_WithRepo() {
	repo := "org/repo"
	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      &types.Session{RepoFullName: &repo},
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					Issue: Issue{Title: "Fix", Identifier: "ENG-1", Description: "desc"},
				},
			},
		},
	}

	prompt := sess.createPrompt()
	s.Contains(prompt, "org/repo")
}

func (s *LinearAISessionSuite) TestCreatePrompt_WithGitRefAndSeed() {
	gitRef := "feature"
	seed := "seed-1"
	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      &types.Session{RepoFullName: nil},
			SessionEvent: &types.SessionEvent{GitRef: &gitRef, Seed: &seed},
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					Issue: Issue{Title: "Fix", Identifier: "ENG-1", Description: "desc"},
				},
			},
		},
	}

	prompt := sess.createPrompt()
	s.Contains(prompt, "review comments")
}

func (s *LinearAISessionSuite) TestCreatePrompt_WithAgentActivity() {
	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      &types.Session{RepoFullName: nil},
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					Issue: Issue{Title: "Fix", Identifier: "ENG-1", Description: "desc"},
				},
				AgentActivity: &AgentActivity{
					Content: ActivityContent{Body: "Please fix the API"},
				},
			},
		},
	}

	prompt := sess.createPrompt()
	s.Contains(prompt, "Please fix the API")
}

func (s *LinearAISessionSuite) TestCreatePrompt_NilGitRef_WithSeed() {
	seed := "seed-1"
	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      &types.Session{RepoFullName: nil},
			SessionEvent: &types.SessionEvent{GitRef: nil, Seed: &seed},
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					Issue: Issue{Title: "Fix", Identifier: "ENG-1", Description: "desc"},
				},
				AgentActivity: &AgentActivity{
					Content: ActivityContent{Body: "Update the code"},
				},
			},
		},
	}

	prompt := sess.createPrompt()
	s.Contains(prompt, "Update the code")
	s.NotContains(prompt, "review comments")
}

func (s *LinearAISessionSuite) TestCreatePrompt_ContainsPullRequestRules() {
	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      &types.Session{RepoFullName: nil},
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					Issue: Issue{Title: "Fix", Identifier: "ENG-1", Description: "desc"},
				},
			},
		},
	}

	prompt := sess.createPrompt()
	s.Contains(prompt, "## Pull Request Rules")
	s.Contains(prompt, "Never close a pull request unless the user explicitly requests that it be closed.")
	s.Contains(prompt, "address all applicable review comments within the same request")
	s.Contains(prompt, "Do not assume that addressing review comments means the pull request should be closed, merged, or otherwise finalized.")
	s.Contains(prompt, "Preserve the pull request's open state unless the user explicitly asks you to change it.")
}

// ---------------------------------------------------------------------------
// isContextCanceledOrDeadlineExceeded
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestIsContextCanceled() {
	sess := &linearAISession{}
	s.True(sess.isContextCanceledOrDeadlineExceeded(context.Canceled))
}

func (s *LinearAISessionSuite) TestIsDeadlineExceeded() {
	sess := &linearAISession{}
	s.True(sess.isContextCanceledOrDeadlineExceeded(context.DeadlineExceeded))
}

func (s *LinearAISessionSuite) TestIsOtherError() {
	sess := &linearAISession{}
	s.False(sess.isContextCanceledOrDeadlineExceeded(errors.New("other")))
}

// ---------------------------------------------------------------------------
// Thought
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestThought() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Session: s.baseSession(),
		},
		accessToken: "at_valid",
	}

	sess.Thought(context.Background(), "thinking...")
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Thought && input.Content.Body == "thinking..."
	}))
}

// ---------------------------------------------------------------------------
// Response
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestResponse() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Session: s.baseSession(),
		},
		accessToken: "at_valid",
	}

	sess.Response(context.Background(), "done!")
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Response && input.Content.Body == "done!"
	}))
}

// ---------------------------------------------------------------------------
// Action
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestAction() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Session: s.baseSession(),
		},
		accessToken: "at_valid",
	}

	action := types.AgentAction{Name: "git_commit", Input: "msg", Output: "ok"}
	sess.Action(context.Background(), action)
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Action &&
			input.Content.Action == "git_commit" &&
			input.Content.Parameter == "msg" &&
			input.Content.Result == "ok"
	}))
}

// ---------------------------------------------------------------------------
// Elicitation
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestElicitation() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Session: s.baseSession(),
		},
		accessToken: "at_valid",
	}

	el := types.AgentElicitation{
		Question: "Which repo?",
		Options:  []types.AgentOption{{Label: "repo1"}, {Label: "repo2"}},
		Multiple: false,
	}
	sess.Elicitation(context.Background(), el)
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Elicitation &&
			input.Content.Body == "Which repo?" &&
			input.Signal == SignalType_Select
	}))
}

// ---------------------------------------------------------------------------
// reportServerInternalError
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestReportServerInternalError() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Payload: s.basePayload(),
		},
		accessToken: "at_valid",
	}

	sess.reportServerInternalError(context.Background())
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Error && input.Content.Body == "Internal Server Error 500"
	}))
}

// ---------------------------------------------------------------------------
// notifyGitHubConnectionRequired
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestNotifyGitHubConnectionRequired() {
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Client:  s.client,
			Session: s.baseSession(),
		},
		accessToken: "at_valid",
	}

	err := sess.notifyGitHubConnectionRequired(context.Background())
	s.NoError(err)
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Content.Type == AgentActivityContentType_Elicitation &&
			input.Signal == SignalType_Auth
	}))
}

// ---------------------------------------------------------------------------
// setAgentSessionExternalUrls
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_GetIssueLabelsError() {
	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, errors.New("labels error"))

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      s.baseSession(),
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_NoRepoLabels() {
	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "bug"},
	}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      s.baseSession(),
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.client.AssertNotCalled(s.T(), "SetExternalURLs")
	s.sessions.AssertNotCalled(s.T(), "UpsertAgentSession")
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_RepoLabel_NilSessionRepo() {
	session := s.baseSession()
	session.RepoFullName = nil

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(&AgentSessionUpdatePayload{}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.sessions.AssertCalled(s.T(), "UpsertAgentSession", mock.Anything, mock.Anything)
	s.client.AssertCalled(s.T(), "SetExternalURLs", mock.Anything, "at_valid", mock.Anything)
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_RepoLabel_DifferentSessionRepo() {
	session := s.baseSession()
	otherRepo := "org/old-repo"
	session.RepoFullName = &otherRepo

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/new-repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(&AgentSessionUpdatePayload{}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.sessions.AssertCalled(s.T(), "UpsertAgentSession", mock.Anything, mock.Anything)
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_RepoLabel_SameSessionRepo() {
	session := s.baseSession()
	sameRepo := "org/repo"
	session.RepoFullName = &sameRepo

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/repo"},
	}, nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(&AgentSessionUpdatePayload{}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.sessions.AssertNotCalled(s.T(), "UpsertAgentSession")
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_AlreadyHasRepoURL() {
	session := s.baseSession()
	session.RepoFullName = nil

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(&AgentSessionUpdatePayload{}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					ID:        "session-1",
					UpdatedAt: "2026-01-01T00:00:00Z",
					Creator:   User{Name: "Alice"},
					Issue: Issue{
						ID:         "issue-1",
						Title:      "Fix bug",
						Identifier: "ENG-1",
					},
					ExternalUrls: []ExternalURL{
						{Label: "repo", URL: "https://github.com/org/repo"},
					},
				},
			},
			Client:   s.client,
			Sessions: s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.client.AssertNotCalled(s.T(), "SetExternalURLs")
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_UpdateExistingRepoLabel() {
	session := s.baseSession()
	session.RepoFullName = nil

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/new-repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(&AgentSessionUpdatePayload{}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					ID:        "session-1",
					UpdatedAt: "2026-01-01T00:00:00Z",
					Creator:   User{Name: "Alice"},
					Issue: Issue{
						ID:         "issue-1",
						Title:      "Fix bug",
						Identifier: "ENG-1",
					},
					ExternalUrls: []ExternalURL{
						{Label: "repo", URL: "https://github.com/org/old-repo"},
					},
				},
			},
			Client:   s.client,
			Sessions: s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.client.AssertCalled(s.T(), "SetExternalURLs", mock.Anything, "at_valid", mock.MatchedBy(func(input SetExternalURLsInput) bool {
		for _, u := range input.ExternalURLs {
			if u.Label == "repo" && u.URL == "https://github.com/org/new-repo" {
				return true
			}
		}
		return false
	}))
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_AppendRepoLabel() {
	session := s.baseSession()
	session.RepoFullName = nil

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(&AgentSessionUpdatePayload{}, nil)

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload: &AgentSessionEventData{
				AgentSession: &AgentSession{
					ID:        "session-1",
					UpdatedAt: "2026-01-01T00:00:00Z",
					Creator:   User{Name: "Alice"},
					Issue: Issue{
						ID:         "issue-1",
						Title:      "Fix bug",
						Identifier: "ENG-1",
					},
					ExternalUrls: []ExternalURL{
						{Label: "docs", URL: "https://docs.example.com"},
					},
				},
			},
			Client:   s.client,
			Sessions: s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.NoError(err)
	s.client.AssertCalled(s.T(), "SetExternalURLs", mock.Anything, "at_valid", mock.MatchedBy(func(input SetExternalURLsInput) bool {
		for _, u := range input.ExternalURLs {
			if u.Label == "repo" && u.URL == "https://github.com/org/repo" {
				return true
			}
		}
		return false
	}))
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_SetExternalURLError() {
	session := s.baseSession()
	session.RepoFullName = nil

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(nil)
	s.client.On("SetExternalURLs", mock.Anything, "at_valid", mock.Anything).Return(nil, errors.New("set urls error"))

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestSetAgentSessionExternalUrls_UpsertSessionError() {
	session := s.baseSession()
	session.RepoFullName = nil

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{
		{ID: "1", Name: "repo=org/repo"},
	}, nil)
	s.sessions.On("UpsertAgentSession", mock.Anything, mock.Anything).Return(errors.New("upsert error"))

	sess := &linearAISession{
		config: linearAISessionConfig{
			Session:      session,
			SessionEvent: s.baseSessionEvent(),
			Payload:      s.basePayload(),
			Client:       s.client,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}

	err := sess.setAgentSessionExternalUrls(context.Background())
	s.Error(err)
}

// ---------------------------------------------------------------------------
// Process — comprehensive branches
// ---------------------------------------------------------------------------

func (s *LinearAISessionSuite) newProcessSession(payload *AgentSessionEventData) *linearAISession {
	return &linearAISession{
		config: linearAISessionConfig{
			Session:      s.baseSession(),
			SessionEvent: s.baseSessionEvent(),
			Payload:      payload,
			Client:       s.client,
			ForSecrets:   s.secrets,
			Sessions:     s.sessions,
		},
		accessToken: "at_valid",
		tracer:      tracerNoop(),
	}
}

func (s *LinearAISessionSuite) TestProcess_SetExternalURLError() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, errors.New("labels error"))
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_NoGitHubRegistry() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)

	err := sess.Process(context.Background())
	s.Error(err)
	s.Contains(err.Error(), "github hosting provider not register")
}

func (s *LinearAISessionSuite) TestProcess_VerifyAccessConnectionReRequested() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(false, "", types.ErrGitHubConnectionReRequested)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
	s.client.AssertCalled(s.T(), "CreateAgentActivity", mock.Anything, "at_valid", mock.MatchedBy(func(input CreateAgentActivityInput) bool {
		return input.Signal == SignalType_Auth
	}))
}

func (s *LinearAISessionSuite) TestProcess_VerifyAccessConnectionReRequested_NotifyError() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(false, "", types.ErrGitHubConnectionReRequested)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(errors.New("activity error"))

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_VerifyAccessOtherError() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(false, "", errors.New("verify error"))
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_AccessDenied() {
	payload := s.basePayload()
	session := s.baseSession()
	repo := "org/repo"
	session.RepoFullName = &repo

	sess := s.newProcessSession(payload)
	sess.config.Session = session

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", &repo).Return(false, "", nil)
	gitHosting.On("RequestConnection", mock.Anything, "evt-1", "org/repo").Return(nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
	gitHosting.AssertCalled(s.T(), "RequestConnection", mock.Anything, "evt-1", "org/repo")
}

func (s *LinearAISessionSuite) TestProcess_AccessDenied_RequestConnectionError() {
	payload := s.basePayload()
	session := s.baseSession()
	repo := "org/repo"
	session.RepoFullName = &repo

	sess := s.newProcessSession(payload)
	sess.config.Session = session

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", &repo).Return(false, "", nil)
	gitHosting.On("RequestConnection", mock.Anything, "evt-1", "org/repo").Return(errors.New("request error"))

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_AccessDenied_NotifyError() {
	payload := s.basePayload()
	session := s.baseSession()
	repo := "org/repo"
	session.RepoFullName = &repo

	sess := s.newProcessSession(payload)
	sess.config.Session = session

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", &repo).Return(false, "", nil)
	gitHosting.On("RequestConnection", mock.Anything, "evt-1", "org/repo").Return(nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(errors.New("notify error"))

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_NoHarness() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.Error(err)
	s.Contains(err.Error(), "harness")
}

func (s *LinearAISessionSuite) TestProcess_CreateHarnessError() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return nil, errors.New("harness error")
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	_ = harness

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_CreateHarnessContextCanceled() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return nil, context.Canceled
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
}

func (s *LinearAISessionSuite) TestProcess_HarnessRunContextCanceled() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	harness.On("Run", mock.Anything).Return(nil, context.Canceled)
	harness.On("Dispose", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
}

func (s *LinearAISessionSuite) TestProcess_HarnessRunDeadlineExceeded() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	harness.On("Run", mock.Anything).Return(nil, context.DeadlineExceeded)
	harness.On("Dispose", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
}

func (s *LinearAISessionSuite) TestProcess_HarnessRunUnhealthy() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	harness.On("Run", mock.Anything).Return(nil, types.ErrHarnessUnhealthy)
	harness.On("Dispose", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.ErrorIs(err, types.ErrHarnessUnhealthy)
}

func (s *LinearAISessionSuite) TestProcess_HarnessRunOtherError() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	harness.On("Run", mock.Anything).Return(nil, errors.New("run error"))
	harness.On("Dispose", mock.Anything).Return(nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_ResultWithPR() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	pr := &types.PullRequest{HeadRefName: "feature", Number: 42, URL: "https://github.com/org/repo/pull/42"}
	result := &types.SessionEventResult{PullRequest: pr}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	harness.On("Run", mock.Anything).Return(result, nil)
	harness.On("Dispose", mock.Anything).Return(nil)
	s.sessions.On("UpdateSessionEventResult", mock.Anything, mock.Anything).Return(nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
	s.sessions.AssertCalled(s.T(), "UpdateSessionEventResult", mock.Anything, mock.Anything)
	s.Equal("feature", *sess.config.SessionEvent.GitRef)
}

func (s *LinearAISessionSuite) TestProcess_ResultWithPR_UpdateError() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	pr := &types.PullRequest{HeadRefName: "feature", Number: 42, URL: "https://github.com/org/repo/pull/42"}
	result := &types.SessionEventResult{PullRequest: pr}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	harness.On("Run", mock.Anything).Return(result, nil)
	harness.On("Dispose", mock.Anything).Return(nil)
	s.sessions.On("UpdateSessionEventResult", mock.Anything, mock.Anything).Return(errors.New("update error"))
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.Error(err)
}

func (s *LinearAISessionSuite) TestProcess_ResultNil() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	harness.On("Run", mock.Anything).Return(nil, nil)
	harness.On("Dispose", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
}

func (s *LinearAISessionSuite) TestProcess_ResultWithoutPR() {
	payload := s.basePayload()
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	result := &types.SessionEventResult{}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	harness.On("Run", mock.Anything).Return(result, nil)
	harness.On("Dispose", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
}

func (s *LinearAISessionSuite) TestProcess_ActionNotCreated() {
	payload := s.basePayload()
	payload.Action = "updated"
	sess := s.newProcessSession(payload)

	harness := new(mockHarness)
	sess.config.HarnessRegistry = ports.HarnessPlatformRegistry{
		types.HarnessProvider_OpenCode: func(c ports.NewHarnessConstructor) (ports.ForHarnessPlatform, error) {
			return harness, nil
		},
	}

	gitHosting := new(mockGitHosting)
	sess.config.GitHostingRegistry = ports.GitHostingPlatformRegistry{
		types.PlatformProvider_GitHub: gitHosting,
	}

	s.client.On("GetIssueLabels", mock.Anything, "at_valid", "issue-1").Return([]IssueLabel{}, nil)
	gitHosting.On("VerifyRepoAccess", mock.Anything, "session-1", (*string)(nil)).Return(true, "", nil)
	s.client.On("CreateAgentActivity", mock.Anything, "at_valid", mock.Anything).Return(nil)
	harness.On("Run", mock.Anything).Return(nil, nil)
	harness.On("Dispose", mock.Anything).Return(nil)

	err := sess.Process(context.Background())
	s.NoError(err)
}
