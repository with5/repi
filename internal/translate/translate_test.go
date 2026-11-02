package translate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTranslateCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("q") != "Why do we need it?" || r.URL.Query().Get("langpair") != "en|zh-CN" {
			t.Errorf("query = %v", r.URL.Query())
		}
		w.Write([]byte(`{"responseData":{"translatedText":"我们为什么需要它？ &#39;好&#39;"},"responseStatus":200}`))
	}))
	defer srv.Close()
	c := &Client{Dir: t.TempDir(), URL: srv.URL}
	if _, ok := c.Cached("Why do we need it?"); ok {
		t.Fatal("cached before asking")
	}
	for i := 0; i < 2; i++ {
		got, err := c.Translate(context.Background(), "  Why do we need it? ")
		if err != nil || got != "我们为什么需要它？ '好'" {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("web calls = %d, want 1", calls.Load())
	}
	if _, ok := c.Cached("Why do we need it?"); !ok {
		t.Error("not cached")
	}
}

func TestTranslateErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"responseData":{"translatedText":"MYMEMORY WARNING: QUOTA"},"responseStatus":429,"responseDetails":"quota used up"}`))
	}))
	defer srv.Close()
	c := &Client{Dir: t.TempDir(), URL: srv.URL}
	if _, err := c.Translate(context.Background(), "Hello."); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Errorf("quota error = %v", err)
	}
	if _, ok := c.Cached("Hello."); ok {
		t.Error("an error was cached")
	}
	if _, err := c.Translate(context.Background(), strings.Repeat("a ", 400)); err == nil {
		t.Error("long text accepted")
	}
	if s, err := c.Translate(context.Background(), "  "); s != "" || err != nil {
		t.Errorf("blank = %q, %v", s, err)
	}
}
