package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"repi/internal/player"
	"repi/internal/store"
)

// view builds the window from the model, on the main thread.
func (m *model) view(c *ui.Context) {
	if m.wake.Load() == nil {
		inv := c.Services().Invalidate
		m.wake.Store(&inv)
	}
	c.SetTheme(m.theme.Theme())
	t := c.Theme()
	if !m.editing {
		if c.Shortcut(ui.Shift, ui.KeySlash) {
			m.showHelp = !m.showHelp
		}
		if c.Shortcut(0, ui.KeySlash) {
			m.focusSearch = true
		}
		if c.Shortcut(0, ui.KeyP) {
			m.togglePin()
		}
		if c.Shortcut(0, ui.KeyQ) {
			mygo.App.Quit()
		}
		if c.Shortcut(0, ui.KeySpace) {
			m.pl.Toggle()
		}
		if c.Shortcut(0, ui.KeyRight) {
			m.pl.Next()
		}
		if c.Shortcut(0, ui.KeyLeft) {
			m.pl.Prev()
		}
		if m.open && c.Shortcut(0, ui.KeyF) {
			m.setBigScreen(!m.bigScreen)
		}
		if m.open && m.bigScreen && c.Shortcut(0, ui.KeyT) {
			m.setTranslate(!m.showTrans)
		}
	}
	m.applySetup()
	m.saveProgress(false)
	m.helpOverlay(c)
	m.lookupOverlay(c)
	m.urlImportOverlay(c)

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		m.sidebar(c)
		ui.Column(c).Grow(1).MinWidth(0).Padding(12, 20).Gap(10).Children(func() {
			if m.setupErr != "" {
				ui.Text(c, m.setupErr).TextColor(t.Danger)
			}
			if m.setupErr == noTTSErr && !m.showVoices {
				m.kokoroSetupCard(c)
			}
			switch {
			case m.showStats:
				m.statsPanel(c)
			case m.showVoices:
				m.voicePanel(c)
			case m.editing:
				m.editor(c)
			case m.open:
				m.reader(c)
			default:
				ui.Column(c).Fill().Center().Gap(8).Children(func() {
					ui.Text(c, "磨耳朵").FontSize(28).Bold()
					ui.Text(c, "新增一篇英文资料，让它一遍遍循环朗读。").TextColor(t.TextMuted)
				})
			}
		})
	})
}

func (m *model) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(240).Shrink(0).PaddingY(12).Gap(8).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Padding(0, 16).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, "资料").Bold().Grow(1)
			ui.PrimaryButton(c, "新增").Label("新增资料").PopoverMenuButton(func(menu *ui.Menu) {
				if menu.Item("导入").Chosen() {
					m.importFile()
				}
				if menu.Item("URL").Chosen() {
					m.openURLImport(c.Services().ReadClipboard())
				}
				if menu.Item("粘贴").Chosen() {
					m.startEdit(store.Material{}, c.Services().ReadClipboard())
				}
			})
		})
		sel := ""
		if m.open {
			sel = strconv.Itoa(m.cur.ID)
		}
		var chosen string
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, 16).Gap(8).Children(func() { chosen = m.library(c) })
		ui.Row(c).Padding(0, 16).Gap(6).Children(func() {
			label := "音色"
			if m.showVoices {
				label = "← 返回"
			}
			if ui.Button(c, label).Label("音色管理").Grow(1).Clicked() {
				m.showVoices = !m.showVoices
				m.showStats = false
				if !m.showVoices {
					m.stopAudition()
				}
			}
			label = "统计"
			if m.showStats {
				label = "← 返回"
			}
			if ui.Button(c.Key("stats"), label).Label("学习统计").Grow(1).Clicked() {
				m.showStats = !m.showStats
				m.showVoices = false
				m.stopAudition()
			}
		})
		if chosen != sel {
			if id, err := strconv.Atoi(chosen); err == nil {
				if mat, ok := m.st.Get(id); ok {
					m.showVoices, m.showStats = false, false
					m.stopAudition()
					m.openMaterial(mat)
				}
			}
		}
	})
}

