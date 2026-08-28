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

package types

import (
	"testing"
	"time"
)

func TestValidateWebhookFreshness_WithinWindow(t *testing.T) {
	now := time.Now()
	timestamp := now.Add(-30 * time.Second)

	err := ValidateWebhookFreshness(timestamp, now)
	if err != nil {
		t.Errorf("Expected no error for fresh webhook, got %v", err)
	}
}

func TestValidateWebhookFreshness_TooOld(t *testing.T) {
	now := time.Now()
	timestamp := now.Add(-90 * time.Second)

	err := ValidateWebhookFreshness(timestamp, now)
	if err != ErrWebhookStale {
		t.Errorf("Expected ErrWebhookStale for old webhook, got %v", err)
	}
}

func TestValidateWebhookFreshness_TooFuture(t *testing.T) {
	now := time.Now()
	timestamp := now.Add(90 * time.Second)

	err := ValidateWebhookFreshness(timestamp, now)
	if err != ErrWebhookStale {
		t.Errorf("Expected ErrWebhookStale for future webhook, got %v", err)
	}
}

func TestValidateWebhookFreshness_EdgeCases(t *testing.T) {
	now := time.Now()

	timestamp := now.Add(-60 * time.Second)
	err := ValidateWebhookFreshness(timestamp, now)
	if err != nil {
		t.Errorf("Expected no error at exact boundary (-60s), got %v", err)
	}

	timestamp = now.Add(60 * time.Second)
	err = ValidateWebhookFreshness(timestamp, now)
	if err != nil {
		t.Errorf("Expected no error at exact boundary (+60s), got %v", err)
	}

	timestamp = now.Add(-61 * time.Second)
	err = ValidateWebhookFreshness(timestamp, now)
	if err != ErrWebhookStale {
		t.Errorf("Expected ErrWebhookStale just past boundary (-61s), got %v", err)
	}

	timestamp = now.Add(61 * time.Second)
	err = ValidateWebhookFreshness(timestamp, now)
	if err != ErrWebhookStale {
		t.Errorf("Expected ErrWebhookStale just past boundary (+61s), got %v", err)
	}
}

func TestValidateWebhookFreshness_CurrentTime(t *testing.T) {
	now := time.Now()

	err := ValidateWebhookFreshness(now, now)
	if err != nil {
		t.Errorf("Expected no error for current timestamp, got %v", err)
	}
}
