// Package filterlist implements the --files-from JSON include/exclude
// filter for ckg build. Supplied at the CLI as a path to a JSON file:
//
//	{
//	  "include": ["internal/**/*.go", "cmd/ckg/*.go"],
//	  "exclude": ["**/testdata/**", "**/*_test.go"]
//	}
//
// Patterns use Go's filepath.Match doublestar extension via the bundled
// matcher: `**` matches any number of path segments. Both fields are
// optional — empty include means "every discovered file is eligible" and
// the exclude list still trims unwanted matches.
//
// Rationale: ckg already has heuristic discovery (detect.Walk for TS/Sol,
// go/packages.Load for Go) and a .ckgignore for project-wide excludes.
// --files-from is the explicit operator override for "I have my own build
// list — use exactly these files." This is the entry point for future
// build-system integration (tsconfig include/exclude, hardhat sources,
// `go list -f '{{.GoFiles}}'`) which all produce a file list as output.
package filterlist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FilterList holds the parsed include/exclude rules.
type FilterList struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// Load reads and parses the JSON file at path. Returns a nil filter (and
// nil error) when path is empty — callers can treat nil as "no filter".
func Load(path string) (*FilterList, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("filterlist: read %s: %w", path, err)
	}
	var fl FilterList
	if err := json.Unmarshal(raw, &fl); err != nil {
		return nil, fmt.Errorf("filterlist: parse %s: %w", path, err)
	}
	return &fl, nil
}

// Allow reports whether relPath (slash-separated, relative to srcRoot)
// passes the filter. Decision rule:
//
//  1. exclude match  → reject (exclude trumps include)
//  2. include empty  → accept
//  3. include match  → accept
//  4. otherwise      → reject
func (f *FilterList) Allow(relPath string) bool {
	if f == nil {
		return true
	}
	rel := filepath.ToSlash(relPath)
	for _, pat := range f.Exclude {
		if matchGlob(pat, rel) {
			return false
		}
	}
	if len(f.Include) == 0 {
		return true
	}
	for _, pat := range f.Include {
		if matchGlob(pat, rel) {
			return true
		}
	}
	return false
}

// FilterPaths returns the subset of paths that f.Allow accepts. nil filter
// returns paths unchanged (zero allocation).
func (f *FilterList) FilterPaths(paths []string) []string {
	if f == nil {
		return paths
	}
	out := paths[:0]
	for _, p := range paths {
		if f.Allow(p) {
			out = append(out, p)
		}
	}
	return out
}

// matchGlob is a small doublestar-aware matcher: `**` matches any number
// of path segments (including zero). Single `*` matches one segment of
// non-separator characters. Patterns and inputs both use forward slashes.
//
// Implementation walks the pattern segment by segment, with a special
// recursive case for `**`. For the V0 use case (build file lists) this is
// sufficient — full POSIX glob semantics (character classes, escapes) are
// not needed because patterns come from operator JSON, not user input.
func matchGlob(pattern, name string) bool {
	pParts := strings.Split(pattern, "/")
	nParts := strings.Split(name, "/")
	return matchSegments(pParts, nParts)
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		switch pat[0] {
		case "**":
			// Match zero or more segments. Try every alignment.
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		default:
			if len(name) == 0 {
				return false
			}
			ok, err := filepath.Match(pat[0], name[0])
			if err != nil || !ok {
				return false
			}
			pat = pat[1:]
			name = name[1:]
		}
	}
	return len(name) == 0
}
