// Package translate turns an English sentence into Chinese with the free
// MyMemory web API, and keeps every answer on disk so a sentence is asked
// for only once.
package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultURL is the MyMemory endpoint.
const DefaultURL = "https://api.mymemory.translated.net/get"

// maxBytes is the longest text MyMemory takes in one request.
const maxBytes = 500

// Client translates sentences and caches them in Dir.
type Client struct {
	Dir  string       // cache directory
	URL  string       // endpoint, DefaultURL when empty
	HTTP *http.Client // a 20 second client when nil
}

func (c *Client) path(text string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return filepath.Join(c.Dir, hex.EncodeToString(h[:16])+".txt")
}

// Cached returns the saved translation of text, if there is one.
func (c *Client) Cached(text string) (string, bool) {
	b, err := os.ReadFile(c.path(text))
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}

// Translate returns the Chinese of text, from the cache or the web.
func (c *Client) Translate(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if s, ok := c.Cached(text); ok {
		return s, nil
	}
	if len(text) > maxBytes {
		return "", fmt.Errorf("句子太长（%d 字节），无法翻译", len(text))
	}
	endpoint := c.URL
	if endpoint == "" {
		endpoint = DefaultURL
	}
	q := url.Values{"q": {text}, "langpair": {"en|zh-CN"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("翻译请求失败：%w", err)
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			Text string `json:"translatedText"`
		} `json:"responseData"`
		Status  json.Number `json:"responseStatus"`
		Details string      `json:"responseDetails"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("翻译返回了无法识别的内容（HTTP %d）", resp.StatusCode)
	}
	if status := body.Status.String(); resp.StatusCode != http.StatusOK || (status != "200" && status != "") {
		msg := strings.TrimSpace(body.Details)
		if msg == "" {
			msg = body.Data.Text
		}
		return "", errors.New("翻译失败：" + msg)
	}
	out := strings.TrimSpace(html.UnescapeString(body.Data.Text))
	if out == "" {
		return "", errors.New("翻译结果为空")
	}
	if err := os.MkdirAll(c.Dir, 0o755); err == nil {
		os.WriteFile(c.path(text), []byte(out), 0o644)
	}
	return out, nil
}
