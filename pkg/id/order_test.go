package id

import (
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// uuidV7Pattern is the standard 8-4-4-4-12 form. We re-validate
// via a regex rather than the package's own parser so the
// test pin doesn't depend on uuid package internals.
var uuidV7Pattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestOrderID_IsValidV7(t *testing.T) {
	id := OrderID()
	if !uuidV7Pattern.MatchString(id) {
		t.Errorf("OrderID must be canonical UUIDv7 form; got %q", id)
	}

	// Cross-check via the package's own parser.
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("uuid.Parse on OrderID output: %v", err)
	}
	if v := u.Version(); v != 7 {
		t.Errorf("OrderID version = %d, want 7 (UUIDv7)", v)
	}
}

func TestJobID_IsValidV7(t *testing.T) {
	id := JobID()
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("uuid.Parse on JobID output: %v", err)
	}
	if v := u.Version(); v != 7 {
		t.Errorf("JobID version = %d, want 7", v)
	}
}

func TestSubscriptionID_IsValidV7(t *testing.T) {
	id := SubscriptionID()
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("uuid.Parse on SubscriptionID output: %v", err)
	}
	if v := u.Version(); v != 7 {
		t.Errorf("SubscriptionID version = %d, want 7", v)
	}
}

// TestOrderIDsAreTimeOrdered is the canonical v7 property
// being sold to callers: lexicographic sort == chronological
// sort. This is the win we cite in the package docstring
// ("B-tree inserts append to the rightmost page").
func TestOrderIDsAreTimeOrdered(t *testing.T) {
	// Generate 5 IDs with a sleep between them so the
	// timestamp bits change. Without the sleep, IDs issued
	// in the same millisecond get the same timestamp prefix
	// and the ordering test is degenerate.
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = OrderID()
		if i < len(ids)-1 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	clone := append([]string(nil), ids...)
	sort.Strings(clone)
	for i := range ids {
		if ids[i] != clone[i] {
			t.Errorf("UUIDv7 ordering broken at index %d: generated=%v sorted=%v",
				i, ids, clone)
			break
		}
	}
}

// TestOrderID_Unique confirms 10k IDs have no collisions.
// v7 has 74 random bits (6 reserved + 16 random in the
// "rand_a" field + 48 in "rand_b" + 6 fixed in "node" per
// RFC 9562 §5.7). The birthday bound for 10k draws is
// ~5×10⁻¹², well below practical concern.
func TestOrderID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 10_000)
	for i := 0; i < 10_000; i++ {
		id := OrderID()
		if _, dup := seen[id]; dup {
			t.Fatalf("UUIDv7 collision after %d draws: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestOrderID_ContainsTimestamp pins the user-visible
// property: extracting the embedded timestamp from a v7 ID
// must yield a recent time. If uuid.NewV7() ever silently
// regresses to a different version, this test catches it.
//
// The UUIDv7 timestamp is the first 48 bits of the 128-bit
// ID, interpreted as a unix-millisecond count (RFC 9562
// §5.7). We decode the bytes by hand rather than relying
// on uuid.Time helpers so the test pin doesn't depend on
// the uuid package's exact helper surface.
func TestOrderID_ContainsTimestamp(t *testing.T) {
	before := time.Now()
	id := OrderID()
	after := time.Now()

	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// First 6 bytes = unix_ms timestamp (big-endian).
	ts := int64(u[0])<<40 | int64(u[1])<<32 |
		int64(u[2])<<24 | int64(u[3])<<16 |
		int64(u[4])<<8 | int64(u[5])
	tsTime := time.UnixMilli(ts)

	if tsTime.Before(before.Add(-time.Second)) || tsTime.After(after.Add(time.Second)) {
		t.Errorf("UUIDv7 timestamp %v outside [%v, %v]", tsTime, before, after)
	}
}

// TestHelpers_UseTheSamePrimitive is a regression guard: if
// someone later changes OrderID to a v4 (or a Snowflake)
// while leaving JobID on v7, the package is fragmented. We
// verify all three helpers produce a v7 by checking the
// version byte on a fresh ID.
func TestHelpers_UseTheSamePrimitive(t *testing.T) {
	for name, fn := range map[string]func() string{
		"OrderID":         OrderID,
		"JobID":           JobID,
		"SubscriptionID":  SubscriptionID,
	} {
		id := fn()
		u, err := uuid.Parse(id)
		if err != nil {
			t.Errorf("%s: parse: %v", name, err)
			continue
		}
		if v := u.Version(); v != 7 {
			t.Errorf("%s version = %d, want 7 (all helpers must agree on v7)", name, v)
		}
	}
}
