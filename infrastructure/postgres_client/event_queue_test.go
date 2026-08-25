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

package postgres_client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/workdock-dev/engine/domain/types"
	"github.com/stretchr/testify/suite"
)

type EventQueueSuite struct {
	suite.Suite
	queue *EventQueue
	pool  *mockPool
	conn  *mockConn
}

func TestEventQueueSuite(t *testing.T) {
	suite.Run(t, new(EventQueueSuite))
}

func (s *EventQueueSuite) SetupTest() {
	s.pool = &mockPool{}
	s.conn = &mockConn{}
	s.queue = &EventQueue{
		client: s.pool,
		conn:   s.conn,
	}
}

// --- Claim ---

func (s *EventQueueSuite) TestClaim_Success() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*string) = "event-1"
			*dest[1].(*types.EventJobStatus) = types.EventJobStatus("queued")
			*dest[2].(*types.EventJobStatus) = types.EventJobStatus("running")
			*dest[3].(*int) = 1
			now := time.Now().UTC()
			*dest[4].(**time.Time) = &now
			owner := "worker-1"
			*dest[5].(**string) = &owner
			*dest[6].(**time.Time) = &now
			*dest[7].(**string) = nil
			*dest[8].(**string) = nil
			*dest[9].(*string) = "session-1"
			return nil
		}}
	}
	job, err := s.queue.Claim(context.Background(), "worker-1")
	s.NoError(err)
	s.NotNil(job)
	s.Equal("event-1", job.SessionEventIdentifier)
	s.Equal("running", string(job.Status))
	s.Equal("worker-1", *job.LeaseOwner)
}

func (s *EventQueueSuite) TestClaim_NoJobs() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return pgx.ErrNoRows
		}}
	}
	job, err := s.queue.Claim(context.Background(), "worker-1")
	s.NoError(err)
	s.Nil(job)
}

func (s *EventQueueSuite) TestClaim_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	job, err := s.queue.Claim(context.Background(), "worker-1")
	s.Error(err)
	s.Nil(job)
}

// --- Heartbeat ---

func (s *EventQueueSuite) TestHeartbeat_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Heartbeat(context.Background(), "event-1")
	s.NoError(err)
}

func (s *EventQueueSuite) TestHeartbeat_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Heartbeat(context.Background(), "event-1")
	s.Error(err)
}

func (s *EventQueueSuite) TestHeartbeat_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Heartbeat(context.Background(), "event-1")
	s.NoError(err)
}

// --- Complete ---

func (s *EventQueueSuite) TestComplete_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Complete(context.Background(), "event-1")
	s.NoError(err)
}

func (s *EventQueueSuite) TestComplete_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Complete(context.Background(), "event-1")
	s.Error(err)
}

func (s *EventQueueSuite) TestComplete_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Complete(context.Background(), "event-1")
	s.NoError(err)
}

// --- Retry ---

func (s *EventQueueSuite) TestRetry_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		s.Equal("timeout", args[1])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Retry(context.Background(), "event-1", fmt.Errorf("timeout"))
	s.NoError(err)
}

func (s *EventQueueSuite) TestRetry_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Retry(context.Background(), "event-1", fmt.Errorf("timeout"))
	s.Error(err)
}

func (s *EventQueueSuite) TestRetry_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Retry(context.Background(), "event-1", fmt.Errorf("timeout"))
	s.NoError(err)
}

// --- Fail ---

func (s *EventQueueSuite) TestFail_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		s.Equal("fatal error", args[1])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Fail(context.Background(), "event-1", fmt.Errorf("fatal error"))
	s.NoError(err)
}

func (s *EventQueueSuite) TestFail_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Fail(context.Background(), "event-1", fmt.Errorf("fatal error"))
	s.Error(err)
}

func (s *EventQueueSuite) TestFail_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Fail(context.Background(), "event-1", fmt.Errorf("fatal error"))
	s.NoError(err)
}

// --- Listen ---

func (s *EventQueueSuite) TestListen_Success() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Listen so ctx.Err() is non-nil
	runnableCh, cancellableCh, err := s.queue.Listen(ctx)
	s.NoError(err)
	s.NotNil(runnableCh)
	s.NotNil(cancellableCh)
	// Drain channels
	for range runnableCh {
	}
	for range cancellableCh {
	}
}

func (s *EventQueueSuite) TestListen_ExecError() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("LISTEN failed")
	}
	runnableCh, cancellableCh, err := s.queue.Listen(context.Background())
	s.Error(err)
	s.NotNil(runnableCh)
	s.NotNil(cancellableCh)
}

func (s *EventQueueSuite) TestListen_ReceivesJobClaimable() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	callCount := 0
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		callCount++
		if callCount == 1 {
			return &pgconn.Notification{Channel: "jobs_claimable"}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runnableCh, _, err := s.queue.Listen(ctx)
	s.NoError(err)
	select {
	case <-runnableCh:
	case <-time.After(time.Second):
		s.Fail("timed out waiting for job claimable notification")
	}
	cancel()
}

func (s *EventQueueSuite) TestListen_ReceivesJobCancelled() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	callCount := 0
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		callCount++
		if callCount == 1 {
			return &pgconn.Notification{Channel: "jobs_cancelled", Payload: "event-1"}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, cancellableCh, err := s.queue.Listen(ctx)
	s.NoError(err)
	select {
	case payload := <-cancellableCh:
		s.Equal("event-1", payload)
	case <-time.After(time.Second):
		s.Fail("timed out waiting for job cancelled notification")
	}
	cancel()
}

func (s *EventQueueSuite) TestListen_ContextCancel() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	runnableCh, cancellableCh, err := s.queue.Listen(ctx)
	s.NoError(err)
	cancel()
	// Wait for channels to close
	deadline := time.After(2 * time.Second)
	<-runnableCh
	<-cancellableCh
	select {
	case <-deadline:
		s.Fail("timed out waiting for channels to close")
	default:
	}
}

// --- Close ---

func (s *EventQueueSuite) TestClose_Success() {
	s.conn.closeFn = func(ctx context.Context) error {
		return nil
	}
	err := s.queue.Close(context.Background())
	s.NoError(err)
	s.True(s.conn.closed)
}

func (s *EventQueueSuite) TestClose_Error() {
	s.conn.closeFn = func(ctx context.Context) error {
		return fmt.Errorf("close failed")
	}
	err := s.queue.Close(context.Background())
	s.Error(err)
}
