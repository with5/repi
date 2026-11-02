package main

import (
	"context"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/align"
	"repi/internal/dict"
	"repi/internal/player"
	"repi/internal/tts"
)

// Real piper-tts: a sentence becomes a non-empty WAV, then comes from the cache.
func TestPiperSynth(t *testing.T) {
	p := &tts.Piper{Cache: t.TempDir(), Voices: tts.ListVoices(tts.DefaultVoiceDirs...)}
	if err := p.Check(); err != nil {
		t.Skip(err)
	}
	t0 := time.Now()
	path, err := p.Synth(context.Background(), "Hello there, this is a test.", "en_US-amy-medium", 1)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < 10000 {
		t.Fatalf("wav %v size %v", err, fi)
	}
	t.Logf("synth took %v, %d bytes", time.Since(t0), fi.Size())
	if !p.Cached("Hello there, this is a test.", "en_US-amy-medium", 1) {
		t.Error("not cached")
	}
}

// silentOut plays nothing but ends sounds at once.
type silentOut struct{}
type endedHandle struct{ c chan struct{} }

func (endedHandle) Stop()                   {}
func (endedHandle) Position() time.Duration { return 0 }
func (endedHandle) Duration() time.Duration { return 0 }
func (h endedHandle) Done() <-chan struct{} { return h.c }
func (silentOut) Play(string) (player.Handle, error) {
	c := make(chan struct{})
	close(c)
	return endedHandle{c}, nil
}

// The window: type a text, save it, see its sentences, play and pause.
func TestViewFlow(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	if m.setupErr != "" {
		t.Skip(m.setupErr)
	}
	defer m.pl.Close()
	tt := ui.NewTester(m.view, 1000, 720)
	if err := tt.Click("新建资料"); err != nil {
		t.Fatal(err)
	}
	m.draftText = "The cat sat. It was happy! Was it?"
	if err := tt.Click("保存并打开"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"The cat sat.", "It was happy!", "Was it?"} {
		if !tt.HasText(s) {
			t.Errorf("missing sentence %q in %q", s, tt.Texts())
		}
	}
	if len(m.materials) != 1 {
		t.Fatalf("materials = %d", len(m.materials))
	}
	if err := tt.Click("上一句"); err != nil {
		t.Log(err)
	}
	if !m.pl.State().Playing {
		t.Error("save-and-open did not start playing")
	}
	m.pl.Pause()
}

