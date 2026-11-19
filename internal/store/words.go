package store

import (
	"sort"
	"strings"
	"time"
)

// Word is a saved word: what the dictionary said, the sentence it was met
// in, and when to review it next.
type Word struct {
	Word     string    `json:"word"`           // headword: the base form of what was clicked
	Form     string    `json:"form,omitempty"` // as written in Sentence, when not Word
	Phonetic string    `json:"phonetic,omitempty"`
	Meanings []string  `json:"meanings,omitempty"`
	Sentence string    `json:"sentence,omitempty"`
	Material int       `json:"material,omitempty"` // ID of the material Sentence is from
	Added    time.Time `json:"added"`

	// The review schedule, after SM-2.
	Due      time.Time `json:"due"`
	Interval int       `json:"interval,omitempty"` // days between the last two reviews
	Ease     float64   `json:"ease,omitempty"`
	Reps     int       `json:"reps,omitempty"`   // reviews in a row that were remembered
	Lapses   int       `json:"lapses,omitempty"` // times forgotten
	Reviewed time.Time `json:"reviewed,omitzero"`
}

// Grade is how well a word was remembered in a review.
type Grade int

const (
	Again Grade = iota // forgotten: seen again in this session
	Hard               // remembered, with effort
	Good               // remembered
	Easy               // remembered at once
)

const startEase = 2.5

func wordKey(w string) string { return strings.ToLower(strings.TrimSpace(w)) }

// Words lists the saved words, newest first, as copies.
func (s *Store) Words() []Word {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Word, 0, len(s.f.Words))
	for i := len(s.f.Words) - 1; i >= 0; i-- {
		out = append(out, *s.f.Words[i])
	}
	return out
}

func (s *Store) find(word string) (int, *Word) {
	k := wordKey(word)
	for i, w := range s.f.Words {
		if wordKey(w.Word) == k {
			return i, w
		}
	}
	return -1, nil
}

// HasWord reports whether word is saved.
func (s *Store) HasWord(word string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, w := s.find(word)
	return w != nil
}

// AddWord saves w, due for review now. A word saved already keeps its
// schedule; only its dictionary text and sentence are replaced.
func (s *Store) AddWord(w Word, now time.Time) {
	s.mu.Lock()
	if _, old := s.find(w.Word); old != nil {
		old.Form, old.Phonetic, old.Meanings = w.Form, w.Phonetic, w.Meanings
		if w.Sentence != "" {
			old.Sentence, old.Material = w.Sentence, w.Material
		}
	} else {
		w.Added, w.Due, w.Ease = now, now, startEase
		w.Interval, w.Reps, w.Lapses, w.Reviewed = 0, 0, 0, time.Time{}
		s.f.Words = append(s.f.Words, &w)
	}
	s.mu.Unlock()
	s.Save()
}

// RemoveWord forgets word.
func (s *Store) RemoveWord(word string) {
	s.mu.Lock()
	if i, w := s.find(word); w != nil {
		s.f.Words = append(s.f.Words[:i], s.f.Words[i+1:]...)
	}
	s.mu.Unlock()
	s.Save()
}

// DueWords lists the words due for review at now, the longest overdue first.
func (s *Store) DueWords(now time.Time) []Word {
	s.mu.Lock()
	var out []Word
	for _, w := range s.f.Words {
		if !w.Due.After(now) {
			out = append(out, *w)
		}
	}
	s.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	return out
}

// DueCount is len(DueWords(now)), without the copies.
func (s *Store) DueCount(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, w := range s.f.Words {
		if !w.Due.After(now) {
			n++
		}
	}
	return n
}

// Review schedules word after a review graded g at now, and returns it.
func (s *Store) Review(word string, g Grade, now time.Time) (Word, bool) {
	s.mu.Lock()
	_, w := s.find(word)
	if w == nil {
		s.mu.Unlock()
		return Word{}, false
	}
	*w = w.Next(g, now)
	out := *w
	s.mu.Unlock()
	s.Save()
	return out, true
}

// Next is w after a review graded g at now. It is SM-2 with four grades,
// counted in whole days: a word is due at the start of its day, so a
// morning review is not put off by hours.
func (w Word) Next(g Grade, now time.Time) Word {
	if w.Ease == 0 {
		w.Ease = startEase
	}
	w.Reviewed = now
	if g == Again {
		w.Lapses++
		w.Reps, w.Interval = 0, 0
		w.Ease = max(1.3, w.Ease-0.2)
		w.Due = now // still due: the session shows it again
		return w
	}
	var days float64
	switch {
	case w.Reps == 0:
		days = map[Grade]float64{Hard: 1, Good: 1, Easy: 4}[g]
	case w.Reps == 1 && g != Hard:
		days = map[Grade]float64{Good: 3, Easy: 6}[g]
	default:
		f := map[Grade]float64{Hard: 1.2, Good: w.Ease, Easy: w.Ease * 1.3}[g]
		days = max(float64(w.Interval)*f, float64(w.Interval+1))
	}
	w.Ease = max(1.3, w.Ease+map[Grade]float64{Hard: -0.15, Good: 0, Easy: 0.15}[g])
	w.Reps++
	w.Interval = int(days + 0.5)
	y, mo, d := now.Date()
	w.Due = time.Date(y, mo, d+w.Interval, 0, 0, 0, 0, now.Location())
	return w
}
