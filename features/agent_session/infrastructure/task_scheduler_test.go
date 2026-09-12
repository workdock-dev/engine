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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type TaskSchedulerSuite struct {
	suite.Suite
}

func TestTaskSchedulerSuite(t *testing.T) {
	suite.Run(t, new(TaskSchedulerSuite))
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func (s *TaskSchedulerSuite) TestNewTaskScheduler_DefaultConfig() {
	q := &mockQueue{runnable: make(chan struct{}, 1), cancellable: make(chan string, 1)}
	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{}, handler)
	s.NoError(err)
	s.NotNil(sched)
	s.Equal(DefaultWorkers, sched.config.Workers)
	s.Equal(DefaultMaxAttempts, sched.config.MaxAttempts)
}

func (s *TaskSchedulerSuite) TestNewTaskScheduler_CustomConfig() {
	q := &mockQueue{runnable: make(chan struct{}, 1), cancellable: make(chan string, 1)}
	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 8, MaxAttempts: 5}, handler)
	s.NoError(err)
	s.Equal(8, sched.config.Workers)
	s.Equal(5, sched.config.MaxAttempts)
}

func (s *TaskSchedulerSuite) TestNewTaskScheduler_ServiceIdUnique() {
	q := &mockQueue{runnable: make(chan struct{}, 1), cancellable: make(chan string, 1)}
	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	s1, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{}, handler)
	s2, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{}, handler)
	s.NotEqual(s1.serviceId, s2.serviceId)
}

// ---------------------------------------------------------------------------
// Run tests
// ---------------------------------------------------------------------------

func (s *TaskSchedulerSuite) TestRun_ListenError() {
	q := &mockQueue{
		listenErr:   errors.New("listen failed"),
		runnable:    make(chan struct{}, 1),
		cancellable: make(chan string, 1),
	}
	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)
	s.Require().NoError(err)

	err = sched.Run(context.Background())
	s.Error(err)
	s.Contains(err.Error(), "listen failed")
}

func (s *TaskSchedulerSuite) TestRun_ShutdownCleanly() {
	q := newMockQueueChannels(1)
	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)
	s.Require().NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		s.NoError(err)
	case <-time.After(2 * time.Second):
		s.Fail("Run did not return in time")
	}
}

func (s *TaskSchedulerSuite) TestRun_ClaimErrorJobNotRunnable() {
	q := newMockQueueChannels(1)
	q.claimErr = interfaces.ErrJobNotRunnable

	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_ClaimReturnsNil() {
	q := newMockQueueChannels(1)
	q.claimJob = nil

	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		s.Fail("handler should not be called")
		return types.EventJobStatus_Succeeded, nil
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.Fail("Run did not return in time")
	}
}

func (s *TaskSchedulerSuite) TestRun_ExecutionSuccess() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-1",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerCalled := make(chan struct{})
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		close(handlerCalled)
		return types.EventJobStatus_Succeeded, nil
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()

	select {
	case <-handlerCalled:
	case <-time.After(2 * time.Second):
		s.Fail("handler was not called in time")
	}

	q.waitForTerminal()
	q.assertCompleted(s.T(), "evt-1", types.EventJobStatus_Succeeded)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_ExecutionAwaitingAction() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-awaiting-action",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerCalled := make(chan struct{})
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		close(handlerCalled)
		return types.EventJobStatus_AwaitingAction, nil
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()

	select {
	case <-handlerCalled:
	case <-time.After(2 * time.Second):
		s.Fail("handler was not called in time")
	}

	q.waitForTerminal()
	q.assertCompleted(s.T(), "evt-awaiting-action", types.EventJobStatus_AwaitingAction)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_ExecutionRetry() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-retry",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerErr := errors.New("transient failure")
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		return types.EventJobStatus_Failed, handlerErr
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 3}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	q.waitForTerminal()
	q.assertRetried(s.T(), "evt-retry", handlerErr)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_ExecutionFail() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-fail",
		QueuedBy:               "sess-1",
		Attempts:               2,
	}
	q.claimJob = job

	handlerErr := errors.New("permanent failure")
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		return types.EventJobStatus_Failed, handlerErr
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	q.waitForTerminal()
	q.assertFailed(s.T(), "evt-fail", handlerErr)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_HandlerCancelled() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-cancel",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		close(handlerStarted)
		<-ctx.Done()
		close(handlerDone)
		return types.EventJobStatus_Failed, ctx.Err()
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not start in time")
	}

	cancel()

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not finish in time")
	}

	q.mu.Lock()
	completed := q.completedIds
	failed := q.failedIds
	q.mu.Unlock()

	s.Empty(completed)
	s.Empty(failed)

	// The parent context was cancelled (e.g. scheduler shutdown), so the job
	// must be released back to the queue for another attempt.
	q.waitForTerminal()
	q.assertRetried(s.T(), "evt-cancel", errShutdownRequeue)

	<-done
}

