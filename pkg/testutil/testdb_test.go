package testutil

// AUD-42 (ODR-065) regression tests.
//
// TestDBConfig.DSN() used to be a hand-rolled fmt.Sprintf — the third such
// copy in the repo. cmd/analysis escaped the password, cmd/data did not,
// and neither did this one. AUD-39 collapsed the two cmd copies onto
// storage.BuildDSN; this file pins that testutil goes through the same
// function, so "how a DSN is built" has one implementation.
//
// What is actually being protected:
//
//  1. a password containing `:` / `@` / `/` must not be able to split the
//     DSN (the fmt.Sprintf version let it — see the 对照组 below);
//  2. an empty password must be reported instead of silently producing an
//     unconnectable string;
//  3. the TEST_DB_* env namespace stays the one that feeds the config.
//
// These tests never dial a database, so they run in CI without Docker.

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func TestTestDBConfig_DSNHasTheExpectedAbsoluteValue(t *testing.T) {
	t.Parallel()

	cfg := TestDBConfig{
		Host:     "db.internal",
		Port:     6543,
		User:     "alice",
		Password: "s3cret",
		Database: "quant_trading_test",
	}

	got, err := cfg.DSN()
	require.NoError(t, err)

	// 绝对值断言：防「推导式断言跟着实现一起漂移」。
	assert.Equal(t,
		"postgres://alice:s3cret@db.internal:6543/quant_trading_test?sslmode=disable",
		got)

	// 推导式断言：DSN() 的结果必须就是 storage.BuildDSN 的结果。
	want, err := storage.BuildDSN(storage.DatabaseConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Name:     cfg.Database,
		SSLMode:  "disable",
	})
	require.NoError(t, err)
	assert.Equal(t, want, got, "TestDBConfig.DSN 必须委托给 storage.BuildDSN")
}

func TestTestDBConfig_DSNEscapesPasswordSoItCannotSplitTheDSN(t *testing.T) {
	t.Parallel()

	// 密码里同时含 `:` `@` `/` —— 三者都是 userinfo 的边界字符。
	const password = "a:b@c/d"

	cfg := TestDBConfig{
		Host:     "h",
		Port:     5432,
		User:     "alice",
		Password: password,
		Database: "quant",
	}

	// 前置断言：这个密码必须真的含边界字符，否则本用例什么都没验到。
	require.Contains(t, password, ":")
	require.Contains(t, password, "@")
	require.Contains(t, password, "/")

	got, err := cfg.DSN()
	require.NoError(t, err)

	parsed, err := url.Parse(got)
	require.NoError(t, err)

	pw, ok := parsed.User.Password()
	require.True(t, ok, "DSN 里应当有密码")
	assert.Equal(t, password, pw, "密码必须原样取回（说明它被按 userinfo 规则转义了）")
	assert.Equal(t, "alice", parsed.User.Username())
	assert.Equal(t, "h:5432", parsed.Host)
	assert.Equal(t, "/quant", parsed.Path)
	assert.Equal(t, "disable", parsed.Query().Get("sslmode"))

	// 对照组：朴素 fmt.Sprintf 拼出来的 DSN 与 BuildDSN 的结果**不同**，
	// 且它的 userinfo 边界确实被密码里的 `@` 抢走 —— 这就是旧实现的样子。
	naive := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	require.NotEqual(t, naive, got, "对照组必须与 BuildDSN 的结果不同，否则本用例没有区分度")

	naiveParsed, err := url.Parse(naive)
	require.NoError(t, err)
	naivePw, _ := naiveParsed.User.Password()
	assert.NotEqual(t, password, naivePw,
		"对照组应当把密码拆坏 —— 这正是旧实现的 bug，也是本用例的区分度来源")
}

func TestTestDBConfig_DSNRejectsAnEmptyPassword(t *testing.T) {
	t.Parallel()

	cfg := TestDBConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "", // 旧实现会静默拼出 postgres://postgres:@localhost:5432/…
		Database: "quant_trading_test",
	}

	// 前置断言：密码必须真的是空的，否则测的是别的东西。
	require.Empty(t, cfg.Password)

	got, err := cfg.DSN()
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrEmptyDBPassword)
	assert.Empty(t, got)
}

func TestDefaultTestDBConfig_ReadsTheTestDBEnvNamespace(t *testing.T) {
	// 不用 t.Parallel()：t.Setenv 与并行测试互斥（会 panic）。
	t.Setenv("TEST_DB_HOST", "envhost")
	t.Setenv("TEST_DB_PORT", "6543")
	t.Setenv("TEST_DB_USER", "envuser")
	t.Setenv("TEST_DB_PASSWORD", "envpass")
	t.Setenv("TEST_DB_NAME", "envdb")

	cfg := DefaultTestDBConfig()

	assert.Equal(t, "envhost", cfg.Host)
	assert.Equal(t, 6543, cfg.Port)
	assert.Equal(t, "envuser", cfg.User)
	assert.Equal(t, "envpass", cfg.Password)
	assert.Equal(t, "envdb", cfg.Database)

	got, err := cfg.DSN()
	require.NoError(t, err)
	assert.Equal(t,
		"postgres://envuser:envpass@envhost:6543/envdb?sslmode=disable", got)
}

func TestDefaultTestDBConfig_FallsBackWhenEnvIsEmpty(t *testing.T) {
	// getEnvOrDefault 把**空串**当作「未设置」—— 这条语义决定了
	// TEST_DB_PASSWORD="" 不会覆盖掉默认密码。显式置空以隔离外部环境。
	t.Setenv("TEST_DB_HOST", "")
	t.Setenv("TEST_DB_PORT", "")
	t.Setenv("TEST_DB_USER", "")
	t.Setenv("TEST_DB_PASSWORD", "")
	t.Setenv("TEST_DB_NAME", "")

	cfg := DefaultTestDBConfig()

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "postgres", cfg.User)
	assert.Equal(t, "postgres", cfg.Password)
	assert.Equal(t, "quant_trading_test", cfg.Database)
}
