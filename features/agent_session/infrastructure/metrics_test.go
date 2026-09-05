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
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/features/agent_session/types"
	"github.com/workdock-dev/engine/shared"
	"go.opentelemetry.io/otel/metric"
)

// --- mock meter: fails the Nth instrument registration ---

type mockMeter struct {
	metric.Meter

	failAt  int // 1-based instrument creation index to fail; 0 never fails
	created int
	instErr error
	gaugeCb []func(ctx context.Context, o metric.Int64Observer) error
}

func (m *mockMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return &mockInt64Counter{}, nil
}

func (m *mockMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	cfg := metric.NewInt64ObservableGaugeConfig(opts...)
	for _, cb := range cfg.Callbacks() {
		m.gaugeCb = append(m.gaugeCb, func(ctx context.Context, o metric.Int64Observer) error {
			return cb(ctx, o)
		})
	}
	return &mockObservableGauge{}, nil
}

func (m *mockMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return &mockInt64Counter{}, nil
}

func (m *mockMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return &mockFloat64Histogram{}, nil
}

func (m *mockMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	m.created++
	if m.created == m.failAt {
		return nil, m.instErr
	}
	return nil, nil
}

func (m *mockMeter) RegisterCallback(f metric.Callback, i ...metric.Observable) (metric.Registration, error) {
	return nil, nil
}

func (m *mockMeter) UnregisterCallback(r metric.Registration) error {
	return nil
}

// --- mock instruments ---

type mockInt64Counter struct {
	metric.Int64Counter
	metric.Int64UpDownCounter
}

func (c *mockInt64Counter) Add(ctx context.Context, incr int64, opts ...metric.AddOption) {}
func (c *mockInt64Counter) Enabled(ctx context.Context) bool                              { return false }

type mockObservableGauge struct {
	metric.Int64ObservableGauge
}

type mockFloat64Histogram struct {
	metric.Float64Histogram
}

func (h *mockFloat64Histogram) Record(ctx context.Context, x float64, opts ...metric.RecordOption) {}
func (h *mockFloat64Histogram) Enabled(ctx context.Context) bool                                   { return false }

// --- mock observer that captures observed values ---

type mockInt64Observer struct {
	metric.Int64Observer

	observed []int64
}

func (o *mockInt64Observer) Observe(value int64, opts ...metric.ObserveOption) {
	o.observed = append(o.observed, value)
}

type MetricsSuite struct {
	suite.Suite
}

func TestMetricsSuite(t *testing.T) {
	suite.Run(t, new(MetricsSuite))
}

func (s *MetricsSuite) TestNewMetrics_Success() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 4}}

	metrics, err := NewMetrics(meter, sched)

	s.Require().NoError(err)
	s.NotNil(metrics)
	s.Equal(5, meter.created)
}

func (s *MetricsSuite) TestNewMetrics_InstrumentErrors() {
	tests := []struct {
		name   string
		failAt int
	}{
		{name: "total workers gauge", failAt: 1},
		{name: "busy workers gauge", failAt: 2},
		{name: "idle workers gauge", failAt: 3},
		{name: "job duration histogram", failAt: 4},
		{name: "job count counter", failAt: 5},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			meter := &mockMeter{failAt: tt.failAt, instErr: errors.New("instrument failed")}
			sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 2}}

			metrics, err := NewMetrics(meter, sched)

			s.Nil(metrics)
			s.Error(err)
			s.ErrorContains(err, "instrument failed")
		})
	}
}

func (s *MetricsSuite) TestTotalWorkersGauge_ObservesConfiguredWorkers() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 7}}

	metrics, err := NewMetrics(meter, sched)
	s.Require().NoError(err)
	s.NotNil(metrics.TotalWorkers)

	// The first gauge registered is the total workers gauge.
	observer := &mockInt64Observer{}
	s.Require().Len(meter.gaugeCb, 3)
	s.Require().NoError(meter.gaugeCb[0](context.Background(), observer))
	s.Equal([]int64{7}, observer.observed)
}

