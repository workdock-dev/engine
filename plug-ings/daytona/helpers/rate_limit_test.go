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
	"net/http"
	"testing"
	"time"

	sdkerrors "github.com/daytona/clients/sdk-go/pkg/errors"
	"github.com/stretchr/testify/suite"
)

type RateLimitSuite struct {
	suite.Suite
}

func TestRateLimitSuite(t *testing.T) {
	suite.Run(t, new(RateLimitSuite))
}

func (s *RateLimitSuite) SetupTest() {
	resetRateLimits()
}

func resetRateLimits() {
	rateLimits.mu.Lock()
	defer rateLimits.mu.Unlock()
	rateLimits.states = make(map[throttler]*rateLimitState)
}

// --- observe tests ---

func (s *RateLimitSuite) TestObserve_EmptyHeaders() {
	rateLimits.observe(http.Header{})
	snapshot, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.False(ok)
	s.Zero(snapshot)
}

func (s *RateLimitSuite) TestObserve_NilHeaders() {
	rateLimits.observe(nil)
	_, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.False(ok)
}

func (s *RateLimitSuite) TestObserve_PartialHeaders() {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-authenticated", "100")
	h.Set("X-RateLimit-Remaining-authenticated", "50")
	h.Set("X-RateLimit-Reset-authenticated", "30")

	rateLimits.observe(h)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(100, state.limit)
	s.Equal(50, state.remaining)
	s.True(state.known)
	s.False(state.resetAt.IsZero())

	_, ok = rateLimits.snapshot(ThrottlerSandboxCreate)
	s.False(ok)
}

func (s *RateLimitSuite) TestObserve_AllThrottlers() {
	h := http.Header{}
	for _, t := range knownThrottlers {
		h.Set("X-RateLimit-Limit-"+string(t), "200")
		h.Set("X-RateLimit-Remaining-"+string(t), "100")
		h.Set("X-RateLimit-Reset-"+string(t), "60")
	}

	rateLimits.observe(h)

	for _, t := range knownThrottlers {
		state, ok := rateLimits.snapshot(t)
		s.True(ok, "throttler %s should exist", t)
		s.Equal(200, state.limit)
		s.Equal(100, state.remaining)
		s.True(state.known)
	}
}

func (s *RateLimitSuite) TestObserve_Overwrite() {
	h1 := http.Header{}
	h1.Set("X-RateLimit-Limit-authenticated", "100")
	h1.Set("X-RateLimit-Remaining-authenticated", "80")
	h1.Set("X-RateLimit-Reset-authenticated", "30")
	rateLimits.observe(h1)

	h2 := http.Header{}
	h2.Set("X-RateLimit-Limit-authenticated", "100")
	h2.Set("X-RateLimit-Remaining-authenticated", "20")
	h2.Set("X-RateLimit-Reset-authenticated", "10")
	rateLimits.observe(h2)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(100, state.limit)
	s.Equal(20, state.remaining)
	s.True(state.known)
}

func (s *RateLimitSuite) TestObserve_OnlyLimitHeader() {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-authenticated", "50")

	rateLimits.observe(h)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(50, state.limit)
	s.Equal(0, state.remaining)
	s.False(state.known)
}

func (s *RateLimitSuite) TestObserve_OnlyRemainingHeader() {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-authenticated", "30")

	rateLimits.observe(h)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(30, state.remaining)
	s.True(state.known)
	s.Equal(0, state.limit)
}

func (s *RateLimitSuite) TestObserve_InvalidNumbers() {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-authenticated", "not-a-number")
	h.Set("X-RateLimit-Remaining-authenticated", "also-not")
	h.Set("X-RateLimit-Reset-authenticated", "nope")

	rateLimits.observe(h)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(0, state.limit)
	s.Equal(0, state.remaining)
	s.True(state.known)
}

func (s *RateLimitSuite) TestObserve_EmptyStringValues() {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-authenticated", "")
	h.Set("X-RateLimit-Remaining-authenticated", "")
	h.Set("X-RateLimit-Reset-authenticated", "")

	rateLimits.observe(h)

	_, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.False(ok)
}

// --- snapshot tests ---

