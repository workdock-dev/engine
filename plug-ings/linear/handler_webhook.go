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

package linear

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/workdock-dev/engine/features/webhook"
	"github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/shared"
)

// WEventType identifies the type of webhook event received from Linear.
const (
	WEventType_Issue        = "issue"
	WEventType_AgentSession = "agent-session"
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
	if !t.isAllowedIP(event) {
		slog.Error("[webhook][linear] received request from invalid IP", "ip", t.clientIP(event))
		return nil, webhook.ErrWForBidden
	}

	rawBody, err := io.ReadAll(event.Body)

	if err != nil {
		slog.Error("[webhook][linear] failed to parse request body", "err", err)
		return nil, webhook.ErrWBadRequest
	}

	if !t.verifyWebhookSignature(event.Get("Linear-Signature"), rawBody) {
		slog.Error("[webhook][linear] failed verifying request signature")
		return nil, webhook.ErrWUnAuthorized
	}

	eventType := event.Get("Linear-Event")
	slog.Debug("[webhook][linear] event accepted", "event_type", eventType)

	if eventType == "Issue" {
		return &webhook.VerifiedWEvent{
			WEventType: WEventType_Issue,
			Payload:    rawBody,
		}, nil
	}

	if eventType == "AgentSessionEvent" {
		return &webhook.VerifiedWEvent{
			WEventType: WEventType_AgentSession,
			Payload:    rawBody,
		}, nil
	}

	slog.Warn("[webhook][linear] unhandled event type", "event_type", eventType)
	return nil, webhook.ErrWBadRequest
}

// isAllowedIP determines whether a webhook request originates from a trusted
// Linear IP address.
//
// Requests originating from untrusted IP addresses should be rejected before
// processing.
func (t *WEventVerifier) isAllowedIP(event *webhook.WEvent) bool {
	ip := t.clientIP(event)
	return slices.Contains(t.config.IPs, ip)
}

// clientIP extracts the originating client IP address from an HTTP request.
//
//   - Prefers proxy forwarding headers when the application is deployed behind a
//     reverse proxy or load balancer.
//   - Falls back to the remote connection address when no forwarding information
//     is available.
//
// The returned IP is intended for request validation and auditing.
func (t *WEventVerifier) clientIP(event *webhook.WEvent) string {
	if xff := event.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}

	if xri := event.Get("X-Real-IP"); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(event.RemoteAddr)

	if err != nil {
		return event.RemoteAddr
	}

	return host
}

// verifyWebhookSignature validates that a webhook request was signed by Linear.
//
//   - Computes the expected HMAC-SHA256 signature using the configured webhook
//     secret.
//   - Compares the computed signature with the signature provided by Linear using
//     a constant-time comparison to prevent timing attacks.
//
// A successful verification confirms the request originated from a trusted
// source and that the payload was not modified in transit.
func (t *WEventVerifier) verifyWebhookSignature(headerSignature string, body []byte) bool {
	if headerSignature == "" {
		return false
	}

	expected, err := hex.DecodeString(headerSignature)

	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(t.config.WebhookSecret))
	mac.Write(body)

	return subtle.ConstantTimeCompare(mac.Sum(nil), expected) == 1
}

type WEventConsumer struct {
	eventBus shared.ForEventBus
}

// NewWEventConsumer creates a webhook consumer for processing verified
// Linear webhook events.
func NewWEventConsumer(eventBus shared.ForEventBus) webhook.WEventConsumer {
	return &WEventConsumer{
		eventBus: eventBus,
	}
}

// Consume decodes and validates a verified webhook before publishing it.
//
// Timestamp validation is performed after unmarshaling to avoid decoding the
// same payload twice.
func (c *WEventConsumer) Consume(_ context.Context, event *webhook.VerifiedWEvent) error {
	slog.Debug("[webhook][linear] consuming event", "event_type", event.WEventType)
	if event.WEventType == WEventType_Issue {
		var payload types.IssueStatusChangePayload

		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			slog.Error("[webhook][linear] failed to unmarshal issue payload", "err", err)
			return webhook.ErrWBadRequest
		}

		// We verify here and not in the verifier to avoid doing double unmarshaling
		if err := c.verifyTimestampRecency(payload.WebhookTimestamp); err != nil {
			return err
		}

		c.eventBus.Publish(context.Background(), shared.IssueChangedEvent{
			Provider: string(shared.PlatformProvider_Linear),
			Payload:  payload,
		})

		return nil
	}

	if event.WEventType == WEventType_AgentSession {
		var payload types.AgentSessionEventData

		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			slog.Error("[webhook][linear] failed unmarshal agent session payload", "err", err)
			return webhook.ErrWBadRequest
		}

		// We verify here and not in the verifier to avoid doing double unmarshaling
		if err := c.verifyTimestampRecency(payload.WebhookTimestamp); err != nil {
			return err
		}

		if payload.AgentActivity.Signal == types.SignalType_Stop {
			c.eventBus.Publish(context.Background(), shared.AgentSessionStopEvent{
				Provider:               string(shared.PlatformProvider_Linear),
				OrganizationIdentifier: payload.OrganizationID,
				SessionIdentifier:      payload.AgentSession.ID,
			})
		} else {
			c.eventBus.Publish(context.Background(), shared.AgentSessionPromptEvent{
				Provider: string(shared.PlatformProvider_Linear),
				Payload:  payload,
			})
		}

		return nil
	}

	slog.Debug("[webhook][linear] unhandled event", "event_type", event.WEventType)
	return webhook.ErrWBadRequest
}

func (c *WEventConsumer) verifyTimestampRecency(timestamp int64) error {
	diff := time.Since(time.UnixMilli(timestamp))

	if diff < -60*time.Second || diff > 60*time.Second {
		slog.Error("[webhook][linear] event is older than one minute")
		return webhook.ErrWUnAuthorized
	}

	return nil
}
