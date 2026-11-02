package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"repi/internal/dict"
	"repi/internal/translate"
)

// dictState is the offline dictionary: whether it is installed, opened, and
// being downloaded. Downloading happens on another goroutine.
type dictState struct {
	path string
	url  string // where the CSV comes from, dict.CSVURL when empty

	mu          sync.Mutex
	d           *dict.Dict
	downloading bool
	status      string
	err         string
}

// translator is what the big screen needs to show the Chinese of a sentence.
type translator interface {
	Cached(sentence string) (string, bool)
	Translate(ctx context.Context, sentence string) (string, error)
}

// transState holds the translations seen in this run, by sentence.
type transState struct {
	mu      sync.Mutex
	done    map[string]string
	errs    map[string]string
	pending map[string]bool
}

// lookupCard is the word card: what was clicked and what the dictionary says.
type lookupCard struct {
	open      bool
	shown     bool // the card was open in the last frame
	resume    bool // reading was on when the card opened
	token     string
	res       dict.Result
	found     bool
	installed bool
}

func (m *model) initDict(dataDir, transCache string) {
	m.dic.path = filepath.Join(dataDir, "dict", "ecdict.bin")
	m.dic.reopen()
	m.tr = &translate.Client{Dir: transCache}
}

// reopen opens the dictionary file if there is one.
func (s *dictState) reopen() {
	d, err := dict.Open(s.path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.d = d
	} else if !os.IsNotExist(err) {
		s.err = err.Error()
	}
}

func (s *dictState) get() *dict.Dict {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d
}

func (s *dictState) snapshot() (installed, downloading bool, status, err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d != nil, s.downloading, s.status, s.err
}

// download fetches ECDICT and builds the dictionary file, in the background.
func (m *model) downloadDict() {
	s := &m.dic
	s.mu.Lock()
	if s.downloading {
		s.mu.Unlock()
		return
	}
	s.downloading, s.err, s.status = true, "", "正在连接…"
	url := s.url
	s.mu.Unlock()
	if url == "" {
		url = dict.CSVURL
	}
	go func() {
		err := dict.Download(context.Background(), url, s.path, func(st string) {
			s.mu.Lock()
			s.status = st
			s.mu.Unlock()
			m.invalidate()
		})
		if err == nil {
			s.reopen()
		}
		s.mu.Lock()
		s.downloading, s.status = false, ""
		if err != nil {
			s.err = err.Error()
		}
		s.mu.Unlock()
		m.invalidate()
	}()
}

// openLookup shows the word card for a word clicked on the big screen, and
// pauses the reading until the card is closed.
func (m *model) openLookup(token string, playing bool) {
	m.lookup.token, m.lookup.resume = token, playing
	m.refreshLookup()
	m.lookup.open = true
	m.pl.Pause()
}

func (m *model) refreshLookup() {
	d := m.dic.get()
	m.lookup.installed = d != nil
	m.lookup.res, m.lookup.found = dict.Result{}, false
	if d != nil {
		m.lookup.res, m.lookup.found = d.Lookup(m.lookup.token)
	}
}

// lookupOverlay is the word card. Closing it (Esc, a click outside) goes on
// reading if it was reading when the card opened.
func (m *model) lookupOverlay(c *ui.Context) {
	t := c.Theme()
	lk := &m.lookup
	if lk.open && !lk.installed && m.dic.get() != nil { // the download finished
		m.refreshLookup()
	}
	ui.Modal(c, &lk.open, func() {
		ui.Column(c).Padding(18, 24).Gap(10).MinWidth(420).MaxWidth(640).BorderWidth(2).BorderColor(t.Accent).Background(t.Background).Children(func() {
			switch {
			case !lk.installed:
				m.dictInstallBody(c)
			case !lk.found:
				ui.Text(c, strings.Trim(lk.token, `.,;:!?"'“”‘’()[]`)).FontSize(24).Bold()
				ui.Text(c, "词典里没有这个词。").TextColor(t.TextMuted)
			default:
				entryView(c, lk.res)
			}
			ui.Text(c, "Esc 关闭，之后继续朗读").FontSize(11).TextColor(t.TextMuted)
		})
	})
	if lk.shown && !lk.open && lk.resume {
		m.pl.Play()
	}
	lk.shown = lk.open
}