func (s *RateLimitSuite) TestSnapshot_UnknownThrottler() {
	_, ok := rateLimits.snapshot("nonexistent")
	s.False(ok)
}

func (s *RateLimitSuite) TestSnapshot_KnownThrottler() {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-authenticated", "100")
	h.Set("X-RateLimit-Remaining-authenticated", "50")
	h.Set("X-RateLimit-Reset-authenticated", "30")
	rateLimits.observe(h)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(100, state.limit)
	s.Equal(50, state.remaining)
	s.True(state.known)
}

func (s *RateLimitSuite) TestSnapshot_ReturnsCopy() {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-authenticated", "100")
	h.Set("X-RateLimit-Remaining-authenticated", "50")
	rateLimits.observe(h)

	s1, _ := rateLimits.snapshot(ThrottlerAuthenticated)
	s2, _ := rateLimits.snapshot(ThrottlerAuthenticated)
	s.Equal(s1, s2)

	s1.remaining = 0
	s3, _ := rateLimits.snapshot(ThrottlerAuthenticated)
	s.Equal(50, s3.remaining)
}

// --- Preflight tests ---

func (s *RateLimitSuite) TestPreflight_NoState() {
	err := Preflight(context.Background(), ThrottlerAuthenticated, "test op")
	s.NoError(err)
}

func (s *RateLimitSuite) TestPreflight_AboveReserve() {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-authenticated", "10")
	h.Set("X-RateLimit-Reset-authenticated", "60")
	rateLimits.observe(h)

	err := Preflight(context.Background(), ThrottlerAuthenticated, "test op")
	s.NoError(err)
}

func (s *RateLimitSuite) TestPreflight_EqualReserve_Blocks() {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-authenticated", "2")
	h.Set("X-RateLimit-Reset-authenticated", "5")
	rateLimits.observe(h)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := Preflight(ctx, ThrottlerAuthenticated, "test op")
	elapsed := time.Since(start)

	s.ErrorIs(err, context.DeadlineExceeded)
	s.GreaterOrEqual(elapsed, 40*time.Millisecond)
}

func (s *RateLimitSuite) TestPreflight_BelowReserve_FutureReset() {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-authenticated", "1")
	h.Set("X-RateLimit-Reset-authenticated", "1")
	rateLimits.observe(h)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := Preflight(ctx, ThrottlerAuthenticated, "test op")
	elapsed := time.Since(start)

	s.ErrorIs(err, context.DeadlineExceeded)
	s.GreaterOrEqual(elapsed, 90*time.Millisecond)
}

func (s *RateLimitSuite) TestPreflight_BelowReserve_ContextCancelled() {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-authenticated", "1")
	h.Set("X-RateLimit-Reset-authenticated", "60")
	rateLimits.observe(h)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Preflight(ctx, ThrottlerAuthenticated, "test op")
	s.ErrorIs(err, context.Canceled)
}

func (s *RateLimitSuite) TestPreflight_BelowReserve_ResetPassed() {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining-authenticated", "1")
	h.Set("X-RateLimit-Reset-authenticated", "0")
	rateLimits.observe(h)

	err := Preflight(context.Background(), ThrottlerAuthenticated, "test op")
	s.NoError(err)
}

// --- RetryRateLimited tests ---

func (s *RateLimitSuite) TestRetryRateLimited_SuccessFirstTry() {
	callCount := 0
	result, err := RetryRateLimited(context.Background(), ThrottlerAuthenticated, "test", func() (string, error) {
		callCount++
		return "ok", nil
	})

	s.NoError(err)
	s.Equal("ok", result)
	s.Equal(1, callCount)
}

func (s *RateLimitSuite) TestRetryRateLimited_RateLimitThenSuccess() {
	callCount := 0
	result, err := RetryRateLimited(context.Background(), ThrottlerAuthenticated, "test", func() (string, error) {
		callCount++
		if callCount == 1 {
			return "", sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
		}
		return "ok", nil
	})

	s.NoError(err)
	s.Equal("ok", result)
	s.Equal(2, callCount)
}

