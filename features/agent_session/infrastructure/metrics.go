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
	"reflect"

	"github.com/workdock-dev/engine/features/agent_session/interfaces"
	"github.com/workdock-dev/engine/shared"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	ResultSucceeded = "succeeded"
	ResultFailed    = "failed"
	ResultRetry     = "retry"
	ResultCancelled = "cancelled"
)

type SchedulerMetrics struct {
	TotalWorkers metric.Int64ObservableGauge
	BusyWorkers  metric.Int64ObservableGauge
	IdleWorkers  metric.Int64ObservableGauge
	JobDuration  metric.Float64Histogram
	JobCount     metric.Int64Counter
}

func NewMetrics(meter metric.Meter, s *TaskScheduler) (*SchedulerMetrics, error) {
	totalWorkers, err := meter.Int64ObservableGauge(
		"scheduler.worker.total",
		metric.WithUnit("{worker}"),
		metric.WithDescription("Total number of workers in the pool"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			o.Observe(int64(s.config.Workers))
			return nil
		}),
	)

	if err != nil {
		return nil, err
	}

	busyWorkers, err := meter.Int64ObservableGauge(
		"scheduler.worker.busy",
		metric.WithUnit("{worker}"),
		metric.WithDescription("Number of workers currently processing a job"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			o.Observe(s.busyWorkers.Load())
			return nil
		}),
	)

	if err != nil {
		return nil, err
	}

	idleWorkers, err := meter.Int64ObservableGauge(
		"scheduler.worker.idle",
		metric.WithUnit("{worker}"),
		metric.WithDescription("Number of workers idle and waiting for work"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			idle := max(int64(s.config.Workers)-s.busyWorkers.Load(), 0)
			o.Observe(idle)
			return nil
		}),
	)

	if err != nil {
		return nil, err
	}

	jobDuration, err := meter.Float64Histogram(
		"scheduler.job.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Time a worker takes to complete a job"),
		metric.WithExplicitBucketBoundaries(
			1, 5, 10, 25, 50, 100, 250, 500, 1000,
			2000, 5000, 10000, 30000, 60000, 120000, 180000,
			300000, 450000, 600000, 900000, 1200000, 1800000,
		),
	)

	if err != nil {
		return nil, err
	}

	jobCount, err := meter.Int64Counter(
		"scheduler.job.count",
		metric.WithUnit("{job}"),
		metric.WithDescription("Jobs processed by the task scheduler"),
	)

	if err != nil {
		return nil, err
	}

	return &SchedulerMetrics{
		TotalWorkers: totalWorkers,
		BusyWorkers:  busyWorkers,
		IdleWorkers:  idleWorkers,
		JobDuration:  jobDuration,
		JobCount:     jobCount,
	}, nil
}

// recordJob increments the job counter for the terminal outcome and records
// the time the worker took to complete the job.
func (m *SchedulerMetrics) recordJob(ctx context.Context, result, errType string, duration float64) {
	attrs := []attribute.KeyValue{
		attribute.String("job.result", result),
	}

	if errType != "" {
		attrs = append(attrs, attribute.String("job.error_type", errType))
	}

	m.JobCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.JobDuration.Record(ctx, duration)
}

// errorType classifies the concrete cause of a failed job. Sentinels defined
// in the domain take precedence so wrapped errors collapse to their canonical
// name; otherwise the error's concrete Go type is used.
func errorType(err error) string {
	if err == nil {
		return ""
	}

	known := []struct {
		name string
		err  error
	}{
		{"ErrBadRequest", shared.ErrBadRequest},
		{"ErrUnAuthorized", shared.ErrUnAuthorized},
		{"ErrForbidden", shared.ErrForbidden},
		{"ErrInternalServerError", shared.ErrInternalServerError},
		{"ErrLinearTokenExpired", shared.ErrLinearTokenExpired},
		{"ErrLinearTokenRefreshFailed", shared.ErrLinearTokenRefreshFailed},
		{"ErrGitHubInstallationUnavailable", shared.ErrGitHubInstallationUnavailable},
		{"ErrGitHubConnectionReRequested", shared.ErrGitConnectionReRequested},
		{"ErrHarnessUnhealthy", shared.ErrHarnessUnhealthy},
		{"ErrJobNotRunnable", interfaces.ErrJobNotRunnable},
	}

	for _, k := range known {
		if errors.Is(err, k.err) {
			return k.name
		}
	}

	t := reflect.TypeOf(err)

	if t != nil {
		if t.Name() != "" {
			return t.Name()
		}

		return t.String()
	}

	return "unknown"
}
