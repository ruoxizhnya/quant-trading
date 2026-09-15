package domain

import "time"

// FundamentalsDetailRow is one row of the fundamentals_detail table
// (contract C1, EQD-P1-1 / EQD-P1-2). The table stores deep financial
// statements one field per row so that every number keeps its source
// coordinates: raw_field_name preserves the source key verbatim and
// snapshot_uri points back at the archived raw response.
//
// Value is the only nullable column: a source-side missing reading must
// stay distinguishable from a reading that is actually zero.
type FundamentalsDetailRow struct {
	TsCode       string    // 600519.SH
	EndDate      time.Time // report period
	AnnDate      time.Time // announcement date — PIT alignment key
	FieldCode    string    // canonical code from the field dictionary
	RawFieldName string    // source key, kept verbatim for traceability
	Value        *float64  // nil = source-side missing (unlike 0)
	Unit         string    // canonical unit, always the dictionary base unit after ETL
	Source       string    // "equitydeep:<producer>"
	FetchedAt    time.Time // snapshot time
	SnapshotURI  string    // back-reference to the archived raw response
}
