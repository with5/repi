package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "m.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Add(Material{Title: "A", Text: "One."})
	b := s.Add(Material{Title: "B", Text: "Two."})
	if a.ID == b.ID || a.Repeat != 3 {
		t.Fatalf("ids %d %d repeat %d", a.ID, b.ID, a.Repeat)
	}
	b.Index = 4
	s.Update(b)
	s.SetSettings(Settings{Voice: "v", Speed: 1.1})
	s.Delete(a.ID)

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l := s2.List()
	if len(l) != 1 || l[0].Title != "B" || l[0].Index != 4 {
		t.Errorf("list = %+v", l)
	}
	if s2.Settings().Voice != "v" {
		t.Errorf("settings = %+v", s2.Settings())
	}
	if c := s2.Add(Material{Title: "C"}); c.ID <= b.ID {
		t.Errorf("new ID %d reuses an old one", c.ID)
	}
}

func TestOldFileHasNoNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	old := `{"settings":{},"materials":[{"id":1,"title":"A","text":"One.","index":0,"mode":0,"repeat":3,"updated":"2026-01-01T00:00:00Z"}],"next":1}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := s.Get(1)
	if m.Pinned || len(m.Tags) != 0 || m.Title != "A" {
		t.Errorf("old material = %+v", m)
	}
}

func TestPinAndTagsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	a := s.Add(Material{Title: "A", Text: "One.", Tags: []string{"work"}})
	s.SetPinned(a.ID, true)
	s2, _ := Open(path)
	m, _ := s2.Get(a.ID)
	if !m.Pinned || len(m.Tags) != 1 || m.Tags[0] != "work" {
		t.Errorf("after reload = %+v", m)
	}
	s2.SetPinned(a.ID, false)
	if m, _ := s2.Get(a.ID); m.Pinned {
		t.Error("still pinned")
	}
}

func TestSettingsMirror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	s.SetSettings(Settings{Mirror: true})
	s2, err := Open(path)
	if err != nil || !s2.Settings().Mirror {
		t.Fatalf("mirror not kept: %v", err)
	}
}
