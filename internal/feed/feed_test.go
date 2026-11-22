package feed

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRSS(t *testing.T) {
	items, err := parseFeed(strings.NewReader(`<?xml version="1.0"?>
<rss version="2.0"><channel><title>x</title>
<item><title><![CDATA[Old post]]></title><link>https://a.example/old</link>
  <pubDate>Fri, 20 Nov 2026 10:00:00 GMT</pubDate>
  <description>&lt;img src="x.png"&gt;Give your &lt;b&gt;agents&lt;/b&gt; tools.</description></item>
<item><title>No link</title></item>
</channel></rss>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("%+v", items)
	}
	it := items[0]
	if it.Title != "Old post" || it.URL != "https://a.example/old" || it.Summary != "Give your agents tools." ||
		!it.Published.Equal(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v", it)
	}
}

func TestAtom(t *testing.T) {
	items, err := parseFeed(strings.NewReader(`<feed xmlns="http://www.w3.org/2005/Atom">
<entry><title>Post</title><link rel="alternate" href="https://b.example/p"/><link rel="self" href="https://b.example/self"/>
<updated>2026-11-01T08:00:00Z</updated><summary>Hello</summary></entry></feed>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != "https://b.example/p" || items[0].Published.Day() != 1 || items[0].Summary != "Hello" {
		t.Fatalf("%+v", items)
	}
}

func TestPage(t *testing.T) {
	base, _ := url.Parse("https://www.anthropic.com/news")
	items, err := parsePage(strings.NewReader(`<html><body>
<a href="/news/announcements">Announcements</a><span>Nov 9, 2026</span>
<a href="/news/s1-sec"><img src="x"><h3>Files S-1</h3><p>Today we filed.</p></a>
<a href="/news/s1-sec"><h3>Files S-1</h3></a>
<div><span>September 3, 2026</span><a href="/news/fund"><h4>The fund</h4></a></div>
<a href="/news/a/b"><h3>Nested path</h3></a>
</body></html>`), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("%+v", items)
	}
	if items[0].Title != "Files S-1" || items[0].URL != "https://www.anthropic.com/news/s1-sec" ||
		items[0].Summary != "Today we filed." || items[0].Published.Format("2006-01-02") != "2026-11-09" {
		t.Fatalf("%+v", items[0])
	}
	if items[1].Title != "The fund" || items[1].Published.Format("2006-01-02") != "2026-09-03" {
		t.Fatalf("%+v", items[1])
	}
}

func TestReaderCachesAndMerges(t *testing.T) {
	calls := map[string]int{}
	var mu sync.Mutex
	fail := false
	r := &Reader{
		Sources: []Source{{ID: "a"}, {ID: "b"}},
		Fetch: func(_ context.Context, s Source) ([]Item, error) {
			mu.Lock()
			calls[s.ID]++
			mu.Unlock()
			if s.ID == "b" && fail {
				return nil, errors.New("down")
			}
			day := 1
			if s.ID == "b" {
				day = 2
			}
			return []Item{{Source: s.ID, Title: s.ID, Published: time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)}}, nil
		},
	}
	res := r.Latest(context.Background(), nil, false)
	if len(res.Items) != 2 || res.Items[0].Source != "b" {
		t.Fatalf("not newest first: %+v", res)
	}
	r.Latest(context.Background(), []string{"a"}, false)
	if calls["a"] != 1 {
		t.Fatalf("not cached: %v", calls)
	}
	fail = true
	res = r.Latest(context.Background(), []string{"b"}, true)
	if calls["b"] != 2 || res.Errors["b"] != "down" || len(res.Items) != 1 {
		t.Fatalf("a failure should keep the old posts and report: %+v", res)
	}
}
