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

package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	domain_service "github.com/workdock-dev/engine/domain/service"
	"github.com/workdock-dev/engine/domain/ports"
	"github.com/workdock-dev/engine/domain/telemetry"
	"github.com/workdock-dev/engine/domain/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Metrics struct {
	SessionCount      metric.Int64Counter
	TokenUsage        metric.Int64Counter
	CostUsage         metric.Float64Counter
	ToolDuration      metric.Float64Histogram
	ToolCount         metric.Int64Counter
	CacheCount        metric.Int64Counter
	SessionDuration   metric.Float64Histogram
	MessageCount      metric.Int64Counter
	SessionTokenTotal metric.Int64Histogram
	SessionCostTotal  metric.Float64Histogram
	ModelUsage        metric.Int64Counter
	RetryCount        metric.Int64Counter
}

type OpenCodeOutput struct {
	parts             ports.ForHarnessParts
	linearAccessToken string
	stdout            <-chan string
	stderr            <-chan string
	sessionId         string

	livenessPolicy *domain_service.LivenessPolicy
	onUnhealthy    func()

	lastEvent atomic.Int64
	done      chan struct{}

	model    string
	provider string
	tracer   trace.Tracer
	metrics  *Metrics

	sessionInputTokens         int64
	sessionOutputTokens        int64
	sessionReasoningTokens     int64
	sessionCacheReadTokens     int64
	sessionCacheCreationTokens int64
	sessionCost                float64

	toolStarts map[string]int64

	stderrError error
}

func NewOpenCodeOutput(
	parts ports.ForHarnessParts,
	provider,
	model,
	linearAccessToken,
	sessionId string,
	stdout, stderr <-chan string,
	livenessTimeout time.Duration,
	maxMisses int,
	onUnhealthy func(),
) (*OpenCodeOutput, error) {
	metrics, err := NewMetrics(otel.Meter("opencode"))

	if err != nil {
		slog.Error("failed to initialize opencode output metrics", "err", err)
		return nil, err
	}

	return &OpenCodeOutput{
		model:             model,
		provider:          provider,
		parts:             parts,
		linearAccessToken: linearAccessToken,
		stdout:            stdout,
		stderr:            stderr,
		sessionId:         sessionId,
		livenessPolicy:    domain_service.NewLivenessPolicy(livenessTimeout, maxMisses),
		onUnhealthy:       onUnhealthy,
		tracer:            otel.Tracer("workdock.gen_ai"),
		metrics:           metrics,
		toolStarts:        make(map[string]int64),
	}, nil
}
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	sessionCount, err := meter.Int64Counter(
		"opencode.session.count",
		metric.WithUnit("{session}"),
		metric.WithDescription("Number of OpenCode sessions created"),
	)
	if err != nil {
		return nil, err
	}

	tokenUsage, err := meter.Int64Counter(
		"opencode.token.usage",
		metric.WithUnit("{token}"),
		metric.WithDescription("Tokens consumed by OpenCode"),
	)
	if err != nil {
		return nil, err
	}

	costUsage, err := meter.Float64Counter(
		"opencode.cost.usage",
		metric.WithUnit("USD"),
		metric.WithDescription("LLM cost incurred by OpenCode"),
	)
	if err != nil {
		return nil, err
	}

	toolDuration, err := meter.Float64Histogram(
		"opencode.tool.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Tool execution duration"),
	)
	if err != nil {
		return nil, err
	}

	toolCount, err := meter.Int64Counter(
		"opencode.tool.count",
		metric.WithUnit("{call}"),
		metric.WithDescription("Tool calls executed"),
	)
	if err != nil {
		return nil, err
	}

	sessionDuration, err := meter.Float64Histogram(
		"opencode.session.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("OpenCode session duration"),
	)
	if err != nil {
		return nil, err
	}

	messageCount, err := meter.Int64Counter(
		"opencode.message.count",
		metric.WithUnit("{message}"),
		metric.WithDescription("Completed assistant messages"),
	)
	if err != nil {
		return nil, err
	}

	sessionTokenTotal, err := meter.Int64Histogram(
		"opencode.session.token.total",
		metric.WithUnit("{token}"),
		metric.WithDescription("Total tokens consumed by a session"),
	)
	if err != nil {
		return nil, err
	}

	sessionCostTotal, err := meter.Float64Histogram(
		"opencode.session.cost.total",
		metric.WithUnit("USD"),
		metric.WithDescription("Total cost of a session"),
	)
	if err != nil {
		return nil, err
	}

	modelUsage, err := meter.Int64Counter(
		"opencode.model.usage",
		metric.WithUnit("{message}"),
		metric.WithDescription("Messages generated by model"),
	)
	if err != nil {
		return nil, err
	}

	retryCount, err := meter.Int64Counter(
		"opencode.retry.count",
		metric.WithUnit("{retry}"),
		metric.WithDescription("API retries"),
	)
	if err != nil {
		return nil, err
	}

	cacheCount, err := meter.Int64Counter(
		"opencode.cache.count",
		metric.WithUnit("{operation}"),
		metric.WithDescription("Cache operations"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		SessionCount:      sessionCount,
		TokenUsage:        tokenUsage,
		CostUsage:         costUsage,
		ToolDuration:      toolDuration,
		ToolCount:         toolCount,
		CacheCount:        cacheCount,
		SessionDuration:   sessionDuration,
		MessageCount:      messageCount,
		SessionTokenTotal: sessionTokenTotal,
		SessionCostTotal:  sessionCostTotal,
		ModelUsage:        modelUsage,
		RetryCount:        retryCount,
	}, nil
}

