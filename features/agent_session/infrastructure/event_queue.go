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

package infrastructure

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
)

const (
	LeaseDuration    = time.Minute * 5
	RetryGracePeriod = 1 * time.Minute
	NextAttemptAt    = LeaseDuration + RetryGracePeriod
)

var (
	//go:embed sql/claim.sql
	ClaimSql string

	//go:embed sql/heartbeat.sql
	HeartbeatSql string

	//go:embed sql/complete.sql
	CompleteSql string

	//go:embed sql/retry.sql
	RetrySql string

	//go:embed sql/fail.sql
	FailSql string
)

type DBConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	WaitForNotification(ctx context.Context) (*pgconn.Notification, error)
	Close(ctx context.Context) error
}

// EventQueue is deliberately backed by the same postgres pool as storage so
// webhook acceptance and workflow state remain durable across VM restarts.
type EventQueue struct {
	client shared.PostgresPool
	conn   DBConn
}

func NewEventQueue(client shared.PostgresPool, conn DBConn) *EventQueue {
	return &EventQueue{
		conn:   conn,
		client: client,
	}
}

// Claim atomically acquires the execution lease for an event job.
//
// A job can be claimed when:
//
//   - Its status is Queued.
//   - Its status is RetryScheduled and its retry time has been reached.
//   - Its status is Running but the previous execution lease has expired,
//     allowing another worker to recover and continue processing.
//
// If the job is not claimable, Claim returns ErrJobNotRunnable.
//
// On success, Claim:
//
//   - Transitions the job to Running.
//   - Assigns the provided owner as the lease holder.
//   - Renews the execution lease.
//   - Increments the attempt counter.
//   - Schedules the next eligible execution time in case the lease expires
//     before the worker completes.
//
// The entire operation is performed within a transaction, guaranteeing that
// only one worker can successfully claim the job at a time.
func (q *EventQueue) Claim(ctx context.Context, owner string) (*types.EventJob, error) {
	now := time.Now().UTC()
	var row types.EventJob

	err := q.client.
		QueryRow(
			ctx,
			ClaimSql,
			new(now.Add(NextAttemptAt)),
			new(owner),
			new(now.Add(LeaseDuration)),
		).
		Scan(
			&row.SessionEventIdentifier,
			&row.PreviousState,
			&row.Status,
			&row.Attempts,
			&row.NextAttemptAt,
			&row.LeaseOwner,
			&row.LeaseExpiresAt,
			&row.LastError,
			&row.CancellationReason,
			&row.QueuedBy,
		)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("There aren't any job to claimns")
			return nil, nil
		}

		slog.Error("failed to claim job", "owner", owner, "err", err)
		return nil, err
	}

	return &row, nil
}

// Heartbeat renews the lease for a running event job.
//
//   - Extends the execution lease held by the current worker.
//   - Prevents the job from becoming eligible for reclamation while it is still
//     actively being processed.
//
// The lease is only renewed if the job is currently owned by the specified
// worker.
func (q *EventQueue) Heartbeat(ctx context.Context, id string) error {
	tags, err := q.client.Exec(ctx, HeartbeatSql, id, time.Now().Add(LeaseDuration))

	if err != nil {
		slog.Error("failed to send jobs heartbeat", "event_identifier", id, "err", err)
		return err
	}

	if tags.RowsAffected() == 0 {
		slog.Error("failed to set heartbeat, not affected rows", "event_identifier", id)
	}

	return nil
}

// Complete marks a running event job as successfully finished.
//
//   - Transitions the job to the succeeded state.
//   - Releases the execution lease held by the current worker.
//   - Releases the associated group lease when this job belongs to a workflow
//     group.
//
// Only the worker that currently owns the job may complete it.
func (q *EventQueue) Complete(ctx context.Context, id string) error {
	tags, err := q.client.Exec(ctx, CompleteSql, id)

	if err != nil {
		slog.Error("failed to send job as completed", "event_identifier", id, "err", err)
		return err
	}

	if tags.RowsAffected() == 0 {
		slog.Error("failed to set job as completed, not affected rows", "event_identifier", id)
	}

	return nil
}

// Retry schedules a running event job for another execution attempt.
//
//   - Marks the job as waiting to be retried.
//   - Records the reason for the retry.
//   - Schedules the next execution attempt.
//   - Releases the execution lease so the job can be claimed again after the
//     retry time is reached.
//
// Only the worker that currently owns the job may schedule a retry.
func (q *EventQueue) Retry(ctx context.Context, id string, cause error) error {
	tags, err := q.client.Exec(ctx, RetrySql, id, cause.Error(), time.Now().Add(RetryGracePeriod))

	if err != nil {
		slog.Error("failed to send job for retry", "event_identifier", id, "err", err)
		return err
	}

	if tags.RowsAffected() == 0 {
		slog.Error("failed to set job for retry, not affected rows", "event_identifier", id)
	}

	return nil
}

// Fail permanently marks a running event job as failed.
//
//   - Transitions the job to the failed state.
//   - Records the failure reason.
//   - Releases the execution lease held by the current worker.
//   - Releases the associated group lease when this job belongs to a workflow
//     group.
//
// Only the worker that currently owns the job may mark it as failed.
func (q *EventQueue) Fail(ctx context.Context, id string, cause error) error {
	tags, err := q.client.Exec(ctx, FailSql, id, cause.Error())

	if err != nil {
		slog.Error("failed to send job for failure", "event_identifier", id, "err", err)
		return err
	}

	if tags.RowsAffected() == 0 {
		slog.Error("failed to set job for failure, not affected rows", "event_identifier", id)
	}

	return nil
}

// Listen subscribes to queue changes and returns two channels: one for jobs
// ready to run and another for jobs marked for cancellation. Both channels are
// closed when the listener stops or the context is cancelled.
func (q *EventQueue) Listen(ctx context.Context) (<-chan struct{}, <-chan string, error) {
	runnableCh := make(chan struct{}, 1)
	cancellableCh := make(chan string, 32)

	_, err := q.conn.Exec(ctx, `
LISTEN jobs_claimable;
LISTEN jobs_cancelled;
		`)

	if err != nil {
		slog.Debug("failed to create listener for jobs updates", "err", err)
		return runnableCh, cancellableCh, err
	}

	go func() {
		defer close(runnableCh)
		defer close(cancellableCh)

		for {
			n, err := q.conn.WaitForNotification(ctx)

			if err != nil {
				if ctx.Err() != nil {
					return
				}

				slog.Error("event queue listener failed", "err", err)
				return
			}

			switch n.Channel {
			case "jobs_claimable":
				select {
				case runnableCh <- struct{}{}:
				default:
				}

			case "jobs_cancelled":
				select {
				case cancellableCh <- n.Payload:
				default:
				}
			}
		}
	}()

	return runnableCh, cancellableCh, nil
}

func (q *EventQueue) Close(ctx context.Context) error {
	return q.conn.Close(ctx)
}