// startEdit opens the editor on mat, with clip as the text of a new one.
func (m *model) startEdit(mat store.Material, clip string) {
	m.showVoices, m.showStats = false, false
	m.stopAudition()
	m.editing, m.editID = true, mat.ID
	m.draftTitle, m.draftText = mat.Title, mat.Text
	m.draftTags = strings.Join(mat.Tags, " ")
	if mat.ID == 0 {
		m.draftText = clip
	}
}

// importFile asks for a text file and opens the editor on its content.
func (m *model) importFile() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:   "选择英文文本",
			Filters: []mygo.FileFilter{{Name: "Text", Extensions: []string{"txt", "md"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		b, err := os.ReadFile(paths[0])
		if err != nil {
			return
		}
		name := paths[0]
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '/' {
				name = name[i+1:]
				break
			}
		}
		m.win.Update(func() {
			m.startEdit(store.Material{}, string(b))
			m.draftTitle = name
		})
	}()
}

func (m *model) editor(c *ui.Context) {
	ui.TextInput(c, &m.draftTitle).Placeholder("标题（可留空）").Label("标题")
	ui.TextInput(c, &m.draftTags).Placeholder("空格或逗号分隔").Label("标签")
	ui.TextArea(c, &m.draftText).Placeholder("在这里粘贴英文资料…").Label("正文").Grow(1).AutoFocus()
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, "保存并打开").Clicked() {
			if m.saveDraft() {
				m.pl.Play()
			}
		}
		if ui.Button(c, "取消").Clicked() {
			m.editing = false
		}
	})
}

func (m *model) reader(c *ui.Context) {
	t := c.Theme()
	st := m.pl.State()
	sents, ready := m.sentenceList()

	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, m.cur.Title).FontSize(18).Bold().Grow(1).MaxLines(1)
		if d := m.st.MaterialListened(m.cur.ID); d > 0 {
			ui.Text(c, "已听 "+fmtListen(d)).TextColor(t.TextMuted).FontSize(12)
		}
		rd, total := m.progress()
		if status := m.kokoro.Status(); status != "" {
			ui.Text(c, status).TextColor(t.TextMuted).FontSize(12).MaxLines(1)
		} else if rd < total {
			ui.Textf(c, "合成中 %d/%d", rd, total).TextColor(t.TextMuted).FontSize(12)
		}
		pin := "置顶"
		if m.cur.Pinned {
			pin = "取消置顶"
		}
		if ui.Button(c, pin).Clicked() {
			m.togglePin()
		}
		if ui.Button(c, "大屏").Label("大屏显示").Clicked() {
			m.setBigScreen(!m.bigScreen)
		}
		if ui.Button(c, "编辑").Clicked() {
			m.startEdit(m.cur, "")
		}
		if m.confirmDel == m.cur.ID {
			if ui.Button(c, "确认删除").Clicked() {
				m.confirmDel = 0
				m.remove(m.cur.ID)
			}
		} else if ui.Button(c, "删除").Clicked() {
			m.confirmDel = m.cur.ID
		}
	})

	if m.bigScreen {
		m.bigView(c, st, sents)
		m.controls(c, st, len(sents))
		return
	}
	ui.Text(c, "灰色：未合成 · 红色：失败 · 左侧竖条：当前句").FontSize(11).TextColor(t.TextMuted)

	ui.Scroll(c).Grow(1).Gap(4).PaddingY(4).Children(func() {
		for i, s := range sents {
			current := i == st.Index
			// Flat rows: the current sentence gets the accent bar and a faint
			// fill; hover and the rest take Omarchy's 4% control fill.
			row := ui.Row(c.Key(i)).Padding(6, 12).AlignItems(ui.Center).Gap(10).BorderWidth(0, 0, 0, 2).BorderColor(ui.Transparent)
			switch {
			case current:
				row.BorderColor(t.Accent).Background(t.Text.Alpha(0.08))
			case st.Failed[i] != "":
				row.BorderColor(t.Danger)
			case row.Hovered():
				row.Background(t.Text.Alpha(0.04))
			}
			row.Children(func() {
				ui.Textf(c, "%d", i+1).FontSize(12).TextColor(t.TextMuted).Width(28)
				shown := s
				if m.hideText && !current {
					shown = "· · ·"
				}
				txt := ui.Text(c, shown).FontSize(16).Grow(1).Wrap()
				switch {
				case st.Failed[i] != "":
					txt.TextColor(t.Danger)
				case !ready[i]:
					txt.TextColor(t.TextMuted)
				}
				if current { // the highlight hides the tint: only say what needs waiting
					switch {
					case st.Waiting:
						ui.Spinner(c)
					case !ready[i]:
						ui.Text(c, "需合成").FontSize(12).TextColor(t.Warning)
					}
				}
			})
			if current && m.lastIdx != i {
				row.ScrollIntoView()
				m.lastIdx = i
			}
			if row.Clicked() {
				m.pl.Seek(i)
				m.pl.Play()
			}
		}
	})

	m.controls(c, st, len(sents))
}

