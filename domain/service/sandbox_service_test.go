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
	"errors"
	"testing"

	"github.com/jazielguerrero/workdock/domain/mocks"
	"github.com/jazielguerrero/workdock/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type SandboxServiceSuite struct {
	suite.Suite
	archiver  *mocks.SandboxArchiver
	sessions  *mocks.SessionRepository
	svc       *SandboxService
}

func TestSandboxServiceSuite(t *testing.T) {
	suite.Run(t, new(SandboxServiceSuite))
}

func (s *SandboxServiceSuite) SetupTest() {
	s.archiver = new(mocks.SandboxArchiver)
	s.sessions = new(mocks.SessionRepository)
	s.svc = NewSandboxService(SandboxServiceConfig{
		ForSandboxArchiver: s.archiver,
		Sessions:           s.sessions,
	})
}

func (s *SandboxServiceSuite) TestOnIssueStatusChanged_NoSessions() {
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return([]*types.Session{}, nil)

	err := s.svc.OnIssueStatusChanged(context.Background(), types.IssueStatusChangedEvent{
		Provider:               types.PlatformProvider_Linear,
		OrganizationIdentifier: "org-1",
		IssueId:                "issue-1",
		PreviousStatus:         "In Progress",
		NewStatus:              "Done",
	})

	s.NoError(err)
	s.sessions.AssertExpectations(s.T())
	s.archiver.AssertNotCalled(s.T(), "ArchiveSandbox")
}

func (s *SandboxServiceSuite) TestOnIssueStatusChanged_GetSessionsError() {
	dbErr := errors.New("db error")
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(nil, dbErr)

	err := s.svc.OnIssueStatusChanged(context.Background(), types.IssueStatusChangedEvent{
		Provider:               types.PlatformProvider_Linear,
		OrganizationIdentifier: "org-1",
		IssueId:                "issue-1",
		PreviousStatus:         "In Progress",
		NewStatus:              "Done",
	})

	s.ErrorIs(err, dbErr)
	s.sessions.AssertExpectations(s.T())
	s.archiver.AssertNotCalled(s.T(), "ArchiveSandbox")
}

func (s *SandboxServiceSuite) TestOnIssueStatusChanged_ArchiveSuccess() {
	sessions := []*types.Session{
		{Identifier: "sess-1", IssueId: "issue-1"},
		{Identifier: "sess-2", IssueId: "issue-1"},
	}
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.archiver.On("ArchiveSandbox", mock.Anything, "sess-1").Return(nil)
	s.archiver.On("ArchiveSandbox", mock.Anything, "sess-2").Return(nil)

	err := s.svc.OnIssueStatusChanged(context.Background(), types.IssueStatusChangedEvent{
		Provider:               types.PlatformProvider_Linear,
		OrganizationIdentifier: "org-1",
		IssueId:                "issue-1",
		PreviousStatus:         "In Progress",
		NewStatus:              "Done",
	})

	s.NoError(err)
	s.sessions.AssertExpectations(s.T())
	s.archiver.AssertExpectations(s.T())
}

func (s *SandboxServiceSuite) TestOnIssueStatusChanged_ArchiveError_Continues() {
	sessions := []*types.Session{
		{Identifier: "sess-1", IssueId: "issue-1"},
		{Identifier: "sess-2", IssueId: "issue-1"},
	}
	s.sessions.On("GetAgentSessionsByIssueId", mock.Anything, "issue-1").Return(sessions, nil)
	s.archiver.On("ArchiveSandbox", mock.Anything, "sess-1").Return(errors.New("archive failed"))
	s.archiver.On("ArchiveSandbox", mock.Anything, "sess-2").Return(nil)

	err := s.svc.OnIssueStatusChanged(context.Background(), types.IssueStatusChangedEvent{
		Provider:               types.PlatformProvider_Linear,
		OrganizationIdentifier: "org-1",
		IssueId:                "issue-1",
		PreviousStatus:         "In Progress",
		NewStatus:              "Done",
	})

	s.NoError(err)
	s.sessions.AssertExpectations(s.T())
	s.archiver.AssertExpectations(s.T())
}