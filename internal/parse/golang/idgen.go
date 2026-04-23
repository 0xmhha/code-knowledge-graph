package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MakeID returns a stable 16-char content-hash for a node identifier triple.
func MakeID(qname, lang string, startByte int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", qname, lang, startByte)))
	return hex.EncodeToString(sum[:])[:16]
}
