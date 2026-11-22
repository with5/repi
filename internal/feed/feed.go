// Package feed lists the latest posts of the AI labs' blogs, so the phone can
// browse them like news and pick one to listen to. Most sources are RSS or
// Atom feeds; Anthropic has none, so its news page is read instead.
package feed

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Source is a blog.
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"` // the feed, or the page for KindPage
	Kind string `json:"-"`   // KindFeed or KindPage
}

const (
	KindFeed = "feed" // RSS or Atom
	KindPage = "page" // an HTML page of links to /news/…, as anthropic.com/news
)

// Sources are the blogs listed by default.
var Sources = []Source{
	{"openai", "OpenAI", "https://openai.com/news/rss.xml", KindFeed},
	{"anthropic", "Anthropic", "https://www.anthropic.com/news", KindPage},
	{"google-ai", "Google AI", "https://blog.google/technology/ai/rss/", KindFeed},
	{"deepmind", "Google DeepMind", "https://deepmind.google/blog/rss.xml", KindFeed},
	{"google-research", "Google Research", "https://research.google/blog/rss/", KindFeed},
	{"huggingface", "Hugging Face", "https://huggingface.co/blog/feed.xml", KindFeed},
}

// Item is a post.
type Item struct {
	Source    string    `json:"source"` // Source.ID
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Summary   string    `json:"summary,omitempty"`
	Published time.Time `json:"published,omitzero"`
}

const (
	maxBytes = 8 << 20
	perFeed  = 30 // newest posts kept of each source
	maxSum   = 300
	ua       = "Mozilla/5.0 (compatible; Repi)"
)

// Fetch reads one source.
func Fetch(ctx context.Context, src Source) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	body := io.LimitReader(res.Body, maxBytes)
	var items []Item
	if src.Kind == KindPage {
		items, err = parsePage(body, res.Request.URL)
	} else {
		items, err = parseFeed(body)
	}
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Source = src.ID
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Published.After(items[j].Published) })
	if len(items) > perFeed {
		items = items[:perFeed]
	}
	return items, nil
}

// feedDoc holds both RSS 2.0 (<rss><channel><item>) and Atom (<feed><entry>).
type feedDoc struct {
	Items   []feedItem `xml:"channel>item"`
	Entries []feedItem `xml:"entry"`
}

type feedItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Summary     string `xml:"summary"`
	PubDate     string `xml:"pubDate"`
	Published   string `xml:"published"`
	Updated     string `xml:"updated"`
	Date        string `xml:"http://purl.org/dc/elements/1.1/ date"`
	Link        []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
		Text string `xml:",chardata"`
	} `xml:"link"`
}

