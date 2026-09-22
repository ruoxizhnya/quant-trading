package storage

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AUD-39：BuildDSN 是 cmd/analysis 与 cmd/data 共用的唯一 DSN 拼装实现。
// 这些用例钉的是「怎么拼」这件事的契约，不是某个调用方的行为。

func TestBuildDSN_AssemblesFromFields(t *testing.T) {
	// 每个字段都取一个**互不相同**的值：任何一个被串错位置都会看出来。
	got, err := BuildDSN(DatabaseConfig{
		Host:     "db.internal",
		Port:     6543,
		User:     "alice",
		Password: "s3cret",
		Name:     "quant",
		SSLMode:  "require",
	})
	require.NoError(t, err)
	// 绝对值断言：过真实现算出来的完整字符串。
	assert.Equal(t, "postgres://alice:s3cret@db.internal:6543/quant?sslmode=require", got)
}

func TestBuildDSN_EscapesPasswordSoItCannotSplitTheDSN(t *testing.T) {
	// 密码里带 `:` 与 `@` —— 朴素 fmt.Sprintf 会拼出
	// postgres://alice:a:b@c@h:5432/quant，userinfo 的边界被密码里的 @ 抢走。
	const password = "a:b@c/d"

	got, err := BuildDSN(DatabaseConfig{
		Host: "h", Port: 5432, User: "alice", Password: password,
		Name: "quant", SSLMode: "disable",
	})
	require.NoError(t, err)
	assert.Equal(t, "postgres://alice:a%3Ab%40c%2Fd@h:5432/quant?sslmode=disable", got)

	// 对照组：朴素拼装必须与 BuildDSN 不同，否则这个用例证明不了转义在起作用。
	naive := fmt.Sprintf("postgres://%s:%s@h:5432/quant?sslmode=disable", "alice", password)
	require.NotEqual(t, naive, got,
		"对照组失效：朴素拼装与 BuildDSN 结果相同，说明密码里没有需要转义的字符")

	// 而且朴素版本解析出来的 userinfo 边界确实是错的 —— 这才是「会坏」的证据，
	// 不只是「字符串不一样」。
	parsed, err := url.Parse(naive)
	require.NoError(t, err)
	require.NotNil(t, parsed.User)
	assert.NotEqual(t, password, parsed.User.String(),
		"朴素拼装把密码里的 @ 当成了 userinfo 的结束符")
}

func TestBuildDSN_UsesRawURLAndIgnoresFields(t *testing.T) {
	const dsn = "postgres://u:p@h:9999/db?sslmode=verify-full"
	got, err := BuildDSN(DatabaseConfig{
		URL: dsn,
		// 字段故意全填成别的值：证明 URL 优先，其余字段被忽略。
		Host: "ignored", Port: 1, User: "ignored",
		Password: "ignored", Name: "ignored", SSLMode: "ignored",
	})
	require.NoError(t, err)
	assert.Equal(t, dsn, got)
}

func TestBuildDSN_RejectsPlaceholderInURL(t *testing.T) {
	cfg := DatabaseConfig{
		URL:      "postgres://postgres:${DATABASE_PASSWORD}@postgres:5432/quant_trading?sslmode=disable",
		Host:     "postgres",
		Port:     5432,
		User:     "postgres",
		Password: "definitely-set",
		Name:     "quant_trading",
		SSLMode:  "disable",
	}
	// 前置条件：密码**是**填了的 —— 这样「报错」只可能来自占位符那条规则，
	// 不可能是 ErrEmptyDBPassword。没有这条前置断言，这个用例会假绿。
	require.NotEmpty(t, cfg.Password,
		"前置条件不成立：密码为空会让 ErrEmptyDBPassword 抢先命中，测的就不是占位符规则了")

	_, err := BuildDSN(cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDSNPlaceholder)
	assert.NotErrorIs(t, err, ErrEmptyDBPassword,
		"两条规则必须有区分度：占位符命中时不该同时报「密码为空」")
}

func TestBuildDSN_RejectsEmptyCredentials(t *testing.T) {
	base := DatabaseConfig{
		Host: "h", Port: 5432, User: "alice", Password: "pw",
		Name: "quant", SSLMode: "disable",
	}

	t.Run("user", func(t *testing.T) {
		c := base
		c.User = ""
		_, err := BuildDSN(c)
		assert.ErrorIs(t, err, ErrEmptyDBUser)
	})

	t.Run("password", func(t *testing.T) {
		c := base
		c.Password = ""
		_, err := BuildDSN(c)
		assert.ErrorIs(t, err, ErrEmptyDBPassword)
	})
}

func TestBuildDSN_FillsDefaultsForOptionalFields(t *testing.T) {
	// 只有凭据是必填的；host / port / name / sslmode 留空各自回落默认值。
	got, err := BuildDSN(DatabaseConfig{User: "alice", Password: "pw"})
	require.NoError(t, err)
	assert.Equal(t, "postgres://alice:pw@localhost:5432/quant_trading?sslmode=disable", got)
}
