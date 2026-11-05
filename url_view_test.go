package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"repi/internal/article"
)

const urlPage = `<html><head><title>River Dispute</title></head><body><article>
<p>The river rises in the northern hills and runs for three hundred miles before it reaches the sea, and farmers have depended on it for centuries.</p>
<p>Engineers proposed a series of small dams in the last decade of the century, and the plan divided the valley for many years afterwards.</p>
</article></body></html>`

// urlModel returns a model whose import results wait in the channel until the
// test runs them, as the window would on its UI thread.
func urlModel(t *testing.T) (*model, *ui.Tester, chan func()) {
	t.Helper()
	m := newOfflineModel(t)
	posts := make(chan func(), 4)
	m.urlImp.post = func(fn func()) { posts <- fn }
	return m, ui.NewTester(m.view, 1000, 720), posts
}

// chooseAdd picks an item of the sidebar's "新增" menu.
func chooseAdd(t *testing.T, tt *ui.Tester, item string) {
	t.Helper()
	if err := tt.Click("新增资料"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(item); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	tt.Frame()
}

func startImport(t *testing.T, m *model, tt *ui.Tester, link string) {
	t.Helper()
	chooseAdd(t, tt, "URL")
	if !m.urlImp.open {
		t.Fatal("dialog did not open")
	}
	m.urlImp.input = link
	tt.Frame()
	if err := tt.Click("导入"); err != nil {
		t.Fatal(err)
	}
	if !m.urlImp.fetching {
		t.Fatal("fetch did not start")
	}
}

func TestURLImportFillsEditor(t *testing.T) {
	m, tt, posts := urlModel(t)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(urlPage))
	}))
	defer s.Close()

	startImport(t, m, tt, s.URL)
	tt.Frame()
	if !tt.HasText("正在抓取…") {
		t.Errorf("no progress shown: %v", tt.Texts())
	}
	(<-posts)()
	tt.Frame()

	if m.urlImp.open || m.urlImp.fetching {
		t.Errorf("dialog still open: %+v", m.urlImp)
	}
	if !m.editing || m.draftTitle != "River Dispute" || !strings.HasPrefix(m.draftText, "The river rises") {
		t.Fatalf("editing=%v title=%q text=%q", m.editing, m.draftTitle, m.draftText)
	}
	if !m.saveDraft() || m.cur.Title != "River Dispute" {
		t.Errorf("saved %+v", m.cur)
	}
}

func TestURLImportFailureKeepsDialogOpen(t *testing.T) {
	m, tt, posts := urlModel(t)
	s := httptest.NewServer(http.NotFoundHandler())
	defer s.Close()

	startImport(t, m, tt, s.URL)
	(<-posts)()
	tt.Frame()

	if m.editing || !m.urlImp.open || m.urlImp.fetching {
		t.Errorf("editing=%v state=%+v", m.editing, m.urlImp)
	}
	if !tt.HasText("网站返回错误（HTTP 404）") {
		t.Errorf("no error shown: %v", tt.Texts())
	}
}

func TestURLImportCancelDropsTheResult(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(*ui.Tester) error
	}{
		{"button", func(tt *ui.Tester) error { return tt.Click("取消") }},
		{"escape", func(tt *ui.Tester) error { tt.Key(0, ui.KeyEscape); tt.Frame(); return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, tt, posts := urlModel(t)
			release := make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
			defer s.Close()
			defer close(release)

			startImport(t, m, tt, s.URL)
			tt.Frame()
			if err := tc.close(tt); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if m.urlImp.open || m.urlImp.fetching {
				t.Errorf("not closed: %+v", m.urlImp)
			}
			(<-posts)() // the request ends because it was cancelled
			tt.Frame()
			if m.editing || m.urlImp.err != "" {
				t.Errorf("editing=%v err=%q", m.editing, m.urlImp.err)
			}
		})
	}
}

func TestURLImportIgnoresStaleResult(t *testing.T) {
	m := newOfflineModel(t)
	m.openURLImport("")
	gen := m.urlImp.gen
	m.urlImp.gen++
	m.finishURLImport(gen, article.Article{Title: "Old", Text: "Old."}, nil)
	m.finishURLImport(gen, article.Article{}, errors.New("old failure"))
	if m.editing || m.urlImp.err != "" {
		t.Errorf("editing=%v err=%q", m.editing, m.urlImp.err)
	}
}

