package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/workdock-dev/engine/features/webhook"
	"github.com/workdock-dev/engine/plug-ings/github/interfaces"
	"github.com/workdock-dev/engine/plug-ings/github/types"
	"github.com/workdock-dev/engine/shared"
)

// WEventType identifies the type of webhook event received from Linear.
const (
	WEventType_Ping                     = "ping"
	WEventType_Installation             = "installation"
	WEventType_InstallationRepositories = "installation_repositories"
	WEventType_Issues                   = "issues"
	WEventType_PullRequestReviewComment = "pull_request_review_comment"
	WEventType_CheckRun                 = "check_run"
	WEventType_CheckSuite               = "check_suite"
)

type WEventTransformer struct{}

func NewWEventTransformer() webhook.WEventTransformer {
	return &WEventTransformer{}
}

// NewWEventTransformer creates a webhook transformer for converting HTTP
// requests into transport-neutral webhook events.
func (t *WEventTransformer) Transform(_ context.Context, r *http.Request) (*webhook.WEvent, error) {
	return &webhook.WEvent{
		Headers:    r.Header,
		RemoteAddr: r.RemoteAddr,
		Body:       r.Body,
	}, nil
}

type WEventVerifier struct {
	config types.Config
}

// NewWEventVerifier creates a webhook verifier configured with the trusted
// Linear IP addresses and webhook signing secret.
func NewWEventVerifier(config types.Config) webhook.WEventVerifier {
	return &WEventVerifier{
		config: config,
	}
}

// Verify validates the request and returns a verified webhook event.
// Payload-specific validation is deferred to the consumer to avoid
// unmarshaling the payload more than once.
func (t *WEventVerifier) Verify(_ context.Context, event *webhook.WEvent) (*webhook.VerifiedWEvent, error) {
	rawBody, err := io.ReadAll(event.Body)

	if err != nil {
		slog.Error("failed to read request body", "err", err)
		return nil, webhook.ErrWBadRequest
	}

	signature := event.Get("X-Hub-Signature-256")

	if !t.verifyWebhookSignature(signature, rawBody) {
		slog.Error("failed verifying github webhook signature")
		return nil, webhook.ErrWUnAuthorized
	}

	eventType := event.Get("X-GitHub-Event")

	if eventType == "" {
		slog.Error("missing X-GitHub-Event header")
		return nil, webhook.ErrWBadRequest
	}

	deliveryID := event.Get("X-GitHub-Delivery")

	// TODO: Implement replay attack verification
	// https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks#use-the-x-github-delivery-header

	return &webhook.VerifiedWEvent{
		WEventType: eventType,
		DeliveryID: deliveryID,
		Payload:    rawBody,
	}, nil
}

// verifyWebhookSignature validates that a webhook request was signed by GitHub.
//
//   - Computes the expected HMAC-SHA256 signature using the configured webhook
//     secret.
//   - Compares the computed signature with the one provided by GitHub using a
//     constant-time comparison to prevent timing attacks.
//
// A successful verification confirms the request originated from a trusted
// source and that the payload was not modified in transit.
func (t *WEventVerifier) verifyWebhookSignature(headerSignature string, body []byte) bool {
	if headerSignature == "" {
		return false
	}

	const prefix = "sha256="

	if len(headerSignature) < len(prefix) || headerSignature[:len(prefix)] != prefix {
		return false
	}

	expected, err := hex.DecodeString(headerSignature[len(prefix):])

	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(t.config.WebhookSecret))
	mac.Write(body)

	return subtle.ConstantTimeCompare(mac.Sum(nil), expected) == 1
}

type WEventConsumer struct {
	config   types.Config
	client   interfaces.Client
	eventBus shared.ForEventBus
}

// NewWEventConsumer creates a webhook consumer for processing verified
// Linear webhook events.
func NewWEventConsumer(
	config types.Config,
	client interfaces.Client,
	eventBus shared.ForEventBus,
) webhook.WEventConsumer {
	return &WEventConsumer{
		config:   config,
		client:   client,
		eventBus: eventBus,
	}
}

// Consume decodes and validates a verified webhook before publishing it.
//
// Timestamp validation is performed after unmarshaling to avoid decoding the
// same payload twice.
func (c *WEventConsumer) Consume(ctx context.Context, event *webhook.VerifiedWEvent) error {
	if event.WEventType == WEventType_Ping {
		slog.Debug("github ping event received")
		return nil
	}

	var payload types.WebhookEvent

	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal webhook payload", "err", err)
		return webhook.ErrWBadRequest
	}

	if event.WEventType == WEventType_Installation {
		return c.handleInstallation(ctx, &payload)
	}

	if event.WEventType == WEventType_InstallationRepositories {
		return c.handleInstallationRepositories(ctx, &payload)
	}

	if event.WEventType == WEventType_Issues {
		// return s.handleIssues(e)
		return nil
	}

	if event.WEventType == WEventType_PullRequestReviewComment {
		return c.handlePullRequestComment(&payload)
	}

	if event.WEventType == WEventType_CheckRun {
		return c.handleCheckRun(&payload)
	}

	if event.WEventType == WEventType_CheckSuite {
		return c.handleCheckSuite(&payload)
	}

	return webhook.ErrWBadRequest
}

