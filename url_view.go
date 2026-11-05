package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"repi/internal/article"
	"repi/internal/store"
)

// urlImport is the state of the "import from a link" dialog, on the UI thread.
type urlImport struct {
	open     bool
	input    string
	fetching bool
	err      string
	cancel   context.CancelFunc
	gen      int             // bumped when a fetch starts or is abandoned
	post     func(fn func()) // runs fn on the UI thread; the window's when nil
}

// openURLImport opens the dialog, filled in when the clipboard holds a link.
func (m *model) openURLImport(clip string) {
	m.stopURLFetch()
	u := &m.urlImp
	u.open, u.err, u.input = true, "", ""
	if article.IsURL(clip) {
		u.input = strings.TrimSpace(clip)
	}
}

// startURLFetch downloads the link in the background.
func (m *model) startURLFetch() {
	u := &m.urlImp
	if u.fetching || strings.TrimSpace(u.input) == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.gen++
	u.fetching, u.err, u.cancel = true, "", cancel
	gen, link, post := u.gen, u.input, u.post
	if post == nil {
		post = m.win.Update
	}
	go func() {
		defer cancel()
		art, err := article.Fetch(ctx, link)
		post(func() { m.finishURLImport(gen, art, err) })
	}()
}

// stopURLFetch abandons the running fetch, if any.
func (m *model) stopURLFetch() {
	u := &m.urlImp
	if u.cancel != nil {
		u.cancel()
	}
	u.gen++
	u.fetching, u.cancel = false, nil
}

// finishURLImport takes the result of the fetch numbered gen: an article opens
// the editor, an error stays in the dialog so the link can be corrected.
func (m *model) finishURLImport(gen int, art article.Article, err error) {
	u := &m.urlImp
	if gen != u.gen {
		return
	}
	u.fetching, u.cancel = false, nil
	if err != nil {
		u.err = err.Error()
		return
	}
	u.open = false
	m.startEdit(store.Material{}, art.Text)
	m.draftTitle = art.Title
}

func (m *model) urlImportOverlay(c *ui.Context) {
	t := c.Theme()
	u := &m.urlImp
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Padding(18, 24).Gap(10).MinWidth(620).MaxWidth(720).BorderWidth(2).BorderColor(t.Accent).Background(t.Background).Children(func() {
			ui.Text(c, "从链接导入").FontSize(16).Bold()
			ui.TextArea(c, &u.input).Placeholder("https://…").Label("文章链接").Lines(3, 6).AutoFocus().
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter || k.Mods != 0 {
						return false
					}
					u.input = k.Text() // what was typed since the last frame
					m.startURLFetch()  // a link has no line breaks to type
					return true
				})
			if u.err != "" {
				ui.Text(c, u.err).TextColor(t.Danger).Wrap()
			}
			if u.fetching {
				ui.Text(c, "正在抓取…").TextColor(t.TextMuted)
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, "导入").Disabled(u.fetching || strings.TrimSpace(u.input) == "").Clicked() {
					m.startURLFetch()
				}
				if ui.Button(c, "取消").Clicked() {
					u.open = false
				}
			})
		})
	})
	if !u.open && u.fetching { // closed by the button or by Esc
		m.stopURLFetch()
	}
}
