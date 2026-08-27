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
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	domain_service "github.com/workdock-dev/engine/domain/service"
	"github.com/workdock-dev/engine/domain/types"
)

type OutputSuite struct {
	suite.Suite
	parts *mockParts
}

func TestOutputSuite(t *testing.T) {
	suite.Run(t, new(OutputSuite))
}

func (s *OutputSuite) SetupTest() {
	s.parts = new(mockParts)
}

func (s *OutputSuite) newOutput(stdout, stderr <-chan string, opts ...func(*OpenCodeOutput)) *OpenCodeOutput {
	o, err := NewOpenCodeOutput(
		nil, // app - not needed for tests with disabled liveness
		s.parts,
		"anthropic",
		"claude-3",
		"lin_at_123",
		"session-1",
		stdout,
		stderr,
		0,   // livenessTimeout disabled
		0,   // maxMisses disabled
		nil, // onUnhealthy
	)
	s.Require().NoError(err)
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func makeWireEvent(typ string, part any) string {
	partJSON, _ := json.Marshal(part)
	e := WireEvent{
		Type:      typ,
		Timestamp: time.Now().UnixNano(),
		SessionID: "session-1",
		Part:      partJSON,
	}
	data, _ := json.Marshal(e)
	return string(data) + "\n"
}

// --- NewOpenCodeOutput tests ---

func (s *OutputSuite) TestNewOpenCodeOutput_Success() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o, err := NewOpenCodeOutput(nil, s.parts, "anthropic", "claude-3", "lin_at_123", "s1", stdout, stderr, 0, 0, nil)
	s.NoError(err)
	s.NotNil(o)
}

// --- Parse() tests ---

func (s *OutputSuite) TestParse_TextEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, "hello world").Once()

	stdout <- makeWireEvent("text", TextPart{Text: "hello world"})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, "hello world")
}

func (s *OutputSuite) TestParse_ReasoningEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "thinking...").Once()

	stdout <- makeWireEvent("reasoning", ReasoningPart{Text: "thinking..."})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Thought", mock.Anything, "thinking...")
}

func (s *OutputSuite) TestParse_ToolUseEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash" && a.Input == "ls"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"command": "ls"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash" && a.Input == "ls"
	}))
}

func (s *OutputSuite) TestParse_StepStartEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("step_start", StepStartPart{ID: "s1"})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Thought", mock.Anything, "")
}

func (s *OutputSuite) TestParse_FileEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("file", FilePart{URL: "https://example.com/file.txt"})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Thought", mock.Anything, "")
}

func (s *OutputSuite) TestParse_SubtaskEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("subtask", SubtaskPart{Description: "do something"})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_SnapshotEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("snapshot", SnapshotPart{Snapshot: "snap1"})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_PatchEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("patch", PatchPart{Hash: "abc", Files: []string{"a.go"}})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_AgentEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("agent", AgentPart{Name: "coder"})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_CompactionEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("compaction", CompactionPart{Auto: true})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_RetryEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()

	stdout <- makeWireEvent("retry", RetryPart{Attempt: 1})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_StepFinishEvent() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, "").Once()

	stdout <- makeWireEvent("step_finish", StepFinishPart{
		Reason: "completed",
		Cost:   0.05,
		Tokens: struct {
			Total     int `json:"total"`
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
			Cache     struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		}{
			Total:  100,
			Input:  50,
			Output: 50,
		},
	})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, "")
}

func (s *OutputSuite) TestParse_StepFinishWithCache() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, "").Once()

	stdout <- makeWireEvent("step_finish", StepFinishPart{
		Reason: "completed",
		Cost:   0.1,
		Tokens: struct {
			Total     int `json:"total"`
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
			Cache     struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		}{
			Total:  200,
			Input:  100,
			Output: 100,
			Cache: struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			}{
				Read:  30,
				Write: 20,
			},
		},
	})
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, "")
}

