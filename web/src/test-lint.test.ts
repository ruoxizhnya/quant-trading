import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { join } from 'path'

// F2-new (ODR-012) regression guard.
//
// Background: vitest 4's `expect(value, message)` accepts an optional
// message, but `expect(value).toBe(expected, message)` does NOT —
// `toBe` is a single-arg matcher. The second argument is silently
// dropped, which masks real test failures: a typo'd expected value
// would produce a useless "expected X to be Y" error instead of the
// helpful "should have been called with X (but received Y)" message
// the author intended.
//
// Two such misuses slipped into the codebase before the original
// fix (see docs/odr/odr-012-comprehensive-code-review.md §F2-new).
// This file is the *runtime* half of a two-layer guard:
//
//  1. scripts/lint-tests.mjs — a fast standalone Node script that
//     scans the same files with the same regex and is wired into
//     `npm test` as `lint:tests`. Fails in <100ms without booting
//     vitest. This is the primary CI gate.
//
//  2. This file — a vitest test that runs the same scan inside the
//     vitest runtime. Belt-and-braces for CI environments that
//     skip the `lint:tests` step (e.g. `vitest run` invoked
//     directly) and as a way to fail loud if the script's regex
//     and this file's regex ever drift apart (see the shared
//     MISUSE_PATTERN constant below — both must stay in sync).
//
// Pattern: `expect(<expr>).toBe(<expr>, <string-literal>)` where the
// second argument is a quoted string. We allow `, <non-string>` and
// `, <number>` etc. to keep the matcher simple and avoid false
// positives on multi-line `toBe` calls.

const TEST_FILE_GLOB = /\.test\.ts$/
const MISUSE_PATTERN = /expect\([^)]*\)\.toBe\([^)]*,\s*['"`]/

function listTestFiles(root: string): string[] {
  const out: string[] = []
  for (const entry of readdirSync(root)) {
    const full = join(root, entry)
    const st = statSync(full)
    if (st.isDirectory()) {
      // Skip node_modules + dist (defence in depth — vitest config
      // already excludes these, but lint at scan time too).
      if (entry === 'node_modules' || entry === 'dist' || entry.startsWith('.')) continue
      out.push(...listTestFiles(full))
    } else if (TEST_FILE_GLOB.test(entry)) {
      out.push(full)
    }
  }
  return out
}

function findMisuses(file: string): { line: number; text: string }[] {
  const src = readFileSync(file, 'utf8')
  const lines = src.split('\n')
  const hits: { line: number; text: string }[] = []
  lines.forEach((text, i) => {
    if (MISUSE_PATTERN.test(text)) {
      hits.push({ line: i + 1, text: text.trim() })
    }
  })
  return hits
}

describe('test-file lint — F2-new regression guard', () => {
  const root = join(__dirname, '..')
  const testFiles = listTestFiles(root)
  // Self-exclude: this file itself contains the regex literal in a
  // string, which would be a false positive.
  const external = testFiles.filter(f => !f.endsWith('test-lint.test.ts'))

  it('discovers at least the known test files (sanity)', () => {
    // If this ever returns 0, the scan logic is broken — fail loud.
    expect(external.length).toBeGreaterThan(5)
  })

  it.each(external)('has no toBe(expected, "message") misuse: %s', (file) => {
    const misuses = findMisuses(file)
    if (misuses.length > 0) {
      const formatted = misuses.map(m => `  line ${m.line}: ${m.text}`).join('\n')
      throw new Error(
        `Found ${misuses.length} vitest toBe() misuse(s) in ${file}.\n` +
          `toBe() is a single-arg matcher; the second argument is silently\n` +
          `dropped, which masks test failures. Use expect(actual).toBe(expected)\n` +
          `or move the message into a t.run() label / a comment instead.\n` +
          `\n${formatted}\n`,
      )
    }
    expect(misuses).toEqual([])
  })
})
