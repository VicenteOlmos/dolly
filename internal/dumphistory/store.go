package dumphistory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Record tracks one completed dump for history and restore.
type Record struct {
	Seq            int       `json:"seq"`
	BaseDir        string    `json:"base_dir"`
	Path           string    `json:"path"`
	CreatedAt      time.Time `json:"created_at"`
	SourceDatabase string    `json:"source_database,omitempty"`
	Schemas        []string  `json:"schemas,omitempty"`
	SchemaLabel    string    `json:"schema_label,omitempty"`
	TableCount     int       `json:"table_count"`
	RowEstimate    int64     `json:"row_estimate,omitempty"`
}

type document struct {
	Records []Record `json:"records"`
}

// Store persists dump history records.
type Store interface {
	List() ([]Record, error)
	ListBase(baseDir string) ([]Record, error)
	Register(r Record) error
}

// FileStore is a JSON file-backed dump history store.
type FileStore struct {
	path string
}

// NewFileStore opens or creates a store at path.
func NewFileStore(path string) (*FileStore, error) {
	return &FileStore{path: path}, nil
}

func (s *FileStore) List() ([]Record, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	out := append([]Record(nil), doc.Records...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Seq > out[j].Seq
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *FileStore) ListBase(baseDir string) ([]Record, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	norm := normalizePath(baseDir)
	var out []Record
	for _, r := range all {
		if normalizePath(r.BaseDir) == norm {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *FileStore) Register(r Record) error {
	if r.Path == "" {
		return errors.New("dump history: path is required")
	}
	if r.Seq <= 0 {
		return errors.New("dump history: seq must be positive")
	}
	lock, err := lockHistFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.close()
	doc, err := s.load()
	if err != nil {
		return err
	}
	for _, existing := range doc.Records {
		if existing.Path == r.Path {
			return fmt.Errorf("dump history: path already registered: %s", r.Path)
		}
	}
	doc.Records = append(doc.Records, r)
	return s.persist(doc)
}

func (s *FileStore) load() (document, error) {
	if info, err := os.Stat(s.path); err == nil {
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			if chmodErr := os.Chmod(s.path, 0o600); chmodErr != nil {
				return document{}, fmt.Errorf("fix dump history permissions (mode %o): %w", perm, chmodErr)
			}
		}
	} else if !os.IsNotExist(err) {
		return document{}, fmt.Errorf("stat dump history: %w", err)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return document{}, nil
		}
		return document{}, fmt.Errorf("read dump history: %w", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("parse dump history: %w", err)
	}
	return doc, nil
}

func (s *FileStore) persist(doc document) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create dump history dir: %w", err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dump history: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write dump history: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename dump history: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("chmod dump history: %w", err)
	}
	return nil
}

// NextSeq returns the next dump sequence number for baseDir using the store and
// existing numbered subdirectories on disk.
func NextSeq(baseDir string, store Store) (int, error) {
	max := 0
	if store != nil {
		recs, err := store.ListBase(baseDir)
		if err != nil {
			return 0, err
		}
		for _, r := range recs {
			if r.Seq > max {
				max = r.Seq
			}
		}
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("scan dump base dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n, err := strconv.Atoi(e.Name())
		if err != nil || n <= 0 {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// AllocateDir picks the next numbered dump directory under baseDir and creates it.
// Uses os.Mkdir in a retry loop to be race-safe even without external locking.
func AllocateDir(baseDir string, store Store) (path string, seq int, err error) {
	// Ensure the base directory exists (MkdirAll for parents, race-safe for the base itself).
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", 0, fmt.Errorf("create dump base directory: %w", err)
	}
	for {
		seq, err = NextSeq(baseDir, store)
		if err != nil {
			return "", 0, err
		}
		path = filepath.Join(baseDir, strconv.Itoa(seq))
		if err := os.Mkdir(path, 0o700); err != nil {
			if os.IsExist(err) {
				continue // race lost, retry with next seq
			}
			return "", 0, fmt.Errorf("create dump directory: %w", err)
		}
		return path, seq, nil
	}
}

func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	clean := filepath.Clean(p)
	if abs, err := filepath.Abs(clean); err == nil {
		return abs
	}
	return clean
}
