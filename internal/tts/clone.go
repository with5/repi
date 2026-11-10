package tts

import (
	"bufio"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed clone_worker.py
var cloneWorker []byte

//go:embed clone_tool.py
var cloneTool []byte

// ClonePrefix marks a voice name as a cloned voice ("clone:3f9a0c1d").
const ClonePrefix = "clone:"

// CloneModelFiles are the files of the Chatterbox repository the worker loads.
var CloneModelFiles = []string{"ve.safetensors", "t3_cfg.safetensors", "s3gen.safetensors", "tokenizer.json", "conds.pt"}

// CloneVoice is a voice cloned from a recording of the user.
type CloneVoice struct {
	ID      string    `json:"id"`   // file stem of the reference recording
	Name    string    `json:"name"` // what the user called it
	Seconds float64   `json:"seconds"`
	Created time.Time `json:"created"`
}

// Clone speaks with voices cloned from short recordings, using Chatterbox in
// one long-lived Python process. The reference recordings and their index
// live in Voices.
type Clone struct {
	Dir    string // holds venv/, model/ and the scripts, ~/.local/share/repi/clone
	Voices string // reference recordings and voices.json, ~/.local/share/repi/voices
	Cache  string // directory of cached WAV files

	w      worker
	vmu    sync.Mutex
	list   []CloneVoice
	loaded bool
}

// Python is the interpreter of the engine's virtual environment.
func (c *Clone) Python() string { return venvPython(filepath.Join(c.Dir, "venv")) }

// ModelDir holds the Chatterbox model files.
func (c *Clone) ModelDir() string { return filepath.Join(c.Dir, "model") }

// Check reports why cloned voices cannot be used, or nil.
func (c *Clone) Check() error {
	if _, err := os.Stat(c.Python()); err != nil {
		return fmt.Errorf("未安装声音克隆环境（%s）", c.Dir)
	}
	for _, f := range CloneModelFiles {
		if _, err := os.Stat(filepath.Join(c.ModelDir(), f)); err != nil {
			return fmt.Errorf("声音克隆模型不完整，缺少 %s", f)
		}
	}
	return nil
}

func (c *Clone) env() []string {
	return append(os.Environ(), "TOKENIZERS_PARALLELISM=false", "PYTHONWARNINGS=ignore",
		"HF_HUB_OFFLINE=1", "REPI_CLONE_MODEL="+c.ModelDir())
}

// Status is a short description of what the worker is doing, for the UI.
func (c *Clone) Status() string { return c.w.Status() }

// Close stops the worker.
func (c *Clone) Close() { c.w.Close() }

func (c *Clone) spec() workerSpec {
	return workerSpec{Name: "声音克隆", Python: c.Python(), Script: filepath.Join(c.Dir, "worker.py"), Code: cloneWorker,
		Env: c.env(), Loading: "正在加载声音克隆模型（约 10–20 秒）…"}
}

// CachePath is where the audio of a sentence by a voice is cached.
func (c *Clone) CachePath(sentence, voice string, speed float64) string {
	return filepath.Join(c.Cache, CacheKey(sentence, voice, speed)+".wav")
}

// Cached reports whether the sentence is already synthesized.
func (c *Clone) Cached(sentence, voice string, speed float64) bool {
	_, err := os.Stat(c.CachePath(sentence, voice, speed))
	return err == nil
}

// Synth implements Synth. voice is "clone:<id>". Chatterbox has no speed
// control, so other speeds are time-stretched afterwards.
func (c *Clone) Synth(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	out := c.CachePath(sentence, voice, speed)
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	ref := c.refPath(strings.TrimPrefix(voice, ClonePrefix))
	if _, err := os.Stat(ref); err != nil {
		return "", fmt.Errorf("克隆音色 %q 的录音不见了", voice)
	}
	if err := c.Check(); err != nil {
		return "", err
	}
	if speed <= 0 {
		speed = 1
	}
	if err := os.MkdirAll(c.Cache, 0o755); err != nil {
		return "", err
	}
	if err := c.w.call(ctx, c.spec, map[string]any{"text": sentence, "voice": ref, "speed": speed, "out": out}); err != nil {
		return "", err
	}
	return out, nil
}

func (c *Clone) refPath(id string) string { return filepath.Join(c.Voices, id+".wav") }

func (c *Clone) index() string { return filepath.Join(c.Voices, "voices.json") }

// List returns the cloned voices, oldest first, leaving out any whose
// recording is gone.
func (c *Clone) List() []CloneVoice {
	c.vmu.Lock()
	defer c.vmu.Unlock()
	c.loadLocked()
	return slices.Clone(c.list)
}

func (c *Clone) loadLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	var all []CloneVoice
	if b, err := os.ReadFile(c.index()); err == nil {
		json.Unmarshal(b, &all)
	}
	c.list = slices.DeleteFunc(all, func(v CloneVoice) bool {
		_, err := os.Stat(c.refPath(v.ID))
		return v.ID == "" || err != nil
	})
}

func (c *Clone) saveLocked() error {
	b, err := json.MarshalIndent(c.list, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.index() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.index())
}

