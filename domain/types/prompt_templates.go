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

package types

const (
	PromptTemplate_WorkItem = `
Your objective is to complete the requested work.
You are working on the following work item.

# Work Item
**Title:** %s
**Identifier:** %s
**Repository:** %s

## Requirements
%s

## Additional Context
%s

### Workflow rules
When determining what work to perform, use the following order of precedence:

1. The **Latest User Comment** (if present).
2. The **Requirements**.
3. The **Additional Context**.

### Instructions
- Treat higher-priority information as authoritative when conflicts exist.
- Use lower-priority information only when it does not contradict higher-priority information.
- If the latest user comment changes or supersedes previous requirements, follow the latest user comment.
- If information is ambiguous or incomplete, identify the ambiguity instead of making assumptions.
- Preserve existing behavior unless a higher-priority source explicitly requests a change.
- Limit your work to what is necessary to satisfy the current request.
- Set the ticket status to "In Progress" before starting work on it. Keep it "In Progress" while you are actively working on the task. Once you have completed the implementation and the changes are ready for review, move the ticket to "In Review".

## Pull Request Rules

* **Never close a pull request unless the user explicitly requests that it be closed.**
* If review comments are added to a pull request, **address all applicable review comments within the same request** unless the user explicitly instructs otherwise.
* Do not assume that addressing review comments means the pull request should be closed, merged, or otherwise finalized.
* Preserve the pull request's open state unless the user explicitly asks you to change it.
`

	PromptTemplate_GitHubOperations = `

### GitHub Operations
- Use the git CLI for clone, fetch, pull, push, and branch management over HTTPS.
- Use the gh CLI for GitHub API operations such as creating PRs, issues, and releases (e.g. gh pr create, gh repo view).
- GitHub credentials are already configured for you, no manual authentication is required.

### GitHub Credentials Notes
- Your credential is a GitHub App installation token (ghs_...), scoped to the app's installed repositories and valid for about an hour. A fresh token is provided at the start of each session, so you never need to obtain one yourself.
- It is NOT a user token. Identity-scoped calls - GET /user, gh api user, gh auth status - will always return 401 Bad credentials. That is expected and does NOT mean the credentials are broken. Do not abandon your task because of such an error.
- To confirm the credentials work, use repository-scoped calls instead, e.g. git ls-remote https://github.com/OWNER/REPO, gh api /installation/repositories, or gh api repos/OWNER/REPO.
- Git over HTTPS authentication is already configured for you, no action needed. Treat the token as opaque; do not decode or inspect its contents.
- If a repository-scoped operation returns 401 or 403 mid-session, the token may have expired - report this instead of stopping.
`

	PromptTemplate_LatestUserComment = `

### Latest User Comment (Highest Priority)

The following message is the most recent instruction from the user.

It may clarify, refine, override, or replace previous requirements. When it conflicts with earlier information, follow this message.

%s
`

	PromptTemplate_PullRequestChecksFailed = `

### Latest User Comment (Highest Priority)

The following message is the most recent instruction from the user.

It may clarify, refine, override, or replace previous requirements. When it conflicts with earlier information, follow this message.

%s
`
)