func (m *model) controls(c *ui.Context, st player.Snapshot, n int) {
	t := c.Theme()
	ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "").Label("上一句").Tooltip("上一句").Children(func() { ui.Icon(c, iconPrev).FontSize(14) }).Clicked() {
			m.pl.Prev()
		}
		label, icon := "播放", iconPlay
		if st.Playing {
			label, icon = "暂停", iconPause
		}
		if ui.PrimaryButton(c, "").Label(label).MinWidth(84).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).FontSize(13)
				ui.Text(c, label)
			})
		}).Clicked() {
			m.pl.Toggle()
		}
		if ui.Button(c, "").Label("下一句").Tooltip("下一句").Children(func() { ui.Icon(c, iconNext).FontSize(14) }).Clicked() {
			m.pl.Next()
		}

		mode := int(st.Mode)
		if ui.Segmented(c, &mode, "整篇循环", "逐句重复").Label("循环方式").Changed() {
			m.pl.SetMode(player.Mode(mode))
		}
		if st.Mode == player.RepeatEach {
			r := float64(st.Repeat)
			ui.Text(c, "每句").TextColor(t.TextMuted)
			if ui.Stepper(c, &r, 1, 20, 1).Label("重复次数").Changed() {
				m.pl.SetRepeat(int(r))
			}
			ui.Textf(c, "%d 次", st.Repeat)
		}
	})
	ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		if voices := m.readingVoices(); len(voices) > 0 {
			m.voiceSel = m.voice
			if ui.Select(c, &m.voiceSel, voices).Width(240).Label("音色").Changed() {
				m.applyVoiceSpeed(m.voiceSel, m.speed)
			}
		}
		if len(m.favorites) == 0 {
			ui.Text(c, "到「音色」里收藏常用的").FontSize(12).TextColor(t.TextMuted)
		}
		if ui.Select(c, &m.speedSel, speedLabels).Width(90).Label("语速").Changed() {
			for i, l := range speedLabels {
				if l == m.speedSel {
					m.applyVoiceSpeed(m.voice, speedValues[i])
				}
			}
		}
		if ui.Switch(c, &m.hideText).Label("隐藏文字").Changed() {
			m.saveSettings()
		}
		ui.Text(c, "隐藏文字").TextColor(t.TextMuted)
		ui.Textf(c, "第 %d/%d 句 · 已循环 %d 轮 · 空格 播放/暂停 · ←→ 切句", min(st.Index+1, n), n, st.Loops).
			FontSize(12).TextColor(t.TextMuted)
	})
}

// helpOverlay lists every shortcut, as Omarchy's apps do on "?".
func (m *model) helpOverlay(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &m.showHelp, func() {
		ui.Column(c).Padding(16, 20).Gap(6).MinWidth(320).BorderWidth(2).BorderColor(t.Accent).Background(t.Background).Children(func() {
			ui.Text(c, "快捷键").Bold().FontSize(14)
			for _, k := range [][2]string{
				{"空格", "播放 / 暂停"},
				{"← →", "上一句 / 下一句"},
				{"F", "大屏显示"},
				{"T", "大屏里显示 / 隐藏整句翻译"},
				{"点单词", "大屏里查词（朗读暂停，关闭后继续）"},
				{"/", "搜索资料"},
				{"P", "置顶 / 取消置顶"},
				{"?", "显示 / 关闭本页"},
				{"Q", "退出"},
				{"Esc", "关闭弹窗"},
			} {
				ui.Row(c.Key(k[0])).Gap(16).Children(func() {
					ui.Text(c, k[0]).TextColor(t.Accent).Width(56)
					ui.Text(c, k[1])
				})
			}
		})
	})
}
