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
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	DefaultWorkers           = 3
	DefaultMaxAttempts       = 2
	DefaultHeartbeatInterval = time.Minute
	DefaultLeaseDuration     = time.Minute * 5
	DefaultRetryGracePeriod  = time.Minute
)

var (
	errShutdownRequeue    = errors.New("scheduler shutdown")
	errJobCancelledByUser = errors.New("cancelled by user")
)

type TaskScheduler struct {
	serviceId        string
	config           types.TaskSchedulerConfig
	cond             sync.Cond
	lastNotification time.Time
	extQueue         interfaces.Queue
	running          sync.Map
	handler          types.JobHandler
	closed           bool
	tracer           trace.Tracer
	busyWorkers      atomic.Int64
	metrics          *SchedulerMetrics
}

// NewTaskScheduler creates a new TaskScheduler with sensible defaults when
// optional configuration values are not provided. The scheduler is responsible
// for coordinating job execution through the configured queue.
func NewTaskScheduler(queue interfaces.Queue, config types.TaskSchedulerConfig, handler types.JobHandler) (*TaskScheduler, error) {
	if config.Workers <= 0 {
		config.Workers = DefaultWorkers
	}

	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultMaxAttempts
	}

	s := &TaskScheduler{
		serviceId:        uuid.NewString(),
		config:           config,
		cond:             *sync.NewCond(&sync.Mutex{}),
		lastNotification: time.Now(),
		extQueue:         queue,
		handler:          handler,
		closed:           false,
		tracer:           otel.Tracer("workdock.task_scheduler"),
	}

	metrics, err := NewMetrics(otel.Meter("workdock.task_scheduler"), s)

	if err != nil {
		slog.Error("[task-scheduler] failed to initialize metrics", "err", err)
		return nil, err
	}

	s.metrics = metrics

	return s, nil
}

// Run starts the scheduler, listens for queue notifications, wakes workers
// when new work may be available, and blocks until the scheduler shuts down.
func (s *TaskScheduler) Run(ctx context.Context) error {
	runnable, cancellable, err := s.extQueue.Listen(ctx)
	slog.Debug("[task-scheduler] listening to queue")

	if err != nil {
		slog.Error("[task-scheduler] listeting to queue failed", "err", err)
		return err
	}

	var wg sync.WaitGroup

	shutdown := func() {
		s.cond.L.Lock()
		s.closed = true
		s.cond.Broadcast()
		s.running.Range(func(key any, value any) bool {
			if cancel, ok := value.(context.CancelCauseFunc); ok {
				slog.Debug("[task-scheduler] shutdown worker", "event_identifier", key)
				cancel(errShutdownRequeue)
			}

			return true
		})
		s.running.Clear()
		s.cond.L.Unlock()
	}

	wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				shutdown()
				return
			case sessionEventIdentifier, ok := <-cancellable:
				if !ok {
					shutdown()
					return
				}

				if val, ok := s.running.Load(sessionEventIdentifier); ok {
					slog.Debug("[task-scheduler] job cancelled", "event_identifier", sessionEventIdentifier)
					val.(context.CancelCauseFunc)(errJobCancelledByUser)
				}

			case _, ok := <-runnable:
				if !ok {
					shutdown()
					return
				}

				s.cond.L.Lock()
				s.lastNotification = time.Now()
				s.cond.Broadcast()
				s.cond.L.Unlock()
			}
		}
	})

	for i := 0; i < s.config.Workers; i++ {
		wg.Go(func() {
			s.worker(ctx, i+1)
		})
	}

	slog.Info("[task-scheduler] worker pool started", "capacity", s.config.Workers)
	wg.Wait()

	return nil
}

// worker waits for notifications that new work may be available, attempts to
// claim a single job from the queue, and executes it until the scheduler shuts
// down.
func (s *TaskScheduler) worker(ctx context.Context, workerId int) {
	lastSeen := time.Time{}

	for {
		s.cond.L.Lock()

		for !lastSeen.Before(s.lastNotification) && !s.closed {
			s.cond.Wait()
		}

		if s.closed {
			s.cond.L.Unlock()
			return
		}

		lastSeen = s.lastNotification
		s.cond.L.Unlock()

		func() {
			jCtx, cancel := context.WithCancelCause(ctx)
			exCtx, exSpan := s.tracer.Start(
				jCtx,
				"job.execute",
				trace.WithAttributes(
					attribute.Int("worker.id", workerId),
				),
			)

			var job *types.EventJob
			var err error

			defer func() {
				if job != nil {
					s.busyWorkers.Add(-1)
					s.running.Delete(job.SessionEventIdentifier)
				}

				exSpan.End()
				// The first cause wins, so this never overwrites an explicit
				// cancellation cause set by shutdown or a user cancellation.
				cancel(nil)
			}()

			cCtx, cSpan := s.tracer.Start(exCtx, "job.claim")
			if job, err = s.extQueue.Claim(cCtx, s.serviceId, time.Now().Add(DefaultLeaseDuration)); err != nil {
				if errors.Is(err, interfaces.ErrJobNotRunnable) {
					slog.Debug("[task-scheduler] failed to claimed job", "worker_id", workerId)
				} else {
					cSpan.RecordError(err)
				}

				cSpan.End()
				return
			}
			cSpan.End()

			// There werent jobs left to claim
			if job == nil {
				return
			}

			s.busyWorkers.Add(1)
			startedAt := time.Now()
			s.running.Store(job.SessionEventIdentifier, cancel)
			slog.Debug("[task-scheduler] claimed job", "worker_id", workerId, "event_identifier", job.SessionEventIdentifier)

			job.SetMaxAttempts(s.config.MaxAttempts)

			telemetry.SpanDo(exCtx, s.tracer, "job.handler", func(ctx context.Context) {
				s.execute(ctx, job, startedAt)
			}, trace.WithAttributes(
				attribute.String("job.event_identifier", job.SessionEventIdentifier),
				attribute.String("job.queued_by", job.QueuedBy),
				attribute.Int("job.attempt", job.Attempts),
				attribute.Int("job.max_attempts", s.config.MaxAttempts),
			))
		}()
	}
}

