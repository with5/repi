package main

import (
	"context"
	"github.com/egoist/mygo"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"repi/internal/player"
	"repi/internal/store"
	"repi/internal/text"
	"repi/internal/tts"
)

// speeds the speed selector offers.
var speedLabels = []string{"0.7x", "0.85x", "1.0x", "1.15x", "1.3x"}
var speedValues = []float64{0.7, 0.85, 1.0, 1.15, 1.3}

// sizes of the big screen's text, in points
const (
	minBigFont     = 28
	maxBigFont     = 120
	defaultBigFont = 60
)

const defaultVoice = "kokoro:af_heart"

// noTTSErr is the setup error when neither piper nor Kokoro is installed.
const noTTSErr = "没有可用的 TTS：可以在下面一键安装 Kokoro"

// model is the app's state. The fields under mu are also read by the
// player and synthesis goroutines; the rest belongs to the UI thread.
type model struct {
	st     *store.Store
	piper  *tts.Piper
	kokoro *tts.Kokoro
	synth  *tts.Router
	pool   *tts.Pool
	pl     *player.Player
	out    player.Output
	aud    auditioner
	// wake asks the window for another frame; safe from any goroutine.
	wake atomic.Pointer[func()]

	mu        sync.Mutex
	voice     string
	speed     float64
	sentences []string
	ready     []bool
	ver       atomic.Int64 // bumped whenever a sentence becomes ready
	gen       int          // bumped when sentences, voice or speed change
	stopFetch context.CancelFunc

	// UI-thread state.
	setupErr    string
	materials   []store.Material
	cur         store.Material
	open        bool // cur is a material
	editing     bool
	editID      int
	draftTitle  string
	draftText   string
	urlImp      urlImport
	voiceSel    string
	speedSel    string
	hideText    bool
	favorites   []string
	bigScreen   bool
	bigFont     int
	showTrans   bool // the big screen shows the Chinese of the sentence
	dic         dictState
	kokoroSetup setupState
	tr          translator
	tran        transState
	lookup      lookupCard
	ticking     atomic.Bool // a frame is scheduled while the big screen follows the sound
	big         bigCache
	showVoices  bool         // the voice panel replaces the main area
	showHelp    bool         // the shortcut overlay is open
	listenID    atomic.Int64 // material whose sentences the player is playing
	showStats   bool         // the statistics panel replaces the main area
	dailyGoal   int          // minutes of listening aimed for each day; 0 means none

	query       string // search box
	tagFilter   string // tag chosen in the sidebar
	draftTags   string // tags being edited, as typed
	focusSearch bool   // give the search box the focus in the next frame
	theme       *themeSource
	sample      string
	proc        processedCache
	confirmDel  int // material ID awaiting a second click on delete
	lastSaved   time.Time
	savedIdx    int
	win         *mygo.Window
	lastIdx     int // sentence last scrolled to
}

func newModel(out player.Output) *model {
	m := &model{theme: newThemeSource()}
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		m.setupErr = "读取资料库失败：" + err.Error()
	}
	m.st = st
	audioDir := filepath.Join(store.CacheDir(), "audio")
	m.piper = &tts.Piper{Cache: audioDir, Voices: tts.ListVoices(tts.DefaultVoiceDirs...)}
	m.kokoro = &tts.Kokoro{Dir: filepath.Join(filepath.Dir(store.DefaultPath()), "kokoro"), Cache: audioDir}
	m.synth = &tts.Router{Piper: m.piper, Kokoro: m.kokoro}
	m.pool = &tts.Pool{S: m.synth}
	if m.piper.Check() != nil && m.kokoro.Check() != nil && m.setupErr == "" {
		m.setupErr = noTTSErr
	}

	s := st.Settings()
	m.voice, m.speed, m.hideText, m.favorites = s.Voice, s.Speed, s.HideText, s.Favorites
	m.bigScreen, m.bigFont, m.showTrans = s.BigScreen, s.BigFont, s.Translate
	m.dailyGoal = s.DailyGoal
	m.initSetup(s.Mirror)
	m.initDict(filepath.Dir(store.DefaultPath()), filepath.Join(store.CacheDir(), "translate"))
	if m.bigFont < minBigFont || m.bigFont > maxBigFont {
		m.bigFont = defaultBigFont
	}
	m.sample = defaultSample
	m.out = out
	if !m.hasVoice(m.voice) {
		if vs := m.voiceNames(); len(vs) > 0 {
			m.voice = vs[0] // Kokoro first when installed
			for _, v := range vs {
				if v == defaultVoice {
					m.voice = v
					break
				}
			}
		}
	}
	if m.speed == 0 {
		m.speed = 1
	}
	m.voiceSel, m.speedSel = m.voice, speedLabel(m.speed)

	m.pl = player.New(m, out, m.invalidate)
	m.pl.OnHeard = func(d time.Duration) { m.st.AddListen(time.Now(), int(m.listenID.Load()), d) }
	m.refresh()
	if id := s.LastOpened; id != 0 {
		if mat, ok := st.Get(id); ok {
			m.openMaterial(mat)
		}
	}
	return m
}

