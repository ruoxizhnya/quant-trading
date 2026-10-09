package repoguard

// Structural guard: **only the allowed higher layers may import `pkg/ai/...`**.
//
// Why this exists. The dependency graph is meant to be one-way: `ai → core`.
// The AI layer sits on top and may reach down into the core (`pkg/domain`,
// `pkg/backtest`, `pkg/strategy`, …), but a core package must never reach
// back up into `pkg/ai`. That "reverse edge" is exactly what let
// `pkg/strategy` depend on `pkg/ai` — first on the LLM client (S7-P1-2),
// then again on `pkg/ai/contracts`, and `pkg/strategy/expression` on
// `pkg/ai/expression`. Each one was invisible until someone looked at the
// import graph by hand.
//
// 2026-10-09 (ADR-027 §5 step 3) broke the last of these by rehoming two
// packages that never belonged in the AI layer at all: the expression DSL
// engine (`pkg/ai/expression` → `pkg/expression`) and the `BacktestRunner`
// execution-carrier contract (`pkg/ai/contracts` → `pkg/backtest/contracts`).
// This guard is the other half of that change: it makes the boundary a
// **structural** property, so the next reverse edge fails CI instead of
// quietly shipping.
//
// Rule (whitelist, fail-closed): a **production** .go file may import a
// `pkg/ai/...` package ONLY if the importing package lives under one of:
//
//   - pkg/ai/...   — the AI layer itself
//   - pkg/tools/... — builtin tools that bridge into the AI layer
//   - cmd/...      — the composition root
//   - e2e/...      — end-to-end entry points
//
// Any other package that imports `pkg/ai/...` is a violation and fails the
// test. The check is fail-closed: the allowlist names what is permitted, so
// a brand-new package that imports `pkg/ai` is rejected by default rather
// than silently accepted.
//
// Boundary of this check:
//   - Only production code is scanned. `_test.go` files are skipped by
//     convention — same as the operator-name drift guard, which also does
//     not scan tests (a test may legitimately reach into a layer to assert
//     on it; the shipped dependency graph is what we are pinning).
//   - Only imports inside this module are considered.
//   - Imports are collected from every parsed non-test .go file regardless
//     of build constraints, so a platform-gated reverse edge still counts.
//   - Files that fail to parse are reported as errors, not skipped silently.

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aiAllowedImporterPrefixes lists the module-relative directory prefixes
// whose packages are permitted to import `pkg/ai/...`. Everything else is a
// reverse edge. Keep this list short and justified — adding an entry loosens
// the boundary.
var aiAllowedImporterPrefixes = []string{
	"pkg/ai",    // the AI layer itself
	"pkg/tools", // builtin tools that bridge into the AI layer
	"cmd",       // composition root
	"e2e",       // end-to-end entry points
}

func TestPkgAIIsOnlyImportedByAllowedLayers(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	mod := readModulePath(t, root)
	aiPrefix := mod + "/pkg/ai"

	// Each violation is rendered as "<importer> -> <pkg/ai/...>".
	var violations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".workbuddy-ai", "node_modules", "web", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		// Production code only: tests are excluded by convention.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, raw, parser.ImportsOnly)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}

		importer := importerDirRel(root, path)
		if importer == "" {
			return nil // outside the module
		}
		if aiImporterAllowed(importer) {
			return nil
		}

		for _, imp := range file.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			if p == aiPrefix || strings.HasPrefix(p, aiPrefix+"/") {
				imported := strings.TrimPrefix(p, mod+"/")
				violations = append(violations, importer+" -> "+imported)
			}
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(violations)
	assert.Empty(t, violations,
		"pkg/ai 只能被这些前缀的包导入（fail-closed 白名单：%v）。"+
			"下面这些 import 构成了指向 AI 层的反向边，必须消除"+
			"（把被依赖的代码归位到 core 侧，或把消费者上移到允许的层）：\n%s",
		aiAllowedImporterPrefixes, strings.Join(violations, "\n"))
}

// importerDirRel returns the module-relative (slash-separated) directory of
// the package that filePath belongs to, e.g. "pkg/strategy/expression". It
// returns "" when the file lives outside the module root.
func importerDirRel(root, filePath string) string {
	rel, err := filepath.Rel(root, filepath.Dir(filePath))
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "."
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}

// aiImporterAllowed reports whether the module-relative package directory is
// permitted to import pkg/ai/... .
func aiImporterAllowed(rel string) bool {
	for _, prefix := range aiAllowedImporterPrefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}
