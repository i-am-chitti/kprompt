// Package remember stores durable local operator facts (S-015 · ADR-0022).
//
// Privacy: ~/.kprompt/memory.json only — never uploaded to the control plane by default.
package remember

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const Version = 1

// Fact is one local memory entry.
type Fact struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Namespace string    `json:"namespace,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the on-disk memory document.
type Store struct {
	Version int    `json:"version"`
	Facts   []Fact `json:"facts"`
}

// DefaultPath returns ~/.kprompt/memory.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kprompt", "memory.json"), nil
}

// Load reads the store (empty if missing).
func Load() (Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return Store{}, err
	}
	return LoadPath(path)
}

// LoadPath loads from a custom path (tests).
func LoadPath(path string) (Store, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: Version}, nil
		}
		return Store{}, err
	}
	var s Store
	if err := json.Unmarshal(raw, &s); err != nil {
		return Store{}, err
	}
	if s.Version == 0 {
		s.Version = Version
	}
	return s, nil
}

// Save writes the store with restrictive permissions.
func Save(s Store) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return SavePath(path, s)
}

// SavePath writes to a custom path (tests).
func SavePath(path string, s Store) error {
	if s.Version == 0 {
		s.Version = Version
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// Upsert adds or updates a fact by key (+ optional namespace scope).
func Upsert(key, value, namespace string) (Fact, error) {
	path, err := DefaultPath()
	if err != nil {
		return Fact{}, err
	}
	return UpsertPath(path, key, value, namespace)
}

// UpsertPath is Upsert for tests.
func UpsertPath(path, key, value, namespace string) (Fact, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	namespace = strings.TrimSpace(namespace)
	if key == "" || value == "" {
		return Fact{}, fmt.Errorf("remember: key and value required")
	}
	s, err := LoadPath(path)
	if err != nil {
		return Fact{}, err
	}
	now := time.Now().UTC()
	for i := range s.Facts {
		if s.Facts[i].Key == key && s.Facts[i].Namespace == namespace {
			s.Facts[i].Value = value
			s.Facts[i].UpdatedAt = now
			if err := SavePath(path, s); err != nil {
				return Fact{}, err
			}
			return s.Facts[i], nil
		}
	}
	f := Fact{Key: key, Value: value, Namespace: namespace, CreatedAt: now, UpdatedAt: now}
	s.Facts = append(s.Facts, f)
	sort.Slice(s.Facts, func(i, j int) bool {
		if s.Facts[i].Namespace != s.Facts[j].Namespace {
			return s.Facts[i].Namespace < s.Facts[j].Namespace
		}
		return s.Facts[i].Key < s.Facts[j].Key
	})
	if err := SavePath(path, s); err != nil {
		return Fact{}, err
	}
	return f, nil
}

// Forget removes facts matching key (and optional namespace).
func Forget(key, namespace string) (int, error) {
	path, err := DefaultPath()
	if err != nil {
		return 0, err
	}
	return ForgetPath(path, key, namespace)
}

// ForgetPath is Forget for tests.
func ForgetPath(path, key, namespace string) (int, error) {
	key = strings.TrimSpace(key)
	namespace = strings.TrimSpace(namespace)
	if key == "" {
		return 0, fmt.Errorf("forget: key required")
	}
	s, err := LoadPath(path)
	if err != nil {
		return 0, err
	}
	kept := s.Facts[:0]
	removed := 0
	for _, f := range s.Facts {
		if f.Key == key && (namespace == "" || f.Namespace == namespace) {
			removed++
			continue
		}
		kept = append(kept, f)
	}
	s.Facts = kept
	if err := SavePath(path, s); err != nil {
		return 0, err
	}
	return removed, nil
}

// List returns all facts (optionally filtered by namespace).
func List(namespace string) ([]Fact, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return s.Facts, nil
	}
	var out []Fact
	for _, f := range s.Facts {
		if f.Namespace == namespace || f.Namespace == "" {
			out = append(out, f)
		}
	}
	return out, nil
}

// ParseStatement turns "payment ns = Team A" or "payments=Team A" into key/value.
func ParseStatement(raw string) (key, value string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty remember statement")
	}
	for _, sep := range []string{" = ", "=", " is ", ": "} {
		if i := strings.Index(strings.ToLower(raw), strings.ToLower(sep)); i >= 0 {
			key = strings.TrimSpace(raw[:i])
			value = strings.TrimSpace(raw[i+len(sep):])
			if key != "" && value != "" {
				return key, value, nil
			}
		}
	}
	return "", "", fmt.Errorf("could not parse %q (use: key = value)", raw)
}

// PromptBias injects local facts into intent extraction (never invents cluster APIs).
func PromptBias() string {
	facts, err := List("")
	if err != nil || len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Local operator memory (from kprompt remember; local-only, not cluster proof):\n")
	limit := 20
	if len(facts) < limit {
		limit = len(facts)
	}
	for _, f := range facts[:limit] {
		if f.Namespace != "" {
			fmt.Fprintf(&b, "- [%s] %s = %s\n", f.Namespace, f.Key, f.Value)
		} else {
			fmt.Fprintf(&b, "- %s = %s\n", f.Key, f.Value)
		}
	}
	b.WriteString("Prefer these as planning hints when relevant; live cluster reads still win.\n")
	return b.String()
}
