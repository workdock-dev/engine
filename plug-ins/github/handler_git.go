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

package github

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/github/interfaces"
	"github.com/workdock-dev/engine/plug-ins/github/types"
	"github.com/workdock-dev/engine/shared"
)

type GitHandler struct {
	client          interfaces.Client
	secretManager   shared.SecretManager
	installationUrl string
}

func NewGitHandler(
	config types.Config,
	client interfaces.Client,
	secretManager shared.SecretManager,
) agent_session_interfaces.HandlerGit {
	return &GitHandler{
		installationUrl: config.AppInstallURL,
		client:          client,
		secretManager:   secretManager,
	}
}

func (h *GitHandler) GetInstallationUrl() string {
	return h.installationUrl
}

func (h *GitHandler) GetGitAccess(ctx context.Context, connection *agent_session_types.GitConnection) (*agent_session_interfaces.GitAccess, error) {
	token, err := getGitHubAccessToken(ctx, h.secretManager, h.client, *connection.InstallationId)

	if err != nil {
		return nil, err
	}

	return &agent_session_interfaces.GitAccess{
		Secret:  token,
		Granted: true,
	}, nil
}

func (h *GitHandler) CreatePullRequest(ctx context.Context, input agent_session_interfaces.CreatePullRequestInput) (*agent_session_types.PullRequest, error) {
	pr, err := h.client.CreatePullRequest(
		ctx,
		input.RepoFullName,
		input.AccessToken,
		types.CreatePullRequestInput{
			Title: input.Title,
			Body:  input.Body,
			Head:  input.Head,
			Base:  input.Base,
			Draft: input.Draft,
		},
	)

	if err != nil {
		return nil, err
	}

	if pr == nil {
		err := errors.New("pull request creation returned no result")
		slog.Error("[github] failed to create pull request", "err", err)
		return nil, err
	}

	return &agent_session_types.PullRequest{
		URL:         pr.URL,
		Number:      pr.Number,
		HeadRefName: pr.Head.Ref,
		HeadRefOID:  pr.Head.SHA,
	}, nil
}

func (h *GitHandler) GetUnresolvedPullRequestComments(ctx context.Context, input agent_session_interfaces.ReadPullRequestInput) ([]agent_session_interfaces.PullRequestComment, error) {
	comments, err := h.client.GetUnresolvedPullRequestComments(ctx, input.RepoFullName, input.AccessToken, input.Number)

	if err != nil {
		return nil, err
	}

	result := make([]agent_session_interfaces.PullRequestComment, 0, len(comments))

	for _, comment := range comments {
		result = append(result, agent_session_interfaces.PullRequestComment{
			ID:           comment.ID,
			ThreadID:     comment.ThreadID,
			URL:          comment.URL,
			Body:         comment.Body,
			Author:       comment.Author.Login,
			CreatedAt:    comment.CreatedAt,
			Path:         comment.Path,
			Line:         comment.Line,
			OriginalLine: comment.OriginalLine,
			DiffHunk:     comment.DiffHunk,
			IsOutdated:   comment.IsOutdated,
		})
	}

	return result, nil
}

func (h *GitHandler) GetFailedPullRequestChecks(ctx context.Context, input agent_session_interfaces.ReadPullRequestInput) ([]agent_session_interfaces.PullRequestCheck, error) {
	checks, err := h.client.GetFailedPullRequestChecks(ctx, input.RepoFullName, input.AccessToken, input.Number)

	if err != nil {
		return nil, err
	}

	result := make([]agent_session_interfaces.PullRequestCheck, 0, len(checks))

	for _, check := range checks {
		annotations := make([]agent_session_interfaces.PullRequestCheckAnnotation, 0, len(check.Annotations))

		for _, annotation := range check.Annotations {
			annotations = append(annotations, agent_session_interfaces.PullRequestCheckAnnotation{
				Path:            annotation.Path,
				StartLine:       annotation.StartLine,
				EndLine:         annotation.EndLine,
				AnnotationLevel: annotation.AnnotationLevel,
				Title:           annotation.Title,
				Message:         annotation.Message,
				RawDetails:      annotation.RawDetails,
			})
		}

		result = append(result, agent_session_interfaces.PullRequestCheck{
			ID:          check.ID,
			Name:        check.Name,
			HeadSHA:     check.HeadSHA,
			URL:         check.URL,
			DetailsURL:  check.DetailsURL,
			Conclusion:  check.Conclusion,
			Title:       check.Output.Title,
			Summary:     check.Output.Summary,
			Text:        check.Output.Text,
			Annotations: annotations,
		})
	}

	return result, nil
}
