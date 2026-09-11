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

// Package types models the events emitted by the pi JSON event stream mode
// (pi --mode json). Each stdout line is a single JSON object: the first line
// is the session header followed by agent session events as they occur.
// https://pi.dev/docs/latest/json
package types

// MessageContent is one of the content blocks of an assistant message.
type MessageContent struct {
	Type string `json:"type"`

	Text      string         `json:"text,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	Id        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// Cost is the provider-reported cost breakdown.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Usage is the cumulative provider-reported usage.
type Usage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	Reasoning   int64 `json:"reasoning,omitempty"`
	TotalTokens int64 `json:"totalTokens"`
	Cost        Cost  `json:"cost"`
}

// AssistantMessage is the assistant message carried by message events.
type AssistantMessage struct {
	Role         string           `json:"role"`
	Content      []MessageContent `json:"content"`
	Api          string           `json:"api"`
	Provider     string           `json:"provider"`
	Model        string           `json:"model"`
	Usage        Usage            `json:"usage"`
	StopReason   string           `json:"stopReason"`
	ErrorMessage string           `json:"errorMessage,omitempty"`
	Timestamp    int64            `json:"timestamp"`
}

// ResultContent is one of the content blocks of a tool result.
type ResultContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is the result payload carried by tool_execution_end.
type ToolResult struct {
	Content []ResultContent `json:"content"`
	Details any             `json:"details,omitempty"`
	Usage   *Usage          `json:"usage,omitempty"`
}

// ToolResultMessage is the tool result message carried by turn_end.
type ToolResultMessage struct {
	Role       string          `json:"role"`
	ToolCallId string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Content    []ResultContent `json:"content"`
	IsError    bool            `json:"isError"`
}

// AssistantMessageEvent is the streaming sub-event of a message_update.
// Streaming message updates omit cumulative snapshots.
type AssistantMessageEvent struct {
	Type string `json:"type"`

	ContentIndex int `json:"contentIndex,omitempty"`

	Delta string `json:"delta,omitempty"`

	Content string `json:"content,omitempty"`

	Id       string           `json:"id,omitempty"`
	ToolName string           `json:"toolName,omitempty"`
	ToolCall *ToolCallContent `json:"toolCall,omitempty"`

	Reason string            `json:"reason,omitempty"`
	Error  *AssistantMessage `json:"error,omitempty"`
}

// ToolCallContent is the tool call carried by toolcall_end.
type ToolCallContent struct {
	Type      string         `json:"type"`
	Id        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// WireEvent is a single JSON line of the event stream.
type WireEvent struct {
	Type                  string                 `json:"type"`
	Message               *AssistantMessage      `json:"message,omitempty"`
	Messages              []AssistantMessage     `json:"messages,omitempty"`
	ToolResults           []ToolResultMessage    `json:"toolResults,omitempty"`
	Usage                 *Usage                 `json:"usage,omitempty"`
	AssistantMessageEvent *AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// Tool execution lifecycle
	ToolCallId    string      `json:"toolCallId,omitempty"`
	ToolName      string      `json:"toolName,omitempty"`
	Args          any         `json:"args,omitempty"`
	PartialResult any         `json:"partialResult,omitempty"`
	Result        *ToolResult `json:"result,omitempty"`
	IsError       bool        `json:"isError,omitempty"`

	// Agent session extensions
	WillRetry bool   `json:"willRetry,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	Delta     string `json:"delta,omitempty"`
}

// ToolExecutionState tracks in-flight tool executions between
// tool_execution_start and tool_execution_end events.
type ToolExecutionState struct {
	ToolName  string
	Args      map[string]any
	StartedAt int64
}
