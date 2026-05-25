package mcphandlers

import (
	"bytes"
	"compress/gzip"
)

// gzipBytes is the in-package gzip helper used by evidence_test.go to
// build hunk blob fixtures the same way emitHunkGraph would. Lives in
// its own file so multiple _test.go files can share it without
// circular helper definitions.
func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(b); err != nil {
		panic(err)
	}
	if err := gw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
