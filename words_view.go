package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/store"
	"repi/internal/tts"
)

// wordsState is the word book panel: its search box, and the review
// session when one is running.
type wordsState struct {
	query      string
	confirmDel string // word awaiting a second click on delete

	reviewing bool
	queue     []string // words left in this session; the first is shown
	total     int      // words the session started with
	done      int      // reviews graded Hard or better
	revealed  bool     // the meaning of queue[0] is shown
	said      string   // queue[0] when its sentence was last played
}

// gradeNames are the buttons that grade a review, in store.Grade order.
var gradeNames = []string{"忘了", "模糊", "记得", "简单"}

// lookupWord is the word card's entry as a word to save: the base form when
// an inflection was clicked, with the clicked form kept for highlighting.
func (m *model) lookupWord() store.Word {
	lk := &m.lookup
	r := lk.res
	clicked := strings.Trim(lk.token, `.,;:!?"'“”‘’()[]`)
	w := store.Word{Word: r.Word, Phonetic: r.Phonetic, Meanings: r.Meanings, Sentence: lk.sentence}
	if r.Lemma != nil {
		w.Word, w.Phonetic, w.Meanings = r.Lemma.Word, r.Lemma.Phonetic, r.Lemma.Meanings
	}
	if !strings.EqualFold(clicked, w.Word) {
		w.Form = clicked
	}
	if m.open {
		w.Material = m.cur.ID
	}
	return w
}

// saveWordButton adds the word on the card to the word book, or says it is
// there already.
func (m *model) saveWordButton(c *ui.Context) {
	t := c.Theme()
	w := m.lookupWord()
	ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingY(4).Children(func() {
		if !m.st.HasWord(w.Word) {
			if ui.PrimaryButton(c, "").Label("加入生词本").Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconPin).FontSize(13)
					ui.Text(c, "加入生词本")
				})
			}).Clicked() {
				m.st.AddWord(w, time.Now())
			}
			return
		}
		ui.Icon(c, iconPin).FontSize(13).TextColor(t.Accent)
		ui.Textf(c, "%s 已在生词本里", w.Word).TextColor(t.Accent).Grow(1)
		if ui.Button(c, "移出").Label("移出生词本").Clicked() {
			m.st.RemoveWord(w.Word)
		}
	})
}

// wordVoice is the voice that reads words and their sentences: the one
// chosen for English, if it can be used.
func (m *model) wordVoice() (string, bool) {
	v := m.enVoice
	return v, m.hasVoice(v) && tts.VoiceLang(v) == "en"
}

// sayWord plays text, the word or its sentence, in the English voice.
func (m *model) sayWord(key, text string) {
	if v, ok := m.wordVoice(); ok && text != "" {
		m.say("word:"+key, text, v)
	}
}

// wordsPanel lists the saved words, or runs a review.
func (m *model) wordsPanel(c *ui.Context) {
	if m.words.reviewing {
		m.reviewView(c)
		return
	}
	t := c.Theme()
	now := time.Now()
	all := m.st.Words()
	due := m.st.DueCount(now)

	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "生词本").FontSize(18).Bold().Grow(1)
		if due == 0 {
			ui.Text(c, "今天没有要复习的").TextColor(t.TextMuted)
		} else if ui.PrimaryButton(c, fmt.Sprintf("开始复习（%d）", due)).Label("开始复习").Clicked() {
			m.startReview(now)
		}
	})
	if len(all) == 0 {
		ui.Column(c).Fill().Center().Gap(8).Children(func() {
			ui.Text(c, "生词本还是空的").FontSize(18).Bold()
			ui.Text(c, "在大屏（F）里点一个单词查词，再点「加入生词本」，单词会连同所在的句子一起收进来。").TextColor(t.TextMuted).Wrap().MaxWidth(420)
		})
		return
	}
	ui.Textf(c, "共 %d 个词 · 今天要复习 %d 个 · 复习时会朗读收词时的那句话", len(all), due).FontSize(12).TextColor(t.TextMuted)
	ui.SearchField(c, &m.words.query).Label("搜索生词").Placeholder("搜索单词或释义")

	_, audState, audErr := m.auditionState()
	if audErr != "" {
		ui.Text(c, audErr).FontSize(12).TextColor(t.Danger).Wrap()
	}
	_, canSay := m.wordVoice()
	q := strings.ToLower(strings.TrimSpace(m.words.query))
	ui.Scroll(c).Grow(1).Gap(2).Children(func() {
		for _, w := range all {
			if q != "" && !strings.Contains(strings.ToLower(w.Word+" "+w.Form+" "+strings.Join(w.Meanings, " ")), q) {
				continue
			}
			row := ui.Row(c.Key(w.Word)).Padding(8, 12).Gap(12).AlignItems(ui.Center)
			if row.Hovered() {
				row.Background(t.Text.Alpha(0.04))
			}
			row.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, w.Word).FontSize(16).Bold()
						if w.Phonetic != "" {
							ui.Text(c, "/"+w.Phonetic+"/").FontSize(12).TextColor(t.TextMuted)
						}
						if len(w.Meanings) > 0 {
							ui.Text(c, w.Meanings[0]).FontSize(13).MaxLines(1).Grow(1).MinWidth(0)
						}
					})
					if w.Sentence != "" {
						sentenceText(c, w, t.TextMuted, t.Text).FontSize(12).MaxLines(2)
					}
				})
				ui.Text(c, dueLabel(w, now)).FontSize(12).TextColor(t.TextMuted).Shrink(0)
				if canSay && w.Sentence != "" {
					key, _, _ := m.auditionState()
					playing := key == "word:"+w.Word && audState != ""
					if ui.Button(c, "").Label("朗读例句").Tooltip("朗读例句").Children(func() {
						if playing {
							ui.Spinner(c)
						} else {
							ui.Icon(c, iconPlay).FontSize(12)
						}
					}).Clicked() {
						if playing {
							m.stopAudition()
						} else {
							m.sayWord(w.Word, w.Sentence)
						}
					}
				}
				if m.words.confirmDel == w.Word {
					if ui.Button(c, "确认删除").Clicked() {
						m.words.confirmDel = ""
						m.st.RemoveWord(w.Word)
					}
				} else if ui.Button(c, "删除").Label("删除 " + w.Word).Clicked() {
					m.words.confirmDel = w.Word
				}
			})
		}
	})
}

