package logging

import (
	"regexp"
	"strings"
)

// sensitiveKeys is the set of map keys whose values should be masked when
// logging structured fields. Matching is case-insensitive.
var sensitiveKeys = map[string]bool{
	"api_key":      true,
	"apikey":       true,
	"api_secret":   true,
	"apisecret":    true,
	"secret":       true,
	"password":     true,
	"passwd":       true,
	"token":        true,
	"access_token": true,
	"accesstoken":  true,
	"refresh_token": true,
	"private_key":  true,
	"privatekey":   true,
	"credential":   true,
	"credentials":  true,
}

// accountNumberPattern matches common account-number shapes:
// 16+ digit sequences (credit-card-like) and 8-12 digit sequences (bank accounts).
var accountNumberPattern = regexp.MustCompile(`\b\d{8,19}\b`)

// apiKeyPattern matches common API key shapes: "sk-..." style keys and
// long alphanumeric tokens (32+ chars).
var apiKeyPattern = regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{16,}|[a-zA-Z0-9]{32,})`)

// MaskAPIKey masks an API key, preserving only the first `prefix` and last
// `suffix` characters. If the key is too short to mask meaningfully it is
// fully replaced with asterisks.
func MaskAPIKey(key string) string {
	return MaskString(key, 4, 4)
}

// MaskAccountNumber masks a bank/card account number, preserving only the
// last 4 digits. Short numbers (<=4 chars) are fully masked.
func MaskAccountNumber(num string) string {
	num = strings.TrimSpace(num)
	if len(num) <= 4 {
		return strings.Repeat("*", len(num))
	}
	// Keep only the last 4 digits visible.
	prefix := num[:len(num)-4]
	return strings.Repeat("*", len(prefix)) + num[len(num)-4:]
}

// MaskString masks the middle of a string, preserving the first `prefix`
// and last `suffix` characters. If the string is too short, it is fully
// replaced with asterisks of the same length.
func MaskString(s string, prefix, suffix int) string {
	s = strings.TrimSpace(s)
	if prefix < 0 {
		prefix = 0
	}
	if suffix < 0 {
		suffix = 0
	}
	if len(s) <= prefix+suffix {
		return strings.Repeat("*", len(s))
	}
	return s[:prefix] + strings.Repeat("*", len(s)-prefix-suffix) + s[len(s)-suffix:]
}

// MaskSensitiveValue scans a string for API keys and account numbers and
// replaces them with masked versions. Non-sensitive content is left intact.
func MaskSensitiveValue(s string) string {
	// Mask API-key-like tokens first.
	s = apiKeyPattern.ReplaceAllStringFunc(s, func(match string) string {
		return MaskAPIKey(match)
	})
	// Then mask account-number-like digit sequences.
	s = accountNumberPattern.ReplaceAllStringFunc(s, func(match string) string {
		return MaskAccountNumber(match)
	})
	return s
}

// MaskMap returns a shallow copy of m with sensitive values masked.
// Keys are matched case-insensitively against the sensitiveKeys set.
// For sensitive keys, the entire string value is masked (preserving a
// small prefix/suffix). Non-string values are passed through unchanged.
func MaskMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitiveKey(k) {
			if s, ok := v.(string); ok {
				out[k] = MaskAPIKey(s)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// isSensitiveKey reports whether k (case-insensitive, trimmed) is a known
// sensitive key.
func isSensitiveKey(k string) bool {
	return sensitiveKeys[strings.ToLower(strings.TrimSpace(k))]
}
