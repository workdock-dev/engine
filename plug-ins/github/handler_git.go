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
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	agent_session_interfaces "github.com/workdock-dev/engine/features/agent_session/interfaces"
	agent_session_types "github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/plug-ins/github/interfaces"
	"github.com/workdock-dev/engine/plug-ins/github/types"
	"github.com/workdock-dev/engine/shared"
)

//go:embed scripts/get_changes.sh
var GET_CHANGES string

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

func (h *GitHandler) GetConfigurationCommands() []string {
	return nil
}

func (h *GitHandler) GetCommands() []string {
	return nil
}

func (h *GitHandler) GetLatestChangesCommand(repoFullName string) string {
	return strings.ReplaceAll(GET_CHANGES, "REPO_FULL_NAME_ARG", strconv.Quote(repoFullName))
}

func (h *GitHandler) GetGitAccess(ctx context.Context, connection *agent_session_types.GitConnection) (*agent_session_interfaces.GitAccess, error) {
	token, err := getGitHubAccessToken(ctx, h.secretManager, h.client, *connection.InstallationId)

	if err != nil {
		return nil, err
	}

	return &agent_session_interfaces.GitAccess{
		EnvVarName: "WORKDOCK_GITHUB_API_TOKEN",
		Secret:     token,
		Hosts:      []string{"api.github.com", "github.com"},
		Granted:    true,
	}, nil
}

func (h *GitHandler) ParseLatestChangesResult(changes string) *agent_session_types.PullRequest {
	if changes == "" {
		return nil
	}

	var pr agent_session_types.PullRequest

	if err := json.Unmarshal([]byte(changes), &pr); err != nil {
		slog.Error("failed to unmarshal pull request metadata", "err", err)
		return nil
	}

	return &pr
}

func (h *GitHandler) CreatePullRequest(ctx context.Context, input agent_session_interfaces.CreatePullRequestInput) (*agent_session_types.PullRequest, error) {
	pr, err := h.client.CreatePullRequest(ctx, input.RepoFullName, input.AccessToken, types.CreatePullRequestInput{
		Title: input.Title, Body: input.Body, Head: input.Head, Base: input.Base, Draft: input.Draft,
	})

	if err != nil {
		return nil, err
	}

	if pr == nil {
		err := errors.New("pull request creation returned no result")
		slog.Error("[github] failed to create pull request", "err", err)
		return nil, err
	}

	return &agent_session_types.PullRequest{
		URL: pr.URL, Number: pr.Number, HeadRefName: pr.Head.Ref, HeadRefOID: pr.Head.SHA,
	}, nil
}
