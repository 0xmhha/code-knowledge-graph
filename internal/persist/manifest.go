package persist

import (
	"encoding/json"
	"fmt"
)

// Manifest captures build-time metadata. Stored as key/value rows in the
// manifest table; complex fields are JSON-encoded.
type Manifest struct {
	SchemaVersion       string         `json:"schema_version"`
	CKGVersion          string         `json:"ckg_version"`
	BuildTimestamp      string         `json:"build_timestamp"`
	SrcRoot             string         `json:"src_root"`
	SrcCommit           string         `json:"src_commit,omitempty"`
	StalenessMethod     string         `json:"staleness_method"` // "git" | "mtime"
	StalenessFiles      []string       `json:"staleness_files,omitempty"`
	StalenessMTimeSum   int64          `json:"staleness_mtime_sum,omitempty"`
	Languages           map[string]int `json:"languages"`
	Stats               map[string]int `json:"stats"`
	CKGIgnore           []string       `json:"ckgignore,omitempty"`
	ParseErrorsCount    int            `json:"parse_errors_count"`
	UnresolvedRefsCount int            `json:"unresolved_refs_count"`
	ClusteringStatus    string         `json:"clustering_status"` // "ok" | "pkg_only"
}

// SetManifest replaces existing manifest rows with fields from m.
func (s *Store) SetManifest(m Manifest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manifest`); err != nil {
		return err
	}
	rows := []struct{ k, v string }{
		{"schema_version", m.SchemaVersion},
		{"ckg_version", m.CKGVersion},
		{"build_timestamp", m.BuildTimestamp},
		{"src_root", m.SrcRoot},
		{"src_commit", m.SrcCommit},
		{"staleness_method", m.StalenessMethod},
		{"clustering_status", m.ClusteringStatus},
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO manifest (key, value) VALUES (?, ?)`, r.k, r.v); err != nil {
			return err
		}
	}
	jsonRows := []struct {
		k string
		v any
	}{
		{"staleness_files", m.StalenessFiles},
		{"staleness_mtime_sum", m.StalenessMTimeSum},
		{"languages", m.Languages},
		{"stats", m.Stats},
		{"ckgignore", m.CKGIgnore},
		{"parse_errors_count", m.ParseErrorsCount},
		{"unresolved_refs_count", m.UnresolvedRefsCount},
	}
	for _, r := range jsonRows {
		buf, err := json.Marshal(r.v)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manifest (key, value) VALUES (?, ?)`, r.k, string(buf)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetManifest reads all manifest rows and reassembles the struct.
func (s *Store) GetManifest() (Manifest, error) {
	rows, err := s.db.Query(`SELECT key, value FROM manifest`)
	if err != nil {
		return Manifest{}, err
	}
	defer rows.Close()
	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Manifest{}, err
		}
		kv[k] = v
	}
	m := Manifest{
		SchemaVersion:    kv["schema_version"],
		CKGVersion:       kv["ckg_version"],
		BuildTimestamp:   kv["build_timestamp"],
		SrcRoot:          kv["src_root"],
		SrcCommit:        kv["src_commit"],
		StalenessMethod:  kv["staleness_method"],
		ClusteringStatus: kv["clustering_status"],
	}
	for _, j := range []struct {
		k   string
		dst any
	}{
		{"staleness_files", &m.StalenessFiles},
		{"staleness_mtime_sum", &m.StalenessMTimeSum},
		{"languages", &m.Languages},
		{"stats", &m.Stats},
		{"ckgignore", &m.CKGIgnore},
		{"parse_errors_count", &m.ParseErrorsCount},
		{"unresolved_refs_count", &m.UnresolvedRefsCount},
	} {
		if v, ok := kv[j.k]; ok && v != "" {
			if err := json.Unmarshal([]byte(v), j.dst); err != nil {
				return m, fmt.Errorf("decode %s: %w", j.k, err)
			}
		}
	}
	return m, nil
}