// dueLabel says when w is reviewed next.
func dueLabel(w store.Word, now time.Time) string {
	if !w.Due.After(now) {
		return "待复习"
	}
	y, mo, d := now.Date()
	days := int(w.Due.Sub(time.Date(y, mo, d, 0, 0, 0, 0, now.Location())).Hours()/24 + 0.5)
	switch days {
	case 0, 1:
		return "明天复习"
	case 2:
		return "后天复习"
	}
	return w.Due.Format("1月2日") + "复习"
}

// intervalLabel is how long g would put the word away for.
func intervalLabel(w store.Word, g store.Grade, now time.Time) string {
	n := w.Next(g, now)
	switch {
	case n.Interval == 0:
		return "再看一次"
	case n.Interval < 30:
		return fmt.Sprintf("%d 天", n.Interval)
	case n.Interval < 365:
		return fmt.Sprintf("%d 个月", (n.Interval+15)/30)
	}
	return fmt.Sprintf("%.1f 年", float64(n.Interval)/365)
}

var wordRE = map[string]*regexp.Regexp{}

// sentenceText draws w's sentence with the word in it marked.
func sentenceText(c *ui.Context, w store.Word, color, mark ui.Color) ui.Element {
	s := w.Sentence
	var spans []ui.Span
	for _, f := range []string{w.Form, w.Word} {
		if f == "" {
			continue
		}
		re, ok := wordRE[f]
		if !ok {
			re = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(f) + `\b`)
			wordRE[f] = re
		}
		if loc := re.FindStringIndex(s); loc != nil {
			spans = []ui.Span{
				{Text: s[:loc[0]], Color: color},
				{Text: s[loc[0]:loc[1]], Color: mark, Weight: 700, Underline: true},
				{Text: s[loc[1]:], Color: color},
			}
			break
		}
	}
	if spans == nil {
		spans = []ui.Span{{Text: s, Color: color}}
	}
	return ui.RichText(c, spans...).Wrap()
}

// startReview queues the words due at now.
func (m *model) startReview(now time.Time) {
	m.pl.Pause()
	ws := &m.words
	ws.queue = ws.queue[:0]
	for _, w := range m.st.DueWords(now) {
		ws.queue = append(ws.queue, w.Word)
	}
	ws.reviewing = len(ws.queue) > 0
	ws.total, ws.done, ws.revealed, ws.said = len(ws.queue), 0, false, ""
}

func (m *model) endReview() {
	m.words.reviewing, m.words.queue = false, nil
	m.stopAudition()
}

// grade schedules the word shown and moves on; a forgotten word goes to the
// end of the session.
func (m *model) grade(g store.Grade) {
	ws := &m.words
	if len(ws.queue) == 0 {
		return
	}
	w := ws.queue[0]
	ws.queue = ws.queue[1:]
	m.st.Review(w, g, time.Now())
	if g == store.Again {
		ws.queue = append(ws.queue, w)
	} else {
		ws.done++
	}
	ws.revealed, ws.said = false, ""
}

