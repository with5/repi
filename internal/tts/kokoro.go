package tts

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed kokoro_worker.py
var kokoroWorker []byte

// KokoroPrefix marks a voice name as a Kokoro voice ("kokoro:af_heart").
const KokoroPrefix = "kokoro:"

// KokoroVoiceInfo describes a Kokoro-82M English voice.
type KokoroVoiceInfo struct {
	ID     string // "af_heart"
	Label  string // "Heart"
	Detail string // "美音 · 女声"
}

// KokoroVoices are the English voices of Kokoro-82M, best-rated first. The
// first letter of the id is the accent (a American, b British), the second the
// gender.
var KokoroVoices = []KokoroVoiceInfo{
	{"af_heart", "Heart", "美音 · 女声"},
	{"af_bella", "Bella", "美音 · 女声"},
	{"af_nicole", "Nicole", "美音 · 女声"},
	{"af_aoede", "Aoede", "美音 · 女声"},
	{"af_kore", "Kore", "美音 · 女声"},
	{"af_sarah", "Sarah", "美音 · 女声"},
	{"af_nova", "Nova", "美音 · 女声"},
	{"af_sky", "Sky", "美音 · 女声"},
	{"af_alloy", "Alloy", "美音 · 女声"},
	{"af_jessica", "Jessica", "美音 · 女声"},
	{"af_river", "River", "美音 · 女声"},
	{"am_michael", "Michael", "美音 · 男声"},
	{"am_fenrir", "Fenrir", "美音 · 男声"},
	{"am_puck", "Puck", "美音 · 男声"},
	{"am_adam", "Adam", "美音 · 男声"},
	{"am_echo", "Echo", "美音 · 男声"},
	{"am_eric", "Eric", "美音 · 男声"},
	{"am_liam", "Liam", "美音 · 男声"},
	{"am_onyx", "Onyx", "美音 · 男声"},
	{"bf_emma", "Emma", "英音 · 女声"},
	{"bf_isabella", "Isabella", "英音 · 女声"},
	{"bf_alice", "Alice", "英音 · 女声"},
	{"bf_lily", "Lily", "英音 · 女声"},
	{"bm_george", "George", "英音 · 男声"},
	{"bm_fable", "Fable", "英音 · 男声"},
	{"bm_lewis", "Lewis", "英音 · 男声"},
	{"bm_daniel", "Daniel", "英音 · 男声"},
}

// KokoroZhVoices are the Mandarin voices of Kokoro-82M (z for Chinese, then
// the gender). They need misaki's Chinese support, see Kokoro.ZhReady.
var KokoroZhVoices = []KokoroVoiceInfo{
	{"zf_xiaoxiao", "Xiaoxiao", "普通话 · 女声"},
	{"zf_xiaobei", "Xiaobei", "普通话 · 女声"},
	{"zf_xiaoni", "Xiaoni", "普通话 · 女声"},
	{"zf_xiaoyi", "Xiaoyi", "普通话 · 女声"},
	{"zm_yunxi", "Yunxi", "普通话 · 男声"},
	{"zm_yunjian", "Yunjian", "普通话 · 男声"},
	{"zm_yunxia", "Yunxia", "普通话 · 男声"},
	{"zm_yunyang", "Yunyang", "普通话 · 男声"},
}

// VoiceLang is the language a voice speaks: "zh" for Kokoro's Mandarin
// voices and piper's zh_* voices, "en" for all others.
func VoiceLang(voice string) string {
	if strings.HasPrefix(voice, KokoroPrefix+"z") || strings.HasPrefix(voice, "zh_") {
		return "zh"
	}
	return "en"
}

// Kokoro runs Kokoro-82M in one long-lived Python process, so the model is
// loaded once. Requests are served one at a time.
type Kokoro struct {
	Dir   string // holds venv/ and worker.py, ~/.local/share/repi/kokoro
	Cache string // directory of cached WAV files

	w worker
}

// Python is the interpreter of the worker's virtual environment.
func (k *Kokoro) Python() string { return venvPython(filepath.Join(k.Dir, "venv")) }

// Check reports why Kokoro cannot be used, or nil.
func (k *Kokoro) Check() error {
	if _, err := os.Stat(k.Python()); err != nil {
		return fmt.Errorf("未安装 Kokoro 环境（%s）", k.Dir)
	}
	return nil
}

// KokoroModelFiles are the files of the Kokoro-82M repository Repi uses,
// relative to it: the config, the weights and one style file per English
// voice. LocalModel needs all of them; the Mandarin voices come apart, in
// KokoroZhFiles, as installs from before they existed lack them.
func KokoroModelFiles() []string {
	files := []string{"config.json", "kokoro-v1_0.pth"}
	for _, v := range KokoroVoices {
		files = append(files, "voices/"+v.ID+".pt")
	}
	return files
}

