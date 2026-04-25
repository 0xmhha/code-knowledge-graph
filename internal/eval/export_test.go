// export_test.go exposes internal functions for white-box testing from the
// eval_test package.  This file is compiled only during `go test`.
package eval

// ScoreTask is exported for testing.
var ScoreTask = scoreTask

// ExtractSymbols is exported for testing.
var ExtractSymbols = extractSymbols

// JSONString is exported for testing.
var JSONString = jsonString

// WriteCSV is exported for testing.
var WriteCSV = writeCSV

// DumpFiles is exported for testing.
var DumpFiles = dumpFiles
