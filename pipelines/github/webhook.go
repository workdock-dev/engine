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

	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/workdock-dev/engine/pipelines/runners"
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

func NewWEventTransformer() runners.WEventTransformer {
	return &WEventTransformer{}
}

// NewWEventTransformer creates a webhook transformer for converting HTTP
// requests into transport-neutral webhook events.
func (t *WEventTransformer) Transform(_ context.Context, r *http.Request) (*runners.WEvent, error) {
	return &runners.WEvent{
		Headers:    r.Header,
		RemoteAddr: r.RemoteAddr,
		Body:       r.Body,
	}, nil
}

type WEventVerifier struct {
	config Config
}

// NewWEventVerifier creates a webhook verifier configured with the trusted
// Linear IP addresses and webhook signing secret.
func NewWEventVerifier(config Config) runners.WEventVerifier {
	return &WEventVerifier{
		config: config,
	}
}

// Verify validates the request and returns a verified webhook event.
// Payload-specific validation is deferred to the consumer to avoid
// unmarshaling the payload more than once.
func (t *WEventVerifier) Verify(_ context.Context, event *runners.WEvent) (*runners.VerifiedWEvent, error) {
	rawBody, err := io.ReadAll(event.Body)

	if err != nil {
		slog.Error("failed to read request body", "err", err)
		return nil, runners.ErrWBadRequest
	}

	signature := event.Get("X-Hub-Signature-256")

	if !t.verifyWebhookSignature(signature, rawBody) {
		slog.Error("failed verifying github webhook signature")
		return nil, runners.ErrWUnAuthorized
	}

	eventType := event.Get("X-GitHub-Event")

	if eventType == "" {
		slog.Error("missing X-GitHub-Event header")
		return nil, runners.ErrWBadRequest
	}

	deliveryID := event.Get("X-GitHub-Delivery")

	// TODO: Implement replay attack verification
	// https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks#use-the-x-github-delivery-header

	return &runners.VerifiedWEvent{
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
	client        ClientInterface
	repository    GitHubConnectionRepository
	secretManager ports.ForSecrets
	eventBus      ports.ForEventBus
}

// NewWEventConsumer creates a webhook consumer for processing verified
// Linear webhook events.
func NewWEventConsumer(
	client ClientInterface,
	repository GitHubConnectionRepository,
	secretManager ports.ForSecrets,
	eventBus ports.ForEventBus,
) runners.WEventConsumer {
	return &WEventConsumer{
		client:        client,
		repository:    repository,
		secretManager: secretManager,
		eventBus:      eventBus,
	}
}

// Consume decodes and validates a verified webhook before publishing it.
//
// Timestamp validation is performed after unmarshaling to avoid decoding the
// same payload twice.
func (c *WEventConsumer) Consume(ctx context.Context, event *runners.VerifiedWEvent) error {
	if event.WEventType == WEventType_Ping {
		slog.Debug("github ping event received")
		return nil
	}

	var payload WebhookEvent

	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal webhook payload", "err", err)
		return runners.ErrWBadRequest
	}

	if event.WEventType == WEventType_Installation {
		return c.handleInstallation(ctx, &payload)
	}

	if event.WEventType == WEventType_InstallationRepositories {
		// return s.handleInstallationRepositories(ctx, e)
		return nil
	}

	if event.WEventType == WEventType_Issues {
		// return s.handleIssues(e)
		return nil
	}

	if event.WEventType == WEventType_PullRequestReviewComment {
		// return s.handlePullRequestComment(e)
		return nil
	}

	if event.WEventType == WEventType_CheckRun {
		// return s.handleCheckRun(e)
		return nil
	}

	if event.WEventType == WEventType_CheckSuite {
		// return s.handleCheckSuite(e)
		return nil
	}

	return runners.ErrWBadRequest
}

// handleInstallation processes a GitHub installation event.
func (c *WEventConsumer) handleInstallation(ctx context.Context, event *WebhookEvent) error {
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

		if err := resetInstallation(
			ctx,
			c.repository,
			c.secretManager,
			installationId,
			repos,
		); err != nil {
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

	if ctx.Err() != nil {
		slog.Error("failed to continue context err", "err", ctx.Err())
		return ctx.Err()
	}

	if err := c.secretManager.Set(ctx, GitHub_SecretPath, installationId, string(tokenData)); err != nil {
		slog.Error("failed to store installation access token", "installation_id", event.Installation.ID, "err", err)
		return err
	}

	repos := make([]string, 0, len(event.Repositories)+len(event.RepositoriesAdded))

	for _, repo := range slices.Concat(event.Repositories, event.RepositoriesAdded) {
		repos = append(repos, repo.FullName)
	}

	connections, err := batchGitHubConnections(
		ctx,
		c.repository,
		installationId,
		repos,
	)

	if err != nil {
		return err
	}

	for _, connection := range connections {
		c.eventBus.Publish(context.Background(), types.GitHubConnectedEvent{
			Connection: connection,
		})
	}

	slog.Debug("GitHub installation stored", "installation_id", event.Installation.ID, "expires_at", token.ExpiresAt)
	return nil
}