// KokoroZhFiles are the style files of the Mandarin voices.
func KokoroZhFiles() []string {
	var files []string
	for _, v := range KokoroZhVoices {
		files = append(files, "voices/"+v.ID+".pt")
	}
	return files
}

// ZhReady reports whether Kokoro can speak Chinese: misaki's Chinese
// dependencies are installed and, with a local model, the Mandarin voices
// are there too (without one the worker fetches them from Hugging Face).
func (k *Kokoro) ZhReady() bool {
	if k.Check() != nil || !hasPackage(filepath.Join(k.Dir, "venv"), "jieba") || !hasPackage(filepath.Join(k.Dir, "venv"), "pypinyin") {
		return false
	}
	if !k.LocalModel() {
		return true
	}
	for _, f := range KokoroZhFiles() {
		if _, err := os.Stat(filepath.Join(k.ModelDir(), filepath.FromSlash(f))); err != nil {
			return false
		}
	}
	return true
}

// hasPackage reports whether the Python package name is installed in venv.
func hasPackage(venv, name string) bool {
	m, _ := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages", name, "__init__.py"))
	if len(m) == 0 { // Windows
		m, _ = filepath.Glob(filepath.Join(venv, "Lib", "site-packages", name, "__init__.py"))
	}
	return len(m) > 0
}

// ModelDir is where the one-click setup puts the model files.
func (k *Kokoro) ModelDir() string { return filepath.Join(k.Dir, "model") }

// LocalModel reports whether every model file is in ModelDir.
func (k *Kokoro) LocalModel() bool {
	for _, f := range KokoroModelFiles() {
		if _, err := os.Stat(filepath.Join(k.ModelDir(), filepath.FromSlash(f))); err != nil {
			return false
		}
	}
	return true
}

// workerEnv is the environment of the worker: with a complete local model it
// loads the files from there and never goes online.
func (k *Kokoro) workerEnv() []string {
	env := append(os.Environ(), "TOKENIZERS_PARALLELISM=false", "PYTHONWARNINGS=ignore")
	if k.LocalModel() {
		env = append(env, "REPI_KOKORO_MODEL="+k.ModelDir(), "HF_HUB_OFFLINE=1")
	}
	return env
}

// Status is a short description of what the worker is doing, for the UI.
func (k *Kokoro) Status() string { return k.w.Status() }

// Close stops the worker.
func (k *Kokoro) Close() { k.w.Close() }

func (k *Kokoro) spec() workerSpec {
	return workerSpec{Name: "Kokoro", Python: k.Python(), Script: filepath.Join(k.Dir, "worker.py"), Code: kokoroWorker,
		Env: k.workerEnv(), Loading: "正在加载 Kokoro 模型（首次使用会下载约 330MB）…"}
}

// CachePath is where the audio of a sentence by a voice is cached.
func (k *Kokoro) CachePath(sentence, voice string, speed float64) string {
	return filepath.Join(k.Cache, CacheKey(sentence, voice, speed)+".wav")
}

// Cached reports whether the sentence is already synthesized.
func (k *Kokoro) Cached(sentence, voice string, speed float64) bool {
	_, err := os.Stat(k.CachePath(sentence, voice, speed))
	return err == nil
}

// Synth implements Synth. voice is "kokoro:<id>"; speed is Kokoro's own
// speed parameter, 1 normal.
func (k *Kokoro) Synth(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	out := k.CachePath(sentence, voice, speed)
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	if speed <= 0 {
		speed = 1
	}
	if err := os.MkdirAll(k.Cache, 0o755); err != nil {
		return "", err
	}
	if err := k.Check(); err != nil {
		return "", err
	}
	req := map[string]any{"text": sentence, "voice": strings.TrimPrefix(voice, KokoroPrefix), "speed": speed, "out": out}
	if err := k.w.call(ctx, k.spec, req); err != nil {
		return "", err
	}
	return out, nil
}

// Router sends "kokoro:" voices to Kokoro, "clone:" voices to Clone and
// all others to Piper.
type Router struct {
	Piper  *Piper
	Kokoro *Kokoro
	Clone  *Clone
}

// Synth implements Synth.
func (r *Router) Synth(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	switch {
	case strings.HasPrefix(voice, KokoroPrefix):
		return r.Kokoro.Synth(ctx, sentence, voice, speed)
	case strings.HasPrefix(voice, ClonePrefix) && r.Clone != nil:
		return r.Clone.Synth(ctx, sentence, voice, speed)
	}
	return r.Piper.Synth(ctx, sentence, voice, speed)
}

// Cached reports whether the sentence is already synthesized by voice.
func (r *Router) Cached(sentence, voice string, speed float64) bool {
	switch {
	case strings.HasPrefix(voice, KokoroPrefix):
		return r.Kokoro.Cached(sentence, voice, speed)
	case strings.HasPrefix(voice, ClonePrefix) && r.Clone != nil:
		return r.Clone.Cached(sentence, voice, speed)
	}
	return r.Piper.Cached(sentence, voice, speed)
}
