// Package player is the playback state machine: it plays sentences one after
// another, looping the whole text or repeating each sentence N times, and
// knows nothing of windows, piper or the sound card.
package player

import (
	"context"
	"sync"
	"time"
)

// Mode says what to do at the end of a sentence.
type Mode int

const (
	// LoopAll plays the sentences in order, then again from the first.
	LoopAll Mode = iota
	// RepeatEach plays each sentence Repeat times before the next one.
	RepeatEach
)

// Source gives the audio file of sentence i, waiting until it is ready.
type Source interface {
	Audio(ctx context.Context, i int) (path string, err error)
}

// Handle is one sound being played.
type Handle interface {
	// Done is closed when the sound has played to its end.
	Done() <-chan struct{}
	// Position is how far the sound has played, as heard; Duration its length.
	Position() time.Duration
	Duration() time.Duration
	// Stop ends the sound early and releases it; it is called once the
	// sound is over too, and may be called any number of times.
	Stop()
}

// Output plays a WAV file.
type Output interface {
	Play(path string) (Handle, error)
}

// Snapshot is the state the interface shows.
type Snapshot struct {
	Index   int  // sentence being played (or to be played)
	Rep     int  // 0-based repetition of Index in RepeatEach
	Playing bool // wanted to be playing (it may be waiting for synthesis)
	Waiting bool // waiting for a sentence to be synthesized
	Loops   int  // completed rounds of the whole text
	Mode    Mode
	Repeat  int
	Failed  map[int]string // sentences that could not be played
}

// Advance returns the position after sentence idx finished its repetition
// rep in a text of n sentences, and whether the text wrapped around.
func Advance(idx, rep, n int, mode Mode, repeat int) (nidx, nrep int, wrapped bool) {
	if n <= 0 {
		return 0, 0, false
	}
	if mode == RepeatEach && rep+1 < max(repeat, 1) {
		return idx, rep + 1, false
	}
	if idx+1 >= n {
		return 0, 0, true
	}
	return idx + 1, 0, false
}

type nowPlaying struct {
	index int
	path  string
	h     Handle
}

// Now describes the sound being played.
type Now struct {
	Index      int           // sentence
	Path       string        // its audio file
	Pos, Total time.Duration // how far it has played, and its length
}

// Now returns the sound being played, if one is.
func (p *Player) Now() (Now, bool) {
	p.mu.Lock()
	n := p.now
	p.mu.Unlock()
	if n == nil {
		return Now{}, false
	}
	return Now{Index: n.index, Path: n.path, Pos: n.h.Position(), Total: n.h.Duration()}, true
}

// Player runs playback on one goroutine; its methods may be called from any.
type Player struct {
	src Source
	out Output
	// Gap is the silence between sentences, LoopGap between rounds.
	Gap, LoopGap time.Duration
	// OnHeard, when set, is called after each sound with how long of it was
	// heard (all of it unless it was cut short). Set it before playing.
	OnHeard func(time.Duration)

	notify func()

	mu     sync.Mutex
	n      int
	snap   Snapshot
	cancel context.CancelFunc // ends the iteration in progress
	now    *nowPlaying        // the sound being played, nil between sounds
	wake   chan struct{}
	quit   chan struct{}
	closed bool
}

// New starts a player. notify is called (on any goroutine) after the
// snapshot changed.
func New(src Source, out Output, notify func()) *Player {
	p := &Player{
		src: src, out: out, notify: notify,
		Gap: 350 * time.Millisecond, LoopGap: 1200 * time.Millisecond,
		wake: make(chan struct{}, 1), quit: make(chan struct{}),
		snap: Snapshot{Repeat: 3, Failed: map[int]string{}},
	}
	go p.run()
	return p
}

// Close stops playback for good.
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.snap.Playing = false
	close(p.quit)
	p.interruptLocked()
	p.mu.Unlock()
}

// State returns a copy of the current state.
func (p *Player) State() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.snap
	s.Failed = make(map[int]string, len(p.snap.Failed))
	for k, v := range p.snap.Failed {
		s.Failed[k] = v
	}
	return s
}

