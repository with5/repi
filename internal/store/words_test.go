package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWordsSaveAndSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	now := time.Date(2026, 11, 11, 9, 30, 0, 0, time.Local)

	s.AddWord(Word{Word: "go", Form: "went", Sentence: "She went home.", Meanings: []string{"v. 去"}}, now)
	if !s.HasWord("Go") {
		t.Fatal("not saved")
	}
	if n := s.DueCount(now); n != 1 {
		t.Fatalf("due = %d, want 1", n)
	}

	// Remembered: due tomorrow at midnight, not in 24 hours.
	w, _ := s.Review("go", Good, now)
	if want := time.Date(2026, 11, 12, 0, 0, 0, 0, time.Local); !w.Due.Equal(want) || w.Interval != 1 {
		t.Fatalf("after Good: due %v interval %d", w.Due, w.Interval)
	}
	if s.DueCount(now) != 0 || s.DueCount(now.AddDate(0, 0, 1)) != 1 {
		t.Fatal("due count does not follow the schedule")
	}
	w, _ = s.Review("go", Good, now.AddDate(0, 0, 1))
	if w.Interval != 3 {
		t.Fatalf("second Good: interval %d, want 3", w.Interval)
	}
	w, _ = s.Review("go", Good, now.AddDate(0, 0, 4))
	if w.Interval != 8 { // 3 × 2.5, rounded
		t.Fatalf("third Good: interval %d, want 8", w.Interval)
	}

	// Forgotten: due again at once, starting over with less ease.
	w, _ = s.Review("go", Again, now.AddDate(0, 0, 12))
	if w.Reps != 0 || w.Lapses != 1 || w.Ease != 2.3 || w.Due.After(now.AddDate(0, 0, 12)) {
		t.Fatalf("after Again: %+v", w)
	}

	// Saving it again keeps the schedule, takes the new sentence.
	s.AddWord(Word{Word: "go", Form: "goes", Sentence: "It goes on."}, now.AddDate(0, 0, 13))
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := again.Words()
	if len(ws) != 1 || ws[0].Lapses != 1 || ws[0].Sentence != "It goes on." || !ws[0].Added.Equal(now) {
		t.Fatalf("on disk: %+v", ws)
	}

	again.RemoveWord("GO")
	if again.HasWord("go") || len(again.Words()) != 0 {
		t.Fatal("not removed")
	}
}

func TestDueWordsOrder(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "m.json"))
	now := time.Now()
	s.AddWord(Word{Word: "late"}, now)
	s.AddWord(Word{Word: "early"}, now.Add(-time.Hour))
	s.AddWord(Word{Word: "future"}, now)
	s.Review("future", Easy, now)
	got := s.DueWords(now)
	if len(got) != 2 || got[0].Word != "early" || got[1].Word != "late" {
		t.Fatalf("due = %+v", got)
	}
}
