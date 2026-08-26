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

package factories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/domain/types"
)

type PromptFactorySuite struct {
	suite.Suite
}

func TestPromptFactorySuite(t *testing.T) {
	suite.Run(t, new(PromptFactorySuite))
}

func (s *PromptFactorySuite) TestBuild_Basic() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Session: &types.Session{
			RepoFullName: &repo,
		},
		Issue: &types.Issue{
			Title:       "Test Issue",
			Identifier:  "ENG-1",
			Description: "Test description",
		},
		PromptContext: "Additional context",
	}

	prompt, err := factory.Build(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "Test Issue")
	s.Contains(prompt, "ENG-1")
	s.Contains(prompt, "owner/repo")
	s.Contains(prompt, "Test description")
	s.Contains(prompt, "Additional context")
}

func (s *PromptFactorySuite) TestBuild_NilRepository() {
	factory := PromptFactory{}
	input := WorkItemPromptInput{
		Session: &types.Session{
			RepoFullName: nil,
		},
		Issue: &types.Issue{
			Title:       "Test Issue",
			Identifier:  "ENG-1",
			Description: "Test description",
		},
		PromptContext: "",
	}

	prompt, err := factory.Build(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "Test Issue")
	s.Contains(prompt, "ENG-1")
	s.Contains(prompt, "Test description")
}

func (s *PromptFactorySuite) TestBuild_WithAgentActivity() {
	factory := PromptFactory{}
	repo := "owner/repo"
	agentActivityBody := "This is a user comment"
	input := WorkItemPromptInput{
		Session: &types.Session{
			RepoFullName: &repo,
		},
		Issue: &types.Issue{
			Title:       "Test Issue",
			Identifier:  "ENG-1",
			Description: "Test description",
		},
		PromptContext: "",
		Prompt:        &agentActivityBody,
	}

	prompt, err := factory.Build(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "This is a user comment")
	s.Contains(prompt, "Latest User Comment")
}

func (s *PromptFactorySuite) TestBuild_WithGitRefAndSeed_CheckRun() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Session: &types.Session{
			RepoFullName: &repo,
		},
		SessionEvent: &types.SessionEvent{
			GitRef: strPtr("feature-branch"),
			Seed:   strPtr("seed-123"),
			Reason: types.SessionEventTriggerReason_CheckRun,
		},
		Issue: &types.Issue{
			Title:       "Test Issue",
			Identifier:  "ENG-1",
			Description: "Test description",
		},
		PromptContext: "",
	}

	prompt, err := factory.Build(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "pull request checks have failed")
}

func (s *PromptFactorySuite) TestBuild_WithGitRefAndSeed_Comment() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Session: &types.Session{
			RepoFullName: &repo,
		},
		SessionEvent: &types.SessionEvent{
			GitRef: strPtr("feature-branch"),
			Seed:   strPtr("seed-123"),
			Reason: types.SessionEventTriggerReason_PRComment,
		},
		Issue: &types.Issue{
			Title:       "Test Issue",
			Identifier:  "ENG-1",
			Description: "Test description",
		},
		PromptContext: "",
	}

	prompt, err := factory.Build(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "review comments on the pull request")
}

func (s *PromptFactorySuite) TestBuild_GitRefWithoutSeed() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Session: &types.Session{
			RepoFullName: &repo,
		},
		SessionEvent: &types.SessionEvent{
			GitRef: strPtr("feature-branch"),
			Seed:   nil,
		},
		Issue: &types.Issue{
			Title:       "Test Issue",
			Identifier:  "ENG-1",
			Description: "Test description",
		},
		PromptContext: "",
	}

	prompt, err := factory.Build(context.Background(), input)

	s.NoError(err)
	s.NotContains(prompt, "review comments on the pull request")
	s.NotContains(prompt, "pull request checks have failed")
}

func strPtr(s string) *string {
	return &s
}
