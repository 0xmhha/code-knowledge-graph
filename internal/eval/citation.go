package eval

import (
	"regexp"
	"strconv"
	"strings"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// CitationResult is the per-response classification of file:line
// citations extracted from an LLM output (T-03).
type CitationResult struct {
	Total        int
	FileExists   int
	LineInNode   int
	Hallucinated []Citation
	Precision    float64
}

// Citation is a single file:line reference extracted from LLM output.
type Citation struct {
	File string
	Line int
}

var citationRe = regexp.MustCompile(
	`(?:` +
		`([a-zA-Z0-9_/.\-]+\.\w{1,5}):(\d{1,6})` + // path/file.go:123
		`|` +
		`([a-zA-Z0-9_/.\-]+\.\w{1,5})#L(\d{1,6})` + // path/file.go#L123
		`)`,
)

// ExtractCitations pulls file:line references from LLM output text.
func ExtractCitations(text string) []Citation {
	matches := citationRe.FindAllStringSubmatch(text, -1)
	seen := map[string]struct{}{}
	var out []Citation
	for _, m := range matches {
		var file string
		var lineStr string
		if m[1] != "" {
			file, lineStr = m[1], m[2]
		} else {
			file, lineStr = m[3], m[4]
		}
		line, err := strconv.Atoi(lineStr)
		if err != nil {
			continue
		}
		key := file + ":" + lineStr
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Citation{File: file, Line: line})
	}
	return out
}

// ValidateCitations checks every file:line citation in output against
// the graph store. For each citation:
//   - FileExists increments if NodesByFilePath returns any nodes
//   - LineInNode increments if the cited line falls within at least one
//     node's [start_line, end_line] range
//
// Precision = LineInNode / Total (0 when Total == 0).
// store may be nil — returns zero result without error.
func ValidateCitations(output string, store pkgstore.Reader) (CitationResult, error) {
	citations := ExtractCitations(output)
	result := CitationResult{Total: len(citations)}
	if store == nil || len(citations) == 0 {
		return result, nil
	}

	pathIndex, err := buildPathIndex(store)
	if err != nil {
		return result, err
	}

	for _, c := range citations {
		matchedPath := resolveFilePath(c.File, pathIndex)
		if matchedPath == "" {
			result.Hallucinated = append(result.Hallucinated, c)
			continue
		}
		result.FileExists++

		nodes, err := store.NodesByFilePath(matchedPath)
		if err != nil {
			return result, err
		}
		if lineInAnyNode(c.Line, nodes) {
			result.LineInNode++
		} else {
			result.Hallucinated = append(result.Hallucinated, c)
		}
	}
	if result.Total > 0 {
		result.Precision = float64(result.LineInNode) / float64(result.Total)
	}
	return result, nil
}

// buildPathIndex collects all distinct file_path values from the store
// across all languages by querying per-language and merging.
func buildPathIndex(store pkgstore.Reader) (map[string]struct{}, error) {
	pathSet := make(map[string]struct{})
	for _, lang := range []string{"go", "typescript", "solidity"} {
		paths, err := store.DistinctFilePaths(lang)
		if err != nil {
			continue
		}
		for _, p := range paths {
			pathSet[p] = struct{}{}
		}
	}
	return pathSet, nil
}

// resolveFilePath matches a citation's file path against the graph's
// known paths. LLM responses often use partial paths (e.g. "backend.go"
// or "eth/backend.go") so we accept suffix matches.
func resolveFilePath(cited string, paths map[string]struct{}) string {
	if _, ok := paths[cited]; ok {
		return cited
	}
	for p := range paths {
		if strings.HasSuffix(p, "/"+cited) || strings.HasSuffix(p, "\\"+cited) {
			return p
		}
	}
	return ""
}

func lineInAnyNode(line int, nodes []types.Node) bool {
	for _, n := range nodes {
		if line >= n.StartLine && line <= n.EndLine {
			return true
		}
	}
	return false
}