func (s *OutputSuite) TestParse_InvalidJSON() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- "not valid json at all {{{"
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	// No panic, no part calls
	s.parts.AssertNotCalled(s.T(), "Thought", mock.Anything, mock.Anything)
	s.parts.AssertNotCalled(s.T(), "Response", mock.Anything, mock.Anything)
	s.parts.AssertNotCalled(s.T(), "Action", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParse_EmptyLine() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- ""
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_PartialLineThenFlushed() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, "hello").Once()

	// Send partial line, then the rest
	stdout <- "not json "
	stdout <- "more not json\n"
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	// Invalid JSON, so no part calls expected
}

func (s *OutputSuite) TestParse_PartialLineThenClose() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, "hello").Once()

	textPart := TextPart{Text: "hello"}
	stdout <- makeWireEvent("text", textPart)
	// Channel close flushes any pending data
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, "hello")
}

func (s *OutputSuite) TestParse_Stderr() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stderr <- "some error output"
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	// No part calls for stderr
}

func (s *OutputSuite) TestParse_BothChannelsClose() {
	stdout := make(chan string)
	stderr := make(chan string)
	o := s.newOutput(stdout, stderr)

	close(stdout)
	close(stderr)

	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_ContextCancel() {
	stdout := make(chan string, 100)
	stderr := make(chan string, 100)
	o := s.newOutput(stdout, stderr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	close(stdout)
	close(stderr)
	o.Parse(ctx)
}

// --- parseToolPart tests (via Parse) ---

func (s *OutputSuite) TestParseTool_Bash() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash" && a.Input == "ls -la" && a.Output == "result"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"command": "ls -la"}, Output: "result", Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_GlobWithPaths() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "glob" && a.Input == "*.go in src/"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "glob",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"pattern": "*.go", "path": "src/"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_GlobNoPath() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "glob" && a.Input == "*.go"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "glob",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"pattern": "*.go"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Read() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "read" && a.Input == "main.go"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "read",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"filePath": "main.go"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_GrepWithPaths() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "grep" && a.Input == "TODO in src/"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "grep",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"pattern": "TODO", "path": "src/"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_GrepNoPath() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "grep" && a.Input == "TODO"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "grep",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"pattern": "TODO"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Webfetch() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "webfetch" && a.Input == "https://example.com"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "webfetch",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"url": "https://example.com"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Websearch() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "websearch" && a.Input == "golang testing"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "websearch",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"query": "golang testing"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Write() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "write" && a.Input == "test.go"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "write",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"filePath": "test.go"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Edit() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "edit" && a.Input == "main.go"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "edit",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"filePath": "main.go"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Task() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "task" && a.Input == "fix the bug"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "task",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"description": "fix the bug"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Execute() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "execute" && a.Input == "go test ./..."
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "execute",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"command": "go test ./..."}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_ApplyPatchWithFiles() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "apply_patch" && a.Input == "a.go, b.go"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "apply_patch",
		CallID: "c1",
		State: ToolState{
			Input: map[string]any{
				"files": []any{
					map[string]any{"filePath": "a.go"},
					map[string]any{"filePath": "b.go"},
				},
			},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_ApplyPatchEmpty() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "apply_patch"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "apply_patch",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"files": []any{}}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Todowrite() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "todowrite" && a.Input == "step 1, step 2"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "todowrite",
		CallID: "c1",
		State: ToolState{
			Input: map[string]any{
				"todos": []any{
					map[string]any{"content": "step 1"},
					map[string]any{"content": "step 2"},
				},
			},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_TodowriteEmpty() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "todowrite"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "todowrite",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"todos": []any{}}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Skill() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "skill" && a.Input == "testing"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "skill",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"name": "testing"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Default() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "unknown_tool"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "unknown_tool",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"foo": "bar"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_WithEndTime() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	endTime := int64(200)
	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"command": "ls"},
			Status: "completed",
			Time:   &ToolTime{Start: 100, End: &endTime},
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_EndTimeZeroStart() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	endTime := int64(200)
	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Times(2)

	// First event: start only (stores in toolStarts)
	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"command": "ls"},
			Status: "running",
			Time:   &ToolTime{Start: 100},
		},
	})
	// Second event: end with Start=0 (should use stored start)
	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"command": "ls"},
			Status: "completed",
			Time:   &ToolTime{Start: 0, End: &endTime},
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_ToolStartOnly() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Once()

	// Start only, no end - should store in toolStarts
	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"command": "ls"},
			Status: "running",
			Time:   &ToolTime{Start: 100},
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_NoTime() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"command": "ls"},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_ErrorStatus() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	endTime := int64(200)
	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"command": "ls"},
			Status: "error",
			Error:  "command not found",
			Time:   &ToolTime{Start: 100, End: &endTime},
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_Question() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Elicitation", mock.Anything, mock.MatchedBy(func(e types.AgentElicitation) bool {
		return e.Question == "Which option?" && len(e.Options) == 2
	})).Once()

	questions := []map[string]any{
		{
			"question": "Which option?",
			"header":   "Choice",
			"multiple": false,
			"options": []any{
				map[string]any{"label": "A", "description": "Option A"},
				map[string]any{"label": "B", "description": "Option B"},
			},
		},
	}
	questionsJSON, _ := json.Marshal(questions)

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "question",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"questions": json.RawMessage(questionsJSON)},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseTool_QuestionInvalidFormat() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "question",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"not_questions": "bad"},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Elicitation", mock.Anything, mock.Anything)
}

