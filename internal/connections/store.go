package connections

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrDuplicateName = errors.New("connection name already exists")
	ErrNotFound      = errors.New("connection not found")
	ErrEncryptKey    = errors.New("DOLLY_CONNECTIONS_KEY required when connections.encrypt is true")
)

// Connection is a saved PostgreSQL profile.
type Connection struct {
	Name     string   `yaml:"name"`
	Host     string   `yaml:"host"`
	Port     string   `yaml:"port"`
	Database string   `yaml:"database"`
	User     string   `yaml:"user"`
	Password string   `yaml:"password"`
	SSLMODE  string   `yaml:"sslmode,omitempty"`
	Schemas  []string `yaml:"schemas,omitempty"`
}

// ConnectionStore persists named connection profiles.
type ConnectionStore interface {
	List() ([]Connection, error)
	Get(name string) (Connection, error)
	Save(c Connection) error
	Put(c Connection) error
	Delete(name string) error
	Rename(oldName, newName string) error
	UpsertBySignature(c Connection) (Connection, error)
}

type fileDocument struct {
	Connections []Connection `yaml:"connections"`
}

// FileStore is a YAML file-backed ConnectionStore.
type FileStore struct {
	path    string
	encrypt bool
}

// NewFileStore opens or creates a store at path.
func NewFileStore(path string, encrypt bool) (*FileStore, error) {
	return &FileStore{path: path, encrypt: encrypt}, nil
}

func (s *FileStore) List() ([]Connection, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	out := append([]Connection(nil), doc.Connections...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *FileStore) Get(name string) (Connection, error) {
	doc, err := s.load()
	if err != nil {
		return Connection{}, err
	}
	for _, c := range doc.Connections {
		if c.Name == name {
			return c, nil
		}
	}
	return Connection{}, ErrNotFound
}

func (s *FileStore) Save(c Connection) error {
	if c.Name == "" {
		return fmt.Errorf("connection name is required")
	}
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.close()
	doc, err := s.load()
	if err != nil {
		return err
	}
	for _, existing := range doc.Connections {
		if existing.Name == c.Name {
			return ErrDuplicateName
		}
	}
	doc.Connections = append(doc.Connections, c)
	return s.persist(doc)
}

func (s *FileStore) Put(c Connection) error {
	if c.Name == "" {
		return fmt.Errorf("connection name is required")
	}
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.close()
	doc, err := s.load()
	if err != nil {
		return err
	}
	idx := -1
	for i, existing := range doc.Connections {
		if existing.Name == c.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	doc.Connections[idx] = c
	return s.persist(doc)
}

func (s *FileStore) Delete(name string) error {
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.close()
	doc, err := s.load()
	if err != nil {
		return err
	}
	idx := -1
	for i, c := range doc.Connections {
		if c.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	doc.Connections = append(doc.Connections[:idx], doc.Connections[idx+1:]...)
	return s.persist(doc)
}

func (s *FileStore) Rename(oldName, newName string) error {
	if newName == "" {
		return fmt.Errorf("new connection name is required")
	}
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.close()
	doc, err := s.load()
	if err != nil {
		return err
	}
	oldIdx := -1
	for i, c := range doc.Connections {
		if c.Name == newName {
			return ErrDuplicateName
		}
		if c.Name == oldName {
			oldIdx = i
		}
	}
	if oldIdx < 0 {
		return ErrNotFound
	}
	doc.Connections[oldIdx].Name = newName
	return s.persist(doc)
}

func (s *FileStore) UpsertBySignature(c Connection) (Connection, error) {
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return Connection{}, err
	}
	defer lock.close()
	doc, err := s.load()
	if err != nil {
		return Connection{}, err
	}
	if c.Name != "" {
		for i, existing := range doc.Connections {
			if existing.Name == c.Name {
				doc.Connections[i] = mergeConnection(existing, c)
				if err := s.persist(doc); err != nil {
					return Connection{}, err
				}
				return doc.Connections[i], nil
			}
		}
	}
	sig := c.Signature()
	for i, existing := range doc.Connections {
		if existing.Signature() == sig {
			doc.Connections[i] = mergeConnection(existing, c)
			if err := s.persist(doc); err != nil {
				return Connection{}, err
			}
			return doc.Connections[i], nil
		}
	}
	c.Name = nextConnName(doc.Connections)
	doc.Connections = append(doc.Connections, c)
	if err := s.persist(doc); err != nil {
		return Connection{}, err
	}
	return c, nil
}

func mergeConnection(existing, incoming Connection) Connection {
	updated := existing
	updated.Host = incoming.Host
	updated.Port = incoming.Port
	updated.Database = incoming.Database
	updated.User = incoming.User
	updated.Password = incoming.Password
	if incoming.SSLMODE != "" {
		updated.SSLMODE = incoming.SSLMODE
	}
	if len(incoming.Schemas) > 0 {
		updated.Schemas = append([]string(nil), incoming.Schemas...)
	}
	return updated
}

var connNamePattern = regexp.MustCompile(`^conn-(\d+)$`)

func nextConnName(existing []Connection) string {
	used := make(map[int]struct{})
	for _, c := range existing {
		m := connNamePattern.FindStringSubmatch(c.Name)
		if len(m) == 2 {
			var n int
			if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
				used[n] = struct{}{}
			}
		}
	}
	for n := 1; ; n++ {
		if _, ok := used[n]; !ok {
			return fmt.Sprintf("conn-%d", n)
		}
	}
}

func (s *FileStore) load() (*fileDocument, error) {
	// Enforce owner-only permissions before reading a store that exists.
	if info, err := os.Stat(s.path); err == nil {
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			if chmodErr := os.Chmod(s.path, 0o600); chmodErr != nil {
				return nil, fmt.Errorf("fix connections store permissions (mode %o): %w", perm, chmodErr)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat connections store: %w", err)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &fileDocument{}, nil
		}
		return nil, fmt.Errorf("read connections store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &fileDocument{}, nil
	}
	plain := data
	if isCipherEnvelope(data) {
		plain, err = openCiphertext(data)
		if err != nil {
			return nil, err
		}
	} else if s.encrypt {
		// ponytail: transparently load plaintext store so upgrades don't
		// break existing files. persist() will re-encrypt on next save.
	}
	var doc fileDocument
	if err := yaml.Unmarshal(plain, &doc); err != nil {
		return nil, fmt.Errorf("parse connections store: %w", err)
	}
	return &doc, nil
}

func (s *FileStore) persist(doc *fileDocument) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	plain, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal connections store: %w", err)
	}
	var data []byte
	if s.encrypt {
		data, err = sealPlaintext(plain)
		if err != nil {
			return err
		}
	} else {
		data = plain
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".dolly.connections-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp store file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write connections store: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod connections store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close connections store: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename connections store: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("chmod connections store: %w", err)
	}
	return nil
}
