package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"repi/internal/store"
	"repi/internal/tts"
)

func TestApplySetupSwitchesToKokoro(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	k := &tts.Kokoro{Dir: filepath.Join(dir, "kokoro")}
	m := &model{st: st, kokoro: k, piper: &tts.Piper{}, setupErr: noTTSErr, voice: "en_US-gone"}
	m.kokoroSetup.mirror = true

	m.applySetup() // nothing finished yet
	if m.setupErr != noTTSErr {
		t.Fatal("applied before the install finished")
	}

	py := k.Python()
	os.MkdirAll(filepath.Dir(py), 0o755)
	os.WriteFile(py, nil, 0o755)
	m.kokoroSetup.done = true
	m.applySetup()
	if m.setupErr != "" || m.voice != defaultVoice || m.voiceSel != defaultVoice {
		t.Fatalf("err %q voice %q sel %q", m.setupErr, m.voice, m.voiceSel)
	}
	if !slices.Contains(m.favorites, defaultVoice) {
		t.Fatalf("favorites %v", m.favorites)
	}
	if s := st.Settings(); s.Voice != defaultVoice || !s.Mirror {
		t.Fatalf("settings %+v", s)
	}
	if m.kokoroSetup.done {
		t.Fatal("done not reset")
	}
}
