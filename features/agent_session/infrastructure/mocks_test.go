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
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// --- dbPool mock ---

type mockPool struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	beginFn    func(ctx context.Context) (pgx.Tx, error)
	closeFn    func()
	closed     bool
}

func (m *mockPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{}
}

func (m *mockPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return nil, nil
}

func (m *mockPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockPool) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return &mockTx{}, nil
}

func (m *mockPool) Close() {
	m.closed = true
	if m.closeFn != nil {
		m.closeFn()
	}
}

// --- dbConn mock ---

type mockConn struct {
	execFn                func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	waitForNotificationFn func(ctx context.Context) (*pgconn.Notification, error)
	closeFn               func(ctx context.Context) error
	closed                bool
}

func (m *mockConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	if m.waitForNotificationFn != nil {
		return m.waitForNotificationFn(ctx)
	}
	// Block until context is cancelled
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *mockConn) Close(ctx context.Context) error {
	m.closed = true
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

// --- pgx.Row mock ---

type mockRow struct {
	scanFn func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

// --- pgx.Rows mock ---

type mockRowValues struct {
	values  []any
	scanErr error
}

type mockRows struct {
	rows    []mockRowValues
	rowsErr error
	closed  bool
}

func (r *mockRows) Next() bool {
	return len(r.rows) > 0
}

func (r *mockRows) Scan(dest ...any) error {
	row := r.rows[0]
	r.rows = r.rows[1:]

	if row.scanErr != nil {
		return row.scanErr
	}

	for i, v := range row.values {
		if v == nil {
			continue
		}

		d := reflect.ValueOf(dest[i])
		if d.Kind() == reflect.Ptr {
			// Follow one level of indirection for nullable columns
			// (e.g. **string for a *string field).
			if d.Elem().Kind() == reflect.Ptr {
				out := reflect.New(reflect.TypeOf(v))
				out.Elem().Set(reflect.ValueOf(v))
				d.Elem().Set(out)
				continue
			}

			d.Elem().Set(reflect.ValueOf(v))
		} else {
			d.Set(reflect.ValueOf(v))
		}
	}

	return nil
}

func (r *mockRows) Err() error {
	return r.rowsErr
}

func (r *mockRows) Close() {
	r.closed = true
}

func (r *mockRows) Values() ([]any, error) {
	return nil, nil
}

func (r *mockRows) RawValues() [][]byte {
	return nil
}

func (r *mockRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *mockRows) Conn() *pgx.Conn {
	return nil
}

// --- pgx.Tx mock ---

type mockTx struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	commitFn   func(ctx context.Context) error
	rollbackFn func(ctx context.Context) error
}

func (t *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return t, nil
}

func (t *mockTx) Commit(ctx context.Context) error {
	if t.commitFn != nil {
		return t.commitFn(ctx)
	}
	return nil
}

func (t *mockTx) Rollback(ctx context.Context) error {
	if t.rollbackFn != nil {
		return t.rollbackFn(ctx)
	}
	return nil
}

func (t *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (t *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

func (t *mockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (t *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (t *mockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if t.execFn != nil {
		return t.execFn(ctx, sql, arguments...)
	}
	return pgconn.CommandTag{}, nil
}

func (t *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (t *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &mockRow{}
}

func (t *mockTx) Conn() *pgx.Conn {
	return nil
}
