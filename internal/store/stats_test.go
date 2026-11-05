package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestListenStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	a := s.Add(Material{Title: "a"})
	b := s.Add(Material{Title: "b"})
	s.AddListen(now, a.ID, 90*time.Second)
	s.AddListen(now, a.ID, 30*time.Second)
	s.AddListen(now.AddDate(0, 0, -1), b.ID, time.Minute)
	s.AddListen(now.AddDate(0, 0, -3), 0, time.Minute)
	s.AddListen(now, a.ID, 0)

	d := s.Daily(now, 7)
	if len(d) != 7 || d[6].Listened != 2*time.Minute || d[5].Listened != time.Minute || d[4].Listened != 0 || d[3].Listened != time.Minute {
		t.Fatalf("daily: %+v", d)
	}
	if got := s.Streak(now); got != 2 {
		t.Errorf("streak %d, want 2", got)
	}
	if got := s.Streak(now.AddDate(0, 0, 1)); got != 2 { // tomorrow, nothing yet: yesterday counts
		t.Errorf("streak tomorrow %d, want 2", got)
	}
	if tot, days := s.TotalListened(); tot != 4*time.Minute || days != 3 {
		t.Errorf("total %v over %d days", tot, days)
	}

	if bm := s.ByMaterial(); len(bm) != 2 || bm[0].Title != "a" || bm[0].Listened != 2*time.Minute || bm[1].Listened != time.Minute {
		t.Errorf("by material: %+v", bm)
	}
	s.Delete(b.ID)
	if len(s.ByMaterial()) != 1 || s.MaterialListened(b.ID) != 0 {
		t.Error("deleted material keeps its time")
	}

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	r, _ := Open(path)
	if r.Daily(now, 1)[0].Listened != 2*time.Minute {
		t.Error("not persisted")
	}
}