// currentReview is the word being reviewed, skipping words removed since
// the session started.
func (m *model) currentReview() (store.Word, bool) {
	ws := &m.words
	for len(ws.queue) > 0 {
		for _, w := range m.st.Words() {
			if w.Word == ws.queue[0] {
				return w, true
			}
		}
		ws.queue = ws.queue[1:]
	}
	return store.Word{}, false
}

// reviewView shows one word: its sentence is read aloud, and the meaning is
// shown on Space; 1–4 grade it.
func (m *model) reviewView(c *ui.Context) {
	t := c.Theme()
	ws := &m.words
	now := time.Now()
	w, ok := m.currentReview()

	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "复习生词").FontSize(18).Bold()
		ui.Textf(c, "已完成 %d/%d", ws.done, ws.total).TextColor(t.TextMuted).Grow(1)
		if ui.Button(c, "结束复习").Clicked() || (!ok && c.Shortcut(0, ui.KeyEscape)) {
			m.endReview()
		}
	})
	if ws.total > 0 {
		ui.Progress(c, float64(ws.done)/float64(ws.total))
	}
	if !ok {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, "今天的生词复习完了").FontSize(22).Bold()
			ui.Textf(c, "复习了 %d 个词。明天再来。", ws.done).TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "回到生词本").Clicked() {
				m.endReview()
			}
		})
		return
	}

	_, canSay := m.wordVoice()
	if canSay && ws.said != w.Word && w.Sentence != "" {
		ws.said = w.Word
		m.sayWord(w.Word, w.Sentence)
	}
	if c.Shortcut(0, ui.KeyR) {
		m.sayWord(w.Word, w.Sentence)
	}
	if !ws.revealed && (c.Shortcut(0, ui.KeySpace) || c.Shortcut(0, ui.KeyEnter)) {
		ws.revealed = true
	} else if ws.revealed {
		for i, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4} {
			if c.Shortcut(0, k) {
				m.grade(store.Grade(i))
				return
			}
		}
	}

	ui.Column(c).Grow(1).Center().Gap(14).Padding(0, 24).Children(func() {
		ui.Text(c, w.Word).FontSize(44).Bold()
		if ws.revealed && w.Phonetic != "" {
			ui.Text(c, "/"+w.Phonetic+"/").FontSize(18).TextColor(t.TextMuted)
		}
		if w.Sentence != "" {
			sentenceText(c, w, t.TextMuted, t.Text).FontSize(20).MaxWidth(760).TextAlign(ui.Center)
		}
		ui.Row(c).Gap(8).Children(func() {
			if canSay && w.Sentence != "" && ui.Button(c, "再听一遍 (R)").Label("再听例句").Clicked() {
				m.sayWord(w.Word, w.Sentence)
			}
			if canSay && ui.Button(c, "读单词").Clicked() {
				m.sayWord(w.Word, w.Word)
			}
		})
		if _, _, e := m.auditionState(); e != "" {
			ui.Text(c, e).FontSize(12).TextColor(t.Danger).Wrap()
		}
		if !ws.revealed {
			if ui.PrimaryButton(c, "显示释义（空格）").Clicked() {
				ws.revealed = true
			}
			return
		}
		ui.Column(c).Gap(4).MaxWidth(640).Children(func() {
			for i, line := range w.Meanings {
				ui.Text(c.Key(i), line).FontSize(16).Wrap()
			}
			if len(w.Meanings) == 0 {
				ui.Text(c, "（没有释义）").TextColor(t.TextMuted)
			}
		})
	})
	if !ws.revealed {
		ui.Text(c, "先回想它的意思，再按空格看释义").FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Row(c).Gap(10).Justify(ui.Center).Children(func() {
		for i, name := range gradeNames {
			g := store.Grade(i)
			btn := ui.Button
			if g == store.Good {
				btn = ui.PrimaryButton
			}
			if btn(c.Key(i), "").Label(name).MinWidth(96).Children(func() {
				ui.Column(c).Gap(2).AlignItems(ui.Center).Children(func() {
					ui.Textf(c, "%s  %d", name, i+1)
					hint := ui.Text(c, intervalLabel(w, g, now)).FontSize(11)
					if g != store.Good { // the primary button sets its own color
						hint.TextColor(t.TextMuted)
					}
				})
			}).Clicked() {
				m.grade(g)
			}
		}
	})
}
