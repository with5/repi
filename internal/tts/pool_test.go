package tts

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type slowSynth struct{ n atomic.Int32 }

func (s *slowSynth) Synth(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	s.n.Add(1)
	time.Sleep(30 * time.Millisecond)
	return "/x/" + sentence, nil
}

func TestPoolDeduplicates(t *testing.T) {
	s := &slowSynth{}
	p := &Pool{S: s}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if path, err := p.Get(context.Background(), "a", "v", 1); err != nil || path != "/x/a" {
				t.Errorf("Get = %q, %v", path, err)
			}
		}()
	}
	wg.Wait()
	if s.n.Load() != 1 {
		t.Errorf("synthesized %d times, want 1", s.n.Load())
	}
}

func TestPoolCancel(t *testing.T) {
	p := &Pool{S: &slowSynth{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Get(ctx, "b", "v", 1); err == nil {
		t.Error("cancelled Get returned no error")
	}
}
