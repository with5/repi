package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/store"
	"repi/internal/tts"
)

const zhText = "今天天气很好。我们用 iPhone 拍照吧！"

// fakeKokoro lays out a Kokoro environment under the data directory newModel
// will use: an interpreter, and with zh misaki's Chinese dependencies.
func fakeKokoro(t *testing.T, zh bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("venv layout")
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := filepath.Join(data, "repi", "kokoro")
	py := filepath.Join(dir, "venv", "bin", "python")
	os.MkdirAll(filepath.Dir(py), 0o755)
	os.WriteFile(py, nil, 0o755)
	if zh {
		addZhPackages(dir)
	}
	return dir
}

func addZhPackages(dir string) {
	for _, p := range []string{"jieba", "pypinyin"} {
		pkg := filepath.Join(dir, "venv", "lib", "python3.12", "site-packages", p)
		os.MkdirAll(pkg, 0o755)
		os.WriteFile(filepath.Join(pkg, "__init__.py"), nil, 0o644)
	}
}

// zhModel is a model on fake Kokoro whose synthesis always succeeds at once.
func zhModel(t *testing.T, zh bool) (*model, string) {
	dir := fakeKokoro(t, zh)
	m := newModel(&holdOut{total: 3 * time.Second})
	t.Cleanup(m.pl.Close)
	m.pool = &tts.Pool{S: stubSynth{t.TempDir()}}
	return m, dir
}

func open(m *model, title, body string) store.Material {
	mat := m.st.Add(store.Material{Title: title, Text: body})
	m.refresh()
	m.openMaterial(mat)
	return mat
}

// Each language keeps its own voice: Chinese material is read by a Mandarin
// voice, English by an English one, and choosing one leaves the other.
func TestVoicePerLanguage(t *testing.T) {
	m, _ := zhModel(t, true)
	en := open(m, "en", "We went home.")
	if m.voice != defaultVoice || m.zhText {
		t.Fatalf("English material: voice %q zh %v", m.voice, m.zhText)
	}
	zh := open(m, "zh", zhText)
	if m.voice != defaultZhVoice || !m.zhText {
		t.Fatalf("Chinese material: voice %q zh %v", m.voice, m.zhText)
	}
	m.applyVoiceSpeed("kokoro:zm_yunxi", 1)
	if m.zhVoice != "kokoro:zm_yunxi" || m.enVoice != defaultVoice {
		t.Fatalf("slots en %q zh %q", m.enVoice, m.zhVoice)
	}
	if s := m.st.Settings(); s.ZhVoice != "kokoro:zm_yunxi" || s.Voice != defaultVoice {
		t.Fatalf("settings %+v", s)
	}
	m.openMaterial(en)
	if m.voice != defaultVoice {
		t.Fatalf("back to English: voice %q", m.voice)
	}
	m.openMaterial(zh)
	if m.voice != "kokoro:zm_yunxi" {
		t.Fatalf("back to Chinese: voice %q", m.voice)
	}

	// The reader offers only voices in the material's language.
	m.favorites = []string{defaultVoice, "kokoro:zf_xiaoyi", "kokoro:bm_george"}
	if got := m.readingVoices(); !slices.Equal(got, []string{"kokoro:zm_yunxi", "kokoro:zf_xiaoyi"}) {
		t.Errorf("Chinese reader offers %v", got)
	}
	m.openMaterial(en)
	if got := m.readingVoices(); !slices.Equal(got, []string{defaultVoice, "kokoro:bm_george"}) {
		t.Errorf("English reader offers %v", got)
	}
}

// Kokoro installed before Repi read Chinese: Chinese material cannot be read
// by an English voice; the reader offers the add-on, and once it is in, the
// material is read by a Mandarin voice, starred for the reader.
func TestChineseNeedsSetup(t *testing.T) {
	m, dir := zhModel(t, false)
	m.favorites = []string{defaultVoice}
	open(m, "zh", zhText)
	if m.canRead() {
		t.Fatalf("readable by %q", m.voice)
	}
	if _, err := m.Audio(t.Context(), 0); err != errNoZhVoice {
		t.Fatalf("audio err %v", err)
	}
	tt := ui.NewTester(m.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("安装中文朗读") {
		t.Fatalf("no install card: %q", tt.Texts())
	}
	if slices.ContainsFunc(m.voiceList(), func(v voiceInfo) bool { return tts.VoiceLang(v.Name) == "zh" }) {
		t.Error("Mandarin voices listed before they can speak")
	}

	addZhPackages(dir)
	m.zhSetup.done = true
	tt.Frame()
	if !m.zhOK || m.voice != defaultZhVoice || !m.canRead() {
		t.Fatalf("after setup: ok %v voice %q", m.zhOK, m.voice)
	}
	if !slices.Equal(m.favorites, []string{defaultVoice, defaultZhVoice}) {
		t.Errorf("favorites %v", m.favorites)
	}
	if m.enVoice != defaultVoice || m.st.Settings().ZhVoice != defaultZhVoice {
		t.Errorf("en %q, saved zh %q", m.enVoice, m.st.Settings().ZhVoice)
	}
	if _, err := m.Audio(t.Context(), 0); err != nil {
		t.Fatalf("audio after setup: %v", err)
	}
	tt.Frame()
	if tt.HasText("安装中文朗读") {
		t.Error("install card still shown")
	}
}

// The big screen sets Chinese a character at a time; characters are not
// looked up in the English dictionary, and there is no translation switch.
func TestChineseBigScreen(t *testing.T) {
	m, _ := zhModel(t, true)
	open(m, "zh", zhText)
	m.bigScreen = true
	tt := ui.NewTester(m.view, 1100, 620)
	tt.Frame()
	for _, s := range []string{"今", "天", "好。"} {
		if !tt.HasText(s) {
			t.Fatalf("%q not shown: %q", s, tt.Texts())
		}
	}
	if tt.HasText("翻译") {
		t.Error("translation switch shown for Chinese")
	}
	m.pl.Play()
	tt.Frame()
	if err := tt.Click("今"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.pl.State().Playing || m.lookup.open {
		t.Error("a character opened the dictionary")
	}
	m.pl.Next()
	waitText(t, tt, "iPhone")
	if !tt.HasText("吧！") {
		t.Errorf("second sentence: %q", tt.Texts())
	}
}

func TestSampleFor(t *testing.T) {
	cases := []struct{ sample, voice, want string }{
		{defaultSample, defaultZhVoice, defaultZhSample},
		{"你好，世界。", defaultZhVoice, "你好，世界。"},
		{"你好，世界。", defaultVoice, defaultSample},
		{"  ", defaultVoice, defaultSample},
		{"Hi there.", "en_US-amy-medium", "Hi there."},
	}
	for _, c := range cases {
		if got := sampleFor(c.sample, c.voice); got != c.want {
			t.Errorf("sampleFor(%q, %q) = %q", c.sample, c.voice, got)
		}
	}
	if !strings.Contains(defaultZhSample, "。") {
		t.Fatal("sample")
	}
}
