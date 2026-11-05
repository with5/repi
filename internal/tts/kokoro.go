package tts

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

// Kokoro runs Kokoro-82M in one long-lived Python process, so the model is
// loaded once. Requests are served one at a time.
type Kokoro struct {
	Dir   string // holds venv/ and worker.py, ~/.local/share/repi/kokoro
	Cache string // directory of cached WAV files

	mu     sync.Mutex // serializes requests and guards the fields below
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	nextID int

	smu    sync.Mutex // guards status, which the UI reads while mu is held
	status string
}

// Python is the interpreter of the worker's virtual environment.
func (k *Kokoro) Python() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(k.Dir, "venv", "Scripts", "python.exe")
	}
	return filepath.Join(k.Dir, "venv", "bin", "python")
}

// Check reports why Kokoro cannot be used, or nil.
func (k *Kokoro) Check() error {
	if _, err := os.Stat(k.Python()); err != nil {
		return fmt.Errorf("未安装 Kokoro 环境（%s）", k.Dir)
	}
	return nil
}

// Status is a short description of what the worker is doing, for the UI.
func (k *Kokoro) Status() string {
	k.smu.Lock()
	defer k.smu.Unlock()
	return k.status
}

func (k *Kokoro) setStatus(s string) {
	k.smu.Lock()
	k.status = s
	k.smu.Unlock()
}

// start launches the worker and waits for its ready line. k.mu is held.
func (k *Kokoro) start() error {
	if err := k.Check(); err != nil {
		return err
	}
	script := filepath.Join(k.Dir, "worker.py")
	if err := os.WriteFile(script, kokoroWorker, 0o644); err != nil {
		return err
	}
	cmd := exec.Command(k.Python(), "-u", script)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "TOKENIZERS_PARALLELISM=false", "PYTHONWARNINGS=ignore")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	k.setStatus("正在加载 Kokoro 模型（首次使用会下载约 330MB）…")
	var loading atomic.Bool
	loading.Store(true)
	go func() { // loading progress: show the last line, until ready
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); l != "" && loading.Load() {
				k.setStatus("Kokoro: " + tail(l, 80))
			}
		}
	}()
	rd := bufio.NewReaderSize(out, 1<<20)
	for { // skip anything a library printed before the ready line
		line, err := rd.ReadString('\n')
		if err != nil {
			cmd.Wait()
			return fmt.Errorf("Kokoro 启动失败：%s", k.Status())
		}
		var ready struct {
			Ready bool `json:"ready"`
		}
		if json.Unmarshal([]byte(line), &ready) == nil && ready.Ready {
			loading.Store(false)
			break
		}
	}
	k.cmd, k.in, k.out = cmd, in, rd
	k.setStatus("")
	return nil
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

// Close stops the worker.
func (k *Kokoro) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopLocked()
}

func (k *Kokoro) stopLocked() {
	if k.cmd != nil {
		k.in.Close()
		k.cmd.Process.Kill()
		k.cmd.Wait()
		k.cmd, k.in, k.out = nil, nil, nil
	}
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
	k.mu.Lock()
	defer k.mu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if k.cmd == nil {
		if err := k.start(); err != nil {
			return "", err
		}
	}
	k.nextID++
	req, _ := json.Marshal(map[string]any{"id": k.nextID, "text": sentence, "voice": strings.TrimPrefix(voice, KokoroPrefix), "speed": speed, "out": out})
	if _, err := k.in.Write(append(req, '\n')); err != nil {
		k.stopLocked()
		return "", err
	}
	line, err := k.out.ReadString('\n')
	if err != nil {
		k.stopLocked()
		return "", errors.New("Kokoro 进程已退出：" + k.Status())
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		k.stopLocked()
		return "", err
	}
	if !resp.OK {
		return "", errors.New("Kokoro: " + resp.Error)
	}
	return out, nil
}

// Router sends "kokoro:" voices to Kokoro and all others to Piper.
type Router struct {
	Piper  *Piper
	Kokoro *Kokoro
}

// Synth implements Synth.
func (r *Router) Synth(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	if strings.HasPrefix(voice, KokoroPrefix) {
		return r.Kokoro.Synth(ctx, sentence, voice, speed)
	}
	return r.Piper.Synth(ctx, sentence, voice, speed)
}

// Cached reports whether the sentence is already synthesized by voice.
func (r *Router) Cached(sentence, voice string, speed float64) bool {
	if strings.HasPrefix(voice, KokoroPrefix) {
		return r.Kokoro.Cached(sentence, voice, speed)
	}
	return r.Piper.Cached(sentence, voice, speed)
}
