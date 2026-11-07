package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestDownloadModel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/repo/voices/broken.pt" {
			http.Error(w, "no", http.StatusBadGateway)
			return
		}
		w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("kept"), 0o644) // already there
	files := []string{"config.json", "kokoro-v1_0.pth", "voices/af_heart.pt"}
	var steps []int
	err := downloadModel(context.Background(), srv.Client(), srv.URL+"/repo/", dir, files, func(i, n int, name string, done, total int64) {
		if n != 3 {
			t.Errorf("n = %d", n)
		}
		steps = append(steps, i)
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d requests, want 2 (config.json skipped)", hits.Load())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "voices", "af_heart.pt")); string(b) != "/repo/voices/af_heart.pt" {
		t.Fatalf("voice %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(b) != "kept" {
		t.Fatal("existing file overwritten")
	}
	if len(steps) == 0 || steps[len(steps)-1] != 3 {
		t.Fatalf("progress %v", steps)
	}

	if err := downloadModel(context.Background(), srv.Client(), srv.URL+"/repo/", dir, []string{"voices/broken.pt"}, nil); err == nil {
		t.Fatal("HTTP error ignored")
	}
	os.WriteFile(filepath.Join(dir, "voices", "x.pt.part"), nil, 0o644)
	removeParts(dir)
	if _, err := os.Stat(filepath.Join(dir, "voices", "x.pt.part")); !os.IsNotExist(err) {
		t.Fatal(".part not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "voices", "af_heart.pt")); err != nil {
		t.Fatal("complete file removed")
	}
}