func (o *OpenCodeOutput) Parse(ctx context.Context) {
	o.metrics.ModelUsage.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("gen_ai.request.model", o.model),
			attribute.String("gen_ai.provider.name", o.provider),
		),
	)
	o.metrics.SessionCount.Add(ctx, 1)
	startedAt := time.Now()

	var pending []byte

	stdout := o.stdout
	stderr := o.stderr

	now := time.Now()
	o.lastEvent.Store(now.UnixNano())
	o.livenessPolicy.OnActivity(now)
	o.done = make(chan struct{})
	defer close(o.done)
	o.startLivenessProbe(ctx)

	ctx, span := o.tracer.Start(ctx, "gen_ai.operation.chat")
	defer span.End()

	var messageSpan trace.Span

	startMessage := func() {
		if messageSpan != nil {
			return
		}

		_, messageSpan = o.tracer.Start(ctx, "opencode.output.message")
	}

	endMessage := func() {
		if messageSpan == nil {
			return
		}

		messageSpan.End()
		messageSpan = nil
	}

	defer endMessage()

	var stdErrBuilder strings.Builder

	for stdout != nil || stderr != nil {
		select {
		case chunk, ok := <-stdout:
			if !ok {
				stdout = nil

				if len(pending) > 0 {
					startMessage()
					o.parseLine(ctx, string(pending))
					pending = nil
					endMessage()
				}

				continue
			}

			now := time.Now()
			o.lastEvent.Store(now.UnixNano())
			o.livenessPolicy.OnActivity(now)

			// We received the first chunk of a new message.
			startMessage()

			pending = append(pending, chunk...)

			for {
				i := bytes.IndexByte(pending, '\n')
				if i == -1 {
					break
				}

				// The complete message has now been received.
				o.parseLine(ctx, string(pending[:i]))
				span.AddEvent("output.line")

				pending = pending[i+1:]

				endMessage()

				// There may already be another complete/partial message
				// in pending, so start measuring the next one.
				if len(pending) > 0 {
					startMessage()
				}
			}

		case chunk, ok := <-stderr:
			if !ok {
				stderr = nil
				continue
			}

			now := time.Now()
			o.lastEvent.Store(now.UnixNano())
			o.livenessPolicy.OnActivity(now)

			if _, err := stdErrBuilder.Write([]byte(chunk)); err != nil {
				slog.Error("failed to write to stderr builder", "err", err, "event_identifier", o.sessionId)
			}

		case <-ctx.Done():
			span.AddEvent("output.cancelled")
			return
		}
	}

	if str := stdErrBuilder.String(); str != "" {
		o.stderrError = fmt.Errorf("%s", str)

		span.AddEvent(
			"output.stderr",
			trace.WithAttributes(
				attribute.String("error", str),
			),
		)

		slog.Error(
			"opencode stderr",
			"event_identifier", o.sessionId,
			"err", str,
		)
	}

	duration := time.Since(startedAt)
	o.metrics.SessionDuration.Record(
		ctx,
		float64(duration.Milliseconds()),
	)
	o.metrics.SessionTokenTotal.Record(
		ctx,
		o.sessionInputTokens+o.sessionOutputTokens+o.sessionReasoningTokens,
	)

	o.metrics.SessionCostTotal.Record(
		ctx,
		o.sessionCost,
	)
}

