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
	defer f.Close()
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
		// match if any path component equals pat, or rel starts with pat/
		if strings.HasPrefix(rel, pat+"/") || rel == pat {
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
