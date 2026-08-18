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

import "encoding/json"

type WireEvent struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part"`
}

type ToolTime struct {
	Start int64  `json:"start"`
	End   *int64 `json:"end,omitempty"`
}

type ToolState struct {
	Status   string         `json:"status"`
	Input    map[string]any `json:"input"`
	Raw      string         `json:"raw,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Title    string         `json:"title,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Time     *ToolTime      `json:"time,omitempty"`
}

type StepStartPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Snapshot  string `json:"snapshot,omitempty"`
}

type ReasoningPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Time      struct {
		Start int64  `json:"start"`
		End   *int64 `json:"end,omitempty"`
	} `json:"time"`
}

type TextPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic,omitempty"`
	Time      struct {
		Start int64  `json:"start"`
		End   *int64 `json:"end,omitempty"`
	} `json:"time"`
}

type ToolPart struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionID"`
	MessageID string    `json:"messageID"`
	Type      string    `json:"type"`
	Tool      string    `json:"tool"`
	CallID    string    `json:"callID"`
	State     ToolState `json:"state"`
}

type StepFinishPart struct {
	ID        string  `json:"id"`
	SessionID string  `json:"sessionID"`
	MessageID string  `json:"messageID"`
	Type      string  `json:"type"`
	Reason    string  `json:"reason"`
	Snapshot  string  `json:"snapshot,omitempty"`
	Cost      float64 `json:"cost"`
	Tokens    struct {
		Total     int `json:"total"`
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

type FilePart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Mime      string `json:"mime"`
	Filename  string `json:"filename,omitempty"`
	URL       string `json:"url"`
}

type SubtaskPart struct {
	ID          string `json:"id"`
	SessionID   string `json:"sessionID"`
	MessageID   string `json:"messageID"`
	Type        string `json:"type"`
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Agent       string `json:"agent"`
}

type SnapshotPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Snapshot  string `json:"snapshot"`
}

type PatchPart struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	MessageID string   `json:"messageID"`
	Type      string   `json:"type"`
	Hash      string   `json:"hash"`
	Files     []string `json:"files"`
}

type AgentPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Name      string `json:"name"`
}

type RetryPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Attempt   int    `json:"attempt"`
	Error     struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error"`
}

type CompactionPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Auto      bool   `json:"auto"`
	Overflow  bool   `json:"overflow,omitempty"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type QuestionInfo struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple,omitempty"`
	Custom   *bool            `json:"custom,omitempty"` // nil = true
}
