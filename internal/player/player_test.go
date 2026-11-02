package player

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAdvance(t *testing.T) {
	type pos struct {
		i, r int
		w    bool
	}
	cases := []struct {
		name        string
		idx, rep, n int
		mode        Mode
		repeat      int
		want        pos
	}{
		{"loop next", 0, 0, 3, LoopAll, 3, pos{1, 0, false}},
		{"loop ignores repeat", 1, 0, 3, LoopAll, 3, pos{2, 0, false}},
		{"loop wraps", 2, 0, 3, LoopAll, 3, pos{0, 0, true}},
		{"repeat again", 1, 0, 3, RepeatEach, 3, pos{1, 1, false}},
		{"repeat last rep moves on", 1, 2, 3, RepeatEach, 3, pos{2, 0, false}},
		{"repeat wraps after last sentence", 2, 2, 3, RepeatEach, 3, pos{0, 0, true}},
		{"repeat 1 is plain", 0, 0, 2, RepeatEach, 1, pos{1, 0, false}},
		{"repeat 0 treated as 1", 0, 0, 2, RepeatEach, 0, pos{1, 0, false}},
		{"single sentence wraps", 0, 0, 1, LoopAll, 1, pos{0, 0, true}},
		{"empty", 0, 0, 0, LoopAll, 1, pos{0, 0, false}},
	}
	for _, c := range cases {
		i, r, w := Advance(c.idx, c.rep, c.n, c.mode, c.repeat)
		if (pos{i, r, w}) != c.want {
			t.Errorf("%s: got %v, want %v", c.name, pos{i, r, w}, c.want)
		}
	}
}

// fakes: every sentence "plays" instantly and is logged.
type fakeSrc struct {
	fail map[int]bool
}

func (f fakeSrc) Audio(ctx context.Context, i int) (string, error) {
	if f.fail[i] {
		return "", errors.New("boom")
	}
	return string(rune('a' + i)), nil
}

type fakeOut struct {
	mu  sync.Mutex
	log []string
}

type doneHandle struct{ c chan struct{} }

func (h doneHandle) Done() <-chan struct{}   { return h.c }
func (h doneHandle) Stop()                   {}
func (h doneHandle) Position() time.Duration { return 0 }
func (h doneHandle) Duration() time.Duration { return 0 }

func (o *fakeOut) Play(path string) (Handle, error) {
	o.mu.Lock()
	o.log = append(o.log, path)
	o.mu.Unlock()
	c := make(chan struct{})
	close(c)
	return doneHandle{c}, nil
}

func (o *fakeOut) played() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return join(o.log)
}

func join(s []string) string {
	r := ""
	for _, x := range s {
		r += x
	}
	return r
}

func newFast(src Source, out Output) *Player {
	p := New(src, out, nil)
	p.Gap, p.LoopGap = time.Millisecond, time.Millisecond
	return p
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestLoopAll(t *testing.T) {
	out := &fakeOut{}
	p := newFast(fakeSrc{}, out)
	defer p.Close()
	p.SetCount(3)
	p.Play()
	waitFor(t, func() bool { return len(out.played()) >= 7 })
	p.Pause()
	if got := out.played(); got[:7] != "abcabca" {
		t.Errorf("played %q, want a repeating abc", got)
	}
	if p.State().Loops < 2 {
		t.Errorf("loops = %d", p.State().Loops)
	}
}

func TestRepeatEach(t *testing.T) {
	out := &fakeOut{}
	p := newFast(fakeSrc{}, out)
	defer p.Close()
	p.SetCount(2)
	p.SetMode(RepeatEach)
	p.SetRepeat(3)
	p.Play()
	waitFor(t, func() bool { return len(out.played()) >= 8 })
	p.Pause()
	if got := out.played(); got[:8] != "aaabbbaa" {
		t.Errorf("played %q, want aaabbbaa...", got)
	}
}

func TestSeekAndSkipFailed(t *testing.T) {
	out := &fakeOut{}
	p := newFast(fakeSrc{fail: map[int]bool{1: true}}, out)
	defer p.Close()
	p.SetCount(3)
	p.Seek(1)
	p.Play()
	waitFor(t, func() bool { return len(out.played()) >= 3 })
	p.Pause()
	if got := out.played(); got[:3] != "cac" {
		t.Errorf("played %q, want the failed sentence skipped (cac...)", got)
	}
	if _, ok := p.State().Failed[1]; !ok {
		t.Error("failed sentence not recorded")
	}
}

func TestAllFailedStops(t *testing.T) {
	p := newFast(fakeSrc{fail: map[int]bool{0: true, 1: true}}, &fakeOut{})
	defer p.Close()
	p.SetCount(2)
	p.Play()
	waitFor(t, func() bool { return !p.State().Playing })
}

func TestPauseStopsSound(t *testing.T) {
	out := &blockOut{started: make(chan struct{}, 4)}
	p := newFast(fakeSrc{}, out)
	defer p.Close()
	p.SetCount(2)
	p.Play()
	<-out.started
	p.Pause()
	waitFor(t, func() bool { return out.stopped.Load() == 1 })
	if p.State().Playing {
		t.Error("still playing after Pause")
	}
}

// blockOut plays forever until stopped.
type blockOut struct {
	started chan struct{}
	stopped atomicInt
}

type atomicInt struct {
	mu sync.Mutex
	n  int
}

func (a *atomicInt) Add()      { a.mu.Lock(); a.n++; a.mu.Unlock() }
func (a *atomicInt) Load() int { a.mu.Lock(); defer a.mu.Unlock(); return a.n }

type blockHandle struct {
	c chan struct{}
	o *blockOut
}

func (h blockHandle) Done() <-chan struct{}   { return h.c }
func (h blockHandle) Stop()                   { h.o.stopped.Add() }
func (h blockHandle) Position() time.Duration { return 0 }
func (h blockHandle) Duration() time.Duration { return 0 }

func (o *blockOut) Play(path string) (Handle, error) {
	o.started <- struct{}{}
	return blockHandle{make(chan struct{}), o}, nil
}