// With REPI_AUDIO=1 a sentence is played through the real sound card, and
// the end of the sound is seen after about its length.
func TestRealAudio(t *testing.T) {
	if os.Getenv("REPI_AUDIO") == "" {
		t.Skip("set REPI_AUDIO=1 to play through the sound card")
	}
	p := &tts.Piper{Cache: t.TempDir(), Voices: tts.ListVoices(tts.DefaultVoiceDirs...)}
	path, err := p.Synth(context.Background(), "Testing one, two, three.", "en_US-amy-medium", 1)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	h, err := (player.SoundCard{}).Play(path)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
		t.Logf("sound lasted %v", time.Since(t0))
		if d := time.Since(t0); d < 500*time.Millisecond || d > 6*time.Second {
			t.Errorf("odd duration %v", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sound never ended")
	}
	h.Stop()
}

// With REPI_KOKORO=1: the Kokoro worker starts once, serves several
// sentences, and answers repeats from the cache.
func TestKokoroSynth(t *testing.T) {
	if os.Getenv("REPI_KOKORO") == "" {
		t.Skip("set REPI_KOKORO=1 to run Kokoro")
	}
	home, _ := os.UserHomeDir()
	k := &tts.Kokoro{Dir: home + "/.local/share/repi/kokoro", Cache: t.TempDir()}
	if err := k.Check(); err != nil {
		t.Skip(err)
	}
	defer k.Close()
	for i, s := range []string{"A pod is the smallest deployable unit.", "Why do we need orchestration?"} {
		t0 := time.Now()
		path, err := k.Synth(context.Background(), s, "kokoro:af_heart", 1)
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil || fi.Size() < 10000 {
			t.Fatalf("wav %v %v", err, fi)
		}
		t.Logf("sentence %d: %v, %d bytes", i, time.Since(t0), fi.Size())
	}
	if _, err := k.Synth(context.Background(), "Mind the gap.", "kokoro:bm_george", 1.2); err != nil {
		t.Fatalf("british voice: %v", err)
	}
	t0 := time.Now()
	if _, err := k.Synth(context.Background(), "Why do we need orchestration?", "kokoro:af_heart", 1); err != nil || time.Since(t0) > 50*time.Millisecond {
		t.Errorf("repeat not served from cache: %v %v", err, time.Since(t0))
	}
}

// The voice panel: star two voices, audition one, and see that the reader
// offers only the starred voices (plus the one in use).
func TestVoicePanel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	if m.setupErr != "" {
		t.Skip(m.setupErr)
	}
	defer m.pl.Close()
	tt := ui.NewTester(m.view, 1000, 4000) // tall: all 37 voices in view
	if err := tt.Click("音色管理"); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"en_US-ryan-high", "en_GB-alan-medium"} {
		if err := tt.Click("收藏 " + v); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.favorites) != 2 {
		t.Fatalf("favorites = %v", m.favorites)
	}
	if err := tt.Click("收藏 en_GB-alan-medium"); err != nil { // unstar
		t.Fatal(err)
	}
	if len(m.favorites) != 1 || m.favorites[0] != "en_US-ryan-high" {
		t.Fatalf("favorites after unstar = %v", m.favorites)
	}
	if got := m.st.Settings().Favorites; len(got) != 1 {
		t.Errorf("favorites not saved: %v", got)
	}

	// audition: synthesizing then playing then back to idle, with no error
	if err := tt.Click("试听 en_US-ryan-high"); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if v, _, _ := m.auditionState(); v == "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if v, _, e := m.auditionState(); v != "" || e != "" {
		t.Errorf("audition did not finish cleanly: voice %q err %q", v, e)
	}
	if !m.piper.Cached(m.sample, "en_US-ryan-high", 1) {
		t.Error("sample was not synthesized")
	}

	rv := m.readingVoices()
	want := map[string]bool{"en_US-ryan-high": true, m.voice: true}
	for _, v := range rv {
		if !want[v] {
			t.Errorf("reader offers %q, which is neither starred nor in use", v)
		}
	}
	if len(rv) < 1 {
		t.Error("reader offers no voice")
	}
}

// The panel says how much of the open material a voice has synthesized:
// none at first, all once the prefetcher finished, and other voices stay at 0.
func TestProcessedCounts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	if m.setupErr != "" {
		t.Skip(m.setupErr)
	}
	defer m.pl.Close()
	m.draftText = "One small step. Two small steps. Three."
	if !m.saveDraft() {
		t.Fatal("not saved")
	}
	voice := m.voice
	for range 200 {
		if r, n := m.progress(); r == n && n == 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if done, total := m.processed(voice); done != 3 || total != 3 {
		t.Errorf("processed(%s) = %d/%d, want 3/3", voice, done, total)
	}
	other := "en_GB-alan-low"
	if other == voice {
		other = "en_US-amy-low"
	}
	if done, total := m.processed(other); done != 0 || total != 3 {
		t.Errorf("processed(%s) = %d/%d, want 0/3", other, done, total)
	}
}

// holdOut "plays" a sound that never ends, at a position the test sets.
type holdOut struct {
	pos   atomic.Int64 // nanoseconds
	total time.Duration
}
type holdHandle struct{ o *holdOut }

func (h holdHandle) Stop()                   {}
func (h holdHandle) Done() <-chan struct{}   { return nil }
func (h holdHandle) Position() time.Duration { return time.Duration(h.o.pos.Load()) }
func (h holdHandle) Duration() time.Duration { return h.o.total }
func (o *holdOut) Play(string) (player.Handle, error) {
	return holdHandle{o}, nil
}

