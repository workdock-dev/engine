package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

type JobHandler func(ctx context.Context, job *EventJob) error

type EventJobStatus string

const (
	EventJobStatus_Queued    EventJobStatus = "queued"
	EventJobStatus_Running   EventJobStatus = "running"
	EventJobStatus_Retry     EventJobStatus = "retry"
	EventJobStatus_Succeeded EventJobStatus = "succeeded"
	EventJobStatus_Failed    EventJobStatus = "failed"
	EventJobStatus_Cancelled EventJobStatus = "cancelled"
)

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
