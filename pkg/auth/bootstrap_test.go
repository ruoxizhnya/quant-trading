package auth

// The first-admin bootstrap is the only unauthenticated write path in the
// system, so its two load-bearing properties are pinned here rather than left
// to the doc comments:
//
//  1. The window closes permanently after the first user exists. A second
//     claimant must not get admin — that is the whole escalation guard, and it
//     is the property an attacker would go after.
//  2. With auth disabled the whole mechanism is inert *and does not touch the
//     database*. The second half is not decoration: it is what lets the
//     open-access dev/CI posture run with no database attached. The pool in
//     those subtests is nil, so "touched the database" is equivalent to
//     "panicked" — the counterexample leg is built into the fixture.
//
// The transaction-scoped advisory lock gets its own deterministic probe
// (TestCreateFirstAdmin_SerialisedByAdvisoryLock) because the race it prevents
// is timing-dependent and a "run two goroutines and see" test would not
// reliably go red without it.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// authTestPool connects to a real PostgreSQL and hands back a pool.
//
// Skips (never fails) when there is no database: this package's other tests are
// pure unit tests, and `go test ./...` must stay green on a machine with no
// PostgreSQL. The DSN matches pkg/storage/postgres_test.go — an env override is
// honoured first so a different host/port can be used without editing code.
func authTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/quant_trading?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping: cannot create pool for %s: %v", dsn, err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: cannot reach database at %s: %v", dsn, err)
		return nil
	}
	// The property under test is "the users table is empty", so an absent users
	// table is a missing migration — not a defect in this package. Skip.
	var reg *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('users')::text`).Scan(&reg); err != nil || reg == nil {
		pool.Close()
		t.Skipf("skipping: users table not present in %s (run the migrations)", dsn)
		return nil
	}
	t.Cleanup(pool.Close)
	return pool
}

// requireEmptyUsers insists the window is open before the test starts, and
// cleans the table back to empty afterwards.
//
// This is the "前置与断言同源" case: the assertion is *about* the empty-table
// state, so the fixture must establish exactly that state rather than something
// merely correlated with it. It is also non-destructive by construction — it
// only ever deletes rows it wrote, and it refuses to run at all if the table
// already has rows (someone else's accounts).
func requireEmptyUsers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n))
	if n != 0 {
		t.Skipf("skipping: users table already holds %d rows; the bootstrap window is already closed on this database", n)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users`); err != nil {
			t.Logf("cleanup: failed to empty users table: %v", err)
		}
	})
}

// ── auth disabled: inert, and no database needed ─────────────────────

func TestBootstrapRequired_AuthDisabled_DoesNotTouchDB(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, Config{}) // nil pool is the point of this test
	require.False(t, svc.Enabled())

	ctx := context.Background()
	var (
		got bool
		err error
	)
	require.NotPanics(t, func() { got, err = svc.BootstrapRequired(ctx) },
		"auth 关闭时不得查询数据库：本测试的 pool 是 nil，碰了就 panic")
	require.NoError(t, err)
	assert.False(t, got, "open-access 实例没有「首个管理员」这回事")
}

func TestCreateFirstAdmin_AuthDisabled_Closed(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, Config{})
	require.False(t, svc.Enabled())

	ctx := context.Background()
	var err error
	require.NotPanics(t, func() { _, err = svc.CreateFirstAdmin(ctx, "admin", "hunter2hunter2") },
		"auth 关闭时不得查询数据库：pool 是 nil")
	assert.ErrorIs(t, err, ErrBootstrapClosed,
		"open-access 实例不得通过 bootstrap 造出管理员")
}

// ── the escalation guard ─────────────────────────────────────────────