// The highlighted word follows the position of the sound, from the first word
// to the last, using the speech found in the real audio.
func TestWordHighlight(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	out := &holdOut{total: 3 * time.Second}
	m := newModel(out)
	if m.setupErr != "" {
		t.Skip(m.setupErr)
	}
	defer m.pl.Close()
	if m.voice != "en_US-amy-medium" && !hasPrefix(m.voice, "en_") {
		t.Skipf("needs a piper voice, have %s", m.voice)
	}
	m.draftText = "Kubernetes groups containers into logical units."
	if !m.saveDraft() {
		t.Fatal("not saved")
	}
	m.pl.Play()
	var now player.Now
	for range 200 {
		var ok bool
		if now, ok = m.pl.Now(); ok {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if now.Path == "" {
		t.Fatal("nothing is playing")
	}
	sp, err := align.SpeechOf(now.Path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("speech %v..%v of %v", sp.Start, sp.End, sp.Total)
	const sentence = "Kubernetes groups containers into logical units."
	tokens := align.Tokens(sentence)
	at := func(frac float64) int {
		out.pos.Store(int64(sp.Start + time.Duration(frac*float64(sp.End-sp.Start))))
		a, _, _ := m.wordState(0, sentence)
		return a
	}
	prev := -1
	for _, f := range []float64{0.01, 0.2, 0.4, 0.6, 0.8, 0.99} {
		got := at(f)
		t.Logf("at %.0f%% of the speech: word %d (%s)", f*100, got, tokens[got].Text)
		if got < prev {
			t.Errorf("highlight went back: %d after %d", got, prev)
		}
		prev = got
	}
	if at(0.01) != 0 || at(0.99) != len(tokens)-1 {
		t.Errorf("first/last: %d, %d", at(0.01), at(0.99))
	}
	if a, _, _ := m.wordState(3, sentence); a != -1 {
		t.Error("a sentence that is not playing has a highlighted word")
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// highlightLeft returns the left edge of the highlight block in the frame, by
// its color (the accent laid over the background), looking only at the area
// of the sentence.
func highlightLeft(img *image.RGBA, th *ui.Theme) int {
	want := blend(th.Background, th.Accent, 0.30)
	near := func(a, b uint8) bool { return int(a)-int(b) <= 6 && int(b)-int(a) <= 6 }
	b := img.Bounds()
	for x := 260; x < b.Dx(); x++ {
		for y := 110; y < 430 && y < b.Dy(); y++ {
			c := img.RGBAAt(x, y)
			if near(c.R, want.R) && near(c.G, want.G) && near(c.B, want.B) {
				return x
			}
		}
	}
	return -1
}

// The highlighted word follows the sound: moving the position of the sound to
// the next word moves the highlight there in the very next frame, and when
// the sound is where it was, no frame is asked for.
func TestHighlightMoves(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	out := &holdOut{total: 4 * time.Second}
	m := newModel(out)
	if m.setupErr != "" || !hasPrefix(m.voice, "en_") {
		t.Skip("needs piper")
	}
	defer m.pl.Close()
	const sentence = "Kubernetes groups containers into logical units."
	m.draftText = sentence
	m.saveDraft()
	m.bigScreen = true
	m.pl.Play()
	var now player.Now
	for range 200 {
		var ok bool
		if now, ok = m.pl.Now(); ok {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if now.Path == "" {
		t.Fatal("nothing is playing")
	}
	out.total = now.Total
	sp, err := align.SpeechOf(now.Path)
	if err != nil {
		t.Fatal(err)
	}
	ends := align.Align(align.Tokens(sentence), sp)
	mid := func(i int) int64 {
		start := sp.Start
		if i > 0 {
			start = ends[i-1]
		}
		return int64((start + ends[i]) / 2)
	}

	tt := ui.NewTester(m.view, 1100, 620)
	out.pos.Store(mid(0))
	tt.Frame()
	first := highlightLeft(tt.Image(), m.theme.Theme())
	out.pos.Store(mid(1))
	tt.Frame()
	second := highlightLeft(tt.Image(), m.theme.Theme())
	t.Logf("highlight left edge: word 0 at %d, word 1 at %d", first, second)
	if first < 0 || second <= first+20 {
		t.Errorf("the highlight did not move to the next word: %d -> %d", first, second)
	}
	out.pos.Store(mid(1)) // the sound has not moved: the highlight stays
	tt.Frame()
	if again := highlightLeft(tt.Image(), m.theme.Theme()); again != second {
		t.Errorf("the highlight moved without the sound: %d -> %d", second, again)
	}
}

// followSound wants one frame for the next word, not one every few ms.
func TestFollowSoundIsSparse(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	defer m.pl.Close()
	var frames atomic.Int32
	f := func() { frames.Add(1) }
	m.wake.Store(&f)
	for range 50 { // asked many times in a burst: one timer
		m.followSound(100 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	if n := frames.Load(); n != 0 {
		t.Errorf("frame came after %d ms, before the word was due", 60)
	}
	time.Sleep(120 * time.Millisecond)
	if n := frames.Load(); n != 1 {
		t.Errorf("%d frames for 50 requests, want 1", n)
	}
}

// noSynth fails at once, so a model under test starts no real synthesis that
// would still be writing into the cache when the test's directory is removed.
type noSynth struct{}

func (noSynth) Synth(context.Context, string, string, float64) (string, error) {
	return "", errors.New("offline")
}

// newOfflineModel is a model on empty data and cache directories, that plays
// nothing and synthesizes nothing.
func newOfflineModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	m.pool = &tts.Pool{S: noSynth{}}
	t.Cleanup(m.pl.Close)
	return m
}

// Pinning changes the material the player will save next, so playback
// progress does not write the old, unpinned copy over it.
func TestPinSurvivesProgressSave(t *testing.T) {
	m := newOfflineModel(t)
	m.draftText = "One. Two. Three."
	m.saveDraft()
	m.togglePin()
	m.pl.Seek(2)
	m.saveProgress(true)
	got, _ := m.st.Get(m.cur.ID)
	if !got.Pinned || got.Index != 2 {
		t.Errorf("stored = %+v", got)
	}
}

func TestSaveDraftStoresTags(t *testing.T) {
	m := newOfflineModel(t)
	m.draftText, m.draftTags = "One.", "#work, study"
	m.saveDraft()
	if got := m.cur.Tags; len(got) != 2 || got[0] != "work" || got[1] != "study" {
		t.Errorf("tags = %v", got)
	}
}

func addMaterial(m *model, title, body, tags string) {
	m.draftTitle, m.draftText, m.draftTags = title, body, tags
	m.saveDraft()
}

func TestSidebarSearchAndTags(t *testing.T) {
	m := newOfflineModel(t)
	addMaterial(m, "Pods", "A pod runs containers.", "work")
	addMaterial(m, "Models", "They build models.", "study")
	m.open = false // the reader's title would show the open one too
	tt := ui.NewTester(m.view, 1000, 640)
	tt.Frame()
	if !tt.HasText("Pods") || !tt.HasText("Models") {
		t.Fatalf("both should show: %v", tt.Texts())
	}

	m.query = "containers"
	tt.Frame()
	if !tt.HasText("Pods") || tt.HasText("Models") {
		t.Errorf("search: %v", tt.Texts())
	}

	m.query, m.tagFilter = "", "study"
	tt.Frame()
	if tt.HasText("Pods") || !tt.HasText("Models") {
		t.Errorf("tag filter: %v", tt.Texts())
	}

	m.query, m.tagFilter = "zzz", ""
	tt.Frame()
	if !tt.HasText("没有匹配的资料") {
		t.Errorf("empty state: %v", tt.Texts())
	}
}

func TestPinShortcutAndTyping(t *testing.T) {
	m := newOfflineModel(t)
	addMaterial(m, "Pods", "A pod runs containers.", "")
	tt := ui.NewTester(m.view, 1000, 640)
	tt.Frame()
	tt.Key(0, ui.KeyP)
	tt.Frame()
	if !m.cur.Pinned {
		t.Error("P did not pin")
	}
	// Typing the same letters into the search box must not trigger them.
	tt.Key(0, ui.KeySlash)
	tt.Frame()
	tt.Type("pq")
	tt.Frame()
	if m.query != "pq" || !m.cur.Pinned {
		t.Errorf("query %q pinned %v", m.query, m.cur.Pinned)
	}
}

// fakeTranslator answers from a map and counts the web calls.
type fakeTranslator struct {
	mu    sync.Mutex
	cache map[string]string
	calls int
	fail  bool
}

func (f *fakeTranslator) Cached(s string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.cache[s]
	return t, ok
}

func (f *fakeTranslator) Translate(_ context.Context, s string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return "", errors.New("翻译失败：额度用完")
	}
	return "译：" + s, nil
}

const dictSample = `word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio
go,gəu,,"vi. 走, 去\nn. 进行",,,,gk,1,1,p:went/d:gone,,
went,went,,go的过去式,,,,,0,0,0:go/1:p,,
`

// stubSynth "synthesizes" every sentence at once, into an empty file.
type stubSynth struct{ dir string }

func (s stubSynth) Synth(_ context.Context, sentence, _ string, _ float64) (string, error) {
	p := filepath.Join(s.dir, "s.wav")
	return p, os.WriteFile(p, []byte("RIFF"), 0o644)
}

// bigModel is a model on empty data and cache directories, that never
// finishes playing a sentence, showing "We went home." on the big screen.
func bigModel(t *testing.T) (*model, *ui.Tester) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(&holdOut{total: 3 * time.Second})
	t.Cleanup(m.pl.Close)
	m.pool = &tts.Pool{S: stubSynth{t.TempDir()}}
	m.draftText = "We went home."
	m.saveDraft()
	m.bigScreen = true
	return m, ui.NewTester(m.view, 1100, 620)
}

// waitText runs frames until the text shows up.
func waitText(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	for range 100 {
		tt.Frame()
		if tt.HasText(s) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%q never shown; screen: %q", s, tt.Texts())
}

// Clicking a word on the big screen pauses the reading and shows its card;
// an inflection shows its base form; Esc closes the card and goes on reading.
func TestLookupCard(t *testing.T) {
	m, tt := bigModel(t)
	if _, err := dict.Build(strings.NewReader(dictSample), m.dic.path); err != nil {
		t.Fatal(err)
	}
	m.dic.reopen()
	m.pl.Play()
	tt.Frame()
	if !m.pl.State().Playing {
		t.Fatal("not playing before the click")
	}
	if err := tt.Click("went"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "went 是 go 的过去式")
	for _, want := range []string{"vi. 走, 去", "高考"} {
		if !tt.HasText(want) {
			t.Errorf("card lacks %q: %q", want, tt.Texts())
		}
	}
	if m.pl.State().Playing {
		t.Error("reading goes on behind the card")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("went 是 go 的过去式") {
		t.Error("card still open after Esc")
	}
	if !m.pl.State().Playing {
		t.Error("reading did not go on after the card closed")
	}
}

// A card opened while paused leaves the reading paused; a word the
// dictionary lacks says so.
func TestLookupCardWhenPausedAndUnknown(t *testing.T) {
	m, tt := bigModel(t)
	if _, err := dict.Build(strings.NewReader(dictSample), m.dic.path); err != nil {
		t.Fatal(err)
	}
	m.dic.reopen()
	tt.Frame()
	if err := tt.Click("home."); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "词典里没有这个词。")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.pl.State().Playing {
		t.Error("closing the card started a reading that was paused")
	}
}

// Without the dictionary the card offers the download; once it is done the
// word is looked up.
func TestLookupDownloadsDictionary(t *testing.T) {
	m, tt := bigModel(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(dictSample)) }))
	defer srv.Close()
	m.dic.url = srv.URL
	tt.Frame()
	if err := tt.Click("went"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "下载词典")
	if err := tt.Click("下载词典"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "went 是 go 的过去式")
}

// The translation switch shows the Chinese under the sentence, asking once
// per sentence; a failure says why and can be retried.
func TestTranslationSwitch(t *testing.T) {
	m, tt := bigModel(t)
	f := &fakeTranslator{}
	m.tr = f
	tt.Frame()
	if tt.HasText("译：We went home.") {
		t.Fatal("translation shown while the switch is off")
	}
	tt.Key(0, ui.KeyT)
	waitText(t, tt, "译：We went home.")
	tt.Frame()
	tt.Frame()
	if f.calls != 1 {
		t.Errorf("web calls = %d, want 1", f.calls)
	}
	if !m.st.Settings().Translate {
		t.Error("the switch was not saved")
	}
	tt.Key(0, ui.KeyT)
	tt.Frame()
	if tt.HasText("译：We went home.") || m.st.Settings().Translate {
		t.Error("switch off did not hide the translation")
	}

	m2, tt2 := bigModel(t)
	m2.tr = &fakeTranslator{fail: true}
	m2.setTranslate(true)
	waitText(t, tt2, "翻译失败：额度用完")
	m2.tr.(*fakeTranslator).fail = false
	if err := tt2.Click("重试"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt2, "译：We went home.")
}
