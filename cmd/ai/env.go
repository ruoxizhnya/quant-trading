package main

import (
	"os"
	"strconv"
)

// envInt reads an integer from an environment variable, returning
// `def` if the variable is unset or unparseable. Used by main() to
// pick up rate-limit knobs (AI_RATE_LIMIT_PER_MIN, AI_RATE_LIMIT_BURST)
// without pulling in viper for the AI service, which has no other
// configuration surface today.
func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}
