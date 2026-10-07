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

package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/plug-ins/github/types"
)

type PullRequestClientSuite struct {
	suite.Suite
}

func TestPullRequestClientSuite(t *testing.T) {
	suite.Run(t, new(PullRequestClientSuite))
}

func (s *PullRequestClientSuite) TestCommentsExcludeResolvedThreadsAndPaginateAllConnections() {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		s.Equal("/graphql", r.URL.Path)
		s.Equal(http.MethodPost, r.Method)
		s.Equal("Bearer private", r.Header.Get("Authorization"))
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		s.Require().NoError(json.NewDecoder(r.Body).Decode(&request))
		cursor := request.Variables["cursor"]

		switch {
		case strings.Contains(request.Query, "reviewThreads"):
			s.Equal("owner", request.Variables["owner"])
			s.Equal("repo", request.Variables["repo"])
			s.Equal(float64(42), request.Variables["number"])

			if cursor == nil {
				fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"resolved","isResolved":true},{"id":"thread","isResolved":false}],"pageInfo":{"hasNextPage":true,"endCursor":"threads-next"}}}}}}`)
				return
			}

			s.Equal("threads-next", cursor)
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"thread-two","isResolved":false}],"pageInfo":{"hasNextPage":false}}}}}}`)
		case strings.Contains(request.Query, "node(id:"):
			id := request.Variables["id"]
			s.NotEqual("resolved", id)

			if id == "thread-two" {
				fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"second-thread","body":"Other feedback","author":null}],"pageInfo":{"hasNextPage":false}}}}}`)
				return
			}

			s.Equal("thread", id)

			if cursor == nil {
				fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"review","url":"comment-url","body":"Fix this","author":{"login":"reviewer"},"createdAt":"today","path":"file.go","line":7,"originalLine":8,"diffHunk":"diff","outdated":true}],"pageInfo":{"hasNextPage":true,"endCursor":"replies-next"}}}}}`)
				return
			}

			s.Equal("replies-next", cursor)
			fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"reply","body":"Reply"}],"pageInfo":{"hasNextPage":false}}}}}`)
		default:

			if cursor == nil {
				fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"comments":{"nodes":[{"id":"conversation","body":"General feedback"}],"pageInfo":{"hasNextPage":true,"endCursor":"conversation-next"}}}}}}`)
				return
			}

			s.Equal("conversation-next", cursor)
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"comments":{"nodes":[{"id":"conversation-two","body":"More feedback"}],"pageInfo":{"hasNextPage":false}}}}}}`)
		}
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	comments, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", 42)
	s.Require().NoError(err)
	s.Require().Len(comments, 5)
	s.Equal(7, calls)
	s.Equal([]string{"review", "reply", "second-thread", "conversation", "conversation-two"}, []string{comments[0].ID, comments[1].ID, comments[2].ID, comments[3].ID, comments[4].ID})
	s.Equal("thread", comments[0].ThreadID)
	s.Equal("thread", comments[1].ThreadID)
	s.Equal("thread-two", comments[2].ThreadID)
	s.Empty(comments[3].ThreadID)
	s.Equal("reviewer", comments[0].Author.Login)
	s.Equal("file.go", comments[0].Path)
	s.Equal(7, *comments[0].Line)
	s.Equal(8, *comments[0].OriginalLine)
	s.Equal("diff", comments[0].DiffHunk)
	s.True(comments[0].IsOutdated)
}

func (s *PullRequestClientSuite) TestChecksFilterFailureAndPaginateChecksAndAnnotations() {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		s.Equal(http.MethodGet, r.Method)
		s.Equal("Bearer private", r.Header.Get("Authorization"))

		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42":
			fmt.Fprint(w, `{"head":{"sha":"head-commit"}}`)
		case "/repos/owner/repo/commits/head-commit/check-runs":
			s.Equal("latest", r.URL.Query().Get("filter"))
			s.Equal("100", r.URL.Query().Get("per_page"))

			if r.URL.Query().Get("page") == "1" {
				checks := make([]map[string]any, 100)

				for i := range checks {
					checks[i] = map[string]any{"id": i + 1, "conclusion": "success"}
				}

				checks[0] = map[string]any{"id": 1, "name": "build", "head_sha": "head-commit", "conclusion": "failure", "html_url": "check-url", "details_url": "details-url", "output": map[string]any{"title": "Error", "summary": "Compilation failed", "text": "Compiler output", "annotations_count": 101}}
				checks[1]["conclusion"] = "cancelled"
				checks[2]["conclusion"] = "timed_out"
				checks[3]["conclusion"] = nil
				s.NoError(json.NewEncoder(w).Encode(map[string]any{"check_runs": checks}))
				return
			}

			s.Equal("2", r.URL.Query().Get("page"))
			fmt.Fprint(w, `{"check_runs":[{"id":101,"name":"test","conclusion":"failure","output":{"summary":"Tests failed"}}]}`)
		case "/repos/owner/repo/check-runs/1/annotations":
			s.Equal("100", r.URL.Query().Get("per_page"))

			if r.URL.Query().Get("page") == "1" {
				annotations := make([]types.PullRequestCheckAnnotation, 100)

				for i := range annotations {
					annotations[i] = types.PullRequestCheckAnnotation{Path: "file.go", StartLine: i + 1, EndLine: i + 1, AnnotationLevel: "failure", Message: "Syntax error", RawDetails: "Compiler details"}
				}

				s.NoError(json.NewEncoder(w).Encode(annotations))
				return
			}

			s.Equal("2", r.URL.Query().Get("page"))
			fmt.Fprint(w, `[{"path":"other.go","start_line":1,"end_line":2,"message":"Last error","raw_details":"details"}]`)
		default:
			s.Fail("unexpected endpoint", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	checks, err := client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", 42)
	s.Require().NoError(err)
	s.Require().Len(checks, 2)
	s.Equal(5, calls)
	s.Equal("build", checks[0].Name)
	s.Equal("head-commit", checks[0].HeadSHA)
	s.Equal("check-url", checks[0].URL)
	s.Equal("details-url", checks[0].DetailsURL)
	s.Equal("Error", checks[0].Output.Title)
	s.Equal("Compilation failed", checks[0].Output.Summary)
	s.Equal("Compiler output", checks[0].Output.Text)
	s.Require().Len(checks[0].Annotations, 101)
	s.Equal("Compiler details", checks[0].Annotations[0].RawDetails)
	s.Equal("Last error", checks[0].Annotations[100].Message)
	s.Equal("Tests failed", checks[1].Output.Summary)
	s.NotNil(checks[1].Annotations)
}

func (s *PullRequestClientSuite) TestReadErrorsReturnNoPartialResults() {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "forbidden", status: http.StatusForbidden, body: `{}`},
		{name: "invalid JSON", status: http.StatusOK, body: `invalid`},
		{name: "GraphQL errors", status: http.StatusOK, body: `{"errors":[{"message":"denied"}]}`},
		{name: "missing PR", status: http.StatusOK, body: `{"data":{"repository":{"pullRequest":null}}}`},
	} {
		s.Run(test.name, func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
			comments, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", 42)
			s.Error(err)
			s.Nil(comments)
			checks, err := client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", 42)
			s.Error(err)
			s.Nil(checks)
		})
	}
}

func (s *PullRequestClientSuite) TestEmptyResultsAndInputValidation() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"comments":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
			return
		}

		if strings.Contains(r.URL.Path, "/pulls/") {
			fmt.Fprint(w, `{"head":{"sha":"head"}}`)
			return
		}

		fmt.Fprint(w, `{"check_runs":[]}`)
	}))
	defer server.Close()
	client := &GitHubClient{config: types.Config{BaseURL: server.URL}, httpClient: server.Client()}
	comments, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", 42)
	s.NoError(err)
	s.NotNil(comments)
	s.Empty(comments)
	checks, err := client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", 42)
	s.NoError(err)
	s.NotNil(checks)
	s.Empty(checks)

	for _, repo := range []string{"", "owner", "/repo", "owner/", "owner/repo/extra"} {
		_, err := client.GetUnresolvedPullRequestComments(context.Background(), repo, "private", 42)
		s.Error(err)
		_, err = client.GetFailedPullRequestChecks(context.Background(), repo, "private", 42)
		s.Error(err)
	}

	for _, number := range []int{0, -1} {
		_, err := client.GetUnresolvedPullRequestComments(context.Background(), "owner/repo", "private", number)
		s.Error(err)
		_, err = client.GetFailedPullRequestChecks(context.Background(), "owner/repo", "private", number)
		s.Error(err)
	}
}