// Add makes a voice from the audio file src: it is converted, trimmed and
// stored as the voice's reference recording. src itself is left alone.
func (c *Clone) Add(ctx context.Context, name, src string) (CloneVoice, error) {
	if err := c.Check(); err != nil {
		return CloneVoice{}, err
	}
	if err := os.MkdirAll(c.Voices, 0o755); err != nil {
		return CloneVoice{}, err
	}
	var b [4]byte
	rand.Read(b[:])
	v := CloneVoice{ID: hex.EncodeToString(b[:]), Name: strings.TrimSpace(name), Created: time.Now()}
	dst := c.refPath(v.ID)
	res, err := c.tool(ctx, nil, "prep", src, dst)
	if err != nil {
		os.Remove(dst)
		return CloneVoice{}, err
	}
	v.Seconds = res.Seconds
	c.vmu.Lock()
	defer c.vmu.Unlock()
	c.loadLocked()
	if v.Name == "" {
		v.Name = "我的声音"
	}
	v.Name = c.uniqueNameLocked(v.Name)
	c.list = append(c.list, v)
	if err := c.saveLocked(); err != nil {
		c.list = c.list[:len(c.list)-1]
		os.Remove(dst)
		return CloneVoice{}, err
	}
	return v, nil
}

func (c *Clone) uniqueNameLocked(name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(c.list, func(v CloneVoice) bool { return v.Name == n })
	}
	out := name
	for i := 2; taken(out); i++ {
		out = fmt.Sprintf("%s %d", name, i)
	}
	return out
}

// Delete removes a cloned voice and its recording.
func (c *Clone) Delete(id string) error {
	c.vmu.Lock()
	defer c.vmu.Unlock()
	c.loadLocked()
	i := slices.IndexFunc(c.list, func(v CloneVoice) bool { return v.ID == id })
	if i < 0 {
		return nil
	}
	c.list = slices.Delete(c.list, i, i+1)
	if err := c.saveLocked(); err != nil {
		return err
	}
	return os.Remove(c.refPath(id))
}

type toolResult struct {
	OK        bool    `json:"ok"`
	Recording bool    `json:"recording"`
	Seconds   float64 `json:"seconds"`
	Error     string  `json:"error"`
}

// tool runs clone_tool.py with args and returns its last JSON line. When
// started is not nil, it gets the stdin of the tool once the tool says
// {"recording": true}.
func (c *Clone) tool(ctx context.Context, started func(io.WriteCloser), args ...string) (toolResult, error) {
	script := filepath.Join(c.Dir, "tool.py")
	if err := os.WriteFile(script, cloneTool, 0o644); err != nil {
		return toolResult{}, err
	}
	cmd := exec.CommandContext(ctx, c.Python(), append([]string{"-u", script}, args...)...)
	hideWindow(cmd)
	cmd.Env = c.env()
	in, err := cmd.StdinPipe()
	if err != nil {
		return toolResult{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return toolResult{}, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return toolResult{}, err
	}
	var res toolResult
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		var r toolResult
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.Recording && started != nil {
			started(in)
			continue
		}
		res = r
	}
	in.Close()
	werr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return toolResult{}, ctx.Err()
	case !res.OK && res.Error != "":
		return toolResult{}, errors.New(res.Error)
	case !res.OK:
		msg := strings.TrimSpace(stderr.String())
		if werr != nil && msg == "" {
			msg = werr.Error()
		}
		return toolResult{}, errors.New("处理录音失败：" + tail(msg, 200))
	}
	return res, nil
}

// Recording is a microphone recording in progress.
type Recording struct {
	started chan struct{}
	stop    chan struct{}
	once    sync.Once
	done    chan struct{}
	path    string
	seconds float64
	err     error
}

// Record starts recording the default microphone into a temporary WAV
// file. It returns once the microphone is open, or with the reason it
// could not be.
func (c *Clone) Record(ctx context.Context) (*Recording, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "repi-rec-*.wav")
	if err != nil {
		return nil, err
	}
	f.Close()
	r := &Recording{started: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}), path: f.Name()}
	go func() {
		defer close(r.done)
		res, err := c.tool(ctx, func(in io.WriteCloser) {
			close(r.started)
			go func() {
				select {
				case <-r.stop:
				case <-ctx.Done():
				}
				io.WriteString(in, "stop\n")
			}()
		}, "record", r.path)
		r.seconds, r.err = res.Seconds, err
		if err != nil {
			os.Remove(r.path)
		}
	}()
	select {
	case <-r.started:
		return r, nil
	case <-r.done:
		if r.err == nil {
			r.err = errors.New("录音没有开始")
		}
		return nil, r.err
	}
}

// Stop ends the recording; Wait then returns it.
func (r *Recording) Stop() { r.once.Do(func() { close(r.stop) }) }

// Done is closed when the recording ended: by Stop, at the time limit, or
// on an error.
func (r *Recording) Done() <-chan struct{} { return r.done }

// Wait returns the WAV file of the finished recording and its length. The
// caller removes the file.
func (r *Recording) Wait() (path string, seconds float64, err error) {
	<-r.done
	return r.path, r.seconds, r.err
}