func speedLabel(v float64) string {
	for i, x := range speedValues {
		if x == v {
			return speedLabels[i]
		}
	}
	return "1.0x"
}

// voiceNames lists the voices that can be used: Kokoro voices when its
// environment is installed, then the piper voices.
func (m *model) voiceNames() []string {
	var out []string
	if m.kokoro.Check() == nil {
		for _, v := range tts.KokoroVoices {
			out = append(out, tts.KokoroPrefix+v.ID)
		}
	}
	for _, v := range m.piper.Voices {
		out = append(out, v.Name)
	}
	return out
}

func (m *model) hasVoice(name string) bool {
	for _, v := range m.voiceNames() {
		if v == name {
			return true
		}
	}
	return false
}

func (m *model) invalidate() {
	if f := m.wake.Load(); f != nil {
		(*f)()
	}
}

func (m *model) refresh() { m.materials = m.st.List() }

// Audio implements player.Source.
func (m *model) Audio(ctx context.Context, i int) (string, error) {
	m.mu.Lock()
	if i < 0 || i >= len(m.sentences) {
		m.mu.Unlock()
		return "", context.Canceled
	}
	s, voice, speed, gen := m.sentences[i], m.voice, m.speed, m.gen
	m.mu.Unlock()
	path, err := m.pool.Get(ctx, s, voice, speed)
	if err == nil {
		m.markReady(gen, i)
	}
	return path, err
}

func (m *model) markReady(gen, i int) {
	m.mu.Lock()
	if gen == m.gen && i < len(m.ready) {
		m.ready[i] = true
	}
	m.mu.Unlock()
	m.ver.Add(1)
	m.invalidate()
}

// openMaterial makes mat the current material, ready to play (paused).
func (m *model) openMaterial(mat store.Material) {
	m.saveProgress(true)
	m.cur, m.open, m.editing = mat, true, false
	m.listenID.Store(int64(mat.ID))
	m.lastIdx = -1
	m.mu.Lock()
	m.sentences = text.Split(mat.Text)
	m.mu.Unlock()
	m.pl.SetCount(len(m.sentences))
	m.pl.SetMode(player.Mode(mat.Mode))
	m.pl.SetRepeat(max(mat.Repeat, 1))
	m.pl.Seek(min(mat.Index, max(len(m.sentences)-1, 0)))
	m.savedIdx = m.pl.State().Index
	m.resynth()
	m.saveSettings()
}

// saveSettings writes the voice, speed, favorites and last opened material.
func (m *model) saveSettings() {
	s := m.st.Settings()
	s.Voice, s.Speed, s.HideText, s.Favorites = m.voice, m.speed, m.hideText, m.favorites
	s.BigScreen, s.BigFont, s.Translate = m.bigScreen, m.bigFont, m.showTrans
	s.DailyGoal = m.dailyGoal
	s.Mirror = m.kokoroSetup.mirror
	if m.open {
		s.LastOpened = m.cur.ID
	}
	m.st.SetSettings(s)
}

