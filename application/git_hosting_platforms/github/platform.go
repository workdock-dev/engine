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
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"

	"github.com/jazielguerrero/workdock/domain/ports"
	"github.com/jazielguerrero/workdock/domain/repositories"
	"github.com/jazielguerrero/workdock/domain/types"
)

type GitHubPlatformConfig struct {
	ForSecrets        ports.ForSecrets
	ForEvents         ports.ForEventBus
	GitHubConnections repositories.GitHubConnectionRepository
	Client            ClientInterface
	BotLoginName      string
}

type githubPlatform struct {
	config GitHubPlatformConfig
	access *githubAccess
}

func New(config GitHubPlatformConfig) ports.ForGitHostingPlatform {
	return &githubPlatform{
		config: config,
		access: newGitHubAccess(githubAccessConfig{
			Client:            config.Client,
			ForSecrets:        config.ForSecrets,
			ForEvent:          config.ForEvents,
			GitHubConnections: config.GitHubConnections,
		}),
	}
}

func (s *githubPlatform) Ingest(ctx context.Context, event any) error {
	e, ok := event.(*WebhookEvent)

	if !ok {
		err := errors.New("failed to cast event to GitHub Webhook Event")
		slog.Error("failed to process github event", "err", err)
		return err
	}

	switch e.EventType {
	case "ping":
		slog.Debug("github ping event received")
		return nil
	case "installation":
		return s.handleInstallation(ctx, e)
	case "installation_repositories":
		return s.handleInstallationRepositories(ctx, e)
	case "issues":
		return s.handleIssues(e)
	case "pull_request_review_comment":
		return s.handlePullRequestComment(e)
	default:
		slog.Warn("unhandled github event", "event", e.EventType)
	}

	return nil
}

func (s *githubPlatform) VerifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repo *string) (bool, string, error) {
	return s.access.verifyRepoAccess(ctx, sessionEventIdentifier, repo)
}

func (s *githubPlatform) RequestConnection(ctx context.Context, sessionEventIdentifier, repo string) error {
	return s.access.RequestConnection(ctx, sessionEventIdentifier, repo)
}

func (s *githubPlatform) Webhook(ctx context.Context, req types.WebhookRequest) (any, types.WebhookEventType, error) {
	event, err := s.config.Client.Webhook(ctx, req)
	if err != nil {
		return nil, types.WebhookEventType_Unknown, err
	}
	return event, types.WebhookEventType_Git, nil
}

// handleInstallation processes a GitHub installation event.
func (s *githubPlatform) handleInstallation(ctx context.Context, event *WebhookEvent) error {
	if event.Installation == nil {
		slog.Warn("installation event without installation data", "action", event.Action)
		return nil
	}

	slog.Debug("Processing GitHub installation event", "action", event.Action, "installation_id", event.Installation.ID)
	installationId := strconv.Itoa(event.Installation.ID)

	if event.Action == "deleted" || event.Action == "removed" {
		repos := make([]string, 0, len(event.Repositories))
		for _, repo := range event.Repositories {
			repos = append(repos, repo.FullName)
		}
		if err := s.access.ResetInstallation(ctx, installationId, repos); err != nil {
			slog.Error("failed to reset github installation", "installation_id", installationId, "err", err)
			return err
		}

		return nil
	}

	if event.Action != "created" && event.Action != "added" {
		slog.Debug("ignoring non-created installation event", "action", event.Action)
		return nil
	}

	if len(event.Repositories) <= 0 && len(event.RepositoriesAdded) <= 0 {
		slog.Debug("user didn't grant access to any repo, skipping getting installation token")
		return nil
	}

	token, err := s.config.Client.CreateInstallationAccessToken(event.Installation.ID)

	if err != nil {
		slog.Error("failed to create installation access token", "installation_id", event.Installation.ID, "err", err)
		return err
	}

	tokenData, err := json.Marshal(token)

	if err != nil {
		slog.Error("failed to marshal installation access token", "installation_id", event.Installation.ID, "err", err)
		return err
	}

	if ctx.Err() != nil {
		slog.Error("failed to continue context err", "err", ctx.Err())
		return ctx.Err()
	}

	if err := s.config.ForSecrets.Set(ctx, GitHub_SecretPath, installationId, string(tokenData)); err != nil {
		slog.Error("failed to store installation access token", "installation_id", event.Installation.ID, "err", err)
		return err
	}

	repos := make([]string, 0, len(event.Repositories)+len(event.RepositoriesAdded))

	for _, repo := range slices.Concat(event.Repositories, event.RepositoriesAdded) {
		repos = append(repos, repo.FullName)
	}

	if err := s.access.CompleteConnection(ctx, installationId, repos); err != nil {
		return err
	}

	slog.Debug("GitHub installation stored", "installation_id", event.Installation.ID, "expires_at", token.ExpiresAt)
	return nil
}