func TestOpenURLImportUsesClipboardLink(t *testing.T) {
	m := newOfflineModel(t)
	m.openURLImport("  https://example.com/a\n")
	if m.urlImp.input != "https://example.com/a" {
		t.Errorf("input = %q", m.urlImp.input)
	}
	m.openURLImport("just some text")
	if m.urlImp.input != "" {
		t.Errorf("input = %q", m.urlImp.input)
	}
}

func TestURLImportNeedsALink(t *testing.T) {
	m, tt, _ := urlModel(t)
	chooseAdd(t, tt, "URL")
	m.urlImp.input = "   "
	tt.Frame()
	tt.Click("导入")
	if m.urlImp.fetching {
		t.Error("fetch started without a link")
	}
}

// Typing a link must not trigger the window's single-key shortcuts.
func TestURLImportBlocksShortcuts(t *testing.T) {
	m, tt, _ := urlModel(t)
	addMaterial(m, "River", "The river rises.", "")
	tt.Frame()
	chooseAdd(t, tt, "URL")
	tt.Key(0, ui.KeyP)
	tt.Frame()
	if m.cur.Pinned {
		t.Error("P pinned the material while the dialog was open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyP)
	tt.Frame()
	if !m.cur.Pinned {
		t.Error("P should pin once the dialog is closed")
	}
}

func TestSidebarHasOneAddButton(t *testing.T) {
	m := newOfflineModel(t)
	tt := ui.NewTester(m.view, 1000, 720)
	tt.Frame()
	for _, old := range []string{"新建", "文件", "链接", "导入", "URL", "粘贴"} {
		if tt.HasText(old) {
			t.Errorf("%q shows before the menu opens: %v", old, tt.Texts())
		}
	}
	if !tt.HasText("新增") {
		t.Errorf("no 新增 button: %v", tt.Texts())
	}
	if err := tt.Click("新增资料"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"导入", "URL", "粘贴"} {
		if !tt.HasText(item) {
			t.Errorf("menu lacks %q: %v", item, tt.Texts())
		}
	}
}

func TestAddMenuPasteOpensEditor(t *testing.T) {
	m := newOfflineModel(t)
	tt := ui.NewTester(m.view, 1000, 720)
	tt.SetClipboard("One.")
	chooseAdd(t, tt, "粘贴")
	if !m.editing || m.urlImp.open || m.draftText != "One." {
		t.Errorf("editing=%v dialog=%v text=%q", m.editing, m.urlImp.open, m.draftText)
	}
	tt.Type(" Two.")
	tt.Frame()
	if !strings.Contains(m.draftText, "Two.") {
		t.Errorf("the editor has no keyboard: %q", m.draftText)
	}
}

// Choosing URL must leave the keyboard in the link field, not on the button
// the menu opened from.
func TestAddMenuURLFocusesTheField(t *testing.T) {
	m := newOfflineModel(t)
	tt := ui.NewTester(m.view, 1000, 720)
	chooseAdd(t, tt, "URL")
	tt.Type("example.com")
	tt.Frame()
	if m.urlImp.input != "example.com" {
		t.Errorf("input = %q", m.urlImp.input)
	}
}

// A long link is easier to check in a box that shows several lines.
func TestURLImportBoxShowsSeveralLines(t *testing.T) {
	m, tt, _ := urlModel(t)
	chooseAdd(t, tt, "URL")
	m.urlImp.input = "https://example.com/" + strings.Repeat("long-path-segment/", 30)
	tt.Frame()
	r, ok := tt.Find("文章链接")
	if !ok {
		t.Fatal("no link box")
	}
	if r.W < 560 || r.H < 60 {
		t.Errorf("box is only %v x %v", r.W, r.H)
	}
}

func TestURLImportEnterStartsFetch(t *testing.T) {
	m, tt, posts := urlModel(t)
	chooseAdd(t, tt, "URL")
	tt.Type("http://127.0.0.1:1/a")
	tt.Key(0, ui.KeyEnter)
	if !m.urlImp.fetching {
		t.Fatal("Enter did not start the fetch")
	}
	if m.urlImp.input != "http://127.0.0.1:1/a" {
		t.Errorf("input = %q", m.urlImp.input)
	}
	(<-posts)()
}