func (s *TaskSchedulerSuite) TestRun_CompleteError() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-complete-err",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job
	q.completeErr = errors.New("complete failed")

	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		return types.EventJobStatus_Succeeded, nil
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	q.waitForTerminal()
	q.assertCompleted(s.T(), "evt-complete-err", types.EventJobStatus_Succeeded)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_CancellationChannel() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-cancel-ch",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		<-ctx.Done()
		return types.EventJobStatus_Failed, ctx.Err()
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	time.Sleep(100 * time.Millisecond)

	q.sendCancel("evt-cancel-ch")

	// A job cancelled by the user is finalized once its teardown is done:
	// Complete transitions the job from 'cancelling' to 'cancelled' so the
	// database trigger releases the jobs the session queued while the
	// cancellation was in progress.
	q.waitForTerminal()

	q.assertCompleted(s.T(), "evt-cancel-ch", types.EventJobStatus_Cancelled)

	q.mu.Lock()
	failed := q.failedIds
	retried := q.retriedIds
	lastCompleteCtx := q.lastCompleteCtx
	q.mu.Unlock()

	// The finalization runs on a non-cancelled context: the parent context is
	// cancelled by the time Complete is called, and the update must still
	// reach the database.
	s.Require().NotNil(lastCompleteCtx)
	s.NoError(lastCompleteCtx.Err())
	s.Empty(failed)
	s.Empty(retried)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_ClaimErrorOther() {
	q := newMockQueueChannels(1)
	q.claimErr = errors.New("some other error")

	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		s.Fail("handler should not be called")
		return types.EventJobStatus_Succeeded, nil
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_ShutdownCancelsRunningJobs() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-shutdown-cancel",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerStarted := make(chan struct{})
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		close(handlerStarted)
		<-ctx.Done()
		return types.EventJobStatus_Failed, ctx.Err()
	}

	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not start in time")
	}

	close(q.runnable)

	select {
	case err := <-done:
		s.NoError(err)
	case <-time.After(2 * time.Second):
		s.Fail("Run did not return in time")
	}
}

func (s *TaskSchedulerSuite) TestRun_RunnableChannelClosed() {
	runnable := make(chan struct{})
	cancellable := make(chan string)
	q := &mockQueue{
		runnable:    runnable,
		cancellable: cancellable,
	}

	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}
	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	close(runnable)

	select {
	case err := <-done:
		s.NoError(err)
	case <-time.After(2 * time.Second):
		s.Fail("Run did not return in time")
	}
}

// NOTE: The heartbeat tests were removed. The scheduler hardcodes
// DefaultHeartbeatInterval (one minute), so a heartbeat cannot be observed
// within a reasonable test window anymore.

func (s *TaskSchedulerSuite) TestRun_CancellableChannelClosed() {
	runnable := make(chan struct{})
	cancellable := make(chan string)
	q := &mockQueue{
		runnable:    runnable,
		cancellable: cancellable,
	}

	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}
	sched, _ := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1}, handler)

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	close(cancellable)

	select {
	case err := <-done:
		s.NoError(err)
	case <-time.After(2 * time.Second):
		s.Fail("Run did not return in time")
	}
}

