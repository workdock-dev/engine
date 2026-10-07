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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/workdock-dev/engine/plug-ins/github/types"
)

func (s *GitHubClient) readPullRequestData(ctx context.Context, token, endpoint, operation string, body io.Reader, result any) error {
	method := http.MethodGet

	if body != nil {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)

	if err != nil {
		slog.Error("[github] failed to create pull request data request", "operation", operation, "err", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := s.httpClient.Do(req)

	if err != nil {
		err := errors.New("GitHub pull request data request failed")
		slog.Error("[github] failed to read pull request data", "operation", operation, "err", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("GitHub pull request data request failed with status %d", resp.StatusCode)
		slog.Error("[github] failed to read pull request data", "operation", operation, "err", err)
		return err
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		slog.Error("[github] failed to decode pull request data", "operation", operation, "err", err)
		return err
	}

	return nil
}

func (s *GitHubClient) queryPullRequestData(ctx context.Context, token, query string, variables map[string]any) (*types.PullRequestGraphQLResponse, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})

	if err != nil {
		slog.Error("[github] failed to encode pull request query", "err", err)
		return nil, err
	}

	endpoint := strings.TrimSuffix(s.baseURL(), "/") + "/graphql"

	if strings.HasSuffix(strings.TrimSuffix(s.baseURL(), "/"), "/api/v3") {
		endpoint = strings.TrimSuffix(strings.TrimSuffix(s.baseURL(), "/"), "/api/v3") + "/api/graphql"
	}

	var result types.PullRequestGraphQLResponse

	if err := s.readPullRequestData(ctx, token, endpoint, "comments", bytes.NewReader(body), &result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		err := errors.New("GitHub pull request query returned errors")
		slog.Error("[github] failed to read pull request comments", "err", err)
		return nil, err
	}

	return &result, nil
}

func (s *GitHubClient) GetUnresolvedPullRequestComments(ctx context.Context, repo, token string, number int) ([]types.PullRequestComment, error) {
	owner, name, ok := strings.Cut(repo, "/")

	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || number <= 0 {
		err := errors.New("valid repository and positive pull request number are required")
		slog.Error("[github] failed to read pull request comments", "err", err)
		return nil, err
	}

	comments := make([]types.PullRequestComment, 0)
	variables := map[string]any{"owner": owner, "repo": name, "number": number, "cursor": nil}
	const threadsQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String) {
		repository(owner:$owner,name:$repo) {
			pullRequest(number:$number) {
				reviewThreads(first:100,after:$cursor) {
					nodes { id isResolved }
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`
	const threadCommentsQuery = `query($id:ID!,$cursor:String) {
		node(id:$id) {
			... on PullRequestReviewThread {
				comments(first:100,after:$cursor) {
					nodes { id url body author { login } createdAt path line originalLine diffHunk outdated }
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	for {
		result, err := s.queryPullRequestData(ctx, token, threadsQuery, variables)

		if err != nil {
			return nil, err
		}

		if result.Data.Repository == nil || result.Data.Repository.PullRequest == nil {
			err := errors.New("GitHub pull request not found")
			slog.Error("[github] failed to read pull request comments", "repo", repo, "number", number, "err", err)
			return nil, err
		}

		threads := result.Data.Repository.PullRequest.ReviewThreads

		for _, thread := range threads.Nodes {

			if thread.IsResolved {
				continue
			}

			threadVariables := map[string]any{"id": thread.ID, "cursor": nil}

			for {
				result, err := s.queryPullRequestData(ctx, token, threadCommentsQuery, threadVariables)

				if err != nil {
					return nil, err
				}

				if result.Data.Node == nil {
					err := errors.New("GitHub pull request review thread not found")
					slog.Error("[github] failed to read pull request comments", "err", err)
					return nil, err
				}

				page := result.Data.Node.Comments

				for _, comment := range page.Nodes {
					comment.ThreadID = thread.ID
					comments = append(comments, comment)
				}

				if !page.PageInfo.HasNextPage {
					break
				}

				threadVariables["cursor"] = page.PageInfo.EndCursor
			}
		}

		if !threads.PageInfo.HasNextPage {
			break
		}

		variables["cursor"] = threads.PageInfo.EndCursor
	}

	variables["cursor"] = nil
	const conversationQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String) {
		repository(owner:$owner,name:$repo) {
			pullRequest(number:$number) {
				comments(first:100,after:$cursor) {
					nodes { id url body author { login } createdAt }
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	for {
		result, err := s.queryPullRequestData(ctx, token, conversationQuery, variables)

		if err != nil {
			return nil, err
		}

		if result.Data.Repository == nil || result.Data.Repository.PullRequest == nil {
			err := errors.New("GitHub pull request not found")
			slog.Error("[github] failed to read pull request comments", "repo", repo, "number", number, "err", err)
			return nil, err
		}

		page := result.Data.Repository.PullRequest.Comments
		comments = append(comments, page.Nodes...)

		if !page.PageInfo.HasNextPage {
			break
		}

		variables["cursor"] = page.PageInfo.EndCursor
	}

	slog.Debug("[github] pull request comments retrieved", "repo", repo, "number", number, "count", len(comments))
	return comments, nil
}

func (s *GitHubClient) GetFailedPullRequestChecks(ctx context.Context, repo, token string, number int) ([]types.PullRequestCheck, error) {
	owner, name, ok := strings.Cut(repo, "/")

	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || number <= 0 {
		err := errors.New("valid repository and positive pull request number are required")
		slog.Error("[github] failed to read pull request checks", "err", err)
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s", s.baseURL(), url.PathEscape(owner), url.PathEscape(name))
	var pr types.CreatePullRequestResponse

	if err := s.readPullRequestData(ctx, token, fmt.Sprintf("%s/pulls/%d", endpoint, number), "checks", nil, &pr); err != nil {
		return nil, err
	}

	if pr.Head.SHA == "" {
		err := errors.New("GitHub pull request head commit not set")
		slog.Error("[github] failed to read pull request checks", "err", err)
		return nil, err
	}

	checks := make([]types.PullRequestCheck, 0)

	for page := 1; ; page++ {
		var result struct {
			CheckRuns []types.PullRequestCheck `json:"check_runs"`
		}

		checksURL := fmt.Sprintf("%s/commits/%s/check-runs?filter=latest&per_page=100&page=%d", endpoint, url.PathEscape(pr.Head.SHA), page)

		if err := s.readPullRequestData(ctx, token, checksURL, "checks", nil, &result); err != nil {
			return nil, err
		}

		for _, check := range result.CheckRuns {

			if check.Conclusion != "failure" {
				continue
			}

			check.Annotations = make([]types.PullRequestCheckAnnotation, 0)

			if check.Output.AnnotationsCount > 0 {

				for annotationPage := 1; ; annotationPage++ {
					var annotations []types.PullRequestCheckAnnotation
					annotationsURL := fmt.Sprintf("%s/check-runs/%d/annotations?per_page=100&page=%d", endpoint, check.ID, annotationPage)

					if err := s.readPullRequestData(ctx, token, annotationsURL, "check annotations", nil, &annotations); err != nil {
						return nil, err
					}

					check.Annotations = append(check.Annotations, annotations...)

					if len(annotations) < 100 {
						break
					}

				}

			}

			checks = append(checks, check)
		}

		if len(result.CheckRuns) < 100 {
			break
		}

	}

	slog.Debug("[github] failed pull request checks retrieved", "repo", repo, "number", number, "count", len(checks))
	return checks, nil
}
