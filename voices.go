package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"repi/internal/tts"
)

// defaultSample is what the voice panel speaks, until the user types another.
const defaultSample = "The quick brown fox jumps over the lazy dog. Can you hear the difference?"

// voiceInfo describes a voice for the voice panel.
type voiceInfo struct {
	Name   string // the value stored and passed to the synthesizer
	Label  string
	Detail string
}

// voiceList lists every usable voice: the user's cloned voices first, then
// Kokoro, then piper.
func (m *model) voiceList() []voiceInfo {
	var out []voiceInfo
	for _, v := range m.cloneVoices() {
		out = append(out, voiceInfo{tts.ClonePrefix + v.ID, v.Name, fmt.Sprintf("克隆 · Chatterbox · 录音 %.0f 秒", v.Seconds)})
	}
	if m.kokoro.Check() == nil {
		for _, v := range tts.KokoroVoices {
			out = append(out, voiceInfo{tts.KokoroPrefix + v.ID, v.Label, "Kokoro · " + v.Detail})
		}
	}
	for _, v := range m.piper.Voices {
		out = append(out, voiceInfo{v.Name, v.Name, "Piper · " + piperDetail(v.Name)})
	}
	return out
}

// piperDetail turns "en_US-amy-medium" into "en_US, medium".
func piperDetail(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return parts[0] + ", " + parts[len(parts)-1]
	}
	return name
}

func (m *model) isFavorite(name string) bool { return slices.Contains(m.favorites, name) }

// toggleFavorite adds or removes a voice from the favorites.
func (m *model) toggleFavorite(name string) {
	if i := slices.Index(m.favorites, name); i >= 0 {
		m.favorites = slices.Delete(slices.Clone(m.favorites), i, i+1)
	} else {
		m.favorites = append(slices.Clone(m.favorites), name)
	}
	m.saveSettings()
}

// voiceLabels are what the reader's selector shows for voices: a cloned
// voice by the name the user gave it, others by their stored name. A name
// that would be shown twice falls back to the stored name.
func (m *model) voiceLabels(voices []string) []string {
	names := map[string]string{}
	for _, v := range m.cloneVoices() {
		names[tts.ClonePrefix+v.ID] = v.Name + "（我的声音）"
	}
	out := make([]string, len(voices))
	seen := map[string]bool{}
	for i, v := range voices {
		l, ok := names[v]
		if !ok || seen[l] || slices.Contains(voices, l) {
			l = v
		}
		seen[l] = true
		out[i] = l
	}
	return out
}

// readingVoices are the voices the reader offers: the favorites that can be
// used, and the voice in use (so the selector never shows a voice that is not
// the one speaking).
func (m *model) readingVoices() []string {
	var out []string
	for _, f := range m.favorites {
		if m.hasVoice(f) {
			out = append(out, f)
		}
	}
	if m.voice != "" && !slices.Contains(out, m.voice) {
		out = append([]string{m.voice}, out...)
	}
	return out
}

// auditioner speaks a sample in one voice at a time, apart from the player.
type auditioner struct {
	mu     sync.Mutex
	gen    int
	voice  string // voice being auditioned, "" when none
	state  string // "合成中" or "播放中"
	err    string
	cancel context.CancelFunc
}

// auditionState returns the voice being auditioned, what it is doing, and the
// last error.
func (m *model) auditionState() (voice, state, err string) {
	m.aud.mu.Lock()
	defer m.aud.mu.Unlock()
	return m.aud.voice, m.aud.state, m.aud.err
}

// stopAudition ends the sample being played or made, if any.
func (m *model) stopAudition() {
	m.aud.mu.Lock()
	if m.aud.cancel != nil {
		m.aud.cancel()
		m.aud.cancel = nil
	}
	m.aud.gen++
	m.aud.voice, m.aud.state = "", ""
	m.aud.mu.Unlock()
	m.invalidate()
}

// audition speaks the sample in voice, stopping the reader and any other
// sample first.
func (m *model) audition(voice string) {
	m.pl.Pause()
	m.stopAudition()
	ctx, cancel := context.WithCancel(context.Background())
	m.aud.mu.Lock()
	m.aud.cancel, m.aud.voice, m.aud.state, m.aud.err = cancel, voice, "合成中", ""
	gen := m.aud.gen
	m.aud.mu.Unlock()
	sample, speed := m.sample, m.speed
	if strings.TrimSpace(sample) == "" {
		sample = defaultSample
	}

	// set changes the state only while this sample is still the current one.
	set := func(f func()) {
		m.aud.mu.Lock()
		if m.aud.gen == gen {
			f()
		}
		m.aud.mu.Unlock()
		m.invalidate()
	}
	go func() {
		path, err := m.pool.Get(ctx, sample, voice, speed)
		if err != nil {
			if ctx.Err() == nil {
				set(func() { m.aud.voice, m.aud.state, m.aud.err = "", "", voice+"："+err.Error() })
			}
			return
		}
		h, err := m.out.Play(path)
		if err != nil {
			set(func() { m.aud.voice, m.aud.state, m.aud.err = "", "", err.Error() })
			return
		}
		defer h.Stop()
		set(func() { m.aud.state = "播放中" })
		select {
		case <-h.Done():
			set(func() { m.aud.voice, m.aud.state = "", "" })
		case <-ctx.Done():
		}
	}()
}

// processedCache remembers, per voice, how many sentences of the open
// material are already synthesized. It is recomputed only when the material,
// the speed or the set of synthesized sentences changed.
type processedCache struct {
	key    string
	counts map[string]int
	total  int
}

// processed returns how many sentences of the open material voice has
// already synthesized (so they play without waiting), and the total.
func (m *model) processed(voice string) (done, total int) {
	if !m.open {
		return 0, 0
	}
	key := fmt.Sprintf("%d/%d/%v", m.cur.ID, m.ver.Load(), m.speed)
	if m.proc.key != key {
		sents, _ := m.sentenceList()
		counts := map[string]int{}
		for _, v := range m.voiceList() {
			for _, s := range sents {
				if m.synth.Cached(s, v.Name, m.speed) {
					counts[v.Name]++
				}
			}
		}
		m.proc = processedCache{key: key, counts: counts, total: len(sents)}
	}
	return m.proc.counts[voice], m.proc.total
}
