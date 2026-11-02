package tts

import (
	"context"
	"sync"
)

// Pool de-duplicates concurrent synthesis of the same sentence, so the
// player waiting on a sentence and the prefetcher share one piper process.
type Pool struct {
	S Synth

	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	path string
	err  error
}

// Get synthesizes (or finds in the cache) one sentence and returns its WAV
// path. Cancelling ctx abandons the wait, not the synthesis in progress.
func (p *Pool) Get(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	key := CacheKey(sentence, voice, speed)
	p.mu.Lock()
	if p.inflight == nil {
		p.inflight = map[string]*call{}
	}
	c, ok := p.inflight[key]
	if !ok {
		c = &call{done: make(chan struct{})}
		p.inflight[key] = c
		go func() {
			c.path, c.err = p.S.Synth(context.Background(), sentence, voice, speed)
			p.mu.Lock()
			delete(p.inflight, key)
			p.mu.Unlock()
			close(c.done)
		}()
	}
	p.mu.Unlock()
	select {
	case <-c.done:
		return c.path, c.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