// ---------------------------------------------------------------------------
// Metrics initialization and heartbeat tests
// ---------------------------------------------------------------------------

type failingMeterProvider struct {
	metric.MeterProvider
}

func (p *failingMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return &failingMeter{}
}

type failingMeter struct {
	metric.Meter
}

// Every instrument creation fails. OTel's global meter replays previously
// registered instruments through setDelegate, which handles creation errors
// gracefully via its error handler, so returning an error everywhere is safe.

func (m *failingMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return nil, errMeterUnavailable
}

func (m *failingMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return nil, errMeterUnavailable
}

var errMeterUnavailable = errors.New("meter unavailable")

func (s *TaskSchedulerSuite) TestNewTaskScheduler_MetricsInitError() {
	// The global default meter provider cannot be restored after delegating to
	// a failing one (otel's delegation happens once), so restore to a fresh
	// no-op provider to keep the rest of the suite isolated.
	noopProvider := noop.NewMeterProvider()
	otel.SetMeterProvider(&failingMeterProvider{})
	defer otel.SetMeterProvider(noopProvider)

	q := &mockQueue{runnable: make(chan struct{}, 1), cancellable: make(chan string, 1)}
	handler := func(ctx context.Context, job *types.EventJob) (types.EventJobStatus, error) {
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{}, handler)

	s.Nil(sched)
	s.Error(err)
	s.ErrorContains(err, "meter unavailable")
}

// withFastHeartbeat shrinks the scheduler's heartbeat interval to 50ms so
// heartbeats can be observed within a test window.
func withFastHeartbeat(sched *TaskScheduler) {
	sched.heartbeatInterval = 50 * time.Millisecond
}

func (s *TaskSchedulerSuite) TestRun_HeartbeatSuccess() {
	q := newMockQueueChannels(1)
	job := &types.EventJob{
		SessionEventIdentifier: "evt-heartbeat",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerReleased := make(chan struct{})
	handlerStarted := make(chan struct{})
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		close(handlerStarted)
		// Stay running long enough for the 50ms heartbeat ticker to fire.
		time.Sleep(200 * time.Millisecond)
		close(handlerReleased)
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)
	s.Require().NoError(err)
	withFastHeartbeat(sched)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not start in time")
	}

	select {
	case <-handlerReleased:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not finish in time")
	}

	q.waitForTerminal()

	// Heartbeat failures are tolerated (logged only); a healthy heartbeat
	// must not trigger any terminal state change beyond the completion.
	q.assertCompleted(s.T(), "evt-heartbeat", types.EventJobStatus_Succeeded)

	cancel()
	<-done
}

func (s *TaskSchedulerSuite) TestRun_HeartbeatErrorIsTolerated() {
	q := newMockQueueChannels(1)
	q.heartbeatErr = errors.New("heartbeat failed")
	job := &types.EventJob{
		SessionEventIdentifier: "evt-heartbeat-err",
		QueuedBy:               "sess-1",
		Attempts:               0,
	}
	q.claimJob = job

	handlerStarted := make(chan struct{})
	handlerReleased := make(chan struct{})
	handler := func(ctx context.Context, j *types.EventJob) (types.EventJobStatus, error) {
		q.claimJob = nil
		close(handlerStarted)
		time.Sleep(200 * time.Millisecond)
		close(handlerReleased)
		return types.EventJobStatus_Succeeded, nil
	}

	sched, err := NewTaskScheduler(q, types.TaskSchedulerConfig{Workers: 1, MaxAttempts: 2}, handler)
	s.Require().NoError(err)
	withFastHeartbeat(sched)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sched.Run(ctx)
	}()

	q.notifyRunnable()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not start in time")
	}

	select {
	case <-handlerReleased:
	case <-time.After(2 * time.Second):
		s.Fail("handler did not finish in time")
	}

	q.waitForTerminal()

	// A failing heartbeat only records a span event: the job still completes.
	q.assertCompleted(s.T(), "evt-heartbeat-err", types.EventJobStatus_Succeeded)
	s.False(q.failed())

	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// Mock queue for scheduler tests
