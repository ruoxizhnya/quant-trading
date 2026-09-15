package equitydeep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Snapshot is one record of contracts/snapshot.schema.json — the shape of a
// single archived upstream response. Data holds the source field names verbatim
// as keys, decoded with json.Number so no precision is lost before conversion.
type Snapshot struct {
	Ticker       string         `json:"ticker"`
	ReportPeriod string         `json:"report_period"`
	AnnDate      string         `json:"ann_date"`
	Source       string         `json:"source"`
	Dataset      string         `json:"dataset"`
	FetchedAt    string         `json:"fetched_at"`
	Unit         string         `json:"unit"`
	Data         map[string]any `json:"data"`
}

// DecodeSnapshot decodes one snapshot, enforcing the contract's
// additionalProperties: false (unknown keys are rejected rather than ignored)
// and preserving numeric literals via json.Number.
func DecodeSnapshot(line []byte) (*Snapshot, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	dec.DisallowUnknownFields()

	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	return &s, nil
}

func ensureEOF(dec *json.Decoder) error {
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing content after snapshot object")
	}
	return nil
}

// DroppedField records a field the normalizer refused to map, together with the
// reason. Dropping is the contract's prescribed answer for anything outside the
// explicit whitelist or the explicit conversion table.
type DroppedField struct {
	RawName string `json:"raw_name"`
	Reason  string `json:"reason"`
}

// Result is the outcome of normalizing one snapshot.
type Result struct {
	Rows    []domain.FundamentalsDetailRow
	Dropped []DroppedField
}

// Normalizer turns snapshots into fundamentals_detail rows. It holds no
// mutable state, so one instance serves concurrent requests.
type Normalizer struct {
	dict         *Dictionary
	sourcePrefix string
	fallbackSrc  string
}

// NewNormalizer builds a Normalizer over a loaded dictionary. fallbackSource is
// used when a snapshot omits its own source (the contract marks source
// optional), and must not include the prefix.
func NewNormalizer(dict *Dictionary, fallbackSource string) *Normalizer {
	return &Normalizer{
		dict:         dict,
		sourcePrefix: "equitydeep:",
		fallbackSrc:  strings.TrimSpace(fallbackSource),
	}
}

// Normalize converts one snapshot into rows.
//
// snapshotURI is the back-reference to the archived raw response and is stored
// on every row, because a number is only meaningful together with the exact
// response it came from. ingestFetchedAt is used when the snapshot carries no
// fetched_at of its own (the contract defers to ingest.raw in that case).
//
// Contract violations (bad ticker, missing ann_date, unknown unit) return an
// error: they mean the producer is off-contract and the run should fail loudly.
// Whitelist misses and unparseable values are dropped and reported instead, so
// one unusual field cannot block the snapshot.
func (n *Normalizer) Normalize(s *Snapshot, snapshotURI string, ingestFetchedAt time.Time) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("snapshot is nil")
	}
	if n.dict == nil {
		return Result{}, fmt.Errorf("normalizer has no field dictionary")
	}

	tsCode, err := ToTsCode(s.Ticker)
	if err != nil {
		return Result{}, err
	}

	endDate, ok, err := parseContractDate("report_period", s.ReportPeriod)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("report_period is required")
	}

	// ann_date is the PIT key. Without it the reading cannot be attributed to a
	// publication date, so it must never be silently back-filled.
	annDate, ok, err := parseContractDate("ann_date", s.AnnDate)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("ann_date is required (PIT alignment; missing it would introduce look-ahead bias)")
	}

	unit := strings.TrimSpace(s.Unit)
	if unit == "" {
		unit = n.dict.BaseUnit // contract: absent unit means base_unit
	}
	unitScale, ok := n.dict.ScaleFor(unit)
	if !ok {
		return Result{}, fmt.Errorf("unit %q is not in the unit_scale table", unit)
	}

	fetchedAt := ingestFetchedAt
	if ts, ok, err := parseContractDate("fetched_at", s.FetchedAt); err != nil {
		return Result{}, err
	} else if ok {
		fetchedAt = ts
	}
	if fetchedAt.IsZero() {
		return Result{}, fmt.Errorf("fetched_at is required (neither the snapshot nor the ingest supplied one)")
	}

	source, err := n.resolveSource(s.Source)
	if err != nil {
		return Result{}, err
	}
	if len(s.Data) == 0 {
		return Result{}, fmt.Errorf("data is empty (contract requires at least one property)")
	}

	// Deterministic output order: map iteration is randomized, and stable rows
	// make the ingest reproducible and the tests readable.
	rawNames := make([]string, 0, len(s.Data))
	for rawName := range s.Data {
		rawNames = append(rawNames, rawName)
	}
	sort.Strings(rawNames)

	res := Result{Rows: make([]domain.FundamentalsDetailRow, 0, len(rawNames))}
	for _, rawName := range rawNames {
		field, ok := n.dict.LookupRaw(rawName)
		if !ok {
			res.Dropped = append(res.Dropped, DroppedField{
				RawName: rawName,
				Reason:  "not in the field dictionary raw_names whitelist",
			})
			continue
		}

		value, err := n.convert(s.Data[rawName], unitScale)
		if err != nil {
			res.Dropped = append(res.Dropped, DroppedField{
				RawName: rawName,
				Reason:  err.Error(),
			})
			continue
		}

		res.Rows = append(res.Rows, domain.FundamentalsDetailRow{
			TsCode:       tsCode,
			EndDate:      endDate,
			AnnDate:      annDate,
			FieldCode:    field.FieldCode,
			RawFieldName: rawName,
			Value:        value,
			Unit:         n.dict.BaseUnit,
			Source:       source,
			FetchedAt:    fetchedAt,
			SnapshotURI:  snapshotURI,
		})
	}
	return res, nil
}

func (n *Normalizer) resolveSource(snapshotSource string) (string, error) {
	name := strings.TrimSpace(snapshotSource)
	if name == "" {
		name = n.fallbackSrc
	}
	if name == "" {
		return "", fmt.Errorf("source is required (neither the snapshot nor the ingest supplied one)")
	}
	return n.sourcePrefix + name, nil
}

// convert turns a contract value (number | string | null) into a base-unit
// number. null stays nil so that "missing" and "zero" remain distinguishable.
func (n *Normalizer) convert(v any, unitScale float64) (*float64, error) {
	switch typed := v.(type) {
	case nil:
		return nil, nil
	case json.Number:
		f, err := typed.Float64()
		if err != nil {
			return nil, fmt.Errorf("number %q is not representable", typed.String())
		}
		return scalePtr(f, unitScale), nil
	case float64:
		return scalePtr(typed, unitScale), nil
	case int:
		return scalePtr(float64(typed), unitScale), nil
	case string:
		f, suffixUnit, err := parseNumber(typed)
		if err != nil {
			return nil, err
		}
		scale := unitScale
		if suffixUnit != "" {
			// A self-described magnitude wins: the suffix names the unit of the
			// literal itself, so it is exact rather than inferred.
			s, ok := n.dict.ScaleFor(suffixUnit)
			if !ok {
				return nil, fmt.Errorf("suffix unit %q is not in the unit_scale table", suffixUnit)
			}
			scale = s
		}
		return scalePtr(f, scale), nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}

func scalePtr(v, scale float64) *float64 {
	scaled := v * scale
	return &scaled
}
