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

package domain_service

import (
	"testing"
	"time"
)

func TestLivenessPolicy_Disabled(t *testing.T) {
	policy := NewLivenessPolicy(0, 0)

	if policy.IsEnabled() {
		t.Fatal("Expected policy to be disabled with zero timeout and maxMisses")
	}

	now := time.Now()
	status := policy.Check(now)

	if status != Healthy {
		t.Errorf("Expected Healthy status when disabled, got %v", status)
	}
}

func TestLivenessPolicy_ActivityResetsMissed(t *testing.T) {
	policy := NewLivenessPolicy(100*time.Millisecond, 3)

	now := time.Now()

	policy.OnActivity(now)

	time.Sleep(150 * time.Millisecond)

	status1 := policy.Check(time.Now())
	if status1 != Missed {
		t.Errorf("Expected Missed status after first timeout, got %v", status1)
	}

	policy.OnActivity(time.Now())

	status2 := policy.Check(time.Now())
	if status2 != Healthy {
		t.Errorf("Expected Healthy status after activity reset, got %v", status2)
	}

	if policy.MissedCount() != 0 {
		t.Errorf("Expected missed count to be 0 after activity reset, got %d", policy.MissedCount())
	}
}

func TestLivenessPolicy_UnhealthyAfterMaxMisses(t *testing.T) {
	policy := NewLivenessPolicy(50*time.Millisecond, 2)

	now := time.Now()
	policy.OnActivity(now)

	time.Sleep(100 * time.Millisecond)

	status1 := policy.Check(time.Now())
	if status1 != Missed {
		t.Errorf("Expected Missed status after first miss, got %v", status1)
	}

	time.Sleep(100 * time.Millisecond)

	status2 := policy.Check(time.Now())
	if status2 != Unhealthy {
		t.Errorf("Expected Unhealthy status after max misses, got %v", status2)
	}

	if policy.MissedCount() != 2 {
		t.Errorf("Expected missed count to be 2, got %d", policy.MissedCount())
	}
}

func TestLivenessPolicy_HealthyWithinTimeout(t *testing.T) {
	policy := NewLivenessPolicy(100*time.Millisecond, 3)

	now := time.Now()
	policy.OnActivity(now)

	time.Sleep(50 * time.Millisecond)

	status := policy.Check(time.Now())
	if status != Healthy {
		t.Errorf("Expected Healthy status within timeout, got %v", status)
	}

	if policy.MissedCount() != 0 {
		t.Errorf("Expected missed count to be 0, got %d", policy.MissedCount())
	}
}

func TestLivenessPolicy_String(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		expected string
	}{
		{Healthy, "healthy"},
		{Missed, "missed"},
		{Unhealthy, "unhealthy"},
	}

	for _, test := range tests {
		if test.status.String() != test.expected {
			t.Errorf("Expected %v.String() to return %q, got %q", test.status, test.expected, test.status.String())
		}
	}
}

func TestLivenessPolicy_MultipleActivities(t *testing.T) {
	policy := NewLivenessPolicy(100*time.Millisecond, 3)

	times := []time.Time{
		time.Now(),
		time.Now().Add(50 * time.Millisecond),
		time.Now().Add(100 * time.Millisecond),
	}

	for _, t := range times {
		policy.OnActivity(t)
	}

	status := policy.Check(time.Now())
	if status != Healthy {
		t.Errorf("Expected Healthy status after multiple activities, got %v", status)
	}

	if policy.MissedCount() != 0 {
		t.Errorf("Expected missed count to be 0 after multiple activities, got %d", policy.MissedCount())
	}
}
