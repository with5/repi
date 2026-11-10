package tts

import (
	"bufio"
	"context"
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

// workerSpec says how to start a worker process.
type workerSpec struct {
	Name    string   // for messages, "Kokoro"
	Python  string   // the interpreter
	Script  string   // where to write Code
	Code    []byte   // the worker's source
	Env     []string // its environment
	Loading string   // status while the model loads
}

// worker is a long-lived Python process that loads a model once, then reads
// one JSON request per line on stdin and answers one JSON line on stdout.
// Its first line is {"ready": true}; stderr is shown as the status while it
// loads. Requests are served one at a time.
type worker struct {
	mu     sync.Mutex // serializes requests and guards the fields below
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	name   string
	nextID int

	smu    sync.Mutex // guards status, which the UI reads while mu is held
	status string
}

// venvPython is the interpreter of the virtual environment at dir.
func venvPython(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "Scripts", "python.exe")
	}
	return filepath.Join(dir, "bin", "python")
}

// Status is a short description of what the worker is doing, for the UI.
func (w *worker) Status() string {
	w.smu.Lock()
	defer w.smu.Unlock()
	return w.status
}

func (w *worker) setStatus(s string) {
	w.smu.Lock()
	w.status = s
	w.smu.Unlock()
}

// start launches the worker and waits for its ready line. w.mu is held.
func (w *worker) start(sp workerSpec) error {
	if err := os.WriteFile(sp.Script, sp.Code, 0o644); err != nil {
		return err
	}
	cmd := exec.Command(sp.Python, "-u", sp.Script)
	hideWindow(cmd)
	cmd.Env = sp.Env
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
	w.setStatus(sp.Loading)
	var loading atomic.Bool
	loading.Store(true)
	go func() { // loading progress: show the last line, until ready
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); l != "" && loading.Load() {
				w.setStatus(sp.Name + ": " + tail(l, 80))
			}
		}
	}()
	rd := bufio.NewReaderSize(out, 1<<20)
	for { // skip anything a library printed before the ready line
		line, err := rd.ReadString('\n')
		if err != nil {
			cmd.Wait()
			return fmt.Errorf("%s 启动失败：%s", sp.Name, w.Status())
		}
		var ready struct {
			Ready bool `json:"ready"`
		}
		if json.Unmarshal([]byte(line), &ready) == nil && ready.Ready {
			loading.Store(false)
			break
		}
	}
	w.cmd, w.in, w.out, w.name = cmd, in, rd, sp.Name
	w.setStatus("")
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
func (w *worker) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopLocked()
}

func (w *worker) stopLocked() {
	if w.cmd != nil {
		w.in.Close()
		w.cmd.Process.Kill()
		w.cmd.Wait()
		w.cmd, w.in, w.out = nil, nil, nil
	}
}

// call sends req (an "id" is added) to the worker, starting it from spec
// first if needed, and waits for the answer.
func (w *worker) call(ctx context.Context, spec func() workerSpec, req map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if w.cmd == nil {
		if err := w.start(spec()); err != nil {
			return err
		}
	}
	w.nextID++
	req["id"] = w.nextID
	b, _ := json.Marshal(req)
	if _, err := w.in.Write(append(b, '\n')); err != nil {
		w.stopLocked()
		return err
	}
	line, err := w.out.ReadString('\n')
	if err != nil {
		w.stopLocked()
		return fmt.Errorf("%s 进程已退出：%s", w.name, w.Status())
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		w.stopLocked()
		return err
	}
	if !resp.OK {
		return errors.New(w.name + ": " + resp.Error)
	}
	return nil
}