func (s *RateLimitSuite) TestRetryRateLimited_MaxRetriesExhausted() {
	callCount := 0
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := RetryRateLimited(ctx, ThrottlerAuthenticated, "test", func() (string, error) {
		callCount++
		return "", sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
	})

	s.Error(err)
}

func (s *RateLimitSuite) TestRetryRateLimited_NonRateLimitError() {
	callCount := 0
	_, err := RetryRateLimited(context.Background(), ThrottlerAuthenticated, "test", func() (string, error) {
		callCount++
		return "", errors.New("some other error")
	})

	s.Error(err)
	s.Equal("some other error", err.Error())
	s.Equal(1, callCount)
}

func (s *RateLimitSuite) TestRetryRateLimited_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := RetryRateLimited(ctx, ThrottlerAuthenticated, "test", func() (string, error) {
		callCount++
		return "", sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
	})

	s.Error(err)
}

func (s *RateLimitSuite) TestRetryRateLimited_ObservesHeaders() {
	callCount := 0
	headers := http.Header{}
	headers.Set("X-RateLimit-Limit-authenticated", "100")
	headers.Set("X-RateLimit-Remaining-authenticated", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _ = RetryRateLimited(ctx, ThrottlerAuthenticated, "test", func() (string, error) {
		callCount++
		if callCount == 1 {
			return "", &sdkerrors.DaytonaError{
				Message:    "rate limited",
				StatusCode: http.StatusTooManyRequests,
				Headers:    headers,
			}
		}
		return "ok", nil
	})

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(100, state.limit)
	s.Equal(1, state.remaining)
}

// --- RetryRateLimitedVoid tests ---

func (s *RateLimitSuite) TestRetryRateLimitedVoid_Success() {
	err := RetryRateLimitedVoid(context.Background(), ThrottlerAuthenticated, "test", func() error {
		return nil
	})
	s.NoError(err)
}

func (s *RateLimitSuite) TestRetryRateLimitedVoid_Error() {
	err := RetryRateLimitedVoid(context.Background(), ThrottlerAuthenticated, "test", func() error {
		return errors.New("some error")
	})
	s.Error(err)
	s.Equal("some error", err.Error())
}

func (s *RateLimitSuite) TestRetryRateLimitedVoid_RateLimitThenSuccess() {
	callCount := 0
	err := RetryRateLimitedVoid(context.Background(), ThrottlerAuthenticated, "test", func() error {
		callCount++
		if callCount == 1 {
			return sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
		}
		return nil
	})

	s.NoError(err)
	s.Equal(2, callCount)
}

// --- isRateLimitError tests ---

func (s *RateLimitSuite) TestIsRateLimitError_RateLimit() {
	err := sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
	s.True(isRateLimitError(err))
}

func (s *RateLimitSuite) TestIsRateLimitError_OtherError() {
	err := sdkerrors.NewDaytonaError("not found", http.StatusNotFound, nil)
	s.False(isRateLimitError(err))
}

func (s *RateLimitSuite) TestIsRateLimitError_GeneralError() {
	err := errors.New("generic error")
	s.False(isRateLimitError(err))
}

// --- backoffDelay tests ---

func (s *RateLimitSuite) TestBackoffDelay_RetryAfterThrottlerHeader() {
	headers := http.Header{}
	headers.Set("Retry-After-authenticated", "5")

	err := &sdkerrors.DaytonaError{Headers: headers}
	delay := backoffDelay(ThrottlerAuthenticated, err, 0)

	s.Equal(5*time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_RetryAfterGenericHeader() {
	headers := http.Header{}
	headers.Set("Retry-After", "10")

	err := &sdkerrors.DaytonaError{Headers: headers}
	delay := backoffDelay(ThrottlerAuthenticated, err, 0)

	s.Equal(10*time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_ThrottlerSpecificOverGeneric() {
	headers := http.Header{}
	headers.Set("Retry-After", "10")
	headers.Set("Retry-After-sandbox-create", "3")

	err := &sdkerrors.DaytonaError{Headers: headers}
	delay := backoffDelay(ThrottlerSandboxCreate, err, 0)

	s.Equal(3*time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_ExponentialFallback() {
	err := errors.New("no headers")
	delay := backoffDelay(ThrottlerAuthenticated, err, 0)
	s.Equal(time.Second, delay)

	delay = backoffDelay(ThrottlerAuthenticated, err, 1)
	s.Equal(2*time.Second, delay)

	delay = backoffDelay(ThrottlerAuthenticated, err, 2)
	s.Equal(4*time.Second, delay)

	delay = backoffDelay(ThrottlerAuthenticated, err, 3)
	s.Equal(8*time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_InvalidRetryAfter() {
	headers := http.Header{}
	headers.Set("Retry-After", "not-a-number")

	err := &sdkerrors.DaytonaError{Headers: headers}
	delay := backoffDelay(ThrottlerAuthenticated, err, 0)

	s.Equal(time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_ZeroRetryAfter() {
	headers := http.Header{}
	headers.Set("Retry-After", "0")

	err := &sdkerrors.DaytonaError{Headers: headers}
	delay := backoffDelay(ThrottlerAuthenticated, err, 0)

	s.Equal(time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_NilHeaders() {
	err := &sdkerrors.DaytonaError{Headers: nil}
	delay := backoffDelay(ThrottlerAuthenticated, err, 2)

	s.Equal(4*time.Second, delay)
}

func (s *RateLimitSuite) TestBackoffDelay_NonDaytonaError() {
	err := errors.New("not a daytona error")
	delay := backoffDelay(ThrottlerAuthenticated, err, 1)

	s.Equal(2*time.Second, delay)
}

// --- waitForRateLimit tests ---

func (s *RateLimitSuite) TestWaitForRateLimit_TimerFires() {
	err := sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
	start := time.Now()
	waitErr := waitForRateLimit(context.Background(), ThrottlerAuthenticated, err, 0)
	elapsed := time.Since(start)

	s.NoError(waitErr)
	s.GreaterOrEqual(elapsed, 900*time.Millisecond)
}

func (s *RateLimitSuite) TestWaitForRateLimit_ContextCancelled() {
	err := sdkerrors.NewDaytonaError("rate limited", http.StatusTooManyRequests, nil)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	waitErr := waitForRateLimit(ctx, ThrottlerAuthenticated, err, 0)
	elapsed := time.Since(start)

	s.ErrorIs(waitErr, context.Canceled)
	s.Less(elapsed, 500*time.Millisecond)
}

func (s *RateLimitSuite) TestWaitForRateLimit_WithRetryAfterHeader() {
	headers := http.Header{}
	headers.Set("Retry-After-authenticated", "1")
	err := &sdkerrors.DaytonaError{
		Message:    "rate limited",
		StatusCode: http.StatusTooManyRequests,
		Headers:    headers,
	}

	start := time.Now()
	waitErr := waitForRateLimit(context.Background(), ThrottlerAuthenticated, err, 0)
	elapsed := time.Since(start)

	s.NoError(waitErr)
	s.GreaterOrEqual(elapsed, 900*time.Millisecond)
}

// --- rateLimitTransport tests ---

func (s *RateLimitSuite) TestRateLimitTransport_RoundTrip_ObservesHeaders() {
	base := &mockTransport{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
		},
	}
	base.response.Header.Set("X-RateLimit-Limit-authenticated", "100")
	base.response.Header.Set("X-RateLimit-Remaining-authenticated", "50")
	base.response.Header.Set("X-RateLimit-Reset-authenticated", "30")

	transport := &rateLimitTransport{base: base}
	req, _ := http.NewRequest("GET", "http://localhost/test", nil)
	resp, err := transport.RoundTrip(req)

	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	state, ok := rateLimits.snapshot(ThrottlerAuthenticated)
	s.True(ok)
	s.Equal(100, state.limit)
	s.Equal(50, state.remaining)
}

func (s *RateLimitSuite) TestRateLimitTransport_RoundTrip_Error() {
	base := &mockTransport{
		err: errors.New("connection refused"),
	}

	transport := &rateLimitTransport{base: base}
	req, _ := http.NewRequest("GET", "http://localhost/test", nil)
	resp, err := transport.RoundTrip(req)

	s.Error(err)
	s.Nil(resp)
}

type mockTransport struct {
	response *http.Response
	err      error
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.response, m.err
}
