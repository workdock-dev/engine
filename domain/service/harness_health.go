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

import "time"

type HealthStatus int

const (
	Healthy HealthStatus = iota
	Missed
	Unhealthy
)

func (s HealthStatus) String() string {
	switch s {
	case Healthy:
		return "healthy"
	case Missed:
		return "missed"
	case Unhealthy:
		return "unhealthy"
	}
	return "unknown"
}

type LivenessPolicy struct {
	timeout   time.Duration
	maxMisses int
	lastEvent time.Time
	missed    int
}

func NewLivenessPolicy(timeout time.Duration, maxMisses int) *LivenessPolicy {
	return &LivenessPolicy{
		timeout:   timeout,
		maxMisses: maxMisses,
		lastEvent: time.Now(),
		missed:    0,
	}
}

func (p *LivenessPolicy) OnActivity(t time.Time) {
	p.lastEvent = t
	p.missed = 0
}

func (p *LivenessPolicy) Check(t time.Time) HealthStatus {
	if p.timeout <= 0 || p.maxMisses <= 0 {
		return Healthy
	}

	if t.Sub(p.lastEvent) < p.timeout {
		return Healthy
	}

	p.missed++

	if p.missed >= p.maxMisses {
		return Unhealthy
	}

	return Missed
}

func (p *LivenessPolicy) MissedCount() int {
	return p.missed
}

func (p *LivenessPolicy) IsEnabled() bool {
	return p.timeout > 0 && p.maxMisses > 0
}

func (p *LivenessPolicy) GetTimeout() time.Duration {
	return p.timeout
}
