package parse

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MakeID returns a stable 16-char hash for (qname, language, startByte).
// All language parsers must use this so node IDs collide deterministically
// across pass boundaries and re-runs.
func MakeID(qname, lang string, startByte int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", qname, lang, startByte)))
	return hex.EncodeToString(sum[:])[:16]
}
