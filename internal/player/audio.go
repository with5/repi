package player

import (
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
)

// SoundCard plays WAV files through MyGo's audio package.
type SoundCard struct{}

type cardHandle struct {
	p    *audio.Player
	done chan struct{}
	once sync.Once // closes done
	free sync.Once // closes the stream
	off  func()
}

// Play implements Output.
func (SoundCard) Play(path string) (Handle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	p, err := audio.OpenStream(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	h := &cardHandle{p: p, done: make(chan struct{})}
	h.off = p.OnEnd(func() { h.once.Do(func() { close(h.done) }) })
	p.Play()
	return h, nil
}

func (h *cardHandle) Done() <-chan struct{} { return h.done }

// Position is where the sound is heard, which the output lags behind what it
// has mixed.
func (h *cardHandle) Position() time.Duration { return h.p.Position() }

// Duration is the length of the sound.
func (h *cardHandle) Duration() time.Duration { return h.p.Duration() }

// Stop ends the sound and releases its file.
func (h *cardHandle) Stop() { h.release() }

func (h *cardHandle) release() {
	h.once.Do(func() { close(h.done) })
	h.free.Do(func() {
		h.off()
		h.p.Close()
	})
}
