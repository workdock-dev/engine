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

package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func Span[T any](
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	fn func(context.Context) (T, error),
	opts ...trace.SpanStartOption,
) (T, error) {
	ctx, span := tracer.Start(ctx, name, opts...)
	defer span.End()

	result, err := fn(ctx)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	return result, err
}

func Span1[T any](
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	fn func(context.Context) T,
	opts ...trace.SpanStartOption,
) T {
	ctx, span := tracer.Start(ctx, name, opts...)
	defer span.End()
	r := fn(ctx)
	return r
}

func Span2[T any, K any](
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	fn func(context.Context) (T, K, error),
	opts ...trace.SpanStartOption,
) (T, K, error) {
	ctx, span := tracer.Start(ctx, name, opts...)
	defer span.End()

	result, result2, err := fn(ctx)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	return result, result2, err
}

func SpanErr(
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	fn func(context.Context) error,
	opts ...trace.SpanStartOption,
) error {
	ctx, span := tracer.Start(ctx, name, opts...)
	defer span.End()

	err := fn(ctx)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	return err
}

func SpanDo(
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	fn func(context.Context),
	opts ...trace.SpanStartOption,
) {
	ctx, span := tracer.Start(ctx, name, opts...)
	defer span.End()

	fn(ctx)
}
