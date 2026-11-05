package store

import (
	"sort"
	"time"
)

const dayLayout = "2006-01-02"

// statsSaveEvery limits how often AddListen writes the file.
const statsSaveEvery = 5 * time.Second

// DayStat is the time listened on one day.
type DayStat struct {
	Day      time.Time // local midnight
	Listened time.Duration
}

// AddListen adds d of heard audio to the day of now and, when id is not 0,
// to material id. It saves at most every few seconds; Save writes the rest.
func (s *Store) AddListen(now time.Time, id int, d time.Duration) {
	if d <= 0 {
		return
	}
	s.mu.Lock()
	if s.f.Listened == nil {
		s.f.Listened = map[string]int64{}
	}
	s.f.Listened[now.Format(dayLayout)] += d.Milliseconds()
	if id != 0 {
		if s.f.PerMaterial == nil {
			s.f.PerMaterial = map[int]int64{}
		}
		s.f.PerMaterial[id] += d.Milliseconds()
	}
	due := time.Since(s.lastStats) >= statsSaveEvery
	if due {
		s.lastStats = time.Now()
	}
	s.mu.Unlock()
	if due {
		s.Save()
	}
}

// Daily returns the last n days up to and including today, oldest first;
// days without listening are zero.
func (s *Store) Daily(now time.Time, n int) []DayStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DayStat, n)
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	for i := range out {
		day := today.AddDate(0, 0, i-(n-1))
		out[i] = DayStat{Day: day, Listened: time.Duration(s.f.Listened[day.Format(dayLayout)]) * time.Millisecond}
	}
	return out
}

// TotalListened is all the time ever listened, and the number of days with any.
func (s *Store) TotalListened() (total time.Duration, days int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ms := range s.f.Listened {
		if ms > 0 {
			total += time.Duration(ms) * time.Millisecond
			days++
		}
	}
	return total, days
}

// Streak counts the consecutive days with listening that end today, or
// yesterday when today has none yet.
func (s *Store) Streak(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := now
	if s.f.Listened[day.Format(dayLayout)] <= 0 {
		day = day.AddDate(0, 0, -1)
	}
	n := 0
	for s.f.Listened[day.Format(dayLayout)] > 0 {
		n++
		day = day.AddDate(0, 0, -1)
	}
	return n
}

// MaterialListened is the time heard of material id.
func (s *Store) MaterialListened(id int) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(s.f.PerMaterial[id]) * time.Millisecond
}

// MaterialStat is the time heard of one material.
type MaterialStat struct {
	ID       int
	Title    string
	Listened time.Duration
}

// ByMaterial lists the materials with any listening, longest first.
func (s *Store) ByMaterial() []MaterialStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []MaterialStat
	for _, m := range s.f.Materials {
		if ms := s.f.PerMaterial[m.ID]; ms > 0 {
			out = append(out, MaterialStat{m.ID, m.Title, time.Duration(ms) * time.Millisecond})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Listened > out[j].Listened })
	return out
}
