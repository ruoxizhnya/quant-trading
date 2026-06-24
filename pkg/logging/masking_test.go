package logging

import (
	"strings"
	"testing"
)

// ---- MaskString ----------------------------------------------------------

func TestMaskString_PreservesPrefixAndSuffix(t *testing.T) {
	got := MaskString("sk-abcdef1234567890", 3, 4)
	if !strings.HasPrefix(got, "sk-") {
		t.Errorf("expected prefix 'sk-', got %q", got)
	}
	if !strings.HasSuffix(got, "7890") {
		t.Errorf("expected suffix '7890', got %q", got)
	}
	if !strings.Contains(got, "*") {
		t.Errorf("expected masked middle, got %q", got)
	}
}

func TestMaskString_TooShortFullyMasked(t *testing.T) {
	got := MaskString("abc", 4, 4)
	if got != "***" {
		t.Errorf("expected '***' for short string, got %q", got)
	}
}

func TestMaskString_ExactLengthFullyMasked(t *testing.T) {
	got := MaskString("abcdef", 3, 3)
	if got != "******" {
		t.Errorf("expected '******' when prefix+suffix==len, got %q", got)
	}
}

func TestMaskString_EmptyString(t *testing.T) {
	got := MaskString("", 4, 4)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestMaskString_NegativePrefixSuffix(t *testing.T) {
	got := MaskString("hello", -1, -1)
	if !strings.Contains(got, "*") {
		t.Errorf("expected masking with negative args, got %q", got)
	}
}

func TestMaskString_OnlyPrefix(t *testing.T) {
	got := MaskString("hello world", 5, 0)
	if got[:5] != "hello" {
		t.Errorf("expected prefix 'hello', got %q", got)
	}
	if !strings.Contains(got[5:], "*") {
		t.Errorf("expected masked suffix, got %q", got)
	}
}

func TestMaskString_OnlySuffix(t *testing.T) {
	got := MaskString("hello world", 0, 5)
	if got[len(got)-5:] != "world" {
		t.Errorf("expected suffix 'world', got %q", got)
	}
	if !strings.Contains(got[:len(got)-5], "*") {
		t.Errorf("expected masked prefix, got %q", got)
	}
}

func TestMaskString_TrimWhitespace(t *testing.T) {
	got := MaskString("  sk-key123  ", 2, 3)
	if !strings.HasPrefix(got, "sk") {
		t.Errorf("expected trimmed prefix 'sk', got %q", got)
	}
}

// ---- MaskAPIKey ----------------------------------------------------------

func TestMaskAPIKey_LongKey(t *testing.T) {
	key := "sk-abcdefghijklmnopqrstuvwxyz1234567890"
	got := MaskAPIKey(key)
	if !strings.HasPrefix(got, "sk-a") {
		t.Errorf("expected prefix 'sk-a', got %q", got)
	}
	if !strings.HasSuffix(got, "7890") {
		t.Errorf("expected suffix '7890', got %q", got)
	}
	if !strings.Contains(got, "*") {
		t.Errorf("expected masked middle, got %q", got)
	}
}

func TestMaskAPIKey_ShortKeyFullyMasked(t *testing.T) {
	got := MaskAPIKey("abc")
	if got != "***" {
		t.Errorf("expected '***', got %q", got)
	}
}

func TestMaskAPIKey_ExactBoundary(t *testing.T) {
	// len == prefix+suffix (4+4=8), should be fully masked.
	got := MaskAPIKey("12345678")
	if got != "********" {
		t.Errorf("expected '********', got %q", got)
	}
}

func TestMaskAPIKey_EmptyKey(t *testing.T) {
	got := MaskAPIKey("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestMaskAPIKey_PreservesLength(t *testing.T) {
	key := "sk-abcdefghijklmnopqrstuvwxyz1234567890"
	got := MaskAPIKey(key)
	if len(got) != len(key) {
		t.Errorf("expected length %d, got %d (masked: %q)", len(key), len(got), got)
	}
}

// ---- MaskAccountNumber --------------------------------------------------

func TestMaskAccountNumber_LongNumber(t *testing.T) {
	got := MaskAccountNumber("1234567890123456")
	if !strings.HasSuffix(got, "3456") {
		t.Errorf("expected suffix '3456', got %q", got)
	}
	if !strings.HasPrefix(got, "****") {
		t.Errorf("expected masked prefix, got %q", got)
	}
}

func TestMaskAccountNumber_ShortNumberFullyMasked(t *testing.T) {
	got := MaskAccountNumber("123")
	if got != "***" {
		t.Errorf("expected '***', got %q", got)
	}
}

func TestMaskAccountNumber_FourDigitsFullyMasked(t *testing.T) {
	got := MaskAccountNumber("1234")
	if got != "****" {
		t.Errorf("expected '****', got %q", got)
	}
}

func TestMaskAccountNumber_FiveDigits(t *testing.T) {
	got := MaskAccountNumber("12345")
	if got != "*2345" {
		t.Errorf("expected '*2345', got %q", got)
	}
}

func TestMaskAccountNumber_Empty(t *testing.T) {
	got := MaskAccountNumber("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestMaskAccountNumber_TrimsWhitespace(t *testing.T) {
	got := MaskAccountNumber("  123456789  ")
	if !strings.HasSuffix(got, "6789") {
		t.Errorf("expected suffix '6789', got %q", got)
	}
}

func TestMaskAccountNumber_PreservesLength(t *testing.T) {
	num := "1234567890123456"
	got := MaskAccountNumber(num)
	if len(got) != len(num) {
		t.Errorf("expected length %d, got %d", len(num), len(got))
	}
}

// ---- MaskSensitiveValue -------------------------------------------------

func TestMaskSensitiveValue_APIKeyPattern(t *testing.T) {
	s := "Using key sk-abcdefghijklmnopqrstuvwxyz1234567890 for auth"
	got := MaskSensitiveValue(s)
	if strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Errorf("expected API key to be masked, got %q", got)
	}
	if !strings.Contains(got, "sk-a") {
		t.Errorf("expected partial prefix preserved, got %q", got)
	}
}

func TestMaskSensitiveValue_AccountNumber(t *testing.T) {
	s := "Account 1234567890123456 charged"
	got := MaskSensitiveValue(s)
	if strings.Contains(got, "1234567890123456") {
		t.Errorf("expected account number masked, got %q", got)
	}
	if !strings.HasSuffix(got, "3456 charged") || !strings.Contains(got, "3456") {
		t.Errorf("expected last 4 digits preserved, got %q", got)
	}
}

func TestMaskSensitiveValue_NoSensitiveData(t *testing.T) {
	s := "This is a normal log message"
	got := MaskSensitiveValue(s)
	if got != s {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestMaskSensitiveValue_MultipleMatches(t *testing.T) {
	s := "key=sk-abcdefghijklmnopqrstuvwxyz1234567890 acct=1234567890123456"
	got := MaskSensitiveValue(s)
	if strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Errorf("expected API key masked, got %q", got)
	}
	if strings.Contains(got, "1234567890123456") {
		t.Errorf("expected account number masked, got %q", got)
	}
}

func TestMaskSensitiveValue_EmptyString(t *testing.T) {
	got := MaskSensitiveValue("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestMaskSensitiveValue_LongAlphanumericToken(t *testing.T) {
	// 32-char alphanumeric token should be masked.
	token := "abcdefghijklmnopqrstuvwxyz123456"
	got := MaskSensitiveValue(token)
	if got == token {
		t.Errorf("expected token masked, got %q", got)
	}
}

// ---- MaskMap -------------------------------------------------------------

func TestMaskMap_MasksSensitiveKeys(t *testing.T) {
	m := map[string]any{
		"api_key":   "sk-abcdefghijklmnopqrstuvwxyz1234567890",
		"password":  "secret123",
		"token":     "tok-abcdefghijklmnopqrstuvwxyz1234567890",
		"user":      "alice",
		"count":     42,
	}
	got := MaskMap(m)
	if got["api_key"].(string) == "sk-abcdefghijklmnopqrstuvwxyz1234567890" {
		t.Error("expected api_key masked")
	}
	if got["password"].(string) == "secret123" {
		t.Error("expected password masked")
	}
	if got["user"] != "alice" {
		t.Errorf("expected non-sensitive value preserved, got %v", got["user"])
	}
	if got["count"] != 42 {
		t.Errorf("expected non-string value preserved, got %v", got["count"])
	}
}

func TestMaskMap_CaseInsensitiveKeys(t *testing.T) {
	m := map[string]any{
		"API_KEY":   "sk-abcdefghijklmnopqrstuvwxyz1234567890",
		"ApiKey":    "sk-abcdefghijklmnopqrstuvwxyz1234567890",
		"api_key":   "sk-abcdefghijklmnopqrstuvwxyz1234567890",
	}
	got := MaskMap(m)
	for _, k := range []string{"API_KEY", "ApiKey", "api_key"} {
		if got[k].(string) == "sk-abcdefghijklmnopqrstuvwxyz1234567890" {
			t.Errorf("expected %s masked", k)
		}
	}
}

func TestMaskMap_NilMap(t *testing.T) {
	got := MaskMap(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestMaskMap_EmptyMap(t *testing.T) {
	got := MaskMap(map[string]any{})
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestMaskMap_DoesNotMutateOriginal(t *testing.T) {
	original := "sk-abcdefghijklmnopqrstuvwxyz1234567890"
	m := map[string]any{"api_key": original}
	_ = MaskMap(m)
	if m["api_key"] != original {
		t.Error("expected original map to be unmodified")
	}
}

func TestMaskMap_NonStringSensitiveValue(t *testing.T) {
	m := map[string]any{"api_key": 12345}
	got := MaskMap(m)
	if got["api_key"] != 12345 {
		t.Errorf("expected non-string value preserved, got %v", got["api_key"])
	}
}

func TestMaskMap_AllSensitiveKeys(t *testing.T) {
	m := map[string]any{
		"api_key":       "val",
		"apikey":        "val",
		"api_secret":    "val",
		"secret":        "val",
		"password":      "val",
		"passwd":        "val",
		"token":         "val",
		"access_token":  "val",
		"refresh_token": "val",
		"private_key":   "val",
		"credential":    "val",
		"credentials":   "val",
	}
	got := MaskMap(m)
	for k, v := range got {
		if v == "val" {
			t.Errorf("expected key %q masked", k)
		}
	}
}

func TestMaskMap_WhitespaceInKeys(t *testing.T) {
	m := map[string]any{" api_key ": "sk-abcdefghijklmnopqrstuvwxyz1234567890"}
	got := MaskMap(m)
	if got[" api_key "].(string) == "sk-abcdefghijklmnopqrstuvwxyz1234567890" {
		t.Error("expected whitespace-padded key masked")
	}
}

// ---- isSensitiveKey ------------------------------------------------------

func TestIsSensitiveKey_KnownKeys(t *testing.T) {
	keys := []string{"api_key", "API_KEY", "ApiKey", "password", "token", "secret"}
	for _, k := range keys {
		if !isSensitiveKey(k) {
			t.Errorf("expected %q to be sensitive", k)
		}
	}
}

func TestIsSensitiveKey_UnknownKey(t *testing.T) {
	if isSensitiveKey("username") {
		t.Error("expected 'username' to not be sensitive")
	}
}

func TestIsSensitiveKey_Empty(t *testing.T) {
	if isSensitiveKey("") {
		t.Error("expected empty key to not be sensitive")
	}
}
