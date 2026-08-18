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

package otlp_client

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	Endpoint string            `yaml:"endpoint"`
	Insecure bool              `yaml:"insecure"`
	Headers  map[string]string `yaml:"headers"`
	Slog     *struct {
		Level  string `yaml:"level"`
		Source bool   `yaml:"source"`
	} `yaml:"slog"`
}

func New(ctx context.Context, cfg Config, serviceName string) (func(context.Context), error) {
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithHeaders(cfg.Headers),
	}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	} else {
		traceOpts = append(traceOpts, otlptracehttp.WithTLSClientConfig(&tls.Config{}))
	}
	exporterTracer, err := otlptracehttp.New(ctx, traceOpts...)

	if err != nil {
		slog.Error("failed to create otlp http trace", "err", err)
		return nil, err
	}

	logOpts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(cfg.Endpoint),
		otlploghttp.WithHeaders(cfg.Headers),
	}
	if cfg.Insecure {
		logOpts = append(logOpts, otlploghttp.WithInsecure())
	} else {
		logOpts = append(logOpts, otlploghttp.WithTLSClientConfig(&tls.Config{}))
	}
	exporterLogger, err := otlploghttp.New(ctx, logOpts...)

	if err != nil {
		slog.Error("failed to create otlp http log", "err", err)
		return nil, err
	}

	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.Endpoint),
		otlpmetrichttp.WithHeaders(cfg.Headers),
		otlpmetrichttp.WithTemporalitySelector(sdkmetric.DeltaTemporalitySelector),
	}
	if cfg.Insecure {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	} else {
		metricOpts = append(metricOpts, otlpmetrichttp.WithTLSClientConfig(&tls.Config{}))
	}
	exporterMetric, err := otlpmetrichttp.New(ctx, metricOpts...)

	if err != nil {
		slog.Error("failed to create otlp http metric", "err", err)
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		slog.Error("failed to resolve hostname", "err", err)
		hostname = "unknown"
	}

	res, err := resource.New(
		ctx,
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.instance.id", fmt.Sprintf("%s-%d", hostname, os.Getpid())),
		),
	)

	if err != nil {
		slog.Error("failed to create otlp resource", "err", err)
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporterTracer),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporterMetric, sdkmetric.WithProducer(runtimemetrics.NewProducer()))),
		sdkmetric.WithResource(res),
	)

	if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(meterProvider)); err != nil {
		slog.Error("failed to start runtime metrics", "err", err)
		return nil, err
	}

	otel.SetMeterProvider(meterProvider)

	var logProvider *log.LoggerProvider

	if cfg.Slog != nil {
		processor := log.NewBatchProcessor(exporterLogger)
		logProvider = log.NewLoggerProvider(
			log.WithProcessor(processor),
			log.WithResource(res),
		)

		options := []otelslog.Option{
			otelslog.WithLoggerProvider(logProvider),
		}

		if cfg.Slog.Source {
			options = append(options, otelslog.WithSource(true))
		}

		handler := otelslog.NewHandler(serviceName, options...)
		slog.SetLogLoggerLevel(parseSlogLevel(cfg.Slog.Level))
		slog.SetDefault(slog.New(handler))
	}

	return func(ctx context.Context) {
		if logProvider != nil {
			if err := logProvider.Shutdown(ctx); err != nil {
				slog.Error("failed to shutdown otpl logger provider", "err", err)
			}
		}

		if err := meterProvider.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown otpl metric provider", "err", err)
		}

		if err := tracerProvider.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown otpl tracer provider", "err", err)
		}
	}, nil
}

func parseSlogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}