func TestCreateFirstAdmin_OnlyOnce(t *testing.T) {
	pool := authTestPool(t)
	requireEmptyUsers(t, pool)

	svc := NewService(pool, Config{JWTSecret: []byte("test-secret")})
	ctx := context.Background()

	// Positive evidence: the endpoint works — a first admin appears.
	require.True(t, mustBootstrapRequired(t, svc), "空表时窗口应当开着")

	first, err := svc.CreateFirstAdmin(ctx, "first-admin", "s3cret-password")
	require.NoError(t, err, "空表上的首次 bootstrap 必须成功")
	require.NotNil(t, first)
	assert.Equal(t, RoleAdmin, first.Role)
	assert.NotZero(t, first.ID)
	assert.NotEqual(t, "s3cret-password", first.PasswordHash, "密码不得明文落库")

	// The window is now shut, and the *flip* is caused by the row rather than
	// by a cached flag: read it back from the database.
	assert.False(t, mustBootstrapRequired(t, svc), "已有用户后窗口必须关闭")

	// Counterexample leg: a second claimant must NOT get admin.
	second, err := svc.CreateFirstAdmin(ctx, "second-admin", "another-password")
	assert.ErrorIs(t, err, ErrBootstrapClosed,
		"第二个申请者必须被拒 —— 这正是提权护栏")
	assert.Nil(t, second)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n))
	assert.Equal(t, 1, n, "被拒的申请者不得留下第二行")

	// And the shut state is durable, not a one-shot: a third attempt also fails.
	_, err = svc.CreateFirstAdmin(ctx, "third-admin", "yet-another-pw")
	assert.ErrorIs(t, err, ErrBootstrapClosed)

	// Finally, prove the gate keys off the table rather than off "we already
	// tried once": empty it and the window is open again.
	_, err = pool.Exec(ctx, `DELETE FROM users`)
	require.NoError(t, err)
	assert.True(t, mustBootstrapRequired(t, svc), "删空 users 后窗口应当重新打开")
}

// TestCreateFirstAdmin_SerialisedByAdvisoryLock pins the *mechanism*, not the
// outcome: holding the documented lock key from another session must block
// CreateFirstAdmin until that session lets go.
//
// The "two goroutines, count the winners" version of this test would pass with
// or without the lock — the in-transaction COUNT(*) already decides that — so it
// would be a guard that cannot fail. Blocking, by contrast, can only happen if
// the same lock key is actually taken. Remove the pg_advisory_xact_lock call and
// the first select fires immediately.
func TestCreateFirstAdmin_SerialisedByAdvisoryLock(t *testing.T) {
	pool := authTestPool(t)
	requireEmptyUsers(t, pool)

	svc := NewService(pool, Config{JWTSecret: []byte("test-secret")})
	ctx := context.Background()

	// Session A takes the lock and holds it inside an open transaction.
	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey)
	require.NoError(t, err)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		require.NoError(t, holder.Rollback(ctx))
	}
	defer release()

	type result struct {
		user *User
		err  error
	}
	done := make(chan result, 1)
	go func() {
		u, err := svc.CreateFirstAdmin(context.Background(), "blocked-admin", "s3cret-password")
		done <- result{u, err}
	}()

	select {
	case r := <-done:
		release()
		t.Fatalf("CreateFirstAdmin 没有等锁就返回了 (err=%v)；"+
			"说明它没有取 bootstrapLockKey(%d) 这把事务级咨询锁", r.err, bootstrapLockKey)
	case <-time.After(500 * time.Millisecond):
		// Still blocked — the expected shape.
	}

	release()

	select {
	case r := <-done:
		require.NoError(t, r.err, "放开锁之后必须成功")
		require.NotNil(t, r.user)
		assert.Equal(t, RoleAdmin, r.user.Role)
	case <-time.After(10 * time.Second):
		t.Fatal("放开锁之后 CreateFirstAdmin 仍未返回")
	}
}

func mustBootstrapRequired(t *testing.T, svc *Service) bool {
	t.Helper()
	need, err := svc.BootstrapRequired(context.Background())
	require.NoError(t, err)
	return need
}

// The sentinel must stay distinguishable from "any error": the HTTP layer maps
// it to 403 and everything else to 500, so a wrapped or replaced value would
// silently turn a refusal into a server error.
func TestErrBootstrapClosed_IsMatchableThroughWrapping(t *testing.T) {
	t.Parallel()
	wrapped := errors.Join(errors.New("context"), ErrBootstrapClosed)
	assert.ErrorIs(t, wrapped, ErrBootstrapClosed)
}
