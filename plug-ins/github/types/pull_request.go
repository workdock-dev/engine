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

type CreatePullRequestInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

type CreatePullRequestResponse struct {
	URL    string `json:"html_url"`
	Number int    `json:"number"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

type PullRequestComment struct {
	ID           string `json:"id"`
	ThreadID     string
	URL          string `json:"url"`
	Body         string `json:"body"`
	CreatedAt    string `json:"createdAt"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"originalLine"`
	DiffHunk     string `json:"diffHunk"`
	IsOutdated   bool   `json:"outdated"`
	Author       struct {
		Login string `json:"login"`
	} `json:"author"`
}

type PullRequestCheckAnnotation struct {
	Path            string `json:"path"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	AnnotationLevel string `json:"annotation_level"`
	Title           string `json:"title"`
	Message         string `json:"message"`
	RawDetails      string `json:"raw_details"`
}

type PullRequestCheck struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	URL        string `json:"html_url"`
	DetailsURL string `json:"details_url"`
	Conclusion string `json:"conclusion"`
	Output     struct {
		Title            string `json:"title"`
		Summary          string `json:"summary"`
		Text             string `json:"text"`
		AnnotationsCount int    `json:"annotations_count"`
	} `json:"output"`
	Annotations []PullRequestCheckAnnotation
}

type PullRequestPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type PullRequestCommentsPage struct {
	Nodes    []PullRequestComment `json:"nodes"`
	PageInfo PullRequestPageInfo  `json:"pageInfo"`
}

type PullRequestReviewThreadsPage struct {
	Nodes []struct {
		ID         string `json:"id"`
		IsResolved bool   `json:"isResolved"`
	} `json:"nodes"`
	PageInfo PullRequestPageInfo `json:"pageInfo"`
}

type PullRequestGraphQLResponse struct {
	Data struct {
		Repository *struct {
			PullRequest *struct {
				ReviewThreads PullRequestReviewThreadsPage `json:"reviewThreads"`
				Comments      PullRequestCommentsPage      `json:"comments"`
			} `json:"pullRequest"`
		} `json:"repository"`
		Node *struct {
			Comments PullRequestCommentsPage `json:"comments"`
		} `json:"node"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}
