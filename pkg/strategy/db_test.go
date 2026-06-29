package strategy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDB_SourceHasNoSwallowedUnmarshal is a regression guard for
// S7-P0-6 (ODR-043): db.go must not swallow json.Unmarshal errors via
// `_ = json.Unmarshal(...)`. A corrupt strategy params blob must surface
// as an error, not silently yield a zero-parameter strategy.
func TestDB_SourceHasNoSwallowedUnmarshal(t *testing.T) {
	source, err := os.ReadFile("db.go")
	require.NoError(t, err)
	assert.NotContains(t, string(source), "_ = json.Unmarshal",
		"db.go must not swallow json.Unmarshal errors (S7-P0-6)")
}
