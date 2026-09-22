package storage

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Sentinel errors for BuildDSN. They are exported so callers and tests can
// tell the misconfigurations apart without matching on message text.
var (
	// ErrDSNPlaceholder 表示 database.url 里还留着 `${...}` 占位符。
	ErrDSNPlaceholder = errors.New("database url contains an unresolved ${...} placeholder")
	// ErrEmptyDBUser 表示逐字段拼装时用户名是空的。
	ErrEmptyDBUser = errors.New("database user is empty")
	// ErrEmptyDBPassword 表示逐字段拼装时密码是空的。
	ErrEmptyDBPassword = errors.New("database password is empty")
)

// DatabaseConfig is the set of values needed to build a PostgreSQL DSN.
//
// It serves both cmd/analysis and cmd/data. Those two used to assemble the
// DSN with their own fmt.Sprintf — one escaped the password, the other did
// not, and neither rejected an empty password (AUD-39). Keeping a single
// implementation is what makes "how a DSN is built" have one answer.
type DatabaseConfig struct {
	// URL is a complete DSN. When non-empty it is used as-is (env:
	// DATABASE_URL), and the fields below are ignored.
	URL string

	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// BuildDSN resolves a DatabaseConfig into a PostgreSQL connection string.
//
// Contract (AUD-39):
//   - URL containing `${` → ErrDSNPlaceholder. This repo has **no** env
//     expander (no os.ExpandEnv / envsubst), so a `${X}` in YAML is not a
//     template — it is those characters, verbatim. It used to be fed to the
//     driver as the password, and the failure surfaced after the TCP connect
//     as "password authentication failed", which reads as a wrong password
//     rather than a missing one.
//   - URL non-empty and placeholder-free → returned unchanged (the escape
//     hatch for embedders that have a full DSN).
//   - Otherwise assembled from the fields, with **user and password
//     required**. An empty password can never connect, so it is reported at
//     startup instead of at first query.
//
// Assembly goes through net/url rather than fmt.Sprintf: url.UserPassword
// percent-escapes by the userinfo rules, so a password containing `:` / `@`
// / `/` cannot split the DSN (the fmt.Sprintf version could).
func BuildDSN(c DatabaseConfig) (string, error) {
	if strings.Contains(c.URL, "${") {
		return "", fmt.Errorf("%w: %s — this repo has no env expander, so it would be "+
			"used as the literal password; leave database.url empty (it is then assembled "+
			"from database.*) or set DATABASE_URL to a real DSN", ErrDSNPlaceholder, c.URL)
	}
	if c.URL != "" {
		return c.URL, nil
	}
	if c.User == "" {
		return "", fmt.Errorf("%w: set database.user (env: DATABASE_USER)", ErrEmptyDBUser)
	}
	if c.Password == "" {
		return "", fmt.Errorf("%w: set DATABASE_PASSWORD — a ${...} placeholder in "+
			"config/*.yaml does NOT work (this repo has no env expander)", ErrEmptyDBPassword)
	}

	host := c.Host
	if host == "" {
		host = "localhost"
	}
	port := c.Port
	if port == 0 {
		port = 5432
	}
	name := c.Name
	if name == "" {
		name = "quant_trading"
	}
	sslmode := c.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}

	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + name,
		RawQuery: url.Values{"sslmode": {sslmode}}.Encode(),
	}
	return u.String(), nil
}
