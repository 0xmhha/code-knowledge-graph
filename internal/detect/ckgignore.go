package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// CKGIgnore is a gitignore-style matcher (no negations, no anchored leading slashes).
// Patterns ending in "/" match directories. Patterns containing "*" use filepath.Match.
type CKGIgnore struct {
	patterns []string
}

// LoadCKGIgnore reads `.ckgignore` from root. Missing file is OK (returns empty matcher).
func LoadCKGIgnore(root string) (*CKGIgnore, error) {
	f, err := os.Open(filepath.Join(root, ".ckgignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return &CKGIgnore{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	c := &CKGIgnore{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c.patterns = append(c.patterns, line)
	}
	return c, sc.Err()
}

// Match reports whether the relative path (filepath separator) is ignored.
func (c *CKGIgnore) Match(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, p := range c.patterns {
		if matchPattern(p, rel) {
			return true
		}
	}
	return false
}

func matchPattern(pat, rel string) bool {
	pat = filepath.ToSlash(pat)
	dirPat := strings.HasSuffix(pat, "/")
	if dirPat {
		pat = strings.TrimSuffix(pat, "/")
		// Match if any path component equals pat. Three cases:
		//   - rel == pat                           (the dir itself, top-level)
		//   - rel starts with pat+"/"              (top-level dir; child)
		//   - rel contains "/"+pat+"/"             (nested dir at any depth)
		//   - rel ends with "/"+pat                (the dir itself, nested)
		// The nested cases matter because gitignore-style "node_modules/"
		// must skip web/viewer-next/node_modules/, not just a top-level
		// node_modules. Without the segment match the walker descends into
		// every nested node_modules and floods the graph with vendor JS.
		if rel == pat || strings.HasPrefix(rel, pat+"/") {
			return true
		}
		if strings.Contains(rel, "/"+pat+"/") || strings.HasSuffix(rel, "/"+pat) {
			return true
		}
		return false
	}
	// glob match against full path or any segment
	if matched, _ := filepath.Match(pat, filepath.Base(rel)); matched {
		return true
	}
	if matched, _ := filepath.Match(pat, rel); matched {
		return true
	}
	return false
}
