package tts

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeClone is an installed engine whose "python" is a shell script: the
// prep tool copies the file, failing on names containing "short".
func fakeClone(t *testing.T) *Clone {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script interpreter")
	}
	dir := t.TempDir()
	c := &Clone{Dir: filepath.Join(dir, "clone"), Voices: filepath.Join(dir, "voices"), Cache: filepath.Join(dir, "cache")}
	py := c.Python()
	os.MkdirAll(filepath.Dir(py), 0o755)
	script := `#!/bin/sh
# -u tool.py prep SRC DST
case "$4" in
*short*) echo '{"ok": false, "error": "too short"}' ;;
*) cp "$4" "$5" && echo 'some banner' && echo '{"ok": true, "seconds": 9.5}' ;;
esac
`
	if err := os.WriteFile(py, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(c.ModelDir(), 0o755)
	for _, f := range CloneModelFiles {
		os.WriteFile(filepath.Join(c.ModelDir(), f), []byte("x"), 0o644)
	}
	return c
}

func TestCloneCheck(t *testing.T) {
	c := &Clone{Dir: t.TempDir()}
	if c.Check() == nil {
		t.Fatal("empty dir passes")
	}
	c = fakeClone(t)
	if err := c.Check(); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(c.ModelDir(), "conds.pt"))
	if err := c.Check(); err == nil || !strings.Contains(err.Error(), "conds.pt") {
		t.Fatalf("missing model file: %v", err)
	}
}

func TestCloneVoices(t *testing.T) {
	c := fakeClone(t)
	src := filepath.Join(t.TempDir(), "me.wav")
	os.WriteFile(src, []byte("RIFF"), 0o644)
	ctx := context.Background()

	a, err := c.Add(ctx, " 我的声音 ", src)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "我的声音" || a.Seconds != 9.5 || len(a.ID) != 8 {
		t.Fatalf("voice %+v", a)
	}
	if b, _ := os.ReadFile(filepath.Join(c.Voices, a.ID+".wav")); string(b) != "RIFF" {
		t.Fatal("reference not stored")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source was moved")
	}
	b, err := c.Add(ctx, "我的声音", src) // same name: numbered
	if err != nil || b.Name != "我的声音 2" || b.ID == a.ID {
		t.Fatalf("second voice %+v %v", b, err)
	}
	if e, err := c.Add(ctx, "", src); err != nil || e.Name != "我的声音 3" {
		t.Fatalf("empty name %+v %v", e, err)
	}
	if _, err := c.Add(ctx, "x", filepath.Join(t.TempDir(), "short.wav")); err == nil || err.Error() != "too short" {
		t.Fatalf("tool error not passed on: %v", err)
	}
	if n := len(c.List()); n != 3 {
		t.Fatalf("%d voices after a failed add", n)
	}

	// the index survives a restart, and voices without a recording are dropped
	os.Remove(filepath.Join(c.Voices, b.ID+".wav"))
	c2 := &Clone{Dir: c.Dir, Voices: c.Voices}
	if l := c2.List(); len(l) != 2 || l[0].ID != a.ID {
		t.Fatalf("reloaded %+v", l)
	}
	if err := c2.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if l := (&Clone{Voices: c.Voices}).List(); len(l) != 1 || l[0].Name != "我的声音 3" {
		t.Fatalf("after delete %+v", l)
	}
	if _, err := os.Stat(filepath.Join(c.Voices, a.ID+".wav")); !os.IsNotExist(err) {
		t.Fatal("recording not deleted")
	}
}

func TestCloneRouting(t *testing.T) {
	c := fakeClone(t)
	r := &Router{Kokoro: &Kokoro{Cache: t.TempDir()}, Piper: &Piper{Cache: t.TempDir()}, Clone: c}
	voice := ClonePrefix + "deadbeef"
	os.MkdirAll(c.Cache, 0o755)
	os.WriteFile(c.CachePath("Hi.", voice, 1), []byte("wav"), 0o644)
	if !r.Cached("Hi.", voice, 1) || r.Cached("Hi.", KokoroPrefix+"af_heart", 1) {
		t.Fatal("clone voice not routed to the clone cache")
	}
	if p, err := r.Synth(context.Background(), "Hi.", voice, 1); err != nil || p != c.CachePath("Hi.", voice, 1) {
		t.Fatalf("cached clone sentence: %q %v", p, err)
	}
	if _, err := r.Synth(context.Background(), "Bye.", voice, 1); err == nil || !strings.Contains(err.Error(), "录音不见了") {
		t.Fatalf("voice without a recording: %v", err)
	}
}
