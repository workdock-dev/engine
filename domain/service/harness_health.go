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
	HealthStatus_Healthy HealthStatus = iota
	HealthStatus_Missed
	HealthStatus_Unhealthy
)

func (s HealthStatus) String() string {
	switch s {
	case HealthStatus_Healthy:
		return "Healthy"
	case HealthStatus_Missed:
		return "Missed"
	case HealthStatus_Unhealthy:
		return "Unhealthy"
	default:
		return "Unknown"
	}
}

type LivenessPolicy struct {
	timeout      time.Duration
	maxMisses    int
	missed       int
	lastActivity time.Time
}

func NewLivenessPolicy(timeout time.Duration, maxMisses int) *LivenessPolicy {
	return &LivenessPolicy{
		timeout:   timeout,
		maxMisses: maxMisses,
		missed:    0,
	}
}

func (p *LivenessPolicy) OnActivity(t time.Time) {
	if p.timeout <= 0 || p.maxMisses <= 0 {
		return
	}
	p.lastActivity = t
	p.missed = 0
}

func (p *LivenessPolicy) Check(t time.Time) HealthStatus {
	if p.timeout <= 0 || p.maxMisses <= 0 {
		return HealthStatus_Healthy
	}

	if p.lastActivity.IsZero() {
		p.lastActivity = t
		return HealthStatus_Healthy
	}

	if t.Sub(p.lastActivity) < p.timeout {
		p.missed = 0
		return HealthStatus_Healthy
	}

	p.missed++

	if p.missed >= p.maxMisses {
		return HealthStatus_Unhealthy
	}

	return HealthStatus_Missed
}

func (p *LivenessPolicy) MissedCount() int {
	return p.missed
}

func (p *LivenessPolicy) Timeout() time.Duration {
	return p.timeout
}

func (p *LivenessPolicy) MaxMisses() int {
	return p.maxMisses
}

func (p *LivenessPolicy) LastActivity() time.Time {
	return p.lastActivity
}

func (p *LivenessPolicy) IsDisabled() bool {
	return p.timeout <= 0 || p.maxMisses <= 0
}