// dictInstallBody offers the download of the dictionary.
func (m *model) dictInstallBody(c *ui.Context) {
	t := c.Theme()
	_, downloading, status, errText := m.dic.snapshot()
	ui.Text(c, "查词需要离线词典").FontSize(18).Bold()
	ui.Text(c, "词典用 ECDICT（约 40 万词条，下载 66MB，建好索引后占 28MB）。只需下载一次，之后完全离线。").TextColor(t.TextMuted).Wrap()
	if errText != "" {
		ui.Text(c, errText).TextColor(t.Danger).Wrap()
	}
	if downloading {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, status)
		})
	} else if ui.PrimaryButton(c, "下载词典").Clicked() {
		m.downloadDict()
	}
}

// entryView draws the dictionary entry of a word, and of its base form.
func entryView(c *ui.Context, r dict.Result) {
	t := c.Theme()
	head := func(e dict.Entry, size float32) {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, e.Word).FontSize(size).Bold()
			if e.Phonetic != "" {
				ui.Text(c, "/"+e.Phonetic+"/").FontSize(size * 0.6).TextColor(t.TextMuted)
			}
			for _, tag := range e.Tags {
				ui.Row(c.Key(tag)).Padding(1, 6).Border(1, t.Border).Children(func() {
					ui.Text(c, tag).FontSize(11).TextColor(t.TextMuted)
				})
			}
		})
	}
	meanings := func(e dict.Entry) {
		for i, line := range e.Meanings {
			ui.Text(c.Key(i), line).FontSize(15).Wrap()
		}
	}
	head(r.Entry, 26)
	if r.Lemma != nil {
		ui.Text(c, fmt.Sprintf("%s 是 %s 的%s", r.Word, r.Lemma.Word, r.Relation)).TextColor(t.Accent)
		if len(r.Meanings) > 1 { // more than "go的过去式"
			meanings(r.Entry)
		}
		ui.Column(c.Key("lemma")).Gap(6).PaddingY(6).Children(func() {
			head(*r.Lemma, 20)
			meanings(*r.Lemma)
		})
		return
	}
	meanings(r.Entry)
	if len(r.Forms) > 0 {
		var parts []string
		for _, f := range r.Forms {
			parts = append(parts, f.Label+" "+f.Word)
		}
		ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(t.TextMuted).Wrap()
	}
}

func (m *model) setTranslate(on bool) {
	m.showTrans = on
	if on { // asking again after a failure
		m.tran.mu.Lock()
		clear(m.tran.errs)
		m.tran.mu.Unlock()
	}
	m.saveSettings()
}

// translation returns the Chinese of a sentence. It is fetched in the
// background the first time, so loading is true until it is there.
func (m *model) translation(sentence string) (text, errText string, loading bool) {
	ts := &m.tran
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if s, ok := ts.done[sentence]; ok {
		return s, "", false
	}
	if e, ok := ts.errs[sentence]; ok {
		return "", e, false
	}
	if ts.pending[sentence] {
		return "", "", true
	}
	if s, ok := m.tr.Cached(sentence); ok {
		if ts.done == nil {
			ts.done = map[string]string{}
		}
		ts.done[sentence] = s
		return s, "", false
	}
	if ts.pending == nil {
		ts.pending, ts.errs = map[string]bool{}, map[string]string{}
	}
	ts.pending[sentence] = true
	go func() {
		s, err := m.tr.Translate(context.Background(), sentence)
		ts.mu.Lock()
		delete(ts.pending, sentence)
		if err != nil {
			ts.errs[sentence] = err.Error()
		} else {
			if ts.done == nil {
				ts.done = map[string]string{}
			}
			ts.done[sentence] = s
		}
		ts.mu.Unlock()
		m.invalidate()
	}()
	return "", "", true
}

// retryTranslation forgets the failure of a sentence, so it is asked again.
func (m *model) retryTranslation(sentence string) {
	m.tran.mu.Lock()
	delete(m.tran.errs, sentence)
	m.tran.mu.Unlock()
}
