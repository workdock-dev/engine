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

package helpers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	sdkerrors "github.com/daytona/clients/sdk-go/pkg/errors"
)

// Daytona rate limits are applied per organization, and every response
// carries the org's current consumption for the throttler that produced it:
//
//	X-RateLimit-Limit-{throttler}     max requests per window
//	X-RateLimit-Remaining-{throttler} requests left in the window
//	X-RateLimit-Reset-{throttler}     seconds until the window resets
//	Retry-After-{throttler}           seconds to wait (on 429)
//
// The throttlers are: anonymous, authenticated, sandbox-create and
// sandbox-lifecycle. Because rate limits are tracked per organization, the
// numbers reported by the API are shared truth across all engine replicas:
// each replica observes them and throttles itself accordingly, without any
// cross-process coordination or tier configuration.
//
// This module:
//   - observes the rate limit headers on every response through a shared
//     http.Transport, keeping a process-wide registry per throttler;
//   - pre-flights rate-limited operations, waiting out the current window
//     before the remaining budget is exhausted (or already is);
//   - retries 429 responses with exponential backoff, honoring Retry-After.
type throttler string

const (
	ThrottlerAnonymous        throttler = "anonymous"
	ThrottlerAuthenticated    throttler = "authenticated"
	ThrottlerSandboxCreate    throttler = "sandbox-create"
	ThrottlerSandboxLifecycle throttler = "sandbox-lifecycle"
)

var knownThrottlers = []throttler{
	ThrottlerAnonymous,
	ThrottlerAuthenticated,
	ThrottlerSandboxCreate,
	ThrottlerSandboxLifecycle,
}

const (
	// throttleReserve is how many remaining requests the pre-flight gate
	// requires before letting an operation through. It covers observation
	// lag and multi-replica races: the reported remaining can be stale by
	// the time the next request is sent, so the gate waits a little before
	// the org's budget is actually exhausted.
	throttleReserve = 2

	// rateLimitRetries is the maximum number of attempts (including the
	// first one) for a rate-limited operation.
	rateLimitRetries = 5

	// rateLimitBackoffBase is the exponential backoff base (1s, 2s, 4s, 8s).
	rateLimitBackoffBase = time.Second

	// daytonaHTTPTimeout matches the SDK default per-request timeout. It is
	// preserved here because injecting a custom HTTPClient overrides it.
	daytonaHTTPTimeout = 60 * time.Second
)

// daytonaHTTPClient is the shared HTTP client for all daytona clients. Its
// Transport observes the rate limit headers on every API response, so the
// registry below reflects the org's consumption regardless of how many
// engine sessions or replicas are making requests.
var HTTPClient = &http.Client{
	Timeout:   daytonaHTTPTimeout,
	Transport: &rateLimitTransport{base: http.DefaultTransport},
}

// rateLimitTransport observes the rate limit headers on every response and
// feeds them into the process-wide registry.
type rateLimitTransport struct {
	base http.RoundTripper
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	rateLimits.observe(resp.Header)
	return resp, nil
}

// rateLimitState is the observed consumption for a single throttler.
type rateLimitState struct {
	limit     int
	remaining int
	resetAt   time.Time
	known     bool
}

// rateLimitRegistry holds the observed rate limit state per throttler. It is
// process-wide and safe for concurrent use.
type rateLimitRegistry struct {
	mu     sync.RWMutex
	states map[throttler]*rateLimitState
}

var rateLimits = &rateLimitRegistry{states: make(map[throttler]*rateLimitState)}

// observe updates the registry from the rate limit headers of a single
// response (success or error).
func (r *rateLimitRegistry) observe(h http.Header) {
	if len(h) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, t := range knownThrottlers {
		suffix := string(t)
		limitStr := h.Get("X-RateLimit-Limit-" + suffix)
		remainingStr := h.Get("X-RateLimit-Remaining-" + suffix)
		resetStr := h.Get("X-RateLimit-Reset-" + suffix)

		if limitStr == "" && remainingStr == "" && resetStr == "" {
			continue
		}

		limit, _ := strconv.Atoi(limitStr)
		remaining, _ := strconv.Atoi(remainingStr)
		reset, _ := strconv.Atoi(resetStr)

		state := r.states[t]
		if state == nil {
			state = &rateLimitState{}
			r.states[t] = state
		}

		if limitStr != "" {
			state.limit = limit
		}

		if remainingStr != "" {
			state.remaining = remaining
			state.known = true
		}

		if resetStr != "" {
			state.resetAt = time.Now().Add(time.Duration(reset) * time.Second)
		}
	}
}

