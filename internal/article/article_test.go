package article

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"repi/internal/text"
)

const para1 = "The river rises in the northern hills and runs for three hundred miles before it reaches the sea. Farmers along its banks have depended on the spring floods for centuries, and their calendars still follow the water."
const para2 = "Engineers proposed a series of small dams in the last decade of the century, and the plan divided the valley. Some families welcomed the promise of steady power, while others feared losing the land their grandparents had cleared."
const para3 = "Today the debate has moved from the town halls to the courts, where judges must weigh old water rights against new needs. Nobody expects a quick ruling, and the river keeps rising every spring regardless of what anyone decides."

// page wraps an article body in the clutter of a real news page.
func page(title, body string) string {
	return `<!doctype html><html><head><title>` + title + ` | The Daily Paper</title>
<meta property="og:title" content="` + title + `"></head><body>
<nav><ul><li><a href="/news">News</a></li><li><a href="/sport">Sport</a></li><li><a href="/about">About us</a></li></ul></nav>
<aside class="sidebar"><h3>Most read</h3><ul><li><a href="/1">Ten things you missed</a></li></ul></aside>
<main><article>` + body + `</article></main>
<div class="newsletter"><form><label>Sign up for our newsletter</label><input name="email"><button>Subscribe</button></form></div>
<footer><p>Copyright The Daily Paper. All rights reserved.</p></footer>
</body></html>`
}

func threeParas() string {
	return "<p>" + para1 + "</p><p>" + para2 + "</p><p>" + para3 + "</p>"
}

func serve(t *testing.T, contentType, body string) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else {
			w.Header()["Content-Type"] = nil
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s.URL
}