func (o *OpenCodeOutput) StderrError() error {
	return o.stderrError
}

// startLivenessProbe watches for a stalled harness using the domain LivenessPolicy.
// The policy is a pure state machine; the adapter keeps the ticker and calls
// policy.OnActivity() when output is received, and policy.Check() on each tick.
// When the policy returns Unhealthy, onUnhealthy is invoked. The probe is disabled
// when the policy is not enabled.
func (o *OpenCodeOutput) startLivenessProbe(ctx context.Context) {
	if !o.livenessPolicy.IsEnabled() || o.onUnhealthy == nil {
		slog.Warn("Harness liveness probe disabled", "event_identifier", o.sessionId)
		return
	}

	span := trace.SpanFromContext(ctx)

	go func() {
		ticker := time.NewTicker(o.livenessPolicy.GetTimeout())
		defer ticker.Stop()

		for {
			select {
			case <-o.done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				status := o.livenessPolicy.Check(time.Now())

				span.AddEvent("opencode.liveness", trace.WithAttributes(
					attribute.Int("missed", o.livenessPolicy.MissedCount()),
					attribute.String("status", status.String()),
				))

				if status == domain_service.Missed {
					slog.Warn("harness health check missed",
						"event_identifier", o.sessionId,
						"missed", o.livenessPolicy.MissedCount(),
						"idle_for", time.Since(time.Unix(0, o.lastEvent.Load())).Round(time.Second),
					)
				}

				if status == domain_service.Unhealthy {
					span.AddEvent("opencode.liveness.unhealthy", trace.WithAttributes(
						attribute.Int("missed", o.livenessPolicy.MissedCount()),
					))
					slog.Error("harness declared unhealthy",
						"event_identifier", o.sessionId,
						"missed_checks", o.livenessPolicy.MissedCount(),
					)
					o.onUnhealthy()
					return
				}
			}
		}
	}()
}

func (o *OpenCodeOutput) parseLine(ctx context.Context, line string) {
	if ctx.Err() != nil {
		return
	}

	line = strings.TrimSpace(line)

	if line == "" {
		return
	}

	var e WireEvent

	if err := json.Unmarshal([]byte(line), &e); err != nil {
		slog.Error("failed to unmarshal opencode output",
			"err", err,
			"line", line,
		)
		return
	}

	o.parsePart(ctx, e, line)
}

