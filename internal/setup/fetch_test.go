package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	body := []byte("hello wheel")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != userAgent {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	dst := filepath.Join(dir, "a", "f.whl")
	var last int64
	if err := download(context.Background(), srv.Client(), srv.URL+"/f", dst, hex.EncodeToString(sum[:]), func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(body) || last != int64(len(body)) {
		t.Fatalf("got %q, progress %d", b, last)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part left behind")
	}

	bad := filepath.Join(dir, "bad")
	if err := download(context.Background(), srv.Client(), srv.URL+"/f", bad, "00", nil); err == nil {
		t.Fatal("sha256 mismatch accepted")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("file kept after mismatch")
	}

	var ne *netError
	if err := download(context.Background(), srv.Client(), srv.URL+"/missing", bad, "", nil); !errors.As(err, &ne) {
		t.Fatalf("404 gives %v, want netError", err)
	}
	if _, err := getPage(context.Background(), srv.Client(), srv.URL+"/missing"); !errors.As(err, &ne) {
		t.Fatalf("getPage 404 gives %v", err)
	}
}
