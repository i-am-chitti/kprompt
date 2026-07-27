package learn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kprompt/kprompt/internal/config"
)

// Store persists cluster tool profiles per kube context.
type Store interface {
	Save(Profile) error
	Load(contextName string) (Profile, bool, error)
}

// FileStore writes profiles under Dir (default ~/.kprompt/profiles).
type FileStore struct {
	Dir string
}

// DefaultStore returns a FileStore under ~/.kprompt/profiles.
func DefaultStore() FileStore {
	dir, err := ProfilesDir()
	if err != nil {
		return FileStore{Dir: filepath.Join(".", "profiles")}
	}
	return FileStore{Dir: dir}
}

// ProfilesDir returns ~/.kprompt/profiles (or $KPROMPT_HOME/profiles).
func ProfilesDir() (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "profiles"), nil
}

func (s FileStore) path(contextName string) string {
	safe := sanitizeContext(contextName)
	return filepath.Join(s.Dir, safe+".json")
}

func sanitizeContext(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "_default"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, name)
}

// Save writes the profile atomically (0600).
func (s FileStore) Save(p Profile) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := Encode(p)
	if err != nil {
		return err
	}
	path := s.path(p.Context)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a profile for the given kube context.
func (s FileStore) Load(contextName string) (Profile, bool, error) {
	b, err := os.ReadFile(s.path(contextName))
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, false, nil
		}
		return Profile{}, false, err
	}
	p, err := Decode(b)
	if err != nil {
		return Profile{}, false, err
	}
	return p, true, nil
}

// LoadBestEffort loads from the default store; missing is ok.
func LoadBestEffort(contextName string) (Profile, bool) {
	p, ok, err := DefaultStore().Load(contextName)
	if err != nil || !ok {
		return Profile{}, false
	}
	return p, true
}

// MemStore is an in-process store for tests.
type MemStore struct {
	mu   sync.Mutex
	data map[string]Profile
}

// NewMemStore creates an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{data: map[string]Profile{}}
}

func (s *MemStore) Save(p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]Profile{}
	}
	s.data[sanitizeContext(p.Context)] = p
	return nil
}

func (s *MemStore) Load(contextName string) (Profile, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[sanitizeContext(contextName)]
	if !ok {
		return Profile{}, false, nil
	}
	return p, true, nil
}

// PathFor is exported for doctor/CLI messaging.
func PathFor(contextName string) (string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizeContext(contextName)+".json"), nil
}

// MustPath formats a display path even when Dir() fails.
func MustPath(contextName string) string {
	path, err := PathFor(contextName)
	if err != nil {
		return fmt.Sprintf("~/.kprompt/profiles/%s.json", sanitizeContext(contextName))
	}
	return path
}