// --- parseQuestions tests (via Parse) ---

func (s *OutputSuite) TestParseQuestions_ValidQuestions() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Elicitation", mock.Anything, mock.MatchedBy(func(e types.AgentElicitation) bool {
		return e.Question == "What?" && e.Multiple == true && len(e.Options) == 1
	})).Once()

	questions := []map[string]any{
		{
			"question": "What?",
			"header":   "Test",
			"multiple": true,
			"options": []any{
				map[string]any{"label": "Yes", "description": "yep"},
			},
		},
	}
	questionsJSON, _ := json.Marshal(questions)

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "question",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"questions": json.RawMessage(questionsJSON)},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParseQuestions_NoQuestionsKey() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "question",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Elicitation", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParseQuestions_InvalidQuestionEntry() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	questions := []any{"not a map"}
	questionsJSON, _ := json.Marshal(questions)

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "question",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"questions": json.RawMessage(questionsJSON)},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Elicitation", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParseQuestions_InvalidOptionEntry() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Elicitation", mock.Anything, mock.MatchedBy(func(e types.AgentElicitation) bool {
		return e.Question == "What?" && len(e.Options) == 0
	})).Once()

	questions := []map[string]any{
		{
			"question": "What?",
			"options": []any{
				"not a map",
			},
		},
	}
	questionsJSON, _ := json.Marshal(questions)

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "question",
		CallID: "c1",
		State: ToolState{
			Input:  map[string]any{"questions": json.RawMessage(questionsJSON)},
			Status: "completed",
		},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

// --- startLivenessProbe tests ---

func (s *OutputSuite) TestLivenessProbe_DisabledTimeout() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	// No panic, no unhealthy callback - livenessPolicy is nil by default (livenessTimeout=0)
}

