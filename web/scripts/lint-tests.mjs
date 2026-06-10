#!/usr/bin/env node
// scripts/lint-tests.mjs — F2-new (ODR-012) standalone lint
//
// Scans every `*.test.ts` under `web/src` for the vitest
// `expect(value).toBe(expected, "message")` misuse pattern.
//
// Why this exists separately from src/test-lint.test.ts:
//
//  - The test in test-lint.test.ts is a *runtime* guard. It runs as
//    part of `npm test` and is the fallback for CI environments that
//    don't have a separate lint step. The downside is that the full
//    vitest runtime must spin up before the check fires.
//
//  - This script is a *static* check. It runs in <100ms on the
//    project and surfaces the same findings with line numbers and
//    pre-formatted error messages — so it can sit in front of `npm
//    test` as a fast gate (and be wired into a future pre-commit
//    hook without a vitest dependency).
//
// The two implementations are kept in sync by sharing the same
// regex constant. If you change one, change the other.
//
// Exit code: 0 = clean, 1 = at least one misuse found.

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)
const WEB_ROOT = join(__dirname, '..')
const SRC_ROOT = join(WEB_ROOT, 'src')

// Single source of truth for the misuse regex. Mirrors the one in
// src/test-lint.test.ts; do not let them diverge.
const TEST_FILE_GLOB = /\.test\.ts$/
const MISUSE_PATTERN = /expect\([^)]*\)\.toBe\([^)]*,\s*['"`]/

// Files that legitimately contain the regex literal in a string
// (e.g. the test that scans for the pattern itself) and would
// otherwise be a false positive.
const SELF_EXCLUDE = new Set(['test-lint.test.ts'])

function listTestFiles(root) {
  const out = []
  for (const entry of readdirSync(root)) {
    const full = join(root, entry)
    const st = statSync(full)
    if (st.isDirectory()) {
      if (entry === 'node_modules' || entry === 'dist' || entry.startsWith('.')) continue
      out.push(...listTestFiles(full))
    } else if (TEST_FILE_GLOB.test(entry) && !SELF_EXCLUDE.has(entry)) {
      out.push(full)
    }
  }
  return out
}

function findMisuses(file) {
  const src = readFileSync(file, 'utf8')
  const lines = src.split('\n')
  const hits = []
  lines.forEach((text, i) => {
    if (MISUSE_PATTERN.test(text)) {
      hits.push({ line: i + 1, text: text.trim() })
    }
  })
  return hits
}

const files = listTestFiles(SRC_ROOT)
let totalMisuses = 0
const fileReports = []

for (const file of files) {
  const misuses = findMisuses(file)
  if (misuses.length > 0) {
    totalMisuses += misuses.length
    fileReports.push({ file, misuses })
  }
}

if (totalMisuses === 0) {
  console.log(
    `lint-tests: scanned ${files.length} test file(s), no toBe(expected, "message") misuse found.`,
  )
  process.exit(0)
}

console.error(
  `lint-tests: found ${totalMisuses} vitest toBe() misuse(s) across ` +
    `${fileReports.length} file(s). toBe() is a single-arg matcher; the ` +
    `second argument is silently dropped, which masks test failures.\n` +
    `Use expect(actual).toBe(expected) or move the message into a ` +
    `t.run() label / a comment instead.\n`,
)
for (const { file, misuses } of fileReports) {
  const rel = file.startsWith(WEB_ROOT + '/') ? file.slice(WEB_ROOT.length + 1) : file
  console.error(`  ${rel}`)
  for (const m of misuses) {
    console.error(`    line ${m.line}: ${m.text}`)
  }
  console.error('')
}
process.exit(1)
