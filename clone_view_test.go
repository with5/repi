package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"repi/internal/player"
	"repi/internal/store"
	"repi/internal/tts"
)

// cloneModel is a model with Kokoro and the voice-cloning engine "installed"
// (fake interpreters), and one cloned voice.
func cloneModel(t *testing.T) (*model, tts.CloneVoice) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script interpreter")
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	k := &tts.Kokoro{Dir: filepath.Join(dir, "kokoro"), Cache: t.TempDir()}
	c := &tts.Clone{Dir: filepath.Join(dir, "clone"), Voices: filepath.Join(dir, "voices"), Cache: k.Cache}
	for _, py := range []string{k.Python(), c.Python()} {
		os.MkdirAll(filepath.Dir(py), 0o755)
		os.WriteFile(py, []byte("#!/bin/sh\ncp \"$4\" \"$5\"; echo '{\"ok\": true, \"seconds\": 9}'\n"), 0o755)
	}
	os.MkdirAll(c.ModelDir(), 0o755)
	for _, f := range tts.CloneModelFiles {
		os.WriteFile(filepath.Join(c.ModelDir(), f), nil, 0o644)
	}
	src := filepath.Join(dir, "me.wav")
	os.WriteFile(src, []byte("RIFF"), 0o644)
	v, err := c.Add(t.Context(), "Me", src)
	if err != nil {
		t.Fatal(err)
	}
	m := &model{st: st, kokoro: k, clone: c, piper: &tts.Piper{}, voice: defaultVoice, speed: 1, out: silentOut{}}
	m.synth = &tts.Router{Kokoro: k, Clone: c, Piper: m.piper}
	m.pool = &tts.Pool{S: m.synth}
	m.pl = player.New(m, m.out, m.invalidate)
	t.Cleanup(m.pl.Close)
	return m, v
}

func TestCloneVoicesListed(t *testing.T) {
	m, v := cloneModel(t)
	name := tts.ClonePrefix + v.ID
	if l := m.voiceList(); len(l) == 0 || l[0].Name != name || l[0].Label != "Me" {
		t.Fatalf("cloned voice not listed first: %+v", l[:1])
	}
	if !m.hasVoice(name) {
		t.Fatal("cloned voice not usable")
	}
	os.Remove(filepath.Join(m.clone.ModelDir(), "conds.pt")) // engine broken: its voices are gone
	if m.hasVoice(name) {
		t.Fatal("cloned voice offered without the engine")
	}
}

// A new voice is starred, so the reader offers it.
func TestApplyCloneStarsVoice(t *testing.T) {
	m, v := cloneModel(t)
	m.cloneUI.added = &v
	m.cloneName = "Me"
	m.applyClone()
	defer m.stopAudition()
	name := tts.ClonePrefix + v.ID
	if !m.isFavorite(name) || !slices.Contains(m.st.Settings().Favorites, name) {
		t.Fatalf("not starred: %v", m.favorites)
	}
	if m.cloneName != "" || m.cloneUI.added != nil || m.cloneUI.note == "" {
		t.Fatalf("state not reset: %q %v %q", m.cloneName, m.cloneUI.added, m.cloneUI.note)
	}
	if a, _, _ := m.auditionState(); a != name {
		t.Fatalf("auditioning %q", a)
	}
}

// Deleting the voice in use, from the panel: a second click is needed, and
// the reader falls back to the default voice.
func TestDeleteVoiceInUse(t *testing.T) {
	m, v := cloneModel(t)
	name := tts.ClonePrefix + v.ID
	m.voice, m.voiceSel, m.favorites = name, name, []string{name, defaultVoice}
	m.showVoices = true
	tt := ui.NewTester(m.voicePanel, 1000, 4000)
	if err := tt.Click("删除 Me"); err != nil {
		t.Fatal(err)
	}
	if !m.hasVoice(name) {
		t.Fatal("deleted on the first click")
	}
	if err := tt.Click("确认删除 Me"); err != nil {
		t.Fatal(err)
	}
	if m.hasVoice(name) || len(m.clone.List()) != 0 {
		t.Fatal("not deleted")
	}
	if m.voice != defaultVoice || m.voiceSel != defaultVoice || slices.Contains(m.favorites, name) {
		t.Fatalf("voice %q sel %q favorites %v", m.voice, m.voiceSel, m.favorites)
	}
	if s := m.st.Settings(); s.Voice != defaultVoice {
		t.Fatalf("settings %+v", s)
	}
}

// With Kokoro but not the engine, the panel offers to install it; once
// installed, it offers recording instead.
func TestCloneCards(t *testing.T) {
	m, _ := cloneModel(t)
	m.kokoroSetup.gpuAsked = true // no nvidia-smi in tests
	tt := ui.NewTester(m.voicePanel, 1000, 4000)
	if _, ok := tt.Find("开始录音"); !ok || !tt.HasText("用我的声音朗读") {
		t.Fatalf("recorder not shown: %v", tt.Texts())
	}

	os.RemoveAll(m.clone.ModelDir())
	tt = ui.NewTester(m.voicePanel, 1000, 4000)
	if _, ok := tt.Find("开始录音"); ok {
		t.Fatal("recorder shown without the engine")
	}
	if !tt.HasText("安装声音克隆（Chatterbox）") {
		t.Fatalf("install card not shown: %v", tt.Texts())
	}
}

// The reader's selector shows a cloned voice by its name, not its id.
func TestVoiceLabels(t *testing.T) {
	m, v := cloneModel(t)
	name := tts.ClonePrefix + v.ID
	got := m.voiceLabels([]string{defaultVoice, name})
	if got[0] != defaultVoice || got[1] != "Me（我的声音）" {
		t.Fatalf("labels %v", got)
	}
}