func fetchPage(t *testing.T, body string) Article {
	t.Helper()
	a, err := Fetch(context.Background(), serve(t, "text/html; charset=utf-8", body))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFetchExtractsTitleAndBody(t *testing.T) {
	a := fetchPage(t, page("River Dispute Reaches the Courts", threeParas()))
	if a.Title != "River Dispute Reaches the Courts" {
		t.Errorf("title = %q", a.Title)
	}
	want := para1 + "\n\n" + para2 + "\n\n" + para3
	if a.Text != want {
		t.Errorf("text = %q\nwant %q", a.Text, want)
	}
}

func TestFetchDropsPageClutter(t *testing.T) {
	a := fetchPage(t, page("River", threeParas()))
	for _, junk := range []string{"newsletter", "Copyright", "Most read", "About us"} {
		if strings.Contains(a.Text, junk) {
			t.Errorf("%q leaked into %q", junk, a.Text)
		}
	}
}

// text.Split breaks at line breaks, so a hard wrap in the HTML must not survive.
func TestFetchKeepsHardWrappedParagraphOnOneLine(t *testing.T) {
	wrapped := "<p>The river rises in the northern hills\n    and runs for three hundred miles.</p>"
	a := fetchPage(t, page("River", wrapped+threeParas()))
	got := text.Split(a.Text)
	if got[0] != "The river rises in the northern hills and runs for three hundred miles." {
		t.Errorf("first sentence = %q", got[0])
	}
}

func TestFetchDoesNotRepeatNestedBlocks(t *testing.T) {
	a := fetchPage(t, page("River", threeParas()+"<blockquote><p>Water is life here.</p></blockquote><ul><li><p>First point stands.</p></li></ul>"))
	for _, s := range []string{"Water is life here.", "First point stands."} {
		if n := strings.Count(a.Text, s); n != 1 {
			t.Errorf("%q appears %d times in %q", s, n, a.Text)
		}
	}
}

func TestFetchSkipsCodeAndTables(t *testing.T) {
	a := fetchPage(t, page("River", threeParas()+
		"<pre><code>rm -rf build</code></pre><table><tr><th>Year</th><th>Tonnage cell</th></tr><tr><td>1990</td><td>120</td></tr><tr><td>2000</td><td>340</td></tr></table>"+
		"<figure><figcaption>A caption here</figcaption></figure>"))
	for _, junk := range []string{"rm -rf", "Tonnage", "caption"} {
		if strings.Contains(a.Text, junk) {
			t.Errorf("%q leaked into %q", junk, a.Text)
		}
	}
}

func TestFetchKeepsInlineCodeAndLinkText(t *testing.T) {
	a := fetchPage(t, page("River", "<p>Run the <code>make</code> tool and read <a href=\"https://x.test/doc\">the guide</a> first.</p>"+threeParas()))
	if !strings.Contains(a.Text, "Run the make tool and read the guide first.") {
		t.Errorf("text = %q", a.Text)
	}
	if strings.Contains(a.Text, "x.test") {
		t.Errorf("link address leaked: %q", a.Text)
	}
}

func TestFetchDropsFirstLineEqualToTitle(t *testing.T) {
	a := fetchPage(t, page("River Dispute", "<h1>river dispute </h1>"+threeParas()))
	if strings.HasPrefix(strings.ToLower(a.Text), "river dispute") {
		t.Errorf("title repeated: %q", a.Text)
	}
}

// Old pages separate paragraphs with <br><br> instead of <p>.
func TestFetchSplitsParagraphsAtDoubleBreaks(t *testing.T) {
	body := "<table><tr><td><img src=\"head.gif\" alt=\"River\"><br><br><font>July 2023<br>\n<br>" + para1 + "<br><br>" + para2 + "<br>still the same paragraph.<br><br>" + para3 + "</font></td></tr></table>"
	a := fetchPage(t, page("River", body))
	want := "July 2023\n\n" + para1 + "\n\n" + para2 + " still the same paragraph.\n\n" + para3
	if a.Text != want {
		t.Errorf("text = %q\nwant %q", a.Text, want)
	}
}

func TestFetchConvertsLatin1(t *testing.T) {
	body := page("River", "<p>The caf\xe9 by the river stayed open all winter long.</p>"+threeParas())
	a, err := Fetch(context.Background(), serve(t, "text/html; charset=iso-8859-1", body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.Text, "The café by the river") {
		t.Errorf("text = %q", a.Text)
	}
}

func TestFetchAcceptsMissingContentType(t *testing.T) {
	if _, err := Fetch(context.Background(), serve(t, "", page("River", threeParas()))); err != nil {
		t.Error(err)
	}
}

func TestFetchRejectsBadURLs(t *testing.T) {
	for _, u := range []string{"", "   ", "ftp://example.com/a", "file:///etc/passwd", "https://"} {
		if _, err := Fetch(context.Background(), u); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("Fetch(%q) = %v", u, err)
		}
	}
}

func TestNormalizeAddsHTTPS(t *testing.T) {
	u, err := normalize("  example.com/a?b=1 ")
	if err != nil || u.String() != "https://example.com/a?b=1" {
		t.Errorf("normalize = %v, %v", u, err)
	}
}

// A link pasted across several lines is still one link.
func TestNormalizeJoinsLines(t *testing.T) {
	u, err := normalize("https://example.com/a-long\r\n/path\n")
	if err != nil || u.String() != "https://example.com/a-long/path" {
		t.Errorf("normalize = %v, %v", u, err)
	}
}

func TestFetchReportsHTTPStatus(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	defer s.Close()
	_, err := Fetch(context.Background(), s.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err = %v", err)
	}
}

func TestFetchExplainsRefusal(t *testing.T) {
	for _, code := range []int{403, 405, 429} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		_, err := Fetch(context.Background(), s.URL)
		s.Close()
		if err == nil || !strings.Contains(err.Error(), "拒绝") || !strings.Contains(err.Error(), "“新增”里的“粘贴”") {
			t.Errorf("HTTP %d: err = %v", code, err)
		}
	}
}

func TestFetchRejectsNonHTML(t *testing.T) {
	_, err := Fetch(context.Background(), serve(t, "application/pdf", "%PDF-1.7"))
	if !errors.Is(err, ErrNotHTML) {
		t.Errorf("err = %v", err)
	}
}

func TestFetchRejectsTooLarge(t *testing.T) {
	big := page("River", threeParas()) + strings.Repeat(" ", maxBytes)
	_, err := Fetch(context.Background(), serve(t, "text/html", big))
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v", err)
	}
}

func TestFetchRejectsEmptyShell(t *testing.T) {
	shell := `<html><head><title>App</title></head><body><div id="root"></div><noscript>Please enable JavaScript.</noscript></body></html>`
	_, err := Fetch(context.Background(), serve(t, "text/html", shell))
	if !errors.Is(err, ErrNoContent) {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "“新增”里的“粘贴”") {
		t.Errorf("hint names the old button: %v", err)
	}
}

func TestFetchStopsWhenCancelled(t *testing.T) {
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer s.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := Fetch(ctx, s.URL); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestIsURL(t *testing.T) {
	for s, want := range map[string]bool{
		"https://example.com/a":       true,
		"  http://example.com \n":     true,
		"example.com":                 false,
		"https://":                    false,
		"ftp://example.com":           false,
		"see https://example.com now": false,
		"":                            false,
	} {
		if got := IsURL(s); got != want {
			t.Errorf("IsURL(%q) = %v", s, got)
		}
	}
}
