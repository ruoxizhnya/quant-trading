package repoguard

// Structural guard: **the AI layer no longer lives in this repo, and core
// must never point at it**.
//
// Why this exists. The dependency graph is one-way: `agent -> core`. The AI
// layer (`pkg/ai` + CopilotService + its composition builders) moved to the
// quant-trading-agent repo on 2026-10-09 (ADR-027 §5 step 8 前置切片，AI 拆仓
// 阶段 2). Before that move, this same guard enforced a whitelist of layers
// allowed to import `pkg/ai` — it caught the pkg/strategy reverse edges that
// motivated S7-P1-2, and it caught internal/bootstrap the day the shared
// builders were created. Now the invariant it pins is stronger and simpler:
//
//  1. `pkg/ai` must NOT exist in this repo (a re-added directory fails CI).
//  2. No production file may import `.../pkg/ai` (belt) ...
//  3. ... nor the agent module at all (suspenders): core is a LIBRARY for
//     agent, never a client of it. The reverse edge would re-create the
//     repo-level cycle that forced the split in the first place.
//
// History worth keeping: the original whitelist version of this guard is in
// git history (2026-10-09 and earlier). Its fail-closed philosophy — name
// what is permitted, reject everything else — carries over: any NEW
// cross-repo dependency must go through a deliberate change here, not a
// quiet import.

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

func TestAILayerIsGoneFromCore(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	mod := readModulePath(t, root)
	aiPrefix := mod + "/pkg/ai"
	agentPrefix := "github.com/ruoxizhnya/quant-trading-agent"

	// 不变量 1：pkg/ai 目录不许回来（有人 re-add 整层 = 退回拆仓前）。
	if _, err := os.Stat(filepath.Join(root, "pkg", "ai")); err == nil {
		t.Errorf("pkg/ai 目录存在于 core 仓 —— AI 层已迁往 quant-trading-agent；" +
			"在这里 re-add 会重新制造仓级环。若确需共享某段 AI 代码，请放到 agent 仓" +
			"或与 core 明确解耦的新包，并在 ADR-027 记录裁决")
	}

	// 不变量 2+3：core 的任何生产代码不得 import pkg/ai 或 agent 模块。
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

		for _, imp := range file.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			if p == aiPrefix || strings.HasPrefix(p, aiPrefix+"/") {
				violations = append(violations, importer+" -> "+strings.TrimPrefix(p, mod+"/")+"（AI 层已迁出 core）")
			}
			if p == agentPrefix || strings.HasPrefix(p, agentPrefix+"/") {
				violations = append(violations, importer+" -> "+p+"（core 不得依赖 agent 仓：依赖方向必须是单向 agent → core）")
			}
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(violations)
	assert.Empty(t, violations,
		"core 对 AI 层的依赖必须为零（AI 拆仓阶段 2 的不变量）。"+
			"下面这些 import 是反向边，必须消除：\n%s",
		strings.Join(violations, "\n"))
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

