package persist

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W11 V11 — attrs compression decision lock.
//
// Background: W11 V7 stores marker metadata as a JSON-blob TEXT
// column on nodes. A natural follow-up would gzip the blob to
// reduce DB size on large repos. V11 audited that option and
// declined it for two reasons:
//
//  1. Payload is small. A typical marker set (booleans +
//     SlotIndex + maybe YulBuiltins slice) marshals to
//     50-150 bytes. gzip's frame header alone is ~10-20 bytes
//     so the compression ratio on individual rows is poor;
//     large repos win more from index-side optimisations than
//     from per-row compression.
//
//  2. Read path frequency. QueryNodes / NodesByFilePath /
//     FTS5 search join all read attrs. Every read would pay
//     a decompress hit. The boot-time viewer JSON export
//     (~2-4MB total attrs payload) is the largest single
//     consumer and benefits from the compression-friendly
//     omitempty JSON encoding more than from gzip.
//
// This audit asserts the on-disk form stays TEXT (plain JSON)
// rather than BYTEA / BLOB or a compressed wrapper format. If a
// future change adds compression, this test fails — the
// rationale should be re-evaluated against fresh size numbers
// before the change lands.
func TestAttrs_StoredAsPlainText(t *testing.T) {
	// (a) SQLite schema declares attrs as TEXT (not BLOB). The
	// column definition line is `attrs          TEXT` (with
	// padding); searching for a leading word boundary skips
	// the rationale comment that mentions "attrs (schema 1.9,…".
	colDef := "attrs          TEXT"
	if !strings.Contains(schemaSQL, colDef) {
		t.Errorf("schemaSQL: missing %q column definition (was it changed to BLOB or compressed?)", colDef)
	}
	if strings.Contains(schemaSQL, "attrs          BLOB") {
		t.Errorf("nodes.attrs is declared as BLOB — V11 decision is plain JSON in TEXT")
	}

	// (b) marshalNodeAttrs returns a plain JSON string, not a
	// gzip header. gzip frames start with bytes 1f 8b; valid
	// JSON starts with `{` or empty string.
	n := types.Node{
		HasExternalCall: true,
		SlotIndex:       3,
	}
	blob := marshalNodeAttrs(&n)
	if blob == "" {
		t.Fatalf("expected non-empty blob for marker-bearing node")
	}
	if blob[0] != '{' {
		t.Errorf("marshalNodeAttrs returned non-JSON output (first byte = 0x%02x): %q", blob[0], blob)
	}
	if strings.HasPrefix(blob, "\x1f\x8b") {
		t.Errorf("marshalNodeAttrs returned a gzip-framed blob — V11 decision is plain JSON")
	}
}
