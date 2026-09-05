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

package interfaces

import (
	"context"
	"errors"
	"time"

	"github.com/workdock-dev/engine/features/agent_session/types"
)

// ErrJobNotRunnable is returned by Claim when the job is not eligible for
// execution or its group lease is held by another worker.
var ErrJobNotRunnable = errors.New("job is not runnable")

type Queue interface {
	Claim(ctx context.Context, owner string, nextAttemptAt time.Time) (*types.EventJob, error)
	Heartbeat(ctx context.Context, id string, leaseDuration time.Duration) error
	Complete(ctx context.Context, id string, status types.EventJobStatus) error
	Retry(ctx context.Context, id string, cause error, retryGracePeriod time.Duration) error
	Fail(ctx context.Context, id string, cause error) error
	Listen(ctx context.Context) (<-chan struct{}, <-chan string, error)
}