// snapshot returns a copy of the observed state for a throttler, if any.
func (r *rateLimitRegistry) snapshot(t throttler) (rateLimitState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, ok := r.states[t]
	if !ok {
		return rateLimitState{}, false
	}

	return *state, true
}

// preflight gates a rate-limited operation on the org's reported remaining
// budget. When the API reports the budget at or below the reserve threshold
// and the window has not reset yet, it waits until the reset. If nothing has
// been observed yet for the throttler, it proceeds: the API is authoritative
// and the 429 retry path remains the backstop.
func Preflight(ctx context.Context, t throttler, op string) error {
	state, ok := rateLimits.snapshot(t)
	if !ok || !state.known {
		return nil
	}

	if state.remaining > throttleReserve {
		return nil
	}

	wait := time.Until(state.resetAt)
	if wait <= 0 {
		return nil
	}

	slog.Warn("daytona rate limit nearly exhausted; waiting for window reset",
		"op", op,
		"throttler", t,
		"remaining", state.remaining,
		"limit", state.limit,
		"wait", wait,
	)

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryRateLimited runs fn, retrying when the daytona API reports a 429.
// The delay between attempts honors Retry-After-{throttler} (or the generic
// Retry-After) when present, and otherwise falls back to exponential backoff
// (1s, 2s, 4s, 8s, ...). Non-rate-limit errors and context cancellation are
// returned immediately.
func RetryRateLimited[T any](ctx context.Context, t throttler, op string, fn func() (T, error)) (T, error) {
	var (
		out T
		err error
	)

	for attempt := range rateLimitRetries {
		out, err = fn()
		if err == nil {
			return out, nil
		}

		if !isRateLimitError(err) {
			return out, err
		}

		if de, ok := errors.AsType[*sdkerrors.DaytonaError](err); ok {
			rateLimits.observe(de.Headers)
		}

		if attempt == rateLimitRetries-1 {
			break
		}

		if err := waitForRateLimit(ctx, t, err, attempt); err != nil {
			return out, err
		}
	}

	slog.Error("giving up on rate-limited daytona operation",
		"op", op,
		"throttler", t,
		"attempts", rateLimitRetries,
		"err", err,
	)

	return out, err
}

// retryRateLimitedVoid is retryRateLimited for operations that only return an
// error.
func RetryRateLimitedVoid(ctx context.Context, t throttler, op string, fn func() error) error {
	_, err := RetryRateLimited(ctx, t, op, func() (struct{}, error) {
		err := fn()
		return struct{}{}, err
	})

	return err
}

func isRateLimitError(err error) bool {
	return errors.Is(err, sdkerrors.ErrRateLimit)
}

// waitForRateLimit sleeps for the delay before the next attempt. The delay
// comes from the error's Retry-After-{throttler} (or Retry-After) header when
// available, otherwise exponential backoff 2^attempt * 1s.
func waitForRateLimit(ctx context.Context, t throttler, err error, attempt int) error {
	delay := backoffDelay(t, err, attempt)

	slog.Warn("daytona request rate limited; waiting to retry",
		"throttler", t,
		"attempt", attempt+1,
		"delay", delay,
	)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// backoffDelay computes the wait before the next attempt, preferring the
// server-provided Retry-After when present.
func backoffDelay(t throttler, err error, attempt int) time.Duration {
	var de *sdkerrors.DaytonaError
	if errors.As(err, &de) && de.Headers != nil {
		for _, header := range []string{"Retry-After-" + string(t), "Retry-After"} {
			if value := de.Headers.Get(header); value != "" {
				if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
					return time.Duration(seconds) * time.Second
				}
			}
		}
	}

	return time.Duration(1<<uint(attempt)) * rateLimitBackoffBase
}