// SetCount sets the number of sentences and goes back to the first, paused.
func (p *Player) SetCount(n int) {
	p.update(func(s *Snapshot) {
		p.n = n
		*s = Snapshot{Mode: s.Mode, Repeat: s.Repeat, Failed: map[int]string{}}
	}, true)
}

// Play starts or resumes.
func (p *Player) Play() {
	p.update(func(s *Snapshot) { s.Playing = p.n > 0 }, false)
}

// Pause stops the sound; Play starts the current sentence again.
func (p *Player) Pause() {
	p.update(func(s *Snapshot) { s.Playing = false }, true)
}

// Toggle switches between Play and Pause.
func (p *Player) Toggle() {
	if p.State().Playing {
		p.Pause()
	} else {
		p.Play()
	}
}

// Seek jumps to sentence i, keeping the playing state.
func (p *Player) Seek(i int) {
	p.update(func(s *Snapshot) {
		if p.n > 0 {
			s.Index, s.Rep = min(max(i, 0), p.n-1), 0
		}
	}, true)
}

// Next jumps to the next sentence, wrapping around.
func (p *Player) Next() { p.Seek((p.State().Index + 1) % max(p.count(), 1)) }

// Prev jumps to the previous sentence, wrapping around.
func (p *Player) Prev() {
	n := max(p.count(), 1)
	p.Seek((p.State().Index + n - 1) % n)
}

// SetMode selects looping the text or repeating each sentence.
func (p *Player) SetMode(m Mode) { p.update(func(s *Snapshot) { s.Mode, s.Rep = m, 0 }, false) }

// SetRepeat sets how often RepeatEach plays a sentence.
func (p *Player) SetRepeat(n int) { p.update(func(s *Snapshot) { s.Repeat = max(n, 1) }, false) }

// Restart plays the current sentence again from its start, as after the
// voice or speed changed.
func (p *Player) Restart() { p.update(func(s *Snapshot) { s.Rep = 0 }, true) }

func (p *Player) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func (p *Player) update(f func(*Snapshot), interrupt bool) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	f(&p.snap)
	if interrupt {
		p.interruptLocked()
	}
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
	p.changed()
}

func (p *Player) interruptLocked() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *Player) changed() {
	if p.notify != nil {
		p.notify()
	}
}

func (p *Player) run() {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		if !p.snap.Playing || p.n == 0 {
			p.mu.Unlock()
			select {
			case <-p.wake:
				continue
			case <-p.quit:
				return
			}
		}
		idx := p.snap.Index
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.snap.Waiting = true
		p.mu.Unlock()
		p.changed()

		err := p.playOne(ctx, idx)

		p.mu.Lock()
		interrupted := ctx.Err() != nil // by a command, before we cancel it ourselves
		cancel()
		p.cancel = nil
		p.snap.Waiting = false
		if interrupted || p.closed {
			// Interrupted by a command, which already set the new position.
			p.mu.Unlock()
			p.changed()
			continue
		}
		if err != nil {
			p.snap.Failed[idx] = err.Error()
		} else {
			delete(p.snap.Failed, idx)
		}
		var wrapped bool
		p.snap.Index, p.snap.Rep, wrapped = Advance(idx, p.snap.Rep, p.n, p.snap.Mode, p.snap.Repeat)
		gap := p.Gap
		if wrapped {
			p.snap.Loops++
			gap = p.LoopGap
		}
		allFailed := len(p.snap.Failed) >= p.n
		if allFailed {
			p.snap.Playing = false // nothing can be played: do not spin
		}
		p.mu.Unlock()
		p.changed()
		select {
		case <-time.After(gap):
		case <-p.wake:
		case <-p.quit:
			return
		}
	}
}

// playOne gets and plays sentence i; it returns early when ctx ends.
func (p *Player) playOne(ctx context.Context, i int) error {
	path, err := p.src.Audio(ctx, i)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.mu.Lock()
	p.snap.Waiting = false
	p.mu.Unlock()
	p.changed()
	h, err := p.out.Play(path)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.now = &nowPlaying{index: i, path: path, h: h}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.now = nil
		p.mu.Unlock()
		if p.OnHeard != nil {
			if heard := min(h.Position(), h.Duration()); heard > 0 {
				p.OnHeard(heard)
			}
		}
		h.Stop()
	}()
	select {
	case <-h.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
