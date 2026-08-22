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

package ports

import (
	"context"

	"github.com/workdock-dev/engine/domain/types"
)

// NewHarnessType constructs a harness for an agent session.
type NewHarnessType = func(NewHarnessConstructor) (ForHarnessPlatform, error)

// NewHarnessConstructor contains the dependencies and context required to
// create a harness for an agent session.
type NewHarnessConstructor struct {
	Parts        ForHarnessParts
	Session      *types.Session
	SessionEvent *types.SessionEvent
	Prompt       string
	Secrets      map[string]string
}

// ForHarnessParts defines the parts/chunks emitted by a harness while an agent
// session is running.
type ForHarnessParts interface {
	Thought(ctx context.Context, text string)
	Response(ctx context.Context, text string)
	Action(ctx context.Context, action types.AgentAction)
	Elicitation(ctx context.Context, elicitation types.AgentElicitation)
}

// ForHarnessPlatform defines the port for running an agent session and
// releasing its associated resources.
type ForHarnessPlatform interface {
	// Run executes the agent session.
	Run(ctx context.Context) (*types.SessionEventResult, error)

	// Dispose releases the resources associated with the agent session.
	Dispose(ctx context.Context) error

	// Archive archives the sandbox associated with the agent session.
	Archive(ctx context.Context) error
}

// HarnessPlatformRegistry maps harness providers to their constructors.
type HarnessPlatformRegistry map[types.HarnessProvider]NewHarnessType
