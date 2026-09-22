package staticcheck

// Tests for the Stage 1 regex gate (Sprint 6 P0-4 / ODR-013).
//
// The point of this file is not "the regexes compile" — it is to pin the
// gate's **capability boundary** as executable evidence. The package doc
// claims certain things are caught and certain things are not; those claims
// were made in prose and never checked (the package had no tests at all).
//
// Two directions matter and both are pinned:
//
//   - the known-bad literals ARE caught → the gate is not decoration;
//   - the documented bypasses ARE NOT caught → the gate is not a boundary.
//
// If a future go/ast or gosec pass closes one of the bypasses below, this
// file will fail. That is intended: the threat-model comment in the package
// doc must be updated in the same change, otherwise it starts lying in the
// safe direction ("we don't catch X" when we now do).

import (
	"strings"
	"testing"
)

// TestCheck_CatchesKnownBadLiterals pins the direction that makes the gate
// worth having: an LLM that writes the dangerous call the ordinary way is
// rejected, with the pattern named so the rejection is explainable.
func TestCheck_CatchesKnownBadLiterals(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		code    string
	}{
		{"os.RemoveAll", "os.RemoveAll", `_ = os.RemoveAll("/tmp/x")`},
		{"os.Remove", "os.Remove", `_ = os.Remove("/tmp/x")`},
		{"exec.Command", "exec.Command", `cmd := exec.Command("sh", "-c", "rm -rf /")`},
		{"exec.CommandContext", "exec.Command", `cmd := exec.CommandContext(ctx, "sh")`},
		{"os.StartProcess", "os.StartProcess", `_, _ = os.StartProcess("/bin/sh", nil, nil)`},
		{"syscall.Exec", "syscall.Exec", `_ = syscall.Exec("/bin/sh", nil, nil)`},
		{"net.Dial", "net.Dial", `c, _ := net.Dial("tcp", "evil.example:80")`},
		{"http.Get", "http.Get/Post", `_, _ = http.Get("http://evil.example")`},
		{"http.Post", "http.Get/Post", `_, _ = http.Post("http://evil.example", "", nil)`},
		{"websocket.Dial", "websocket.Dial", `_, _, _ = websocket.Dial("ws://evil.example", "", "http://x")`},
		{"os.Exit", "os.Exit", `os.Exit(1)`},
		{"panic", "panic", `panic("boom")`},
		{"unsafe.Pointer", "unsafe.Pointer", `p := unsafe.Pointer(&x)`},
		{"syscall.RawSyscall", "syscall.RawSyscall", `_, _, _ = syscall.RawSyscall(0, 0, 0, 0)`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.code)
			if len(findings) == 0 {
				t.Fatalf("Check(%q) found nothing, want at least one finding", tc.code)
			}
			var names []string
			for _, f := range findings {
				names = append(names, f.Pattern)
				if f.Line != 1 {
					t.Errorf("finding %s: Line = %d, want 1", f.Pattern, f.Line)
				}
				if f.Excerpt == "" {
					t.Errorf("finding %s: Excerpt is empty", f.Pattern)
				}
			}
			if !contains(names, tc.pattern) {
				t.Errorf("findings = %v, want one named %q", names, tc.pattern)
			}
		})
	}
}

// TestCheck_IgnoresComments pins the comment stripping in scanLineByLine: a
// strategy that merely *mentions* a forbidden call in prose must not be
// rejected. This is a Stage 1 heuristic (no go/ast comment map), so it is
// worth having a test.
func TestCheck_IgnoresComments(t *testing.T) {
	code := strings.Join([]string{
		"package strategy",
		"",
		"// we deliberately do not call os.RemoveAll here",
		"// nor exec.Command(anything)",
		"func GenerateSignals() error { return nil }",
	}, "\n")

	if findings := Check(code); len(findings) != 0 {
		t.Errorf("Check found %v in commented prose, want none", findings)
	}
}

// TestCheck_FalsePositiveOnStringLiterals documents the gate's asymmetry:
// comments are stripped, but **string literals are not**. A strategy that
// merely puts the dangerous text in a string is rejected. That is the right
// trade for a fail-closed gate (a false positive costs one rejected build, a
// false negative costs a host), but it should be known rather than discovered
// by someone whose legitimate strategy gets rejected.
func TestCheck_FalsePositiveOnStringLiterals(t *testing.T) {
	code := `package s
func GenerateSignals() error {
	_ = "never call os.RemoveAll() in a strategy"
	return nil
}`

	findings := Check(code)
	if len(findings) == 0 {
		t.Fatal("a string literal mentioning os.RemoveAll was not flagged; " +
			"if comment stripping was extended to string literals, update this test " +
			"and the package doc")
	}
}

// TestCheck_DocumentedBypasses pins the *other* direction: the gate is a
// regex over source text, so anything that hides the literal identifier gets
// through. This is what the threat model says — "not a security boundary" —
// and it is the reason the package doc tells readers not to rely on it alone.
//
// Each case is a real Go construct, not a hypothetical: all four compile.
func TestCheck_DocumentedBypasses(t *testing.T) {
	cases := []struct {
		name string
		code string
		why  string
	}{
		{
			name: "aliased import",
			code: `package s
import fs "os"
func GenerateSignals() error { return fs.RemoveAll("/tmp") }`,
			why: `the regex looks for the literal "os.RemoveAll("; an alias renames the selector`,
		},
		{
			name: "indirect call through a variable",
			code: `package s
import "os"
func GenerateSignals() error { rm := os.RemoveAll; return rm("/tmp") }`,
			why: "the identifier appears at the assignment, not at the call; both are legal Go",
		},
		{
			name: "dot import",
			code: `package s
import . "os"
func GenerateSignals() error { return RemoveAll("/tmp") }`,
			why: "a dot import drops the package qualifier entirely",
		},
		{
			name: "call split across lines",
			code: `package s
import "os"
func GenerateSignals() error {
	rm := os.
		RemoveAll
	return rm("/tmp")
}`,
			why: "the scan is line-by-line (that is how Line stays exact), so a selector split " +
				"by a newline is never seen as one token — a gap this implementation added " +
				"in exchange for accurate line numbers",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := Check(tc.code); len(findings) != 0 {
				t.Errorf("Check caught %v — the documented bypass (%s) no longer applies; "+
					"update the threat model in the package doc in the same change", findings, tc.why)
			}
		})
	}
}

// TestCheckOrError_FailClosed pins the caller contract: no findings means no
// error, and any finding produces an error that names the pattern. A gate
// that returns findings but no error would be silently advisory.
func TestCheckOrError_FailClosed(t *testing.T) {
	if err := CheckOrError("package s\nfunc GenerateSignals() error { return nil }\n"); err != nil {
		t.Errorf("clean code produced an error: %v", err)
	}

	err := CheckOrError("package s\nfunc GenerateSignals() error { os.Exit(1); return nil }\n")
	if err == nil {
		t.Fatal("dangerous code produced no error")
	}
	if !strings.Contains(err.Error(), "os.Exit") {
		t.Errorf("error = %v, want it to name the matched pattern", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %v, want the 1-indexed line number", err)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
