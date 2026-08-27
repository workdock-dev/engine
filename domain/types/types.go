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

package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

type PlatformProvider string
type HarnessProvider string
type EventJobStatus string
type SessionEventTriggerReason string
type IssueState string

const (
	SessionEventTriggerReason_Unknown   SessionEventTriggerReason = "unknown"
	SessionEventTriggerReason_PRComment SessionEventTriggerReason = "pr_comment"
	SessionEventTriggerReason_CheckRun  SessionEventTriggerReason = "check_run"

	IssueStateCompleted IssueState = "completed"
)

const (
	PlatformProvider_Linear PlatformProvider = "linear"
	PlatformProvider_GitHub PlatformProvider = "github"

	HarnessProvider_OpenCode HarnessProvider = "opencode"

	EventJobStatus_Queued    EventJobStatus = "queued"
	EventJobStatus_Running   EventJobStatus = "running"
	EventJobStatus_Retry     EventJobStatus = "retry"
	EventJobStatus_Succeeded EventJobStatus = "succeeded"
	EventJobStatus_Failed    EventJobStatus = "failed"
	EventJobStatus_Cancelled EventJobStatus = "cancelled"
)

type Organization struct {
	Identifier string
	Provider   PlatformProvider
	Name       string
}

type Session struct {
	OrganizationIdentifier string
	Identifier             string
	Provider               PlatformProvider
	IssueId                string
	Creator                string
	RepoFullName           *string
}

type Issue struct {
	Title       string
	Identifier  string
	Description string
}

type SessionEvent struct {
	SessionIdentifier string
	Identifier        string
	Payload           json.RawMessage

	// Set when event originates from a previous
	// event. Recreate the original identifier using
	// the session's provider ingest
	Seed   *string
	Result *SessionEventResult
	GitRef *string

	// Reason indicates why a session event was re-triggered.
	// This is set when the event originates from a GitHub webhook
	// (e.g., pull_request_review_comment, check_run) to preserve
	// context about what happened.
	Reason SessionEventTriggerReason
}

type SessionEventResult struct {
	PullRequest *PullRequest
}

type GitHubConnection struct {
	SessionEventIdentifier *string
	RepoFullName           string
	Connected              bool
	InstallationId         *string
}

type PullRequest struct {
	HeadRefName string `json:"headRefName"`
	HeadRefOID  string `json:"headRefOid"`
	Number      int    `json:"number"`
	URL         string `json:"url"`
}

type AgentAction struct {
	Name   string
	Input  string
	Output string
}

type AgentElicitation struct {
	Question string
	Options  []AgentOption
	Multiple bool
}

type AgentOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

const (
	GitHubUrl = "https://github.com/"
)

type ExternalURL struct {
	Label string
	URL   string
}

// EventJob is the durable unit of work. It references a persisted SessionEvent
// whose payload contains only validated webhook data, so processing never needs
// to trust raw HTTP input.
type EventJob struct {
	SessionEventIdentifier string
	QueuedBy               string // Agent session identifier that triggered this job; used to cancel every job of the session
	PreviousState          EventJobStatus
	Status                 EventJobStatus
	Attempts               int
	willRetry              bool
	NextAttemptAt          *time.Time
	LeaseOwner             *string
	LeaseExpiresAt         *time.Time
	LastError              *string
	CancellationReason     *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func GenerateIdempotencyKey(payload any) (string, error) {
	if payload == nil {
		err := errors.New("expected non-nil payload for generating idempotency key, got nil")
		slog.Error("failed to generate idempotency key", "err", err)
		return "", err
	}

	data, err := json.Marshal(payload)

	if err != nil {
		slog.Error("failed to generate idempotency key", "err", err)
		return "", err
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:18]), nil // 36 hex chars
}

func (j EventJob) WillRetry() bool {
	return j.willRetry
}

func (j *EventJob) SetMaxAttempts(maxAttempts int) {
	j.willRetry = j.Attempts < maxAttempts
}
