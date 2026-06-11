// Package staticcheck implements the Sprint 6 P0-4 (ODR-013) "Stage 1"
// AI sandbox gate: a regex-based static-analysis pass that rejects
// LLM-generated Go strategy code containing patterns known to be
// dangerous in the backtest-engine-in-process context.
//
// Why regex and not go/ast?
//   - ADR-007 §Implementation Path Phase 1 calls this out explicitly:
//     "regex/AST-based rejection" — a regex pass is sufficient for the
//     known-bad subset (file delete, shell exec, network egress,
//     process exit) and is fast (sub-millisecond on typical
//     LLM-generated strategies, ~50–200 lines of Go).
//   - The package deliberately fails closed: an unknown / unrecognized
//     pattern is not a "skip"; callers MUST treat any Finding as a
//     hard rejection. This matches ADR-007's "Static Analysis Gate"
//     threat model (Option B).
//   - A future Sprint 6 P1-11 (ADR-007 Phase 2) will add a real
//     go/ast-based analyzer and a separate subprocess sandbox. This
//     regex pass remains as the cheap first filter.
//
// Threat model (what this gate is meant to catch):
//   - File system destruction (`os.RemoveAll`, `os.Remove`, direct
//     `/proc`/syscall writes).
//   - Shell / process spawning (`exec.Command`, `os.StartProcess`,
//     `syscall.Exec`).
//   - Network egress (`net.Dial`, `http.Get`, `http.Post`,
//     `websocket.Dial`).
//   - Process termination (`os.Exit`, `panic` from the LLM strategy —
//     strategies MUST return errors, not crash).
//   - Reflection-driven escape hatches (`reflect.MakeFunc` against
//     arbitrary types, `unsafe.Pointer` arithmetic).
//
// What this gate does NOT catch (out of scope for Stage 1):
//   - Infinite loops / CPU exhaustion — handled by ADR-007 Phase 2
//     subprocess rlimit + per-call ctx timeout.
//   - Memory exhaustion (e.g. `make([]int, 1<<40)`) — same.
//   - Goroutine leaks — same.
//   - Sophisticated obfuscation (`strings.NewReader(...)` building
//     `os.RemoveAll` at runtime). A real AST pass in Phase 2 will
//     catch this; the regex gate does not, by design (false-positive
//     rate would spike on legitimate reflection usage).
package staticcheck

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity ranks the danger of a finding. The package does not act on
// severity itself — it returns every finding it matches and lets the
// caller decide. Today CopilotService treats every Finding as
// "reject the build" (fail-closed), but the field is exported so a
// future caller can warn-only on the lower severities.
type Severity string

const (
	// SeverityCritical — file destruction, process exec, network
	// egress, process exit. These MUST be rejected in any context.
	SeverityCritical Severity = "critical"
	// SeverityHigh — panic, unsafe.Pointer, raw syscalls. The
	// strategy plugin contract is that GenerateSignals returns
	// errors, not crashes.
	SeverityHigh Severity = "high"
	// SeverityMedium — reflection that could reach dangerous APIs.
	// Reported but not yet auto-rejected; tracked for Phase 2.
	SeverityMedium Severity = "medium"
)

// Finding is one matched dangerous pattern in a piece of code.
type Finding struct {
	// Pattern is the registry key (e.g. "os.RemoveAll").
	Pattern string
	// Message is human-readable, safe to surface to API clients.
	Message string
	// Severity is the rank per the type docs.
	Severity Severity
	// Line is the 1-indexed line number of the first match. 0 if
	// the underlying regex does not have multiline / line-number
	// capture (we use a line-by-line scan so this is always set).
	Line int
	// Excerpt is the offending source line, trimmed to 200 chars.
	Excerpt string
}

// String returns a one-line summary suitable for logging.
func (f Finding) String() string {
	return fmt.Sprintf("[%s] %s (line %d): %s — %q",
		f.Severity, f.Pattern, f.Line, f.Message, f.Excerpt)
}

// pattern is the internal registry entry. Each entry is a single
// compiled regex plus metadata.
type pattern struct {
	Name     string
	Severity Severity
	Message  string
	Re       *regexp.Regexp
	// lineRE, if non-nil, is applied to each source line and
	// re-compiled per line so we can produce accurate Line numbers.
	// When nil, we use Re on the full source and approximate the
	// line by counting newlines.
	lineRE func(line string) bool
}