func parseFeed(r io.Reader) ([]Item, error) {
	d := xml.NewDecoder(r)
	d.CharsetReader = charset.NewReaderLabel
	d.Strict = false
	var doc feedDoc
	if err := d.Decode(&doc); err != nil {
		return nil, fmt.Errorf("读不懂这个订阅源：%w", err)
	}
	var out []Item
	for _, f := range append(doc.Items, doc.Entries...) {
		it := Item{Title: clean(f.Title), Summary: summary(first(f.Description, f.Summary))}
		for _, l := range f.Link {
			switch {
			case strings.TrimSpace(l.Text) != "":
				it.URL = strings.TrimSpace(l.Text)
			case l.Href != "" && (l.Rel == "" || l.Rel == "alternate") && it.URL == "":
				it.URL = l.Href
			}
		}
		it.Published = parseTime(first(f.PubDate, f.Published, f.Date, f.Updated))
		if it.Title != "" && strings.HasPrefix(it.URL, "http") {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("订阅源里没有文章")
	}
	return out, nil
}

var timeLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02T15:04:05Z0700", "2006-01-02",
	"Jan 2, 2006", "January 2, 2006",
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// dateText finds a date written out on a page, as "Nov 9, 2026".
var dateText = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]* \d{1,2}, \d{4}$`)

// parsePage reads a news index page: each post is a link to /news/<slug>
// holding a heading (the title) and maybe a paragraph (the summary); the
// date is the last one written before the link.
func parsePage(r io.Reader, base *url.URL) ([]Item, error) {
	doc, err := xhtml.Parse(r)
	if err != nil {
		return nil, err
	}
	var out []Item
	seen := map[string]bool{}
	var date time.Time
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			if t := strings.TrimSpace(n.Data); dateText.MatchString(t) {
				// "Nov 9, 2026", "November 9, 2026" or "Sept 9, 2026"
				date = parseTime(t[:3] + t[strings.IndexByte(t, ' '):])
			}
		}
		if n.Type == xhtml.ElementNode && n.Data == "a" {
			if it, ok := pageItem(n, base); ok {
				if !seen[it.URL] {
					seen[it.URL] = true
					it.Published = date
					out = append(out, it)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if len(out) == 0 {
		return nil, errors.New("页面上没有找到文章，网站可能改版了")
	}
	return out, nil
}

func pageItem(a *xhtml.Node, base *url.URL) (Item, bool) {
	href := attr(a, "href")
	if !strings.HasPrefix(href, "/news/") || strings.Count(href, "/") != 2 {
		return Item{}, false
	}
	var title, sum string
	var find func(n *xhtml.Node)
	find = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4":
				if title == "" {
					title = clean(text(n))
				}
				return
			case "p":
				if sum == "" {
					sum = clean(text(n))
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(a)
	if title == "" { // a category link or an image: not a post
		return Item{}, false
	}
	u, err := base.Parse(href)
	if err != nil {
		return Item{}, false
	}
	return Item{Title: title, URL: u.String(), Summary: summary(sum)}, true
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *xhtml.Node) string {
	var b strings.Builder
	var f func(*xhtml.Node)
	f = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return b.String()
}

var tags = regexp.MustCompile(`<[^>]*>`)

// clean turns feed text, which may hold escaped HTML, into one plain line.
func clean(s string) string {
	s = html.UnescapeString(tags.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

func summary(s string) string {
	s = clean(s)
	if r := []rune(s); len(r) > maxSum {
		s = strings.TrimSpace(string(r[:maxSum])) + "…"
	}
	return s
}

func first(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Reader keeps the sources' posts for a while, so browsing does not fetch
// every blog on each request.
type Reader struct {
	Sources []Source
	TTL     time.Duration // 30 minutes when 0
	Fetch   func(ctx context.Context, s Source) ([]Item, error) // Fetch when nil

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	items []Item
	err   error
	at    time.Time
}

// Result is the posts of the sources asked for, newest first, and why the
// sources that failed did.
type Result struct {
	Items  []Item            `json:"items"`
	Errors map[string]string `json:"errors,omitempty"`
}

// Latest returns the posts of the sources whose IDs are given, or of all of
// them; refresh fetches them again even if they were fetched lately.
func (r *Reader) Latest(ctx context.Context, ids []string, refresh bool) Result {
	var want []Source
	for _, s := range r.Sources {
		if len(ids) == 0 || contains(ids, s.ID) {
			want = append(want, s)
		}
	}
	ttl := r.TTL
	if ttl == 0 {
		ttl = 30 * time.Minute
	}
	fetch := r.Fetch
	if fetch == nil {
		fetch = Fetch
	}
	results := make([]cached, len(want))
	var wg sync.WaitGroup
	for i, s := range want {
		r.mu.Lock()
		c, ok := r.cache[s.ID]
		r.mu.Unlock()
		// a failure is tried again sooner than a success
		if ok && !refresh && (time.Since(c.at) < ttl && c.err == nil || time.Since(c.at) < time.Minute) {
			results[i] = c
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := fetch(ctx, s)
			c := cached{items, err, time.Now()}
			if err != nil {
				r.mu.Lock()
				if old, ok := r.cache[s.ID]; ok && old.err == nil {
					c.items = old.items // keep showing what was there
				}
				r.mu.Unlock()
			}
			r.mu.Lock()
			if r.cache == nil {
				r.cache = map[string]cached{}
			}
			r.cache[s.ID] = c
			r.mu.Unlock()
			results[i] = c
		}()
	}
	wg.Wait()
	res := Result{Items: []Item{}}
	for i, c := range results {
		res.Items = append(res.Items, c.items...)
		if c.err != nil {
			if res.Errors == nil {
				res.Errors = map[string]string{}
			}
			res.Errors[want[i].ID] = c.err.Error()
		}
	}
	sort.SliceStable(res.Items, func(i, j int) bool { return res.Items[i].Published.After(res.Items[j].Published) })
	return res
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
