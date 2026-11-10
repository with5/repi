package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"repi/internal/tts"
)

// TestRealInstall installs Kokoro for real and synthesizes one sentence.
// REPI_E2E_SETUP=1 runs it; REPI_E2E_MIRROR=1 uses the mirrors, and
// REPI_E2E_DIR keeps the install in a directory of your choice.
func TestRealInstall(t *testing.T) {
	if os.Getenv("REPI_E2E_SETUP") != "1" {
		t.Skip("set REPI_E2E_SETUP=1 to install for real")
	}
	dir := os.Getenv("REPI_E2E_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	last := ""
	err := Install(ctx, Options{Dir: dir, Mirror: os.Getenv("REPI_E2E_MIRROR") == "1"}, func(p Progress) {
		if p.Title != last { // one log line per step, not per byte
			last = p.Title
			t.Logf("%d/%d %s", p.Step, p.Steps, p.Title)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	k := &tts.Kokoro{Dir: dir, Cache: filepath.Join(dir, "cache")}
	defer k.Close()
	if !k.LocalModel() {
		t.Fatal("model files missing")
	}
	wav, err := k.Synth(ctx, "Hello from Repi.", tts.KokoroPrefix+"af_heart", 1)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(wav); err != nil || fi.Size() < 10000 {
		t.Fatalf("wav %v %v", fi, err)
	}
}

// TestRealCloneInstall installs the voice-cloning engine for real next to an
// installed Kokoro and speaks one sentence in a cloned voice.
// REPI_E2E_CLONE=<kokoro dir> runs it; REPI_E2E_CLONE_DIR keeps the install,
// REPI_E2E_REF is the reference recording (any audio file).
func TestRealCloneInstall(t *testing.T) {
	kokoro, ref := os.Getenv("REPI_E2E_CLONE"), os.Getenv("REPI_E2E_REF")
	if kokoro == "" || ref == "" {
		t.Skip("set REPI_E2E_CLONE=<kokoro dir> and REPI_E2E_REF=<audio file> to install for real")
	}
	dir := os.Getenv("REPI_E2E_CLONE_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	last := ""
	err := Install(ctx, Options{Dir: kokoro, Clone: dir, Mirror: os.Getenv("REPI_E2E_MIRROR") == "1"}, func(p Progress) {
		if p.Title != last {
			last = p.Title
			t.Logf("%d/%d %s", p.Step, p.Steps, p.Title)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &tts.Clone{Dir: dir, Voices: filepath.Join(t.TempDir(), "voices"), Cache: t.TempDir()}
	defer c.Close()
	if err := c.Check(); err != nil {
		t.Fatal(err)
	}
	v, err := c.Add(ctx, "ref", ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, speed := range []float64{1, 0.85} {
		t0 := time.Now()
		wav, err := c.Synth(ctx, "Hello from Repi, in a voice you lent me.", tts.ClonePrefix+v.ID, speed)
		if err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(wav); err != nil || fi.Size() < 20000 {
			t.Fatalf("wav %v %v", fi, err)
		}
		t.Logf("speed %v: %v → %s", speed, time.Since(t0), wav)
	}
}
