package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"repi/internal/store"
)

// library is the sidebar's list: the search box, the tags, and the materials
// matching them. It returns the ID of the material the user clicked, or of
// the open one if they did not click.
func (m *model) library(c *ui.Context) string {
	t := c.Theme()
	chosen := ""
	if m.open {
		chosen = strconv.Itoa(m.cur.ID)
	}

	search := ui.SearchField(c, &m.query).Placeholder("搜索  /").Label("搜索资料").Shrink(0).MinHeight(30) // an empty field is measured without the CJK fallback font and would clip the placeholder
	if m.focusSearch {
		search.Focus()
		m.focusSearch = false
	}

	tags := store.Tags(m.materials)
	if m.tagFilter != "" && !hasTagCount(tags, m.tagFilter) {
		m.tagFilter = "" // the last material with it is gone
	}
	if len(tags) > 0 {
		ui.Row(c).Wrap().Gap(4).Shrink(0).Children(func() {
			for _, tg := range tags {
				on := strings.EqualFold(tg.Name, m.tagFilter)
				b := ui.Button(c.Key("tag:"+tg.Name), fmt.Sprintf("%s %d", tg.Name, tg.Count)).Label("标签 " + tg.Name).FontSize(11)
				if on {
					b.BorderColor(t.Accent).TextColor(t.Accent)
				}
				if b.Clicked() {
					if on {
						m.tagFilter = ""
					} else {
						m.tagFilter = tg.Name
					}
				}
			}
		})
	}

	hits := store.Filter(m.materials, m.query, m.tagFilter)
	if len(hits) == 0 {
		ui.Text(c, "没有匹配的资料").TextColor(t.TextMuted).FontSize(12).Padding(8, 0)
		return chosen
	}
	ui.Scroll(c).Grow(1).Gap(2).Children(func() {
		for _, h := range hits {
			current := m.open && h.ID == m.cur.ID
			row := ui.Column(c.Key(h.ID)).Padding(6, 8).Gap(2).
				BorderWidth(0, 0, 0, 2).BorderColor(ui.Transparent).Label(h.Title)
			switch {
			case current:
				row.BorderColor(t.Accent).Background(t.Text.Alpha(0.08))
			case row.Hovered():
				row.Background(t.Text.Alpha(0.04))
			}
			row.Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if h.Pinned {
						ui.Icon(c, iconPin).FontSize(10).TextColor(t.Accent)
					}
					ui.Text(c, h.Title).MaxLines(1).Grow(1).MinWidth(0)
				})
				if len(h.Tags) > 0 {
					ui.Text(c, "#"+strings.Join(h.Tags, " #")).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
				}
				if h.Snippet != "" {
					ui.Text(c, h.Snippet).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
				}
			})
			if row.Clicked() {
				chosen = strconv.Itoa(h.ID)
			}
		}
	})
	return chosen
}

func hasTagCount(tags []store.TagCount, name string) bool {
	for _, t := range tags {
		if strings.EqualFold(t.Name, name) {
			return true
		}
	}
	return false
}