func (s *githubPlatform) handleInstallationRepositories(ctx context.Context, event *WebhookEvent) error {
	if event.Installation == nil {
		slog.Warn("installation_repositories event without installation data", "action", event.Action)
		return nil
	}

	slog.Debug("Processing GitHub installation_repositories event", "action", event.Action, "installation_id", event.Installation.ID)
	installationId := strconv.Itoa(event.Installation.ID)

	if event.Action == "added" {
		if len(event.RepositoriesAdded) <= 0 {
			slog.Debug("no repositories added in installation_repositories event")
			return nil
		}

		repos := make([]string, 0, len(event.RepositoriesAdded))
		for _, repo := range event.RepositoriesAdded {
			repos = append(repos, repo.FullName)
		}

		if err := s.access.CompleteConnection(ctx, installationId, repos); err != nil {
			return err
		}

		slog.Debug("GitHub installation_repositories handled", "installation_id", event.Installation.ID, "repos_count", len(repos))
		return nil
	}

	if event.Action == "removed" {
		if len(event.RepositoriesRemoved) <= 0 {
			slog.Debug("no repositories removed in installation_repositories event")
			return nil
		}

		repos := make([]string, 0, len(event.RepositoriesRemoved))
		for _, repo := range event.RepositoriesRemoved {
			repos = append(repos, repo.FullName)
		}

		if err := s.access.ResetInstallation(ctx, installationId, repos); err != nil {
			return err
		}

		slog.Debug("GitHub installation_repositories removed handled", "installation_id", event.Installation.ID, "repos_count", len(repos))
		return nil
	}

	slog.Debug("ignoring non-added/removed installation_repositories event", "action", event.Action)
	return nil
}

func (s *githubPlatform) handleIssues(event *WebhookEvent) error {
	slog.Debug("github issues event", "action", event.Action)
	return nil
}

func (s *githubPlatform) handlePullRequestComment(event *WebhookEvent) error {
	// The bot's same message can trigger github's webhook event
	if event.Sender != nil && event.Sender.Login == s.config.BotLoginName {
		slog.Debug("pull request comment event received, ignoring", "action", event.Action, "sender", s.config.BotLoginName)
		return nil
	}

	if event.Action == "deleted" {
		slog.Debug("pull request comment event received, ignoring", "action", event.Action)
		return nil
	}

	if event.PullRequest == nil {
		slog.Warn("pull request comment event without pull request data", "action", event.Action)
		return nil
	}

	// The GitHub App installation. Webhook payloads contain the installation property
	// when the event is configured for and sent to a GitHub App.
	if event.Installation == nil {
		slog.Warn("pull request comment event without installation data", "action", event.Action)
		return nil
	}

	installationId := strconv.Itoa(event.Installation.ID)
	slog.Debug("github pull_request event", "action", event.Action, "delivery_id", event.DeliveryID)

	s.config.ForEvents.Publish(context.Background(), types.PullRequestCommentedEvent{
		Provider:       types.PlatformProvider_GitHub,
		GitRef:         event.PullRequest.Head.Ref,
		RepoFullName:   event.PullRequest.Head.Repo.FullName,
		InstallationId: installationId,
	})

	return nil
}
