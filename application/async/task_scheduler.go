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

package async

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jazielguerrero/workdock/application/interfaces"
	"github.com/jazielguerrero/workdock/domain/telemetry"
	"github.com/jazielguerrero/workdock/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	DefaultWorkers     = 3
	DefaultMaxAttempts = 2
	HeartbeatInterval  = time.Minute
)

type HandlerFunc func(ctx context.Context, job *types.EventJob) error

type TaskSchedulerConfig struct {
	Workers           int
	LeaseDuration     time.Duration
	MaxAttempts       int
	HeartbeatInterval time.Duration
}

type TaskScheduler struct {
	serviceId        string
	config           TaskSchedulerConfig
	cond             sync.Cond
	lastNotification time.Time
	extQueue         interfaces.Queue
	running          sync.Map
	handler          HandlerFunc
	closed           bool
	tracer           trace.Tracer
	busyWorkers      atomic.Int64
	metrics          *SchedulerMetrics
}

// NewTaskScheduler creates a new TaskScheduler with sensible defaults when
// optional configuration values are not provided. The scheduler is responsible
// for coordinating job execution through the configured queue.
func NewTaskScheduler(queue interfaces.Queue, config TaskSchedulerConfig, handler HandlerFunc) (*TaskScheduler, error) {
	if config.Workers <= 0 {
		config.Workers = DefaultWorkers
	}

	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultMaxAttempts
	}

	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = HeartbeatInterval
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
		slog.Error("failed to initialize task scheduler metrics", "err", err)
		return nil, err
	}

	s.metrics = metrics

	return s, nil
}

// Run starts the scheduler, listens for queue notifications, wakes workers
// when new work may be available, and blocks until the scheduler shuts down.
func (s *TaskScheduler) Run(ctx context.Context) error {
	runnable, cancellable, err := s.extQueue.Listen(ctx)
	slog.Debug("Started listening to queue")

	if err != nil {
		slog.Error("failed to start queue listener", "err", err)
		return err
	}

	var wg sync.WaitGroup

	shutdown := func() {
		s.cond.L.Lock()
		s.closed = true
		s.cond.Broadcast()
		s.running.Range(func(key any, value any) bool {
			if cancel, ok := value.(context.CancelFunc); ok {
				slog.Debug("Shutdown worker", "event_identifier", key)
				cancel()
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
					slog.Debug("Cancelled job", "event_identifier", sessionEventIdentifier)
					val.(context.CancelFunc)()
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

	slog.Info("WorkerPool started", "capacity", s.config.Workers)
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
			jCtx, cancel := context.WithCancel(ctx)
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
				cancel()
			}()

			cCtx, cSpan := s.tracer.Start(exCtx, "job.claim")
			if job, err = s.extQueue.Claim(cCtx, s.serviceId); err != nil {
				if errors.Is(err, interfaces.ErrJobNotRunnable) {
					slog.Debug("worker failed to claimed job", "worker_id", workerId)
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
			slog.Debug("worker claimed job", "worker_id", workerId, "event_identifier", job.SessionEventIdentifier)

			job.WillRetry = job.Attempts < s.config.MaxAttempts

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
		t := time.NewTicker(s.config.HeartbeatInterval)
		defer t.Stop()

		for {
			select {
			case <-hCtx.Done():
				slog.Debug("Job heartbeat stopped", "event_identifier", job.SessionEventIdentifier)
				return
			case <-t.C:
				if err := s.extQueue.Heartbeat(hCtx, job.SessionEventIdentifier); err != nil {
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

	slog.Debug("Process job", "event_identifier", job.SessionEventIdentifier)
	err := s.handler(ctx, job)
	cancel()

	duration := float64(time.Since(startedAt)) / float64(time.Millisecond)

	if ctx.Err() != nil {
		span.SetAttributes(attribute.String("job.result", "cancelled"))
		span.AddEvent("job.cancelled")
		slog.Debug("User cancelled job, skipping status update", "event_identifier", job.SessionEventIdentifier, "success", "-")

		s.metrics.recordJob(ctx, ResultCancelled, "", duration)
		return
	}

	if err != nil {
		maxAttempReached := job.Attempts >= s.config.MaxAttempts

		span.SetAttributes(attribute.String("job.result", "failed"))
		span.SetAttributes(attribute.Bool("job.retry", !maxAttempReached))

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		result := ResultFailed
		if !maxAttempReached {
			result = ResultRetry
		}

		s.metrics.recordJob(ctx, result, errorType(err), duration)

		if maxAttempReached {
			telemetry.SpanErr(ctx, s.tracer, "job.fail", func(ctx context.Context) error {
				return s.extQueue.Fail(ctx, job.SessionEventIdentifier, err)
			})
		} else {
			telemetry.SpanErr(ctx, s.tracer, "job.retry", func(ctx context.Context) error {
				return s.extQueue.Retry(ctx, job.SessionEventIdentifier, err)
			})
		}

		slog.Debug("Processed job", "event_identifier", job.SessionEventIdentifier, "success", "false", "cause", err)
		return
	}

	slog.Debug("Processed job", "event_identifier", job.SessionEventIdentifier, "success", "true")

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
