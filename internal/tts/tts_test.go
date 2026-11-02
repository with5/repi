package tts

import "testing"

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
