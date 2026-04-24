package golang

import "github.com/0xmhha/code-knowledge-graph/internal/parse"

// MakeID delegates to the shared parse.MakeID so all language parsers compute
// identical IDs for the same (qname, lang, startByte) tuple.
func MakeID(qname, lang string, startByte int) string {
	return parse.MakeID(qname, lang, startByte)
}