func (o *OpenCodeOutput) parsePart(ctx context.Context, event WireEvent, rawLine string) {
	m := o.metrics

	partType := event.Type

	if partType == "tool_use" {
		partType = "tool"
	}

	slog.Debug("OpenCode received part", "event_identifier", o.sessionId, "part", partType)

	telemetry.SpanDo(ctx, o.tracer, "gen_ai.response.parse_chunk", func(ctx context.Context) {
		switch partType {
		case "retry":
			m.RetryCount.Add(
				ctx,
				1,
				metric.WithAttributes(
					attribute.String("gen_ai.provider.name", o.provider),
				),
			)
			fallthrough
		case "step_start":
			fallthrough
		case "file":
			fallthrough
		case "subtask":
			fallthrough
		case "snapshot":
			fallthrough
		case "patch":
			fallthrough
		case "agent":
			fallthrough
		case "compaction":
			o.parts.Thought(ctx, "")
		case "reasoning":
			var p ReasoningPart

			if err := json.Unmarshal(event.Part, &p); err != nil {
				slog.Error("unmarshal reasoning", "event_identifier", o.sessionId, "error", err)
				return
			}

			o.parts.Thought(ctx, p.Text)
		case "text":
			var p TextPart

			if err := json.Unmarshal(event.Part, &p); err != nil {
				slog.Error("unmarshal text", "event_identifier", o.sessionId, "error", err)
				return
			}

			m.MessageCount.Add(ctx, 1)
			o.parts.Response(ctx, p.Text)

		case "tool":
			var p ToolPart

			if err := json.Unmarshal(event.Part, &p); err != nil {
				slog.Error("unmarshal tool", "event_identifier", o.sessionId, "error", err)
				return
			}

			o.parseToolPart(ctx, p)

		case "step_finish":
			var p StepFinishPart
			if err := json.Unmarshal(event.Part, &p); err != nil {
				slog.Error("unmarshal step-finish", "event_identifier", o.sessionId, "error", err)
				return
			}

			o.tokenUsageAdd(ctx, "input", int64(p.Tokens.Input))
			o.tokenUsageAdd(ctx, "output", int64(p.Tokens.Output))
			o.tokenUsageAdd(ctx, "reasoning", int64(p.Tokens.Reasoning))
			o.tokenUsageAdd(ctx, "cacheRead", int64(p.Tokens.Cache.Read))
			o.tokenUsageAdd(ctx, "cacheCreation", int64(p.Tokens.Cache.Write))

			if p.Tokens.Cache.Read > 0 {
				m.CacheCount.Add(
					ctx,
					1,
					metric.WithAttributes(
						attribute.String("type", "cacheRead"),
					),
				)
			}

			if p.Tokens.Cache.Write > 0 {
				m.CacheCount.Add(
					ctx,
					1,
					metric.WithAttributes(
						attribute.String("type", "cacheCreation"),
					),
				)
			}

			m.CostUsage.Add(
				ctx,
				p.Cost,
				metric.WithAttributes(
					attribute.String("gen_ai.request.model", o.model),
					attribute.String("gen_ai.provider.name", o.provider),
				),
			)

			o.sessionCost += p.Cost
			o.sessionInputTokens += int64(p.Tokens.Input)
			o.sessionOutputTokens += int64(p.Tokens.Output)
			o.sessionReasoningTokens += int64(p.Tokens.Reasoning)
			o.sessionCacheReadTokens += int64(p.Tokens.Cache.Read)
			o.sessionCacheCreationTokens += int64(p.Tokens.Cache.Write)

			slog.Debug("OpenCoded finished",
				"event_identifier", o.sessionId,
				"reason", p.Reason,
				"reasoning", p.Tokens.Reasoning,
				"tokens_total", p.Tokens.Total,
				"tokens_input", p.Tokens.Input,
				"tokens_output", p.Tokens.Output,
				"cache_read", p.Tokens.Cache.Read,
				"cache_write", p.Tokens.Cache.Write,
			)

			o.parts.Response(ctx, "")
		default:
			slog.Warn("opencode received unexpected part type",
				"event_identifier", o.sessionId,
				"part_type", partType,
			)

			o.parts.Response(ctx, fmt.Sprintf("An unexpected format has been received by the harness:\n\n%s", rawLine))
		}
	}, trace.WithAttributes(
		attribute.String("gen_ai.operation.name", partType),
	))
}

func (o *OpenCodeOutput) tokenUsageAdd(ctx context.Context, typ string, n int64) {
	o.metrics.TokenUsage.Add(
		ctx,
		n,
		metric.WithAttributes(
			attribute.String("type", typ),
			attribute.String("gen_ai.request.model", o.model),
			attribute.String("gen_ai.provider.name", o.provider),
		),
	)
}