// execute processes a claimed job using its registered handler while keeping
// its lease alive through periodic heartbeats. Based on the execution result,
// the job is completed, retried, or marked as failed.
func (s *TaskScheduler) execute(ctx context.Context, job *types.EventJob, startedAt time.Time) {
	span := trace.SpanFromContext(ctx)
	hCtx, cancel := context.WithCancel(ctx)

	go func() {
		t := time.NewTicker(time.Duration(DefaultHeartbeatInterval))
		defer t.Stop()

		for {
			select {
			case <-hCtx.Done():
				slog.Debug("[task-scheduler] job heartbeat stopped", "event_identifier", job.SessionEventIdentifier)
				return
			case <-t.C:
				if err := s.extQueue.Heartbeat(hCtx, job.SessionEventIdentifier, time.Duration(DefaultLeaseDuration)); err != nil {
					span.AddEvent(
						"job.heartbeat.failed",
						trace.WithAttributes(
							attribute.String("error", err.Error()),
						),
					)
					span.RecordError(err)
				} else {
					span.AddEvent("job.heartbeat")
				}
			}
		}
	}()

	slog.Debug("[task-scheduler] processing job", "event_identifier", job.SessionEventIdentifier)
	err := s.handler(ctx, job)
	cancel()

	duration := float64(time.Since(startedAt)) / float64(time.Millisecond)

	if ctx.Err() != nil {
		span.SetAttributes(attribute.String("job.result", "cancelled"))
		span.AddEvent("job.cancelled")

		s.metrics.recordJob(ctx, ResultCancelled, "", duration)

		if errors.Is(context.Cause(ctx), errJobCancelledByUser) {
			slog.Debug("[task-scheduler] job cancelled by user, job already cancelled in database", "event_identifier", job.SessionEventIdentifier, "success", "-")
			return
		}

		// The scheduler is shutting down while the job was still running.
		// Without a status update the job would stay running until its lease
		// expires, so release it back to the queue for another attempt. The
		// parent context is already cancelled, so run the update on a
		// non-cancelled context.
		span.SetAttributes(attribute.Bool("job.retry", true))
		slog.Debug("[task-scheduler] job cancelled by shutdown, releasing job for retry", "event_identifier", job.SessionEventIdentifier, "success", "-")

		telemetry.SpanErr(context.WithoutCancel(ctx), s.tracer, "job.release", func(ctx context.Context) error {
			return s.extQueue.Retry(ctx, job.SessionEventIdentifier, errShutdownRequeue, DefaultRetryGracePeriod)
		})

		return
	}

	if err != nil {
		span.SetAttributes(attribute.String("job.result", "failed"))
		span.SetAttributes(attribute.Bool("job.retry", job.WillRetry()))

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		result := ResultFailed
		if job.WillRetry() {
			result = ResultRetry
		}

		s.metrics.recordJob(ctx, result, errorType(err), duration)

		if !job.WillRetry() {
			telemetry.SpanErr(ctx, s.tracer, "job.fail", func(ctx context.Context) error {
				return s.extQueue.Fail(ctx, job.SessionEventIdentifier, err)
			})
		} else {
			telemetry.SpanErr(ctx, s.tracer, "job.retry", func(ctx context.Context) error {
				return s.extQueue.Retry(ctx, job.SessionEventIdentifier, err, DefaultRetryGracePeriod)
			})
		}

		slog.Debug("[task-scheduler] job processed", "event_identifier", job.SessionEventIdentifier, "success", "false", "cause", err)
		return
	}

	slog.Debug("[task-scheduler] job processed", "event_identifier", job.SessionEventIdentifier, "success", "true")

	err = telemetry.SpanErr(ctx, s.tracer, "job.complete", func(ctx context.Context) error {
		return s.extQueue.Complete(ctx, job.SessionEventIdentifier)
	})

	if err != nil {
		span.SetAttributes(attribute.String("job.result", "failed"))
		span.SetStatus(codes.Error, "complete state not set")
		s.metrics.recordJob(ctx, ResultFailed, errorType(err), duration)
	} else {
		span.SetAttributes(attribute.String("job.result", "success"))
		span.SetStatus(codes.Ok, "")
		s.metrics.recordJob(ctx, ResultSucceeded, "", duration)
	}
}