// ---------------------------------------------------------------------------

type mockQueue struct {
	runnable     chan struct{}
	cancellable  chan string
	claimJob     *types.EventJob
	claimErr     error
	completeErr  error
	heartbeatErr error

	mu                sync.Mutex
	completedIds      []string
	completedStatuses []types.EventJobStatus
	lastCompleteCtx   context.Context
	retriedIds        []string
	retryCauses       []error
	failedIds         []string
	failCauses        []error

	terminalCh chan struct{}
	listenErr  error
}

func newMockQueueChannels(workers int) *mockQueue {
	return &mockQueue{
		runnable:    make(chan struct{}, workers*10),
		cancellable: make(chan string, workers*10),
		terminalCh:  make(chan struct{}, 1),
	}
}

func (m *mockQueue) notifyRunnable() {
	m.runnable <- struct{}{}
}

func (m *mockQueue) sendCancel(id string) {
	m.cancellable <- id
}

func (m *mockQueue) signalTerminal() {
	select {
	case m.terminalCh <- struct{}{}:
	default:
	}
}

func (m *mockQueue) waitForTerminal() {
	select {
	case <-m.terminalCh:
	case <-time.After(5 * time.Second):
	}
}

func (m *mockQueue) assertCompleted(t *testing.T, id string, expectedStatus types.EventJobStatus) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, cid := range m.completedIds {
		if cid == id {
			if m.completedStatuses[i] == expectedStatus {
				return
			}
			t.Errorf("expected Complete to be called with status %q, got %q", expectedStatus, m.completedStatuses[i])
			return
		}
	}
	t.Errorf("expected Complete to be called with %q, completed: %v", id, m.completedIds)
}

func (m *mockQueue) assertRetried(t *testing.T, id string, cause error) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, rid := range m.retriedIds {
		if rid == id && errors.Is(m.retryCauses[i], cause) {
			return
		}
	}
	t.Errorf("expected Retry to be called with %q, retried: %v", id, m.retriedIds)
}

func (m *mockQueue) assertFailed(t *testing.T, id string, cause error) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, fid := range m.failedIds {
		if fid == id && errors.Is(m.failCauses[i], cause) {
			return
		}
	}
	t.Errorf("expected Fail to be called with %q, failed: %v", id, m.failedIds)
}

func (m *mockQueue) Listen(ctx context.Context) (<-chan struct{}, <-chan string, error) {
	if m.listenErr != nil {
		return nil, nil, m.listenErr
	}
	return m.runnable, m.cancellable, nil
}

func (m *mockQueue) Claim(ctx context.Context, owner string, nextAttemptAt time.Time) (*types.EventJob, error) {
	if m.claimErr != nil {
		return nil, m.claimErr
	}
	return m.claimJob, nil
}

func (m *mockQueue) Heartbeat(ctx context.Context, id string, leaseDuration time.Duration) error {
	return m.heartbeatErr
}

// failed reports whether the Fail method was ever invoked.
func (m *mockQueue) failed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.failedIds) > 0
}

func (m *mockQueue) Complete(ctx context.Context, id string, status types.EventJobStatus) error {
	m.mu.Lock()
	m.completedIds = append(m.completedIds, id)
	m.completedStatuses = append(m.completedStatuses, status)
	m.lastCompleteCtx = ctx
	err := m.completeErr
	m.mu.Unlock()
	m.signalTerminal()
	return err
}

func (m *mockQueue) Retry(ctx context.Context, id string, cause error, retryGracePeriod time.Duration) error {
	m.mu.Lock()
	m.retriedIds = append(m.retriedIds, id)
	m.retryCauses = append(m.retryCauses, cause)
	m.mu.Unlock()
	m.signalTerminal()
	return nil
}

func (m *mockQueue) Fail(ctx context.Context, id string, cause error) error {
	m.mu.Lock()
	m.failedIds = append(m.failedIds, id)
	m.failCauses = append(m.failCauses, cause)
	m.mu.Unlock()
	m.signalTerminal()
	return nil
}

var _ interfaces.Queue = (*mockQueue)(nil)
