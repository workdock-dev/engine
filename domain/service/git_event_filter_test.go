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
	"testing"

	"github.com/stretchr/testify/suite"
)

type GitEventFilterServiceSuite struct {
	suite.Suite
}

func TestGitEventFilterServiceSuite(t *testing.T) {
	suite.Run(t, new(GitEventFilterServiceSuite))
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCommentEvent_SameSender() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCommentEvent("bot", "bot", "created")

	s.False(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCommentEvent_DifferentSender() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCommentEvent("user", "bot", "created")

	s.True(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCommentEvent_DeletedAction() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCommentEvent("user", "bot", "deleted")

	s.False(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCommentEvent_CreatedAction() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCommentEvent("user", "bot", "created")

	s.True(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_SameSender() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("bot", "bot", "completed", "failure")

	s.False(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_NotCompleted() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("user", "bot", "opened", "failure")

	s.False(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_EmptyConclusion() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("user", "bot", "completed", "")

	s.False(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_FailureConclusion() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("user", "bot", "completed", "failure")

	s.True(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_TimedOutConclusion() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("user", "bot", "completed", "timed_out")

	s.True(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_SuccessConclusion() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("user", "bot", "completed", "success")

	s.False(result)
}

func (s *GitEventFilterServiceSuite) TestShouldTriggerCheckRunEvent_DifferentSender_Failure() {
	service := GitEventFilterService{}
	result := service.ShouldTriggerCheckRunEvent("user", "bot", "completed", "failure")

	s.True(result)
}
