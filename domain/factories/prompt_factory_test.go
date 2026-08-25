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

	"github.com/workdock-dev/engine/domain/types"
	"github.com/stretchr/testify/suite"
)

type PromptFactorySuite struct {
	suite.Suite
}

func TestPromptFactorySuite(t *testing.T) {
	suite.Run(t, new(PromptFactorySuite))
}

func (s *PromptFactorySuite) TestBuildWorkItemPrompt_Basic() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Title:       "Test Issue",
		Identifier:   "ENG-1",
		Repository:   &repo,
		Description: "Test description",
		PromptContext: "Additional context",
	}

	prompt, err := factory.BuildWorkItemPrompt(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "Test Issue")
	s.Contains(prompt, "ENG-1")
	s.Contains(prompt, "owner/repo")
	s.Contains(prompt, "Test description")
	s.Contains(prompt, "Additional context")
}

func (s *PromptFactorySuite) TestBuildWorkItemPrompt_NilRepository() {
	factory := PromptFactory{}
	input := WorkItemPromptInput{
		Title:       "Test Issue",
		Identifier:   "ENG-1",
		Repository:   nil,
		Description: "Test description",
		PromptContext: "",
	}

	prompt, err := factory.BuildWorkItemPrompt(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "Test Issue")
	s.Contains(prompt, "ENG-1")
	s.Contains(prompt, "Test description")
}

func (s *PromptFactorySuite) TestBuildWorkItemPrompt_WithAgentActivity() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Title:       "Test Issue",
		Identifier:   "ENG-1",
		Repository:   &repo,
		Description: "Test description",
		PromptContext: "",
		AgentActivity: &types.AgentActivityContent{
			Type: "comment",
			Body: "This is a user comment",
		},
	}

	prompt, err := factory.BuildWorkItemPrompt(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "This is a user comment")
	s.Contains(prompt, "Latest User Comment")
}

func (s *PromptFactorySuite) TestBuildWorkItemPrompt_WithGitRefAndSeed_CheckRun() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Title:       "Test Issue",
		Identifier:   "ENG-1",
		Repository:   &repo,
		Description: "Test description",
		PromptContext: "",
		GitRef:        strPtr("feature-branch"),
		Seed:          strPtr("seed-123"),
		TriggerReason: types.SessionEventTriggerReason_CheckRun,
	}

	prompt, err := factory.BuildWorkItemPrompt(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "pull request checks have failed")
}

func (s *PromptFactorySuite) TestBuildWorkItemPrompt_WithGitRefAndSeed_Comment() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Title:       "Test Issue",
		Identifier:   "ENG-1",
		Repository:   &repo,
		Description: "Test description",
		PromptContext: "",
		GitRef:        strPtr("feature-branch"),
		Seed:          strPtr("seed-123"),
		TriggerReason: types.SessionEventTriggerReason_PRComment,
	}

	prompt, err := factory.BuildWorkItemPrompt(context.Background(), input)

	s.NoError(err)
	s.Contains(prompt, "review comments on the pull request")
}

func (s *PromptFactorySuite) TestBuildWorkItemPrompt_GitRefWithoutSeed() {
	factory := PromptFactory{}
	repo := "owner/repo"
	input := WorkItemPromptInput{
		Title:       "Test Issue",
		Identifier:   "ENG-1",
		Repository:   &repo,
		Description: "Test description",
		PromptContext: "",
		GitRef:        strPtr("feature-branch"),
		Seed:          nil,
	}

	prompt, err := factory.BuildWorkItemPrompt(context.Background(), input)

	s.NoError(err)
	s.NotContains(prompt, "review comments on the pull request")
	s.NotContains(prompt, "pull request checks have failed")
}

func strPtr(s string) *string {
	return &s
}