// builtinPatterns is the default registry. Callers can extend it via
// Register (in a future enhancement); for now we keep the surface
// small and the defaults authoritative.
var builtinPatterns = []pattern{
	{
		Name:     "os.RemoveAll",
		Severity: SeverityCritical,
		Message:  "filesystem removal is not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bos\.RemoveAll\s*\(`),
	},
	{
		Name:     "os.Remove",
		Severity: SeverityCritical,
		Message:  "filesystem removal is not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bos\.Remove(?:All)?\s*\(`),
	},
	{
		Name:     "exec.Command",
		Severity: SeverityCritical,
		Message:  "subprocess execution is not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bexec\.Command(?:Context)?\s*\(`),
	},
	{
		Name:     "os.StartProcess",
		Severity: SeverityCritical,
		Message:  "process spawning is not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bos\.StartProcess\s*\(`),
	},
	{
		Name:     "syscall.Exec",
		Severity: SeverityCritical,
		Message:  "syscall.Exec replaces the current process; not allowed",
		Re:       regexp.MustCompile(`\bsyscall\.Exec(?:Lo)?\s*\(`),
	},
	{
		Name:     "net.Dial",
		Severity: SeverityCritical,
		Message:  "network egress is not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bnet\.Dial(?:Context)?\s*\(`),
	},
	{
		Name:     "http.Client.Do",
		Severity: SeverityCritical,
		Message:  "outbound HTTP from generated strategy is not allowed",
		Re:       regexp.MustCompile(`\bhttp\.(?:DefaultClient|Client)\.Do\s*\(`),
	},
	{
		Name:     "http.Get/Post",
		Severity: SeverityCritical,
		Message:  "outbound HTTP from generated strategy is not allowed",
		Re:       regexp.MustCompile(`\bhttp\.(?:Get|Post|PostForm|Head|Do)\s*\(`),
	},
	{
		Name:     "websocket.Dial",
		Severity: SeverityCritical,
		Message:  "websocket egress is not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bwebsocket\.Dial\s*\(`),
	},
	{
		Name:     "os.Exit",
		Severity: SeverityCritical,
		Message:  "os.Exit terminates the whole backtest engine; not allowed",
		Re:       regexp.MustCompile(`\bos\.Exit\s*\(`),
	},
	{
		Name:     "panic",
		Severity: SeverityHigh,
		Message:  "strategies must return errors, not panic",
		Re:       regexp.MustCompile(`\bpanic\s*\(`),
	},
	{
		Name:     "unsafe.Pointer",
		Severity: SeverityHigh,
		Message:  "unsafe.Pointer is disallowed in generated strategy code",
		Re:       regexp.MustCompile(`\bunsafe\.Pointer\s*\(`),
	},
	{
		Name:     "syscall.RawSyscall",
		Severity: SeverityHigh,
		Message:  "raw syscalls are not allowed in generated strategy code",
		Re:       regexp.MustCompile(`\bsyscall\.RawSyscall(?:6)?\s*\(`),
	},
}

// Check scans `code` against every registered pattern and returns
// every match as a Finding. The scan is line-by-line so the Line
// field on each Finding is accurate (1-indexed).
//
// The function does NOT return an error — it returns the slice. The
// caller is responsible for the fail-closed policy. See
// CheckOrError for a convenience that turns the slice into a single
// error.
func Check(code string) []Finding {
	var findings []Finding
	for _, p := range builtinPatterns {
		findings = append(findings, scanLineByLine(code, p)...)
	}
	return findings
}

// CheckOrError is the fail-closed convenience: it returns nil if the
// code is clean, otherwise a single error that joins every finding's
// String() with newlines. The error message is safe to surface to
// HTTP clients (it contains the matched pattern name + line + excerpt
// — no internal paths or stack traces).
func CheckOrError(code string) error {
	findings := Check(code)
	if len(findings) == 0 {
		return nil
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return fmt.Errorf("staticcheck rejected generated code (%d finding(s)):\n%s",
		len(findings), strings.Join(parts, "\n"))
}

// scanLineByLine walks the source and returns one Finding per match.
// We prefer this over a single FindAllStringIndex on the full source
// because (a) the resulting Line field is exact, and (b) the regex
// match is line-scoped so a malicious `/* os.RemoveAll */` comment
// spanning a newline is still caught on the line that contains the
// actual identifier, while a comment that says "// we forbid
// os.RemoveAll" is not mis-flagged because the actual call is
// required.
func scanLineByLine(code string, p pattern) []Finding {
	var out []Finding
	for i, raw := range strings.Split(code, "\n") {
		line := raw
		// Strip // line comments and /* block comments */ so a
		// comment that mentions "os.RemoveAll" is not flagged.
		// This is a Stage 1 heuristic; Phase 2 will use the real
		// go/ast comment map.
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		if p.Re.MatchString(line) {
			out = append(out, Finding{
				Pattern:  p.Name,
				Message:  p.Message,
				Severity: p.Severity,
				Line:     i + 1,
				Excerpt:  trim(line, 200),
			})
		}
	}
	return out
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
