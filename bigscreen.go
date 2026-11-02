package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/align"
	"repi/internal/player"
)

func (m *model) setBigScreen(on bool) {
	m.bigScreen = on
	m.saveSettings()
}

// bigCache keeps what is worked out once per sound rather than every frame.
type bigCache struct {
	path   string
	index  int
	tokens []align.Token
	ends   []time.Duration // when each token has been said

	fitKey  string
	fitSize float32
}

// wordState says which word of the sentence at index is being said, and in
// how long the next word begins; active is -1 when that sentence is not
// playing.
func (m *model) wordState(index int, sentence string) (active int, next time.Duration, tokens []align.Token) {
	bc := &m.big
	now, ok := m.pl.Now()
	if !ok || now.Index != index {
		return -1, 0, align.Tokens(sentence)
	}
	if bc.path != now.Path || bc.index != index || bc.tokens == nil {
		sp, err := align.SpeechOf(now.Path)
		if err != nil || sp.End <= sp.Start {
			// unreadable audio: spread the words over the whole sound
			sp = align.Speech{End: now.Total, Total: now.Total}
		}
		bc.path, bc.index, bc.tokens = now.Path, index, align.Tokens(sentence)
		bc.ends = align.Align(bc.tokens, sp)
	}
	i := align.Index(bc.ends, now.Pos)
	if i < 0 {
		return -1, 0, bc.tokens
	}
	return i, bc.ends[i] - now.Pos, bc.tokens
}

// followSound asks for one more frame when the highlighted word is due to
// change, rather than drawing continuously: nothing else on the screen moves.
// One timer at a time, whatever calls it.
func (m *model) followSound(next time.Duration) {
	if m.ticking.CompareAndSwap(false, true) {
		time.AfterFunc(min(max(next, 5*time.Millisecond), 500*time.Millisecond), func() {
			m.ticking.Store(false)
			m.invalidate()
		})
	}
}

// bigView shows the sentence being read in large type, the word being said
// highlighted and the words not said yet dimmed.
func (m *model) bigView(c *ui.Context, st player.Snapshot, sents []string) {
	t := c.Theme()
	if len(sents) == 0 {
		return
	}
	idx := min(st.Index, len(sents)-1)
	active, next, tokens := m.wordState(idx, sents[idx])
	if st.Playing {
		m.followSound(next)
	}

	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		info := ""
		if st.Mode == player.RepeatEach {
			info = " · 第 " + strconv.Itoa(st.Rep+1) + "/" + strconv.Itoa(st.Repeat) + " 遍"
		}
		ui.Text(c, "第 "+strconv.Itoa(idx+1)+"/"+strconv.Itoa(len(sents))+" 句"+info).FontSize(14).TextColor(t.TextMuted).Grow(1)
		if ui.Switch(c, &m.showTrans).Label("翻译").Changed() {
			m.setTranslate(m.showTrans)
		}
		ui.Text(c, "翻译").FontSize(12).TextColor(t.TextMuted)
		size := float64(m.bigFont)
		ui.Text(c, "字号").FontSize(12).TextColor(t.TextMuted)
		if ui.Stepper(c, &size, minBigFont, maxBigFont, 4).Label("字号").Changed() {
			m.bigFont = int(size)
			m.saveSettings()
		}
		ui.Textf(c, "%d", m.bigFont).FontSize(12).TextColor(t.TextMuted).Width(28)
	})

	area := ui.Column(c.Key("big")).Grow(1).MinHeight(0).Center().Padding(0, 24)
	b := area.Bounds()     // the previous frame's; empty in the first
	reserve := float32(40) // room under the sentence for the Chinese
	if m.showTrans {
		reserve += 110
	}
	fs := m.fitFont(sents[idx], float32(m.bigFont), b.W-48, b.H-reserve)

	area.Children(func() {
		ui.Scroll(c).MaxWidth(1400).Children(func() {
			ui.Row(c).Wrap().Justify(ui.Center).GapX(fs * 0.3).GapY(fs * 0.25).Children(func() {
				for i, tk := range tokens {
					w := ui.Row(c.Key(i)).Padding(fs*0.04, fs*0.16).Cursor(ui.CursorPointer)
					if i == active {
						w.Background(t.Accent.Alpha(0.30))
					} else if w.Hovered() {
						w.Background(t.Text.Alpha(0.10)) // a word can be clicked to look it up
					}
					w.Children(func() {
						txt := ui.Text(c, tk.Text).FontSize(fs)
						if active >= 0 && i > active {
							txt.TextColor(t.TextMuted) // not said yet
						}
					})
					if w.Clicked() {
						m.openLookup(tk.Text, st.Playing)
					}
				}
			})
		})
		if m.showTrans {
			m.translationView(c, sents[idx], fs)
		}
		if st.Waiting {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "合成中…").TextColor(t.TextMuted)
			})
		}
	})
}

// fitFont returns the largest font size up to max at which the sentence fits
// in w by h, never below minBigFont. Words are set apart by gaps and padding,
// which make the lines taller and the words wider than the plain text. The
// answer is kept until the sentence, the size or the room changes.
func (m *model) fitFont(sentence string, max, w, h float32) float32 {
	if w <= 0 || h <= 0 {
		return max // not laid out yet
	}
	key := fmt.Sprintf("%s|%v|%.0f|%.0f", sentence, max, w, h)
	if m.big.fitKey == key {
		return m.big.fitSize
	}
	fits := func(fs float32) bool {
		l := ui.ShapeText(sentence, ui.Font{Size: fs}, w/1.12)
		_, th := l.Size()
		return th*1.3 <= h
	}
	out := max
	if lo, hi := float32(minBigFont), max; hi >= lo && !fits(hi) {
		for hi-lo > 1 {
			mid := (lo + hi) / 2
			if fits(mid) {
				lo = mid
			} else {
				hi = mid
			}
		}
		out = lo
	}
	m.big.fitKey, m.big.fitSize = key, out
	return out
}

// translationView shows the Chinese of the sentence under it, in a size that
// follows the sentence's but stays readable.
func (m *model) translationView(c *ui.Context, sentence string, fs float32) {
	t := c.Theme()
	text, errText, loading := m.translation(sentence)
	size := min(max(fs*0.45, 18), 32)
	ui.Row(c.Key("trans")).Justify(ui.Center).AlignItems(ui.Center).Gap(10).PaddingY(10).Children(func() {
		switch {
		case loading:
			ui.Spinner(c)
			ui.Text(c, "翻译中…").FontSize(size * 0.7).TextColor(t.TextMuted)
		case errText != "":
			ui.Text(c, errText).FontSize(14).TextColor(t.Danger).Wrap().MaxWidth(900)
			if ui.Button(c, "重试").Clicked() {
				m.retryTranslation(sentence)
			}
		default:
			ui.Text(c, text).FontSize(size).TextColor(t.TextMuted).Wrap().MaxWidth(1100)
		}
	})
}
