// Package equitydeep ingests EquityDeep financial snapshots into the shape
// the Quant Lab计算面 needs: fundamentals_detail rows.
//
// The package consumes two frozen contracts and adds no semantics of its own
// beyond what they declare:
//
//   - contracts/snapshot.schema.json — the upstream record shape (contract C1
//     upstream), one JSON object per snapshot.
//   - contracts/field_dictionary.yaml — the field_code / raw_names whitelist
//     plus the explicit unit_scale conversion table.
//
// Rules enforced here, all of them contract rules:
//
//  1. raw_names is an explicit whitelist. A raw field name outside it is
//     dropped and reported as a warning — never guessed, never fuzzy-matched,
//     never LLM-translated.
//  2. Unit conversion uses unit_scale (base_value = value × unit_scale[unit]).
//     A snapshot that declares a unit outside the table is rejected instead of
//     guessed.
//  3. ann_date is a PIT hard requirement: a snapshot without it is
//     non-compliant and is rejected, because silently substituting the report
//     period introduces look-ahead bias.
//  4. ticker is a bare 6-digit code; ts_code (600519 → 600519.SH) is derived
//     by ToTsCode, mirroring the existing symbol rule in the repo.
//
// The package is deliberately free of HTTP and database dependencies so the
// normalization rules are unit-testable on their own.
package equitydeep

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Contract C1-a field codes (contracts/field_dictionary.yaml). Only the codes
// the bridge-B1 factors need are named here; the full 20-code set stays in the
// dictionary file so that adding a code is a contract change, not a code change.
const (
	FieldTotalRevenue      = "total_revenue"
	FieldOperatingCost     = "operating_cost"
	FieldNetProfitAttr     = "net_profit_attr"
	FieldTotalAssets       = "total_assets"
	FieldEquityAttr        = "equity_attr"
	FieldContractLiability = "contract_liability"
	FieldInventory         = "inventory"
	FieldOCFNet            = "ocf_net"
)

// snapshotDateLayout is the only date form the contract allows
// (JSON Schema "format: date"). Deliberately strict: the repo has a second,
// compact YYYYMMDD form on the HTTP-handler side, and accepting both here would
// hide a producer-side format drift.
const snapshotDateLayout = "2006-01-02"

// ToTsCode normalizes a bare 6-digit ticker into a ts_code, e.g.
// 600519 → 600519.SH. The exchange rule mirrors the repo's existing
// normalizeSymbol: a leading 6 or 9 is Shanghai, everything else Shenzhen.
// An input that already carries a suffix is validated and returned as-is.
func ToTsCode(ticker string) (string, error) {
	t := strings.TrimSpace(ticker)
	if t == "" {
		return "", fmt.Errorf("ticker is empty")
	}
	if idx := strings.LastIndex(t, "."); idx > 0 {
		code, exch := t[:idx], strings.ToUpper(t[idx+1:])
		if !isDigits(code) || len(code) != 6 {
			return "", fmt.Errorf("invalid ticker %q: code must be 6 digits", ticker)
		}
		if exch != "SH" && exch != "SZ" && exch != "BJ" {
			return "", fmt.Errorf("invalid ticker %q: unknown exchange %q", ticker, exch)
		}
		return code + "." + exch, nil
	}
	if !isDigits(t) || len(t) != 6 {
		return "", fmt.Errorf("invalid ticker %q: expected 6 digits", ticker)
	}
	switch t[0] {
	case '6', '9':
		return t + ".SH", nil
	default:
		return t + ".SZ", nil
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseContractDate parses a contract date (format: date). An empty value is
// reported separately from an invalid one so callers can apply their own
// required-field policy.
func parseContractDate(field, value string) (time.Time, bool, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return time.Time{}, false, nil
	}
	ts, err := time.Parse(snapshotDateLayout, v)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s must be YYYY-MM-DD, got %q", field, value)
	}
	return ts.UTC(), true, nil
}

// stringSuffixUnit maps a source-side magnitude suffix onto a unit_scale key.
// The table is closed on purpose: a suffix outside it is not resolved, because
// inferring a magnitude from an unrecognized suffix would be guesswork.
var stringSuffixUnit = map[string]string{
	"亿元": "CNY_100m",
	"亿":  "CNY_100m",
	"万元": "CNY_10k",
	"万":  "CNY_10k",
}

// suffixKeys is stringSuffixUnit's keys ordered longest-first, so that "亿元"
// is matched before "亿". Built once at init because map order is randomized.
var suffixKeys = func() []string {
	keys := make([]string, 0, len(stringSuffixUnit))
	for k := range stringSuffixUnit {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && len(keys[j]) > len(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}()

// parseNumber parses a plain decimal number in source form. Thousands
// separators are stripped (a formatting detail, not a magnitude); a magnitude
// suffix is resolved only through stringSuffixUnit.
func parseNumber(raw string) (float64, string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, "", fmt.Errorf("empty string value")
	}
	suffixUnit := ""
	for _, suffix := range suffixKeys {
		if strings.HasSuffix(s, suffix) {
			suffixUnit = stringSuffixUnit[suffix]
			s = strings.TrimSuffix(s, suffix)
			break
		}
	}
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, "", fmt.Errorf("not a plain decimal number: %q", raw)
	}
	return v, suffixUnit, nil
}
