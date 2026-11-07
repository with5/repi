// Package store keeps the study materials and their playback settings in a
// JSON file.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Material is one piece of study text and where the user is in it.
type Material struct {
	ID      int       `json:"id"`
	Title   string    `json:"title"`
	Text    string    `json:"text"`
	Index   int       `json:"index"`  // sentence to continue from
	Mode    int       `json:"mode"`   // player.Mode
	Repeat  int       `json:"repeat"` // repetitions per sentence in RepeatEach
	Updated time.Time `json:"updated"`
	Pinned  bool      `json:"pinned,omitempty"`
	Tags    []string  `json:"tags,omitempty"`
}

// Settings are shared by all materials.
type Settings struct {
	Voice      string  `json:"voice"`
	Speed      float64 `json:"speed"`
	HideText   bool    `json:"hideText"`
	LastOpened int     `json:"lastOpened"`
	// Favorites are the voices shown while reading, in the order added.
	Favorites []string `json:"favorites,omitempty"`
	// Translate shows the Chinese of the sentence under it on the big screen.
	Translate bool `json:"translate,omitempty"`
	// BigScreen shows the sentence being read in large type, BigFont its size.
	BigScreen bool `json:"bigScreen,omitempty"`
	BigFont   int  `json:"bigFont,omitempty"`
	// DailyGoal is the listening aimed for each day, in minutes; 0 means none.
	DailyGoal int `json:"dailyGoal,omitempty"`
	// Mirror makes the one-click Kokoro setup use mirrors in China.
	Mirror bool `json:"mirror,omitempty"`
}

type file struct {
	Settings  Settings    `json:"settings"`
	Materials []*Material `json:"materials"`
	Next      int         `json:"next"`
	// Listened is the time heard per local day ("2006-01-02"), in milliseconds.
	Listened map[string]int64 `json:"listened,omitempty"`
	// PerMaterial is the time heard per material ID, in milliseconds. It is
	// kept apart from Material so that saving a stale copy cannot reset it.
	PerMaterial map[int]int64 `json:"perMaterial,omitempty"`
}

// Store is safe for use from several goroutines.
type Store struct {
	path      string
	mu        sync.Mutex
	f         file
	lastStats time.Time  // last save caused by AddListen
	saveMu    sync.Mutex // one Save at a time: they share the temporary file
}

// DefaultPath is materials.json inside DataDir.
func DefaultPath() string { return filepath.Join(DataDir(), "materials.json") }

// Open loads the store at path; a missing file gives an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s.f); err != nil {
		return s, err
	}
	for _, m := range s.f.Materials {
		s.f.Next = max(s.f.Next, m.ID)
	}
	return s, nil
}

// Settings returns the shared settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Settings
}

// SetSettings replaces the shared settings and saves.
func (s *Store) SetSettings(v Settings) error {
	s.mu.Lock()
	s.f.Settings = v
	s.mu.Unlock()
	return s.Save()
}

// List returns the materials, newest first, as copies.
func (s *Store) List() []Material {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Material, 0, len(s.f.Materials))
	for i := len(s.f.Materials) - 1; i >= 0; i-- {
		out = append(out, *s.f.Materials[i])
	}
	return out
}

// Get returns the material with id.
func (s *Store) Get(id int) (Material, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.f.Materials {
		if m.ID == id {
			return *m, true
		}
	}
	return Material{}, false
}

// Add stores a new material and returns it with its ID.
func (s *Store) Add(m Material) Material {
	s.mu.Lock()
	s.f.Next++
	m.ID = s.f.Next
	m.Updated = time.Now()
	if m.Repeat < 1 {
		m.Repeat = 3
	}
	s.f.Materials = append(s.f.Materials, &m)
	s.mu.Unlock()
	s.Save()
	return m
}

// Update replaces the material with m.ID and saves.
func (s *Store) Update(m Material) {
	s.mu.Lock()
	for i, old := range s.f.Materials {
		if old.ID == m.ID {
			m.Updated = time.Now()
			s.f.Materials[i] = &m
		}
	}
	s.mu.Unlock()
	s.Save()
}

// SetPinned pins or unpins the material with id and saves.
func (s *Store) SetPinned(id int, pinned bool) {
	s.mu.Lock()
	for _, m := range s.f.Materials {
		if m.ID == id {
			m.Pinned = pinned
		}
	}
	s.mu.Unlock()
	s.Save()
}

// Delete removes the material with id.
func (s *Store) Delete(id int) {
	s.mu.Lock()
	kept := s.f.Materials[:0]
	for _, m := range s.f.Materials {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	s.f.Materials = kept
	delete(s.f.PerMaterial, id)
	s.mu.Unlock()
	s.Save()
}

// Save writes the file atomically.
func (s *Store) Save() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	b, err := json.MarshalIndent(s.f, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
