package tui

import (
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

// mockConnectionStore is an in-memory ConnectionStore for tests.
type mockConnectionStore struct {
	byName map[string]connections.Connection
}

func newMockConnectionStore(initial ...connections.Connection) *mockConnectionStore {
	m := &mockConnectionStore{byName: make(map[string]connections.Connection)}
	for _, c := range initial {
		m.byName[c.Name] = c
	}
	return m
}

func (m *mockConnectionStore) List() ([]connections.Connection, error) {
	out := make([]connections.Connection, 0, len(m.byName))
	for _, c := range m.byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *mockConnectionStore) Get(name string) (connections.Connection, error) {
	c, ok := m.byName[name]
	if !ok {
		return connections.Connection{}, connections.ErrNotFound
	}
	return c, nil
}

func (m *mockConnectionStore) Save(c connections.Connection) error {
	if c.Name == "" {
		return errors.New("connection name is required")
	}
	if _, ok := m.byName[c.Name]; ok {
		return connections.ErrDuplicateName
	}
	m.byName[c.Name] = c
	return nil
}

func (m *mockConnectionStore) Put(c connections.Connection) error {
	if c.Name == "" {
		return errors.New("connection name is required")
	}
	if _, ok := m.byName[c.Name]; !ok {
		return connections.ErrNotFound
	}
	m.byName[c.Name] = c
	return nil
}

func (m *mockConnectionStore) Delete(name string) error {
	if _, ok := m.byName[name]; !ok {
		return connections.ErrNotFound
	}
	delete(m.byName, name)
	return nil
}

func (m *mockConnectionStore) Rename(oldName, newName string) error {
	if newName == "" {
		return errors.New("new connection name is required")
	}
	if _, ok := m.byName[newName]; ok {
		return connections.ErrDuplicateName
	}
	c, ok := m.byName[oldName]
	if !ok {
		return connections.ErrNotFound
	}
	delete(m.byName, oldName)
	c.Name = newName
	m.byName[newName] = c
	return nil
}

func (m *mockConnectionStore) UpsertBySignature(c connections.Connection) (connections.Connection, error) {
	if c.Name != "" {
		if existing, ok := m.byName[c.Name]; ok {
			updated := mergeMockConnection(existing, c)
			m.byName[c.Name] = updated
			return updated, nil
		}
	}
	sig := c.Signature()
	for name, existing := range m.byName {
		if existing.Signature() == sig {
			updated := mergeMockConnection(existing, c)
			m.byName[name] = updated
			return updated, nil
		}
	}
	c.Name = nextMockConnName(m.byName)
	m.byName[c.Name] = c
	return c, nil
}

func mergeMockConnection(existing, incoming connections.Connection) connections.Connection {
	updated := existing
	updated.Host = incoming.Host
	updated.Port = incoming.Port
	updated.Database = incoming.Database
	updated.User = incoming.User
	updated.Password = incoming.Password
	if len(incoming.Schemas) > 0 {
		updated.Schemas = append([]string(nil), incoming.Schemas...)
	}
	return updated
}

var mockConnNamePattern = regexp.MustCompile(`^conn-(\d+)$`)

func nextMockConnName(byName map[string]connections.Connection) string {
	used := make(map[int]struct{})
	for name := range byName {
		m := mockConnNamePattern.FindStringSubmatch(name)
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
