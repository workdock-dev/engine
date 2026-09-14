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
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/suite"
	"github.com/workdock-dev/engine/features/agent_session/types"
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
	job, err := s.queue.Claim(context.Background(), "worker-1", time.Now())
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
	job, err := s.queue.Claim(context.Background(), "worker-1", time.Now())
	s.NoError(err)
	s.Nil(job)
}

func (s *EventQueueSuite) TestClaim_Error() {
	s.pool.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{scanFn: func(dest ...any) error {
			return fmt.Errorf("db error")
		}}
	}
	job, err := s.queue.Claim(context.Background(), "worker-1", time.Now())
	s.Error(err)
	s.Nil(job)
}

// --- Heartbeat ---

func (s *EventQueueSuite) TestHeartbeat_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Heartbeat(context.Background(), "event-1", time.Minute)
	s.NoError(err)
}

func (s *EventQueueSuite) TestHeartbeat_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Heartbeat(context.Background(), "event-1", time.Minute)
	s.Error(err)
}

func (s *EventQueueSuite) TestHeartbeat_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Heartbeat(context.Background(), "event-1", time.Minute)
	s.NoError(err)
}

// --- Complete ---

func (s *EventQueueSuite) TestComplete_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		s.Equal(types.EventJobStatus_Succeeded, args[1])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Complete(context.Background(), "event-1", types.EventJobStatus_Succeeded)
	s.NoError(err)
}

func (s *EventQueueSuite) TestComplete_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Complete(context.Background(), "event-1", types.EventJobStatus_Succeeded)
	s.Error(err)
}

func (s *EventQueueSuite) TestComplete_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Complete(context.Background(), "event-1", types.EventJobStatus_Succeeded)
	s.NoError(err)
}

func (s *EventQueueSuite) TestComplete_AwaitingAction() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		s.Equal(types.EventJobStatus_AwaitingAction, args[1])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Complete(context.Background(), "event-1", types.EventJobStatus_AwaitingAction)
	s.NoError(err)
}

// --- Retry ---

func (s *EventQueueSuite) TestRetry_Success() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		s.Equal("event-1", args[0])
		s.Equal("timeout", args[1])
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	err := s.queue.Retry(context.Background(), "event-1", fmt.Errorf("timeout"), time.Minute)
	s.NoError(err)
}

func (s *EventQueueSuite) TestRetry_Error() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, fmt.Errorf("db error")
	}
	err := s.queue.Retry(context.Background(), "event-1", fmt.Errorf("timeout"), time.Minute)
	s.Error(err)
}

func (s *EventQueueSuite) TestRetry_ZeroRowsAffected() {
	s.pool.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	err := s.queue.Retry(context.Background(), "event-1", fmt.Errorf("timeout"), time.Minute)
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

// --- Constructor ---

func (s *EventQueueSuite) TestNewEventQueue() {
	queue := NewEventQueue(s.pool, s.conn, "postgresql://localhost:5432/workdock")

	s.Require().NotNil(queue)
	s.Equal(s.pool, queue.client)
	s.Equal(s.conn, queue.conn)
	s.NotNil(queue.connect)
	s.Equal(DefaultReconnectInterval, queue.reconnectInterval)
	s.Equal(DefaultReconnectGracePeriod, queue.reconnectGracePeriod)
}

// --- Listen notification error paths ---

func (s *EventQueueSuite) TestListen_GivesUpReconnectingAfterGracePeriod() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		// A notification error while the context is still live starts the
		// reconnect loop; the dialer never succeeds, so the grace period is
		// exhausted and the listener gives up.
		return nil, fmt.Errorf("connection lost")
	}
	s.queue.reconnectInterval = 20 * time.Millisecond
	s.queue.reconnectGracePeriod = 100 * time.Millisecond
	s.queue.connect = func(ctx context.Context) (DBConn, error) {
		return nil, fmt.Errorf("dial failed")
	}

	runnableCh, cancellableCh, err := s.queue.Listen(context.Background())
	s.Require().NoError(err)

	select {
	case _, ok := <-runnableCh:
		s.False(ok, "runnable channel should be closed")
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for runnable channel to close")
	}

	select {
	case _, ok := <-cancellableCh:
		s.False(ok, "cancellable channel should be closed")
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for cancellable channel to close")
	}

	// The broken connection must be released before the reconnect attempts.
	s.True(s.conn.closed)
}

