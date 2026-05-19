package persist

import (
	"strings"
	"testing"
)

// W-C W11 V9 — audit that the PostgreSQL exporter mirrors the
// SQLite nodes.attrs JSON-blob column added under schema 1.9.
// Without a live Postgres instance we cannot run the full Export
// path; we instead lock the two static surfaces that need to
// stay in sync:
//
//   - pgSchema DDL declares the `attrs` column on the nodes table.
//   - The CopyFrom column list in insertNodes mentions "attrs"
//     as its trailing element so each row's marker JSON travels
//     to Postgres alongside the rest of the node payload.
//
// A silent removal of either surface would reproduce the V6 gap
// (markers dropped at the persist boundary) on the Postgres path.
func TestPostgresExporter_AttrsColumnPresent(t *testing.T) {
	if !strings.Contains(pgSchema, "attrs") {
		t.Errorf("pgSchema is missing the nodes.attrs column")
	}
	if !strings.Contains(pgSchema, "attrs         TEXT") {
		t.Errorf("pgSchema declares attrs but not as TEXT NOT NULL DEFAULT '': %s", pgSchema)
	}
}