func (s *MetricsSuite) TestBusyWorkersGauge_ObservesBusyCount() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 4}}
	sched.busyWorkers.Store(2)

	metrics, err := NewMetrics(meter, sched)
	s.Require().NoError(err)
	s.NotNil(metrics.BusyWorkers)

	observer := &mockInt64Observer{}
	s.Require().Len(meter.gaugeCb, 3)
	s.Require().NoError(meter.gaugeCb[1](context.Background(), observer))
	s.Equal([]int64{2}, observer.observed)
}

func (s *MetricsSuite) TestIdleWorkersGauge_ObservesIdleCount() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 4}}
	sched.busyWorkers.Store(3)

	metrics, err := NewMetrics(meter, sched)
	s.Require().NoError(err)
	s.NotNil(metrics.IdleWorkers)

	observer := &mockInt64Observer{}
	s.Require().Len(meter.gaugeCb, 3)
	s.Require().NoError(meter.gaugeCb[2](context.Background(), observer))
	s.Equal([]int64{1}, observer.observed)
}

func (s *MetricsSuite) TestIdleWorkersGauge_NeverNegative() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 2}}
	sched.busyWorkers.Store(5)

	_, err := NewMetrics(meter, sched)
	s.Require().NoError(err)

	observer := &mockInt64Observer{}
	s.Require().Len(meter.gaugeCb, 3)
	s.Require().NoError(meter.gaugeCb[2](context.Background(), observer))
	s.Equal([]int64{0}, observer.observed)
}

func (s *MetricsSuite) TestRecordJob_WithoutErrorType() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 1}}

	metrics, err := NewMetrics(meter, sched)
	s.Require().NoError(err)

	s.NotPanics(func() {
		metrics.recordJob(context.Background(), ResultSucceeded, "", 12.5)
	})
}

func (s *MetricsSuite) TestRecordJob_WithErrorType() {
	meter := &mockMeter{}
	sched := &TaskScheduler{config: types.TaskSchedulerConfig{Workers: 1}}

	metrics, err := NewMetrics(meter, sched)
	s.Require().NoError(err)

	s.NotPanics(func() {
		metrics.recordJob(context.Background(), ResultFailed, "ErrBadRequest", 3.0)
	})
}

func (s *MetricsSuite) TestErrorType_Nil() {
	s.Empty(errorType(nil))
}

func (s *MetricsSuite) TestErrorType_KnownSentinels() {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "bad request", err: shared.ErrBadRequest, expected: "ErrBadRequest"},
		{name: "unauthorized", err: shared.ErrUnAuthorized, expected: "ErrUnAuthorized"},
		{name: "forbidden", err: shared.ErrForbidden, expected: "ErrForbidden"},
		{name: "internal server error", err: shared.ErrInternalServerError, expected: "ErrInternalServerError"},
		{name: "linear token expired", err: shared.ErrLinearTokenExpired, expected: "ErrLinearTokenExpired"},
		{name: "linear token refresh failed", err: shared.ErrLinearTokenRefreshFailed, expected: "ErrLinearTokenRefreshFailed"},
		{name: "github installation unavailable", err: shared.ErrGitHubInstallationUnavailable, expected: "ErrGitHubInstallationUnavailable"},
		{name: "git connection re-requested", err: shared.ErrGitConnectionReRequested, expected: "ErrGitHubConnectionReRequested"},
		{name: "harness unhealthy", err: shared.ErrHarnessUnhealthy, expected: "ErrHarnessUnhealthy"},
		{name: "job not runnable", err: interfaces.ErrJobNotRunnable, expected: "ErrJobNotRunnable"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.expected, errorType(tt.err))
		})
	}
}

func (s *MetricsSuite) TestErrorType_WrappedSentinel() {
	wrapped := fmt.Errorf("pipeline: %w", shared.ErrBadRequest)

	s.Equal("ErrBadRequest", errorType(wrapped))
}

func (s *MetricsSuite) TestErrorType_NamedErrorType() {
	type validationError struct{ error }

	err := validationError{errors.New("invalid")}

	s.Equal("validationError", errorType(err))
}

func (s *MetricsSuite) TestErrorType_AnonymousErrorType() {
	err := errors.New("plain error") // *errorString has an empty type name

	s.Equal("*errors.errorString", errorType(err))
}
