package detect

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Result groups file paths by language. Paths are RELATIVE to the walk root.
type Result struct {
	Go  []string
	TS  []string
	Sol []string
}

// Walk walks root, classifies files by extension, and skips paths matching .ckgignore.
func Walk(root string) (*Result, error) {
	ignore, err := LoadCKGIgnore(root)
	if err != nil {
		return nil, fmt.Errorf("load .ckgignore: %w", err)
	}
	r := &Result{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if ignore.Match(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go":
			r.Go = append(r.Go, rel)
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
			r.TS = append(r.TS, rel)
		case ".sol":
			r.Sol = append(r.Sol, rel)
		}
		return nil
	})
	return r, err
}
