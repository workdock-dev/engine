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

package shared

import "time"

const (
	TokenLifecycleKeep    = "keep"
	TokenLifecycleRenew   = "renew"
	TokenLifecycleExpired = "expired"
)

type TokenLifecycleDecision string

type TokenState struct {
	ExpiresAt  time.Time
	HasRefresh bool
}

func (t TokenState) LifecycleDecision(now time.Time, refreshWindow time.Duration) TokenLifecycleDecision {
	if now.Add(refreshWindow).After(t.ExpiresAt) {
		if !t.HasRefresh {
			return TokenLifecycleExpired
		}
		return TokenLifecycleRenew
	}

	return TokenLifecycleKeep
}

func ShouldRenewToken(expiresAt time.Time, hasRefresh bool, now time.Time, refreshWindow time.Duration) TokenLifecycleDecision {
	state := TokenState{
		ExpiresAt:  expiresAt,
		HasRefresh: hasRefresh,
	}
	return state.LifecycleDecision(now, refreshWindow)
}

const DefaultTokenRefreshWindow = 5 * time.Minute
