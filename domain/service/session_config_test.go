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

	"github.com/workdock-dev/engine/domain/mocks"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type SessionConfigServiceSuite struct {
	suite.Suite
	sessionRepo *mocks.SessionRepository
	session    *types.Session
}

func TestSessionConfigServiceSuite(t *testing.T) {
	suite.Run(t, new(SessionConfigServiceSuite))
}

func (s *SessionConfigServiceSuite) SetupTest() {
	s.sessionRepo = new(mocks.SessionRepository)
	s.session = &types.Session{
		Identifier:             "session-1",
		OrganizationIdentifier: "org-1",
	}
}

func (s *SessionConfigServiceSuite) TestExtractRepoFromLabels_Found() {
	labels := []string{"bug", "repo=owner/repo", "feature"}

	service := SessionConfigService{}
	repo, found := service.ExtractRepoFromLabels(labels)

	s.True(found)
	s.Equal("owner/repo", repo)
}

func (s *SessionConfigServiceSuite) TestExtractRepoFromLabels_NotFound() {
	labels := []string{"bug", "feature"}

	service := SessionConfigService{}
	repo, found := service.ExtractRepoFromLabels(labels)

	s.False(found)
	s.Empty(repo)
}

func (s *SessionConfigServiceSuite) TestExtractRepoFromLabels_EmptyLabels() {
	labels := []string{}

	service := SessionConfigService{}
	repo, found := service.ExtractRepoFromLabels(labels)

	s.False(found)
	s.Empty(repo)
}

func (s *SessionConfigServiceSuite) TestConfigureSessionRepo_NoRepoLabel() {
	service := SessionConfigService{}
	err := service.ConfigureSessionRepo(context.Background(), s.session, []string{"bug", "feature"}, s.sessionRepo)

	s.NoError(err)
	s.Nil(s.session.RepoFullName)
	s.sessionRepo.AssertNotCalled(s.T(), "UpsertAgentSession")
}

func (s *SessionConfigServiceSuite) TestConfigureSessionRepo_NilSessionRepo() {
	s.session.RepoFullName = nil
	labels := []string{"repo=owner/repo"}

	service := SessionConfigService{}
	s.sessionRepo.On("UpsertAgentSession", mock.Anything, s.session).Return(nil)

	err := service.ConfigureSessionRepo(context.Background(), s.session, labels, s.sessionRepo)

	s.NoError(err)
	s.NotNil(s.session.RepoFullName)
	s.Equal("owner/repo", *s.session.RepoFullName)
	s.sessionRepo.AssertCalled(s.T(), "UpsertAgentSession", mock.Anything, s.session)
}

func (s *SessionConfigServiceSuite) TestConfigureSessionRepo_DifferentRepo() {
	oldRepo := "owner/old-repo"
	s.session.RepoFullName = &oldRepo
	labels := []string{"repo=owner/new-repo"}

	service := SessionConfigService{}
	s.sessionRepo.On("UpsertAgentSession", mock.Anything, s.session).Return(nil)

	err := service.ConfigureSessionRepo(context.Background(), s.session, labels, s.sessionRepo)

	s.NoError(err)
	s.NotNil(s.session.RepoFullName)
	s.Equal("owner/new-repo", *s.session.RepoFullName)
	s.sessionRepo.AssertCalled(s.T(), "UpsertAgentSession", mock.Anything, s.session)
}

func (s *SessionConfigServiceSuite) TestConfigureSessionRepo_SameRepo() {
	sameRepo := "owner/repo"
	s.session.RepoFullName = &sameRepo
	labels := []string{"repo=owner/repo"}

	service := SessionConfigService{}

	err := service.ConfigureSessionRepo(context.Background(), s.session, labels, s.sessionRepo)

	s.NoError(err)
	s.Equal("owner/repo", *s.session.RepoFullName)
	s.sessionRepo.AssertNotCalled(s.T(), "UpsertAgentSession")
}

func (s *SessionConfigServiceSuite) TestConfigureSessionRepo_UpsertError() {
	s.session.RepoFullName = nil
	labels := []string{"repo=owner/repo"}

	service := SessionConfigService{}
	s.sessionRepo.On("UpsertAgentSession", mock.Anything, s.session).Return(errors.New("db error"))

	err := service.ConfigureSessionRepo(context.Background(), s.session, labels, s.sessionRepo)

	s.Error(err)
	s.Equal("db error", err.Error())
}