func (s *OutputSuite) TestLivenessProbe_DisabledMaxMisses() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestLivenessProbe_DisabledOnUnhealthy() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr, func(out *OpenCodeOutput) {
		out.SetLivenessPolicy(domain_service.NewLivenessPolicy(time.Millisecond, 1))
		out.onUnhealthy = nil
	})

	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestLivenessProbe_Unhealthy() {
	stdout := make(chan string)
	stderr := make(chan string)
	unhealthyCalled := make(chan struct{}, 1)
	o := s.newOutput(stdout, stderr, func(out *OpenCodeOutput) {
		out.SetLivenessPolicy(domain_service.NewLivenessPolicy(10*time.Millisecond, 1))
		out.onUnhealthy = func() {
			unhealthyCalled <- struct{}{}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go o.Parse(ctx)

	select {
	case <-unhealthyCalled:
		// Expected
	case <-time.After(2 * time.Second):
		s.Fail("onUnhealthy was not called")
	}
	cancel()
	close(stdout)
	close(stderr)
}

func (s *OutputSuite) TestLivenessProbe_ActivityResets() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr, func(out *OpenCodeOutput) {
		out.SetLivenessPolicy(domain_service.NewLivenessPolicy(20*time.Millisecond, 2))
	})

	s.parts.On("Response", mock.Anything, mock.Anything).Maybe()

	// Send an event right before Parse to keep it alive
	textPart := TextPart{Text: "keep alive"}
	stdout <- makeWireEvent("text", textPart)

	// Wait a bit less than liveness timeout, then send another event
	time.Sleep(15 * time.Millisecond)
	stdout <- makeWireEvent("text", TextPart{Text: "still alive"})

	// Wait for first tick to pass (should see activity)
	time.Sleep(25 * time.Millisecond)

	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	// No unhealthy callback should have been called
}

// --- tokenUsageAdd and metric-related tests ---

func (s *OutputSuite) TestParse_PartTypeToolUseNormalized() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Once()

	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"command": "ls"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParse_MultipleEvents() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Thought", mock.Anything, "").Once()
	s.parts.On("Response", mock.Anything, "hello").Once()
	s.parts.On("Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	})).Once()

	stdout <- makeWireEvent("step_start", StepStartPart{})
	stdout <- makeWireEvent("text", TextPart{Text: "hello"})
	stdout <- makeWireEvent("tool_use", ToolPart{
		Tool:   "bash",
		CallID: "c1",
		State:  ToolState{Input: map[string]any{"command": "ls"}, Status: "completed"},
	})
	close(stdout)
	close(stderr)
	o.Parse(context.Background())

	s.parts.AssertCalled(s.T(), "Thought", mock.Anything, "")
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, "hello")
	s.parts.AssertCalled(s.T(), "Action", mock.Anything, mock.MatchedBy(func(a types.AgentAction) bool {
		return a.Name == "bash"
	}))
}

func (s *OutputSuite) TestParseLine_CancelledContext() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	close(stdout)
	close(stderr)
	o.Parse(ctx)
}

func (s *OutputSuite) TestParseLine_WhitespaceOnly() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- "   \t  \n"
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
}

func (s *OutputSuite) TestParsePart_ReasoningUnmarshalError() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- makeWireEvent("reasoning", json.RawMessage(`"not an object"`))
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Thought", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParsePart_TextUnmarshalError() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- makeWireEvent("text", json.RawMessage(`"not an object"`))
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Response", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParsePart_ToolUnmarshalError() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- makeWireEvent("tool_use", json.RawMessage(`"not an object"`))
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Action", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParsePart_StepFinishUnmarshalError() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	stdout <- makeWireEvent("step_finish", json.RawMessage(`"not an object"`))
	close(stdout)
	close(stderr)
	o.Parse(context.Background())
	s.parts.AssertNotCalled(s.T(), "Response", mock.Anything, mock.Anything)
}

func (s *OutputSuite) TestParse_UnexpectedPartType() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, mock.MatchedBy(func(text string) bool {
		return strings.Contains(text, "An unexpected format has been received by the harness") && strings.Contains(text, "APIError")
	})).Once()

	errorEvent := `{"type":"error","timestamp":1787101287693,"sessionID":"ses_test","error":{"name":"APIError","data":{"message":"Unauthorized","statusCode":401,"isRetryable":false}}}`
	stdout <- errorEvent + "\n"
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, mock.MatchedBy(func(text string) bool {
		return strings.Contains(text, "An unexpected format has been received by the harness") && strings.Contains(text, "APIError")
	}))
}

func (s *OutputSuite) TestParse_UnexpectedPartType_UnknownType() {
	stdout := make(chan string, 10)
	stderr := make(chan string, 10)
	o := s.newOutput(stdout, stderr)

	s.parts.On("Response", mock.Anything, mock.MatchedBy(func(text string) bool {
		return strings.Contains(text, "An unexpected format has been received by the harness") && strings.Contains(text, "something_new")
	})).Once()

	unknownEvent := `{"type":"something_new","timestamp":1787101287693,"sessionID":"ses_test","part":{"id":"1"}}`
	stdout <- unknownEvent + "\n"
	close(stdout)
	close(stderr)

	o.Parse(context.Background())
	s.parts.AssertCalled(s.T(), "Response", mock.Anything, mock.MatchedBy(func(text string) bool {
		return strings.Contains(text, "An unexpected format has been received by the harness") && strings.Contains(text, "something_new")
	}))
}
