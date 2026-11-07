package tts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCacheKey(t *testing.T) {
	a := CacheKey("Hello.", "v1", 1)
	if a != CacheKey("Hello.", "v1", 1) {
		t.Fatal("key not stable")
	}
	for _, other := range []string{CacheKey("Hello!", "v1", 1), CacheKey("Hello.", "v2", 1), CacheKey("Hello.", "v1", 1.2)} {
		if other == a {
			t.Fatal("different inputs share a key")
		}
	}
}

func TestKokoroVoices(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range KokoroVoices {
		if seen[v.ID] || len(v.ID) < 4 || v.ID[2] != '_' || (v.ID[0] != 'a' && v.ID[0] != 'b') {
			t.Errorf("bad voice id %q", v.ID)
		}
		seen[v.ID] = true
	}
	r := &Router{Kokoro: &Kokoro{Cache: t.TempDir()}, Piper: &Piper{Cache: t.TempDir()}}
	if r.Cached("Hi.", KokoroPrefix+"af_heart", 1) {
		t.Error("nothing is cached yet")
	}
}

func TestKokoroWorkerEnvLocalModel(t *testing.T) {
	k := &Kokoro{Dir: t.TempDir()}
	has := func(prefix string) bool {
		return slices.ContainsFunc(k.workerEnv(), func(e string) bool { return strings.HasPrefix(e, prefix) })
	}
	if k.LocalModel() || has("REPI_KOKORO_MODEL=") || has("HF_HUB_OFFLINE=") {
		t.Fatal("empty model dir treated as local model")
	}
	files := KokoroModelFiles()
	if len(files) != len(KokoroVoices)+2 || files[0] != "config.json" || files[2] != "voices/af_heart.pt" {
		t.Fatalf("files %v", files[:3])
	}
	for _, f := range files {
		p := filepath.Join(k.ModelDir(), filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	if !k.LocalModel() || !has("REPI_KOKORO_MODEL="+k.ModelDir()) || !has("HF_HUB_OFFLINE=1") {
		t.Fatal("complete model dir not used")
	}
}