// resynth recomputes which sentences are cached and prefetches the rest,
// starting at the sentence being played.
func (m *model) resynth() {
	m.mu.Lock()
	if m.stopFetch != nil {
		m.stopFetch()
	}
	m.gen++
	gen := m.gen
	sents, voice, speed := m.sentences, m.voice, m.speed
	m.ver.Add(1)
	m.ready = make([]bool, len(sents))
	for i, s := range sents {
		m.ready[i] = m.synth.Cached(s, voice, speed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.stopFetch = cancel
	m.mu.Unlock()

	start := m.pl.State().Index
	var next atomic.Int64
	for range 2 { // two piper processes at a time
		go func() {
			for ctx.Err() == nil {
				k := int(next.Add(1)) - 1
				if k >= len(sents) {
					return
				}
				i := (start + k) % len(sents)
				m.mu.Lock()
				done := m.ready[i]
				m.mu.Unlock()
				if done {
					continue
				}
				if _, err := m.pool.Get(ctx, sents[i], voice, speed); err == nil {
					m.markReady(gen, i)
				}
			}
		}()
	}
	m.invalidate()
}

// progress returns how many sentences are synthesized, and the total.
func (m *model) progress() (ready, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.ready {
		if r {
			ready++
		}
	}
	return ready, len(m.ready)
}

func (m *model) sentenceList() ([]string, []bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentences, append([]bool(nil), m.ready...)
}

// applyVoiceSpeed switches voice or speed and starts that audio.
func (m *model) applyVoiceSpeed(voice string, speed float64) {
	m.mu.Lock()
	m.voice, m.speed = voice, speed
	m.mu.Unlock()
	m.resynth()
	m.pl.Restart()
	m.saveSettings()
}

// saveProgress stores the sentence, mode and repeat count of the current
// material; unless force, at most every two seconds.
func (m *model) saveProgress(force bool) {
	if !m.open {
		return
	}
	s := m.pl.State()
	if s.Index == m.savedIdx && s.Repeat == m.cur.Repeat && int(s.Mode) == m.cur.Mode {
		return
	}
	if !force && time.Since(m.lastSaved) < 2*time.Second {
		return
	}
	m.cur.Index, m.cur.Mode, m.cur.Repeat = s.Index, int(s.Mode), s.Repeat
	m.savedIdx, m.lastSaved = s.Index, time.Now()
	m.st.Update(m.cur)
}

// saveDraft stores the edited text as a new or existing material, opens it
// and returns whether there was anything to store.
func (m *model) saveDraft() bool {
	body := strings.TrimSpace(m.draftText)
	if len(text.Split(body)) == 0 {
		return false
	}
	title := strings.TrimSpace(m.draftTitle)
	if title == "" {
		title = firstWords(text.Split(body)[0], 36)
	}
	var mat store.Material
	if old, ok := m.st.Get(m.editID); ok && m.editID != 0 {
		mat = old
		if mat.Text != body {
			mat.Index = 0
		}
		mat.Title, mat.Text = title, body
		mat.Tags = store.NormalizeTags(m.draftTags)
		m.st.Update(mat)
	} else {
		mat = m.st.Add(store.Material{Title: title, Text: body, Tags: store.NormalizeTags(m.draftTags)})
	}
	m.refresh()
	m.open = false // force openMaterial not to save the previous one over this
	m.openMaterial(mat)
	return true
}

// togglePin pins or unpins the open material. It changes m.cur as well,
// which saveProgress writes back whole.
func (m *model) togglePin() {
	if !m.open {
		return
	}
	m.cur.Pinned = !m.cur.Pinned
	m.st.SetPinned(m.cur.ID, m.cur.Pinned)
	m.refresh()
}

func firstWords(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// remove deletes a material, closing it if it is the current one.
func (m *model) remove(id int) {
	if m.open && m.cur.ID == id {
		m.pl.Pause()
		m.pl.SetCount(0)
		m.mu.Lock()
		m.sentences, m.ready = nil, nil
		if m.stopFetch != nil {
			m.stopFetch()
		}
		m.mu.Unlock()
		m.open = false
		m.listenID.Store(0)
	}
	m.st.Delete(id)
	m.refresh()
}