func (o *OpenCodeOutput) parseToolPart(ctx context.Context, p ToolPart) {
	if t := p.State.Time; t != nil {
		if t.End != nil && *t.End > 0 {
			start := t.Start

			if start == 0 {
				start = o.toolStarts[p.CallID]
			}

			delete(o.toolStarts, p.CallID)

			if start > 0 {
				o.metrics.ToolDuration.Record(
					ctx,
					float64(*t.End-start),
					metric.WithAttributes(
						attribute.String("tool", p.Tool),
					),
				)
			}

			o.metrics.ToolCount.Add(
				ctx,
				1,
				metric.WithAttributes(
					attribute.String("tool", p.Tool),
					attribute.Bool("success", p.State.Status != "error" && p.State.Error == ""),
				),
			)
		} else if t.Start > 0 {
			o.toolStarts[p.CallID] = t.Start
		}
	}

	var input string
	var output string

	switch p.Tool {
	case "bash":
		input, _ = p.State.Input["command"].(string)
		output = p.State.Output

	case "glob":
		pattern, _ := p.State.Input["pattern"].(string)
		path, _ := p.State.Input["path"].(string)
		if path != "" {
			input = pattern + " in " + path
		} else {
			input = pattern
		}
		output = p.State.Output

	case "read":
		input, _ = p.State.Input["filePath"].(string)
		output = p.State.Output

	case "grep":
		pattern, _ := p.State.Input["pattern"].(string)
		path, _ := p.State.Input["path"].(string)
		if path != "" {
			input = pattern + " in " + path
		} else {
			input = pattern
		}
		output = p.State.Output

	case "webfetch":
		input, _ = p.State.Input["url"].(string)
		output = p.State.Output

	case "websearch":
		input, _ = p.State.Input["query"].(string)
		output = p.State.Output

	case "write":
		input, _ = p.State.Input["filePath"].(string)
		output = p.State.Output

	case "edit":
		input, _ = p.State.Input["filePath"].(string)
		output = p.State.Output

	case "task":
		input, _ = p.State.Input["description"].(string)
		output = p.State.Output

	case "execute":
		input, _ = p.State.Input["command"].(string)
		output = p.State.Output

	case "apply_patch":
		if files, ok := p.State.Input["files"].([]any); ok {
			paths := make([]string, 0, len(files))
			for _, f := range files {
				if m, ok := f.(map[string]any); ok {
					if fp, ok := m["filePath"].(string); ok {
						paths = append(paths, fp)
					}
				}
			}
			input = strings.Join(paths, ", ")
		}
		output = p.State.Output

	case "todowrite":
		if todos, ok := p.State.Input["todos"].([]any); ok {
			items := make([]string, 0, len(todos))
			for _, t := range todos {
				if m, ok := t.(map[string]any); ok {
					if content, ok := m["content"].(string); ok {
						items = append(items, content)
					}
				}
			}
			input = strings.Join(items, ", ")
		}
		output = p.State.Output

	case "question":
		questions := o.parseQuestions(p.State.Input)

		for _, q := range questions {
			options := make([]types.AgentOption, 0, len(q.Options))

			for _, opt := range q.Options {
				options = append(options, types.AgentOption{
					Label:       opt.Label,
					Description: opt.Description,
				})
			}

			o.parts.Elicitation(ctx, types.AgentElicitation{
				Question: q.Question,
				Multiple: q.Multiple,
				Options:  options,
			})
		}

		return
	case "skill":
		input, _ = p.State.Input["name"].(string)
		output = p.State.Output

	default:
		input = fmt.Sprintf("%v", p.State.Input)
		output = p.State.Output
	}

	o.parts.Action(ctx, types.AgentAction{
		Name:   p.Tool,
		Input:  input,
		Output: output,
	})
}

func (o *OpenCodeOutput) parseQuestions(input map[string]any) []QuestionInfo {
	questionsRaw, ok := input["questions"].([]any)

	if !ok {
		return nil
	}

	var questions []QuestionInfo

	for _, q := range questionsRaw {
		m, ok := q.(map[string]any)

		if !ok {
			continue
		}

		info := QuestionInfo{}
		info.Question, _ = m["question"].(string)
		info.Header, _ = m["header"].(string)

		if mult, ok := m["multiple"].(bool); ok {
			info.Multiple = mult
		}

		if opts, ok := m["options"].([]any); ok {
			for _, o := range opts {
				om, ok := o.(map[string]any)
				if !ok {
					continue
				}
				opt := QuestionOption{}
				opt.Label, _ = om["label"].(string)
				opt.Description, _ = om["description"].(string)
				info.Options = append(info.Options, opt)
			}
		}

		questions = append(questions, info)
	}

	return questions
}