// handleInstallation processes a GitHub installation event.
func (c *WEventConsumer) handleInstallation(ctx context.Context, event *types.WebhookEvent) error {
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

		c.eventBus.Publish(ctx, shared.GitResetConnectionEvent{
			Repos:          repos,
			InstallationId: installationId,
			Delete:         true,
		})

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

	token, err := c.client.CreateInstallationAccessToken(event.Installation.ID)

	if err != nil {
		slog.Error("failed to create installation access token", "installation_id", event.Installation.ID, "err", err)
		return err
	}

	tokenData, err := json.Marshal(token)

	if err != nil {
		slog.Error("failed to marshal installation access token", "installation_id", event.Installation.ID, "err", err)
		return err
	}

	repos := make([]string, 0, len(event.Repositories)+len(event.RepositoriesAdded))

	for _, repo := range slices.Concat(event.Repositories, event.RepositoriesAdded) {
		repos = append(repos, repo.FullName)
	}

	c.eventBus.Publish(ctx, shared.GitCompleteConnectionEvent{
		Repos:          repos,
		InstallationId: installationId,
		Token:          tokenData,
	})

	return nil
}

func (c *WEventConsumer) handleInstallationRepositories(ctx context.Context, event *types.WebhookEvent) error {
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

		c.eventBus.Publish(ctx, shared.GitCompleteConnectionEvent{
			Repos:          repos,
			InstallationId: installationId,
		})

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

		c.eventBus.Publish(ctx, shared.GitResetConnectionEvent{
			Repos:          repos,
			InstallationId: installationId,
			Delete:         false,
		})

		slog.Debug("GitHub installation_repositories removed handled", "installation_id", event.Installation.ID, "repos_count", len(repos))
		return nil
	}

	slog.Debug("ignoring non-added/removed installation_repositories event", "action", event.Action)
	return nil
}

func (c *WEventConsumer) handlePullRequestComment(event *types.WebhookEvent) error {
	if event.Sender == nil {
		return nil
	}

	if event.Sender.Login == c.config.BotLoginId {
		return nil
	}

	if event.Action == "deleted" {
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

	c.eventBus.Publish(context.Background(), shared.PullRequestCommentedEvent{
		Provider:       shared.PlatformProvider_GitHub,
		GitRef:         event.PullRequest.Head.Ref,
		RepoFullName:   event.PullRequest.Head.Repo.FullName,
		InstallationId: installationId,
	})

	return nil
}

func (c *WEventConsumer) handleCheckRun(event *types.WebhookEvent) error {
	if event.CheckRun == nil {
		slog.Warn("check_run event without check_run data", "action", event.Action)
		return nil
	}

	if event.Sender == nil {
		return nil
	}

	if event.Sender.Login == c.config.BotLoginId {
		return nil
	}

	if event.CheckRun.Conclusion != nil {
		return nil
	}

	if event.Action != "completed" {
		return nil
	}

	if *event.CheckRun.Conclusion != "failure" && *event.CheckRun.Conclusion != "timed_out" {
		return nil
	}

	if event.Installation == nil {
		slog.Warn("check_run event without installation data", "action", event.Action)
		return nil
	}

	if len(event.CheckRun.PullRequests) == 0 {
		slog.Debug("check_run event without pull requests, ignoring")
		return nil
	}

	installationId := strconv.Itoa(event.Installation.ID)

	for _, pr := range event.CheckRun.PullRequests {
		slog.Debug("github check_run event", "action", event.Action, "conclusion", *event.CheckRun.Conclusion, "check_run_id", event.CheckRun.ID, "pr", pr.Head.Ref)

		c.eventBus.Publish(context.Background(), shared.PullRequestChecksFailedEvent{
			Provider:       shared.PlatformProvider_GitHub,
			GitRef:         pr.Head.Ref,
			RepoFullName:   pr.Head.Repo.FullName,
			InstallationId: installationId,
			ChecksFailed:   []string{event.CheckRun.URL},
		})
	}

	return nil
}

func (c *WEventConsumer) handleCheckSuite(event *types.WebhookEvent) error {
	if event.CheckSuite == nil {
		slog.Warn("check_suite event without check_suite data", "action", event.Action)
		return nil
	}

	if event.Sender == nil {
		return nil
	}

	if event.Sender.Login == c.config.BotLoginId {
		return nil
	}

	if event.CheckRun.Conclusion != nil {
		return nil
	}

	if event.Action != "completed" {
		return nil
	}

	if *event.CheckRun.Conclusion != "failure" && *event.CheckRun.Conclusion != "timed_out" {
		return nil
	}

	if event.Installation == nil {
		slog.Warn("check_suite event without installation data", "action", event.Action)
		return nil
	}

	if len(event.CheckSuite.PullRequests) == 0 {
		slog.Debug("check_suite event without pull requests, ignoring")
		return nil
	}

	installationId := strconv.Itoa(event.Installation.ID)

	for _, pr := range event.CheckSuite.PullRequests {
		slog.Debug("github check_suite event", "action", event.Action, "conclusion", *event.CheckRun.Conclusion, "check_suite_id", event.CheckSuite.ID, "pr", pr.Head.Ref)

		c.eventBus.Publish(context.Background(), shared.PullRequestChecksFailedEvent{
			Provider:       shared.PlatformProvider_GitHub,
			GitRef:         pr.Head.Ref,
			RepoFullName:   pr.Head.Repo.FullName,
			InstallationId: installationId,
			ChecksFailed:   []string{pr.URL},
		})
	}

	return nil
}
