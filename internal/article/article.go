// Package article fetches a web page and extracts its title and body as plain
// text ready to be read aloud.
package article

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Errors the caller shows as they are.
var (
	ErrInvalidURL = errors.New("链接无效，请输入 http 或 https 地址")
	ErrNotHTML    = errors.New("这个链接不是网页")
	ErrTooLarge   = errors.New("网页太大，无法导入")
	ErrNoContent  = errors.New("没有提取到正文。这个页面可能需要 JS 或登录，可以复制正文后用“新增”里的“粘贴”")
)

const (
	timeout   = 30 * time.Second
	maxBytes  = 5 << 20
	minChars  = 200 // shorter bodies are JS shells and navigation pages
	userAgent = "Mozilla/5.0 (compatible; Repi)"
)

// Article is what Fetch extracts from a page.
type Article struct {
	Title string
	Text  string
}

// Fetch downloads rawURL and extracts its article.
func Fetch(ctx context.Context, rawURL string) (Article, error) {
	u, err := normalize(rawURL)
	if err != nil {
		return Article{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Article{}, ErrInvalidURL
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Article{}, fmt.Errorf("无法访问该网页：%w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusTooManyRequests:
		return Article{}, fmt.Errorf("网站拒绝了这次抓取（HTTP %d），可以复制正文后用“新增”里的“粘贴”", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Article{}, fmt.Errorf("网站返回错误（HTTP %d）", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !isHTML(contentType) {
		return Article{}, ErrNotHTML
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return Article{}, fmt.Errorf("无法访问该网页：%w", err)
	}
	if len(raw) > maxBytes {
		return Article{}, ErrTooLarge
	}
	utf8Body, err := charset.NewReader(bytes.NewReader(raw), contentType)
	if err != nil {
		return Article{}, fmt.Errorf("无法识别网页编码：%w", err)
	}
	parsed, err := readability.FromReader(utf8Body, resp.Request.URL)
	if err != nil || parsed.Node == nil {
		return Article{}, ErrNoContent
	}
	title := collapse(parsed.Title())
	lines := blocks(parsed.Node)
	if len(lines) > 0 && strings.EqualFold(lines[0], title) {
		lines = lines[1:]
	}
	body := strings.Join(lines, "\n\n")
	if utf8.RuneCountInString(body) < minChars {
		return Article{}, ErrNoContent
	}
	return Article{Title: title, Text: body}, nil
}

// IsURL reports whether s is a single http or https link.
func IsURL(s string) bool {
	if len(strings.Fields(s)) != 1 || !strings.Contains(s, "://") {
		return false
	}
	_, err := normalize(s)
	return err == nil
}

// normalize parses what the user typed, assuming https without a scheme.
func normalize(raw string) (*url.URL, error) {
	raw = strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(raw))
	if raw == "" {
		return nil, ErrInvalidURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, ErrInvalidURL
	}
	return u, nil
}

// isHTML accepts HTML media types and a missing Content-Type.
func isHTML(contentType string) bool {
	media, _, _ := strings.Cut(contentType, ";")
	switch strings.ToLower(strings.TrimSpace(media)) {
	case "", "text/html", "application/xhtml+xml":
		return true
	}
	return false
}

// skipped elements contribute nothing to the spoken text.
var skipped = map[string]bool{
	"pre": true, "table": true, "figure": true, "figcaption": true, "img": true, "picture": true,
	"svg": true, "video": true, "audio": true, "iframe": true, "script": true, "style": true, "noscript": true,
}

// blockTags start a new line of text.
var blockTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dd": true, "dt": true, "blockquote": true,
	"div": true, "section": true, "article": true, "main": true, "header": true, "footer": true,
	"aside": true, "address": true, "details": true, "summary": true, "hr": true,
}

// blocks returns the text of n as one line per innermost block, a double <br>
// also ending a block. text.Split breaks sentences at line breaks, so
// whitespace inside a block, source line breaks included, collapses to single
// spaces.
func blocks(n *html.Node) []string {
	var lines []string
	var cur strings.Builder
	flush := func() {
		if s := collapse(cur.String()); s != "" {
			lines = append(lines, s)
		}
		cur.Reset()
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			cur.WriteString(n.Data)
			return
		case html.ElementNode:
			if skipped[n.Data] {
				return
			}
			if n.Data == "br" {
				if followsBreak(n) {
					flush()
				} else {
					cur.WriteByte(' ')
				}
				return
			}
		}
		block := n.Type == html.ElementNode && blockTags[n.Data]
		if block {
			flush()
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
		if block {
			flush()
		}
	}
	walk(n)
	flush()
	return lines
}

// followsBreak reports whether only whitespace separates n from an earlier <br>.
func followsBreak(n *html.Node) bool {
	for p := n.PrevSibling; p != nil; p = p.PrevSibling {
		if p.Type == html.TextNode && strings.TrimSpace(p.Data) == "" {
			continue
		}
		return p.Type == html.ElementNode && p.Data == "br"
	}
	return false
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
