package storage

// AUD-42 (ODR-065) structural guard.
//
// AUD-39/AUD-42 established the invariant that **this package is the only
// place that builds a PostgreSQL DSN**. Three copies used to exist:
// cmd/analysis escaped the password, cmd/data and pkg/testutil did not.
// A fourth would re-open the same class of bug, so adding one has to be a
// conscious act rather than an oversight.
//
// The check is structural (it walks the repo) because a behavioural test
// only covers the call sites that exist today — it cannot see a new
// hand-rolled copy in a package nobody has written yet.
//
// It parses **string literals via go/ast**, not the raw text: a text grep
// cannot tell a call apart from a mention in a comment, so a guard written
// that way goes red on its own documentation (AUD-29 taught this the hard
// way). Comments are therefore invisible to it, by construction.
//
// Boundary of this check:
//   - Only string literals are inspected; the pattern must be a compile-time
//     constant. A DSN assembled from pieces at runtime is not matched.
//   - Only the URL form is matched (a postgres URL carrying a printf verb).
//     The keyword/value form (host=… user=…) is not; the repo has none and
//     no driver call uses that shape today.
//   - It does not judge whether a match is *wrong*, only that it is
//     hand-rolled — so every match must be named in the allowlist with a
//     reason.
//   - Files that fail to parse are reported as errors, not skipped silently.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handRolledDSNLiteral matches a postgres URL that carries a printf verb,
// i.e. a DSN being assembled by hand rather than delegated to BuildDSN.
var handRolledDSNLiteral = regexp.MustCompile(`postgres(?:ql)?://[^"\n]*%[sdv]`)

// dsnAssemblyAllowlist maps a repo-relative slash path to the reason it is
// allowed to contain a hand-rolled DSN. Adding an entry has to be a
// deliberate act, which is the whole point of listing them.
var dsnAssemblyAllowlist = map[string]string{
	"pkg/storage/dsn_test.go":         "对照组：故意用朴素 fmt.Sprintf 证明它与 BuildDSN 的结果不同",
	"pkg/storage/integration_test.go": "测试局部：端口是运行时才知道的动态端口，且不连接真实库",
	"pkg/testutil/testdb_test.go":     "对照组：同 dsn_test.go，证明旧实现会把密码拆坏",
}

func TestNoHandRolledPostgresDSNAssemblyOutsideStorage(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	var unexpected []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		found, err := fileHasHandRolledDSNLiteral(path, raw)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := dsnAssemblyAllowlist[rel]; !ok {
			unexpected = append(unexpected, rel)
		}
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, unexpected,
		"这些文件手写了 postgres DSN 拼装 —— 改用 storage.BuildDSN，"+
			"或把它加进 dsnAssemblyAllowlist 并写明理由：%v", unexpected)
}

// fileHasHandRolledDSNLiteral reports whether src contains a string literal
// that looks like a hand-assembled postgres DSN. Comments are not literals,
// so mentioning the pattern in prose does not trip it.
func fileHasHandRolledDSNLiteral(filename string, src []byte) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, src, 0)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", filename, err)
	}

	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if handRolledDSNLiteral.MatchString(value) {
			found = true
			return false
		}
		return true
	})
	return found, nil
}
