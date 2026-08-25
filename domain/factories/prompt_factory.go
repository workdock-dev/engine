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
	"fmt"
	"log/slog"
	"strings"

	"github.com/workdock-dev/engine/domain/types"
)

type PromptFactory struct{}

type WorkItemPromptInput struct {
	Title         string
	Identifier    string
	Repository    *string
	Description   string
	PromptContext string
	AgentActivity *string
	GitRef        *string
	Seed          *string
	TriggerReason types.SessionEventTriggerReason
}

func (f *PromptFactory) BuildWorkItemPrompt(ctx context.Context, input WorkItemPromptInput) (string, error) {
	repo := ""
	if input.Repository != nil {
		repo = *input.Repository
	}

	prompt := strings.TrimSpace(fmt.Sprintf(types.PromptTemplate_WorkItem,
		input.Title,
		input.Identifier,
		repo,
		input.Description,
		input.PromptContext,
	))

	if input.GitRef != nil && input.Seed != nil {
		if input.TriggerReason == types.SessionEventTriggerReason_CheckRun {
			prompt += fmt.Sprintf(types.PromptTemplate_PullRequestChecksFailed, "The pull request checks have failed. Review the check failures, fix the issues, and ensure all checks pass before the pull request can be merged.")
		} else {
			prompt += fmt.Sprintf(types.PromptTemplate_LatestUserComment, "There are review comments on the pull request. Retrieve all review comments and address each one that is applicable to the current implementation. Make the necessary code changes, verify the changes, and ensure the pull request is ready for review again.")
		}
	} else if input.AgentActivity != nil {
		prompt += fmt.Sprintf(types.PromptTemplate_LatestUserComment, *input.AgentActivity)
	}

	slog.Debug("Prompt prepared", "identifier", input.Identifier)
	return prompt, nil
}