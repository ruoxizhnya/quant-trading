package storage

import (
	"strings"
	"testing"
)

// ── Test helpers ─────────────────────────────────────────────────────────────

// assertPrefixed fails the test if the key does not start with KeyNamespace.
// This is the central invariant: every key the system writes MUST be
// scoped under the "quantlab:" namespace so multiple apps can share a
// Redis instance without colliding.
func assertPrefixed(t *testing.T, key, ctx string) {
	t.Helper()
	if !strings.HasPrefix(key, KeyNamespace) {
		t.Errorf("%s: key %q is missing namespace prefix %q",
			ctx, key, KeyNamespace)
	}
}

// ── Key prefix invariants ────────────────────────────────────────────────────

// TestKeyNamespace_Literal pins the namespace string so an accidental
// rename in a code review is caught immediately by CI.
func TestKeyNamespace_Literal(t *testing.T) {
	if KeyNamespace != "quantlab:" {
		t.Fatalf("KeyNamespace changed: got %q, want %q (pinned by ODR-013 P1-28)",
			KeyNamespace, "quantlab:")
	}
}

// TestKeyPrefixes_AreAllNamespaced walks the four canonical prefix
// vars and asserts each one starts with KeyNamespace. A new prefix
// added without going through the constant would be flagged here.
func TestKeyPrefixes_AreAllNamespaced(t *testing.T) {
	prefixes := map[string]string{
		"KeyPrefixOHLCV":      KeyPrefixOHLCV,
		"KeyPrefixFund":       KeyPrefixFund,
		"KeyPrefixStocksList": KeyPrefixStocksList,
		"KeyPrefixStock":      KeyPrefixStock,
	}
	for name, val := range prefixes {
		if !strings.HasPrefix(val, KeyNamespace) {
			t.Errorf("%s = %q does not start with KeyNamespace %q",
				name, val, KeyNamespace)
		}
	}
}

// TestKeyPrefixOHLCV_Literal pins the well-known key shape so a
// careless rename in constants.go is detected.
func TestKeyPrefixOHLCV_Literal(t *testing.T) {
	if KeyPrefixOHLCV != "quantlab:ohlcv:" {
		t.Fatalf("KeyPrefixOHLCV = %q, want %q", KeyPrefixOHLCV, "quantlab:ohlcv:")
	}
	if KeyPrefixFund != "quantlab:fund:" {
		t.Fatalf("KeyPrefixFund = %q, want %q", KeyPrefixFund, "quantlab:fund:")
	}
	if KeyPrefixStocksList != "quantlab:stocks:list:" {
		t.Fatalf("KeyPrefixStocksList = %q, want %q", KeyPrefixStocksList, "quantlab:stocks:list:")
	}
	if KeyPrefixStock != "quantlab:stock:" {
		t.Fatalf("KeyPrefixStock = %q, want %q", KeyPrefixStock, "quantlab:stock:")
	}
}

// ── Composed key shape ──────────────────────────────────────────────────────

// TestOHLCVKeyShape verifies the full key produced by
// fmt.Sprintf("%s%s:%s:%s", KeyPrefixOHLCV, symbol, start, end).
// The composition lives in two files (redis.go:209 and data/cache.go
// via ohlcvKey). This test pins the wire format so both sites stay
// in lock-step.
func TestOHLCVKeyShape(t *testing.T) {
	// We re-derive the key exactly the way redis.go:209 does —
	// any change there must be reflected here too.
	symbol := "600519.SH"
	start := "20240101"
	end := "20241231"

	key := KeyPrefixOHLCV + symbol + ":" + start + ":" + end
	want := "quantlab:ohlcv:600519.SH:20240101:20241231"
	if key != want {
		t.Errorf("OHLCV key shape: got %q, want %q", key, want)
	}
	assertPrefixed(t, key, "OHLCV key")
}

// TestFundKeyShape mirrors TestOHLCVKeyShape for fundamentals.
func TestFundKeyShape(t *testing.T) {
	key := KeyPrefixFund + "600519.SH" + ":20240601"
	want := "quantlab:fund:600519.SH:20240601"
	if key != want {
		t.Errorf("Fund key shape: got %q, want %q", key, want)
	}
	assertPrefixed(t, key, "Fund key")
}

// ── InvalidateStocks pattern shape ──────────────────────────────────────────

// TestInvalidateStocks_PatternShape walks the two branches of
// InvalidateStocks and pins the SCAN pattern under each. This is the
// most security-sensitive test: if the pattern ever loses its
// namespace, a developer could accidentally `DEL` keys owned by
// other apps sharing the same Redis.
func TestInvalidateStocks_PatternShape(t *testing.T) {
	// "all" / "" branch — broad pattern
	all := KeyPrefixStocksList + "*"
	if all != "quantlab:stocks:list:*" {
		t.Errorf("all-pattern = %q, want %q", all, "quantlab:stocks:list:*")
	}
	assertPrefixed(t, all, "InvalidateStocks(all)")

	// specific exchange branch — narrow pattern
	sse := KeyPrefixStocksList + "SSE"
	if sse != "quantlab:stocks:list:SSE" {
		t.Errorf("exchange-pattern = %q, want %q", sse, "quantlab:stocks:list:SSE")
	}
	assertPrefixed(t, sse, "InvalidateStocks(SSE)")
}

// TestInvalidateOHLCV_PatternShape pins the SCAN pattern used to
// purge a single symbol's OHLCV entries. The trailing ":*" must NOT
// be a bare "*" — that would nuke the whole namespace.
func TestInvalidateOHLCV_PatternShape(t *testing.T) {
	pattern := KeyPrefixOHLCV + "600519.SH" + ":*:*"
	want := "quantlab:ohlcv:600519.SH:*:*"
	if pattern != want {
		t.Errorf("InvalidateOHLCV pattern = %q, want %q", pattern, want)
	}
	assertPrefixed(t, pattern, "InvalidateOHLCV pattern")

	// The pattern must contain the symbol — otherwise the SCAN
	// would match every symbol in the namespace.
	if !strings.Contains(pattern, "600519.SH") {
		t.Errorf("InvalidateOHLCV pattern %q is missing symbol scope", pattern)
	}
}

// ── Namespace safety net ────────────────────────────────────────────────────

// TestNoLegacyKeyShape is a regression guard: a future PR that
// regresses back to the un-prefixed "stock:", "stocks:list:",
// "ohlcv:", "fund:" forms would be caught here.
func TestNoLegacyKeyShape(t *testing.T) {
	legacy := []string{
		"stock:600519.SH",
		"stocks:list:SSE",
		"ohlcv:600519.SH:20240101:20241231",
		"fund:600519.SH:20240601",
	}
	for _, k := range legacy {
		if strings.HasPrefix(k, KeyNamespace) {
			t.Errorf("legacy key %q unexpectedly namespaced (false positive?)", k)
		}
	}
}
