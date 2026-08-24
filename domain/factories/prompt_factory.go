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
	"fmt"
	"strings"

	"github.com/workdock-dev/engine/domain/types"
)

type PromptFactory struct{}

func NewPromptFactory() *PromptFactory {
	return &PromptFactory{}
}

func (f *PromptFactory) BuildWorkItemPrompt(
	session *types.Session,
	sessionEvent *types.SessionEvent,
	issueTitle string,
	issueIdentifier string,
	issueDescription string,
	promptContext string,
	latestComment *string,
) string {
	repo := ""
	if session.RepoFullName != nil {
		repo = *session.RepoFullName
	}

	prompt := strings.TrimSpace(fmt.Sprintf(types.PromptTemplate_WorkItem,
		issueTitle,
		issueIdentifier,
		repo,
		issueDescription,
		promptContext,
	))

	if sessionEvent.GitRef != nil && sessionEvent.Seed != nil {
		if sessionEvent.Reason == types.SessionEventTriggerReason_CheckRun {
			prompt += fmt.Sprintf(types.PromptTemplate_PullRequestChecksFailed,
				"The pull request checks have failed. Review the check failures, fix the issues, and ensure all checks pass before the pull request can be merged.")
		} else {
			prompt += fmt.Sprintf(types.PromptTemplate_LatestUserComment,
				"There are review comments on the pull request. Retrieve all review comments and address each one that is applicable to the current implementation. Make the necessary code changes, verify the changes, and ensure the pull request is ready for review again.")
		}
	} else if latestComment != nil {
		prompt += fmt.Sprintf(types.PromptTemplate_LatestUserComment, *latestComment)
	}

	return prompt
}