func (s *EventQueueSuite) TestListen_ReconnectsAndResumesNotifications() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		return nil, fmt.Errorf("FATAL: terminating connection due to administrator command (SQLSTATE 57P01)")
	}

	listenSql := ""
	notified := false
	reconnected := &mockConn{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			listenSql = sql
			return pgconn.CommandTag{}, nil
		},
		waitForNotificationFn: func(ctx context.Context) (*pgconn.Notification, error) {
			if !notified {
				notified = true
				return &pgconn.Notification{Channel: "jobs_cancelled", Payload: "evt-1"}, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	s.queue.reconnectInterval = 10 * time.Millisecond
	s.queue.reconnectGracePeriod = 2 * time.Second
	s.queue.connect = func(ctx context.Context) (DBConn, error) {
		return reconnected, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, cancellableCh, err := s.queue.Listen(ctx)
	s.Require().NoError(err)

	select {
	case payload := <-cancellableCh:
		s.Equal("evt-1", payload)
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for notification after reconnect")
	}

	// The broken connection was closed and the new one re-subscribed.
	s.True(s.conn.closed)
	s.Contains(listenSql, "LISTEN jobs_claimable")
	s.Contains(listenSql, "LISTEN jobs_cancelled")
}

func (s *EventQueueSuite) TestListen_ReconnectSignalsRunnableCatchUp() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		return nil, fmt.Errorf("connection lost")
	}

	reconnected := &mockConn{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		waitForNotificationFn: func(ctx context.Context) (*pgconn.Notification, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	s.queue.reconnectInterval = 10 * time.Millisecond
	s.queue.reconnectGracePeriod = 2 * time.Second
	s.queue.connect = func(ctx context.Context) (DBConn, error) {
		return reconnected, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runnableCh, _, err := s.queue.Listen(ctx)
	s.Require().NoError(err)

	// No notification is delivered after the reconnect, so the only runnable
	// signal is the catch-up wake up that makes the workers reclaim jobs
	// queued while the listener was disconnected.
	select {
	case <-runnableCh:
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for catch-up signal")
	}

	s.True(s.conn.closed)
}

func (s *EventQueueSuite) TestListen_ReconnectSubscribeErrorRetries() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		return nil, fmt.Errorf("connection lost")
	}

	badConn := &mockConn{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("LISTEN failed")
		},
	}
	notified := false
	goodConn := &mockConn{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		waitForNotificationFn: func(ctx context.Context) (*pgconn.Notification, error) {
			if !notified {
				notified = true
				return &pgconn.Notification{Channel: "jobs_cancelled", Payload: "evt-2"}, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	dialCount := 0
	s.queue.reconnectInterval = 10 * time.Millisecond
	s.queue.reconnectGracePeriod = 2 * time.Second
	s.queue.connect = func(ctx context.Context) (DBConn, error) {
		dialCount++
		if dialCount == 1 {
			return badConn, nil
		}
		return goodConn, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, cancellableCh, err := s.queue.Listen(ctx)
	s.Require().NoError(err)

	select {
	case payload := <-cancellableCh:
		s.Equal("evt-2", payload)
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for notification after resubscribe")
	}

	// The dialled connection that failed to subscribe was closed and a fresh
	// dial re-established the listener.
	s.True(badConn.closed)
	s.False(goodConn.closed)
}

func (s *EventQueueSuite) TestListen_ContextCancelDuringReconnect() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		return nil, fmt.Errorf("connection lost")
	}
	s.queue.reconnectInterval = 50 * time.Millisecond
	s.queue.reconnectGracePeriod = 10 * time.Second
	s.queue.connect = func(ctx context.Context) (DBConn, error) {
		return nil, fmt.Errorf("dial failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	runnableCh, cancellableCh, err := s.queue.Listen(ctx)
	s.Require().NoError(err)

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case _, ok := <-runnableCh:
		s.False(ok, "runnable channel should be closed")
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for runnable channel to close")
	}

	select {
	case _, ok := <-cancellableCh:
		s.False(ok, "cancellable channel should be closed")
	case <-time.After(2 * time.Second):
		s.Fail("timed out waiting for cancellable channel to close")
	}
}

func (s *EventQueueSuite) TestListen_DuplicateClaimableNotificationsSkipped() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	callCount := 0
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		callCount++
		switch callCount {
		case 1, 2: // two claimable notifications; the second must hit the default branch
			return &pgconn.Notification{Channel: "jobs_claimable"}, nil
		default:
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runnableCh, _, err := s.queue.Listen(ctx)
	s.Require().NoError(err)

	select {
	case <-runnableCh:
	case <-time.After(time.Second):
		s.Fail("timed out waiting for first claimable notification")
	}

	// The channel has capacity 1 and was never drained: the duplicate must not
	// have deadlocked the listener.
}

func (s *EventQueueSuite) TestListen_DuplicateCancelledNotificationsSkipped() {
	s.conn.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, nil
	}
	callCount := 0
	s.conn.waitForNotificationFn = func(ctx context.Context) (*pgconn.Notification, error) {
		callCount++
		switch callCount {
		case 1, 2: // two cancellations; the second must hit the default branch
			return &pgconn.Notification{Channel: "jobs_cancelled", Payload: "evt-1"}, nil
		default:
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, cancellableCh, err := s.queue.Listen(ctx)
	s.Require().NoError(err)

	select {
	case <-cancellableCh:
	case <-time.After(time.Second):
		s.Fail("timed out waiting for first cancellation notification")
	}
}
