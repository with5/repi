package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// voicePanel lists all voices: audition each one with the sample, and star
// the ones the reader should offer.
func (m *model) voicePanel(c *ui.Context) {
	t := c.Theme()
	playing, state, errText := m.auditionState()

	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "音色").FontSize(18).Bold().Grow(1)
		ui.Textf(c, "已收藏 %d 个", len(m.favorites)).TextColor(t.TextMuted)
	})
	ui.Text(c, "点「试听」听一下，点星标收藏。朗读时只会列出收藏的音色。").FontSize(12).TextColor(t.TextMuted)
	ui.TextInput(c, &m.sample).Placeholder("试听用的句子").Label("试听文本")
	if errText != "" {
		ui.Text(c, errText).TextColor(t.Danger).FontSize(12)
	}
	if status := m.kokoro.Status(); status != "" {
		ui.Text(c, status).TextColor(t.TextMuted).FontSize(12).MaxLines(1)
	}

	ui.Scroll(c).Grow(1).Gap(4).PaddingY(4).Children(func() {
		for _, v := range m.voiceList() {
			fav := m.isFavorite(v.Name)
			current := v.Name == m.voice
			row := ui.Row(c.Key(v.Name)).Padding(6, 12).AlignItems(ui.Center).Gap(10).BorderWidth(0, 0, 0, 2).BorderColor(ui.Transparent)
			switch { // being auditioned, the voice in use
			case playing == v.Name:
				row.BorderColor(t.Accent).Background(t.Text.Alpha(0.08))
			case current:
				row.BorderColor(t.Accent)
			case row.Hovered():
				row.Background(t.Text.Alpha(0.04))
			}
			done, total := m.processed(v.Name)
			row.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					name := v.Label
					if current {
						name += "  · 当前"
					}
					ui.Text(c, name).FontSize(15)
					ui.Text(c, v.Detail).FontSize(12).TextColor(t.TextMuted)
				})
				if total > 0 {
					chip := ui.Row(c.Key("proc")).Padding(2, 8).AlignItems(ui.Center).Border(1, t.Border)
					label := fmt.Sprintf("已处理 %d/%d", done, total)
					switch {
					case done == total:
						chip.BorderColor(t.Success)
						label += " · 无需等待"
					case done > 0:
						chip.BorderColor(t.Warning)
					default:
						label = "未处理"
					}
					chip.Children(func() { ui.Text(c, label).FontSize(12) })
				}
				if playing == v.Name {
					ui.Text(c, state).FontSize(12).TextColor(t.TextMuted)
					if ui.Button(c.Key("stop"), "停止").Clicked() {
						m.stopAudition()
					}
				} else if ui.Button(c.Key("play"), "试听").Label("试听 " + v.Label).Clicked() {
					m.audition(v.Name)
				}
				star := "☆"
				if fav {
					star = "★"
				}
				b := ui.Button(c.Key("fav"), star).Label("收藏 " + v.Label).Width(40)
				if fav {
					b.TextColor(t.Accent)
				}
				if b.Clicked() {
					m.toggleFavorite(v.Name)
				}
			})
		}
	})
}
