# One-Click Kokoro Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A button in Repi that installs Kokoro-82M (uv, Python, PyTorch, kokoro, spaCy model, model files) on Linux, macOS (Apple Silicon) and Windows, with an optional China mirror.

**Architecture:** New package `internal/setup` downloads a pinned `uv` wheel, runs uv to build `kokoro/venv.partial` and install packages, downloads the model files with Go, self-checks, then renames to `kokoro/venv`. The worker loads the local model files when they are complete. A card in the UI (`setup_view.go`) drives it like the dictionary download.

**Tech Stack:** Go 1.27 standard library (`archive/zip`, `net/http`, `os/exec`), uv 0.13.7, MyGo UI.

Spec: `docs/superpowers/specs/2026-11-07-one-click-tts-setup-design.md`

Commit identity for every commit in this repo (no Co-Authored-By line):

```bash
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "..."
```

---

## File structure

| File | Responsibility |
| --- | --- |
| `internal/tts/kokoro.go` (modify) | `KokoroModelFiles`, `ModelDir`, `LocalModel`, `workerEnv` |
| `internal/tts/kokoro_worker.py` (modify) | load local config / weights / voices when `REPI_KOKORO_MODEL` is set |
| `internal/setup/consts.go` | pinned versions, source URLs, platform tags, step titles |
| `internal/setup/fetch.go` | HTTP helpers: `getPage`, `download` (with `.part` and sha256), `netError` |
| `internal/setup/uv.go` | find uv wheel in a simple index, extract the binary, `ensureUV` |
| `internal/setup/model.go` | `downloadModel`, `removeParts` |
| `internal/setup/run.go` | `runner` interface, `execRunner`, `hideWindow` copies |
| `internal/setup/install.go` | `Options`, `Progress`, `Install`, the installer steps, `promote`, `GPUAvailable`, `EstimateSize` |
| `internal/store/store.go` (modify) | `Settings.Mirror` |
| `setup_view.go` | `setupState`, `startSetup`, `cancelSetup`, `applySetup`, `kokoroSetupCard` |
| `model.go`, `view.go`, `voices_view.go` (modify) | wire the state, show the card |

---

### Task 1: Worker loads a local model

**Files:**
- Modify: `internal/tts/kokoro.go` (add helpers after `Check`, use `workerEnv` in `start`)
- Modify: `internal/tts/kokoro_worker.py`
- Test: `internal/tts/tts_test.go`

- [ ] **Step 1: Write the failing test** — append to `internal/tts/tts_test.go` (add `os`, `path/filepath`, `slices`, `strings` imports if missing):

```go
func TestKokoroWorkerEnvLocalModel(t *testing.T) {
	k := &Kokoro{Dir: t.TempDir()}
	has := func(prefix string) bool {
		return slices.ContainsFunc(k.workerEnv(), func(e string) bool { return strings.HasPrefix(e, prefix) })
	}
	if k.LocalModel() || has("REPI_KOKORO_MODEL=") || has("HF_HUB_OFFLINE=") {
		t.Fatal("empty model dir treated as local model")
	}
	files := KokoroModelFiles()
	if len(files) != len(KokoroVoices)+2 || files[0] != "config.json" || files[2] != "voices/af_heart.pt" {
		t.Fatalf("files %v", files[:3])
	}
	for _, f := range files {
		p := filepath.Join(k.ModelDir(), filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	if !k.LocalModel() || !has("REPI_KOKORO_MODEL="+k.ModelDir()) || !has("HF_HUB_OFFLINE=1") {
		t.Fatal("complete model dir not used")
	}
}
```

- [ ] **Step 2: Run it, expect a compile failure** (`workerEnv`, `LocalModel`… undefined)

Run: `go test ./internal/tts/ -run TestKokoroWorkerEnvLocalModel`

- [ ] **Step 3: Implement** — in `internal/tts/kokoro.go` after `Check`:

```go
// KokoroModelFiles are the files of the Kokoro-82M repository Repi uses,
// relative to it: the config, the weights and one style file per voice.
func KokoroModelFiles() []string {
	files := []string{"config.json", "kokoro-v1_0.pth"}
	for _, v := range KokoroVoices {
		files = append(files, "voices/"+v.ID+".pt")
	}
	return files
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
```

In `start`, replace

```go
	cmd.Env = append(os.Environ(), "TOKENIZERS_PARALLELISM=false", "PYTHONWARNINGS=ignore")
```

with

```go
	cmd.Env = k.workerEnv()
```

- [ ] **Step 4: Update the worker** — in `internal/tts/kokoro_worker.py`, after `DEVICE = ...` add:

```python
MODEL_DIR = os.environ.get("REPI_KOKORO_MODEL", "")


def local(name):
    """Path of a model file in MODEL_DIR, or None."""
    if not MODEL_DIR:
        return None
    p = os.path.join(MODEL_DIR, *name.split("/"))
    return p if os.path.exists(p) else None
```

Replace `model = KModel(repo_id=REPO).to(DEVICE).eval()` with:

```python
    cfg, pth = local("config.json"), local("kokoro-v1_0.pth")
    if cfg and pth:
        model = KModel(repo_id=REPO, config=cfg, model=pth)
    else:
        model = KModel(repo_id=REPO)
    model = model.to(DEVICE).eval()
```

In the request loop, replace

```python
                for r in pipeline(req["voice"])(req["text"], voice=req["voice"], speed=req.get("speed") or 1.0):
```

with

```python
                voice = req["voice"]
                for r in pipeline(voice)(req["text"], voice=local(f"voices/{voice}.pt") or voice, speed=req.get("speed") or 1.0):
```

(`KPipeline.load_single_voice` reads a path ending in `.pt` directly; `pipeline()` still picks the language from `voice[0]`.)

- [ ] **Step 5: Verify**

Run: `go test ./internal/tts/ && python3 -I -m py_compile internal/tts/kokoro_worker.py && echo ok`
Expected: `ok` and tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tts
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat(tts): load Kokoro model files from a local directory"
```

---

### Task 2: Setup constants and HTTP helpers

**Files:**
- Create: `internal/setup/consts.go`, `internal/setup/fetch.go`
- Test: `internal/setup/fetch_test.go`

- [ ] **Step 1: Write `consts.go`** (no test of its own):

```go
// Package setup installs the Kokoro environment: uv, a Python virtual
// environment with PyTorch and kokoro, and the model files.
package setup

import "errors"

const (
	uvVersion     = "0.13.7"
	pythonVersion = "3.12"
	torchVersion  = "2.15.0"
	cudaFlavor    = "cu132"
	kokoroVersion = "0.9.4"
	spacyModelURL = "https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl"

	officialPyPI = "https://pypi.org/simple/"
	mirrorPyPI   = "https://pypi.tuna.tsinghua.edu.cn/simple/"
	officialTorch = "https://download.pytorch.org/whl/" // + flavor, an index
	mirrorTorch   = "https://mirrors.aliyun.com/pytorch-wheels/" // + flavor + "/", find-links
	mirrorPython  = "https://mirror.nju.edu.cn/github-release/astral-sh/python-build-standalone"
	officialModel = "https://huggingface.co/hexgrad/Kokoro-82M/resolve/main/"
	mirrorModel   = "https://www.modelscope.cn/models/AI-ModelScope/Kokoro-82M/resolve/master/"
)

// uvTags are the platform tags of the uv wheels, by GOOS/GOARCH. Intel Macs
// are missing on purpose: PyTorch has no macOS x86_64 wheels any more.
var uvTags = map[string]string{
	"linux/amd64":   "manylinux_2_17_x86_64",
	"linux/arm64":   "manylinux_2_17_aarch64",
	"darwin/arm64":  "macosx_11_0_arm64",
	"windows/amd64": "win_amd64",
	"windows/arm64": "win_arm64",
}

// cudaPlatforms have CUDA builds of PyTorch.
var cudaPlatforms = map[string]bool{"linux/amd64": true, "linux/arm64": true, "windows/amd64": true}

var stepTitles = []string{"获取 uv", "准备 Python", "安装 PyTorch", "安装 Kokoro", "下载模型", "检查安装"}

var errUnsupported = errors.New("这个系统不支持一键安装，请参照 README 手动安装 Kokoro")
```

- [ ] **Step 2: Write the failing test** `internal/setup/fetch_test.go`:

```go
package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	body := []byte("hello wheel")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	dst := filepath.Join(dir, "a", "f.whl")
	var last int64
	if err := download(context.Background(), srv.Client(), srv.URL+"/f", dst, hex.EncodeToString(sum[:]), func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(body) || last != int64(len(body)) {
		t.Fatalf("got %q, progress %d", b, last)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part left behind")
	}

	bad := filepath.Join(dir, "bad")
	if err := download(context.Background(), srv.Client(), srv.URL+"/f", bad, "00", nil); err == nil {
		t.Fatal("sha256 mismatch accepted")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("file kept after mismatch")
	}

	var ne *netError
	if err := download(context.Background(), srv.Client(), srv.URL+"/missing", bad, "", nil); !errors.As(err, &ne) {
		t.Fatalf("404 gives %v, want netError", err)
	}
	if _, err := getPage(context.Background(), srv.Client(), srv.URL+"/missing"); !errors.As(err, &ne) {
		t.Fatalf("getPage 404 gives %v", err)
	}
}
```

- [ ] **Step 3: Run it, expect compile failure**

Run: `go test ./internal/setup/`

- [ ] **Step 4: Implement `fetch.go`**:

```go
package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
)

// netError is a failed download; the UI suggests switching the mirror.
type netError struct{ msg string }

func (e *netError) Error() string { return e.msg }

func get(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	name := path.Base(req.URL.Path)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &netError{fmt.Sprintf("下载 %s 失败：%v", name, err)}
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, &netError{fmt.Sprintf("下载 %s 失败：HTTP %d", name, resp.StatusCode)}
	}
	return resp, nil
}

// getPage fetches a small page, such as a package index.
func getPage(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	resp, err := get(ctx, client, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// download fetches url into dst through dst+".part", so dst only ever holds
// a complete file. sha is the expected hex sha256, or "" to skip the check.
// progress, if not nil, gets the bytes done and the total (-1 if unknown).
func download(ctx context.Context, client *http.Client, url, dst, sha string, progress func(done, total int64)) error {
	resp, err := get(ctx, client, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				os.Remove(part)
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &netError{fmt.Sprintf("下载 %s 中断：%v", path.Base(dst), rerr)}
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if sha != "" && hex.EncodeToString(h.Sum(nil)) != sha {
		os.Remove(part)
		return fmt.Errorf("%s 校验失败，文件可能已损坏", path.Base(dst))
	}
	return os.Rename(part, dst)
}
```

- [ ] **Step 5: Run tests** — `go test ./internal/setup/` → PASS

- [ ] **Step 6: Commit**

```bash
git add internal/setup
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat(setup): add pinned sources and download helpers"
```

---

### Task 3: Find and extract the uv wheel

**Files:**
- Create: `internal/setup/uv.go`
- Test: `internal/setup/uv_test.go`

- [ ] **Step 1: Write the failing test** `internal/setup/uv_test.go`:

```go
package setup

import (
	"archive/zip"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

const indexPage = `<html><body>
<a href="../../packages/aa/uv-0.13.6-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.whl#sha256=11">old</a>
<a href="../../packages/bb/uv-0.13.7-py3-none-musllinux_1_1_x86_64.whl#sha256=22">musl</a>
<a href="../../packages/cc/uv-0.13.7-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.whl#sha256=33">x</a>
<a href="https://files.example/uv-0.13.7-py3-none-win_amd64.whl#sha256=44">win</a>
<a href="../../packages/dd/uv-0.13.7.tar.gz#sha256=55">sdist</a>
</body></html>`

func TestFindWheel(t *testing.T) {
	base, _ := url.Parse("https://pypi.example/simple/uv/")
	cases := []struct{ tag, link, sha string }{
		{"manylinux_2_17_x86_64", "https://pypi.example/packages/cc/uv-0.13.7-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.whl", "33"},
		{"win_amd64", "https://files.example/uv-0.13.7-py3-none-win_amd64.whl", "44"},
	}
	for _, c := range cases {
		link, sha, err := findWheel([]byte(indexPage), base, "0.13.7", c.tag)
		if err != nil || link != c.link || sha != c.sha {
			t.Errorf("%s: %q %q %v", c.tag, link, sha, err)
		}
	}
	if _, _, err := findWheel([]byte(indexPage), base, "0.13.7", "macosx_11_0_arm64"); err == nil {
		t.Error("missing tag found")
	}
}

// fakeWheel is a wheel holding the uv binary under its scripts directory.
func fakeWheel(t *testing.T, exe string, body []byte) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("uv-0.13.7.dist-info/METADATA")
	w.Write([]byte("Name: uv"))
	w, _ = z.Create("uv-0.13.7.data/scripts/uvx")
	w.Write([]byte("not me"))
	w, _ = z.Create("uv-0.13.7.data/scripts/" + exe)
	w.Write(body)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestExtractUV(t *testing.T) {
	dir := t.TempDir()
	whl := filepath.Join(dir, "uv.whl")
	os.WriteFile(whl, fakeWheel(t, "uv.exe", []byte("binary")), 0o644)
	dst := filepath.Join(dir, "bin", "uv.exe")
	if err := extractUV(whl, "uv.exe", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "binary" {
		t.Fatalf("got %q", b)
	}
	if err := extractUV(whl, "uv", filepath.Join(dir, "uv")); err == nil {
		t.Fatal("extracted a binary that is not there")
	}
}
```

- [ ] **Step 2: Run, expect compile failure** — `go test ./internal/setup/`

- [ ] **Step 3: Implement `uv.go`**:

```go
package setup

import (
	"archive/zip"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

// findWheel picks the uv wheel of version for the platform tag from a PEP
// 503 simple index page fetched from base. It returns the absolute URL and
// the sha256 the link carries ("" if none).
func findWheel(page []byte, base *url.URL, version, tag string) (link, sha string, err error) {
	prefix := "uv-" + version + "-py3-none-"
	for _, m := range hrefRe.FindAllSubmatch(page, -1) {
		u, perr := base.Parse(html.UnescapeString(string(m[1])))
		if perr != nil {
			continue
		}
		name := path.Base(u.Path)
		plat, ok := strings.CutPrefix(name, prefix)
		if !ok || !strings.HasSuffix(plat, ".whl") {
			continue
		}
		if !slices.Contains(strings.Split(strings.TrimSuffix(plat, ".whl"), "."), tag) {
			continue
		}
		sha, _ = strings.CutPrefix(u.Fragment, "sha256=")
		u.Fragment = ""
		return u.String(), sha, nil
	}
	return "", "", fmt.Errorf("软件源里找不到 uv %s（%s）", version, tag)
}

// extractUV copies the file exe from the scripts directory of the wheel to
// dst, executable.
func extractUV(whl, exe, dst string) error {
	z, err := zip.OpenReader(whl)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		if path.Base(f.Name) != exe || !strings.Contains(f.Name, ".data/scripts/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		tmp := dst + ".tmp"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			os.Remove(tmp)
			return err
		}
		if err := out.Close(); err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, dst)
	}
	return fmt.Errorf("uv 安装包里没有 %s", exe)
}
```

(`ensureUV`, which uses these, is a method of the installer and comes in Task 5.)

- [ ] **Step 4: Run tests** — `go test ./internal/setup/` → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/setup
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat(setup): pick and extract the uv wheel"
```

---

### Task 4: Model download

**Files:**
- Create: `internal/setup/model.go`
- Test: `internal/setup/model_test.go`

- [ ] **Step 1: Write the failing test** `internal/setup/model_test.go`:

```go
package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestDownloadModel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/repo/voices/broken.pt" {
			http.Error(w, "no", http.StatusBadGateway)
			return
		}
		w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("kept"), 0o644) // already there
	files := []string{"config.json", "kokoro-v1_0.pth", "voices/af_heart.pt"}
	var steps []int
	err := downloadModel(context.Background(), srv.Client(), srv.URL+"/repo/", dir, files, func(i, n int, name string, done, total int64) {
		if n != 3 {
			t.Errorf("n = %d", n)
		}
		steps = append(steps, i)
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d requests, want 2 (config.json skipped)", hits.Load())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "voices", "af_heart.pt")); string(b) != "/repo/voices/af_heart.pt" {
		t.Fatalf("voice %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(b) != "kept" {
		t.Fatal("existing file overwritten")
	}
	if len(steps) == 0 || steps[len(steps)-1] != 3 {
		t.Fatalf("progress %v", steps)
	}

	if err := downloadModel(context.Background(), srv.Client(), srv.URL+"/repo/", dir, []string{"voices/broken.pt"}, nil); err == nil {
		t.Fatal("HTTP error ignored")
	}
	os.WriteFile(filepath.Join(dir, "voices", "x.pt.part"), nil, 0o644)
	removeParts(dir)
	if _, err := os.Stat(filepath.Join(dir, "voices", "x.pt.part")); !os.IsNotExist(err) {
		t.Fatal(".part not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "voices", "af_heart.pt")); err != nil {
		t.Fatal("complete file removed")
	}
}
```

- [ ] **Step 2: Run, expect compile failure** — `go test ./internal/setup/`

- [ ] **Step 3: Implement `model.go`**:

```go
package setup

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// downloadModel fetches every file of files that is missing in dir from
// base + file. progress, if not nil, gets the file number (1-based), the
// count, the name and the bytes of that file.
func downloadModel(ctx context.Context, client *http.Client, base, dir string, files []string, progress func(i, n int, name string, done, total int64)) error {
	for i, f := range files {
		dst := filepath.Join(dir, filepath.FromSlash(f))
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		var p func(done, total int64)
		if progress != nil {
			p = func(done, total int64) { progress(i+1, len(files), f, done, total) }
		}
		if err := download(ctx, client, base+f, dst, "", p); err != nil {
			return err
		}
	}
	return nil
}

// removeParts deletes unfinished downloads under dir.
func removeParts(dir string) {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".part") {
			os.Remove(p)
		}
		return nil
	})
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/setup/` → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/setup
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat(setup): download the Kokoro model files"
```

---

### Task 5: Runner and the installer

**Files:**
- Create: `internal/setup/run.go`, `internal/setup/proc_windows.go`, `internal/setup/proc_other.go`, `internal/setup/install.go`
- Test: `internal/setup/install_test.go`

- [ ] **Step 1: Write `run.go` and the hideWindow copies** (no unit test: it is the thin exec wrapper, exercised by the e2e test in Task 8):

`internal/setup/proc_windows.go`:

```go
//go:build windows

package setup

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps child processes from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
```

`internal/setup/proc_other.go`:

```go
//go:build !windows

package setup

import "os/exec"

func hideWindow(*exec.Cmd) {}
```

`internal/setup/run.go`:

```go
package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// runner runs an external command, passing each non-empty output line
// (stdout and stderr) to onLine.
type runner interface {
	Run(ctx context.Context, env []string, onLine func(string), name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.WaitDelay = 5 * time.Second
	hideWindow(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	var last []string // the last lines, for the error message
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" {
				continue
			}
			if last = append(last, l); len(last) > 4 {
				last = last[1:]
			}
			onLine(l)
		}
		io.Copy(io.Discard, pr) // a line too long for the scanner
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%v：%s", err, strings.Join(last, " / "))
	}
	return nil
}
```

- [ ] **Step 2: Write the failing test** `internal/setup/install_test.go`. It drives the whole `run` with a fake runner and an httptest server standing in for PyPI and the model repository:

```go
package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// call is one command the fake runner was asked to run.
type call struct {
	name string
	args []string
	env  []string
}

// fakeRunner records calls. "uv venv" creates the interpreter, "uv --version"
// prints version, and the self-check fails when failCheck is set.
type fakeRunner struct {
	calls     []call
	version   string
	failCheck bool
	cancel    func() // called on "pip install" when set
}

func (f *fakeRunner) Run(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
	f.calls = append(f.calls, call{name, args, env})
	switch {
	case len(args) == 1 && args[0] == "--version":
		if f.version == "" {
			return errors.New("not found")
		}
		onLine("uv " + f.version + " (x86_64-unknown-linux-gnu)")
	case len(args) > 0 && args[0] == "venv":
		py := venvPython(args[len(args)-1], "linux")
		os.MkdirAll(filepath.Dir(py), 0o755)
		os.WriteFile(py, nil, 0o755)
	case len(args) > 0 && args[0] == "pip" && f.cancel != nil:
		f.cancel()
		return ctx.Err()
	case len(args) > 0 && args[0] == "-c":
		if f.failCheck {
			return errors.New("No module named kokoro")
		}
	}
	onLine("ok")
	return nil
}

// fakeSources serves a simple index with one linux uv wheel and any model file.
func fakeSources(t *testing.T) *httptest.Server {
	whl := fakeWheel(t, "uv", []byte("uv binary"))
	sum := sha256.Sum256(whl)
	mux := http.NewServeMux()
	mux.HandleFunc("/simple/uv/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<a href="../../packages/uv-%s-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.whl#sha256=%s">x</a>`, uvVersion, hex.EncodeToString(sum[:]))
	})
	mux.HandleFunc("/packages/", func(w http.ResponseWriter, r *http.Request) { w.Write(whl) })
	mux.HandleFunc("/model/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("weights")) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testInstaller(t *testing.T, o Options, r runner) *installer {
	srv := fakeSources(t)
	in := newInstaller(o, r)
	in.client, in.goos, in.goarch = srv.Client(), "linux", "amd64"
	in.pypi, in.modelBase = srv.URL+"/simple/", srv.URL+"/model/"
	in.files = []string{"config.json", "voices/af_heart.pt"}
	return in
}

func argsOf(calls []call, first string) []string {
	for _, c := range calls {
		if len(c.args) > 0 && c.args[0] == first {
			return c.args
		}
	}
	return nil
}

func TestInstall(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	var seen []Progress
	in := testInstaller(t, Options{Dir: dir}, r)
	if err := in.run(context.Background(), func(p Progress) { seen = append(seen, p) }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "bin", "uv")); string(b) != "uv binary" {
		t.Fatalf("uv %q", b)
	}
	if _, err := os.Stat(venvPython(filepath.Join(dir, "venv"), "linux")); err != nil {
		t.Fatal("venv not promoted")
	}
	if _, err := os.Stat(filepath.Join(dir, "venv.partial")); !os.IsNotExist(err) {
		t.Fatal("venv.partial left")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "model", "voices", "af_heart.pt")); string(b) != "weights" {
		t.Fatal("model not downloaded")
	}
	if last := seen[len(seen)-1]; last.Step != 6 || last.Steps != 6 {
		t.Fatalf("last progress %+v", last)
	}
	uv := filepath.Join(dir, "bin", "uv")
	for _, c := range r.calls { // fresh dir: no --version probe; every call but the self-check is uv
		if c.args[0] != "-c" && c.name != uv {
			t.Errorf("ran %s", c.name)
		}
		if !slices.Contains(c.env, "UV_PYTHON_INSTALL_DIR="+filepath.Join(dir, "python")) || !slices.Contains(c.env, "UV_NO_CONFIG=1") {
			t.Errorf("env of %v misses uv settings", c.args)
		}
	}
	partial := filepath.Join(dir, "venv.partial")
	if got := argsOf(r.calls, "venv"); !slices.Equal(got, []string{"venv", "--relocatable", "--python", "3.12", partial}) {
		t.Errorf("venv args %v", got)
	}
	torch := r.calls[1].args // [0] venv, [1] torch, [2] kokoro, [3] self-check
	if !slices.Contains(torch, "torch==2.15.0+cpu") || !slices.Contains(torch, "https://download.pytorch.org/whl/cpu") {
		t.Errorf("torch args %v", torch)
	}
	kokoro := r.calls[2].args
	if !slices.Contains(kokoro, "kokoro==0.9.4") || !slices.Contains(kokoro, spacyModelURL) {
		t.Errorf("kokoro args %v", kokoro)
	}

	// A second run finds uv by its version and does not download it again.
	r2 := &fakeRunner{version: uvVersion}
	in2 := testInstaller(t, Options{Dir: dir}, r2)
	in2.pypi = "http://127.0.0.1:1/simple/" // unreachable: must not be used
	if err := in2.run(context.Background(), func(Progress) {}); err != nil {
		t.Fatal(err)
	}
}

func TestTorchArgs(t *testing.T) {
	py := "/v/bin/python"
	cases := []struct {
		goos   string
		o      Options
		want   []string
		absent string
	}{
		{"linux", Options{}, []string{"--extra-index-url", officialTorch + "cpu", "torch==2.15.0+cpu", officialPyPI}, "--find-links"},
		{"windows", Options{GPU: true}, []string{officialTorch + "cu132", "torch==2.15.0+cu132"}, "--find-links"},
		{"linux", Options{Mirror: true}, []string{"--find-links", mirrorTorch + "cpu/", "torch==2.15.0+cpu", mirrorPyPI}, "--extra-index-url"},
		{"linux", Options{Mirror: true, GPU: true}, []string{mirrorTorch + "cu132/", "torch==2.15.0+cu132"}, "--extra-index-url"},
		{"darwin", Options{GPU: true}, []string{"torch==2.15.0", officialPyPI}, "--extra-index-url"},
	}
	for _, c := range cases {
		in := newInstaller(c.o, nil)
		in.goos = c.goos
		got := in.torchArgs(py)
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("%s %+v: %v lacks %s", c.goos, c.o, got, w)
			}
		}
		if slices.Contains(got, c.absent) {
			t.Errorf("%s %+v: %v has %s", c.goos, c.o, got, c.absent)
		}
	}
}

func TestMirrorEnv(t *testing.T) {
	in := newInstaller(Options{Dir: "/d", Mirror: true}, nil)
	if !slices.Contains(in.env(), "UV_PYTHON_INSTALL_MIRROR="+mirrorPython) || in.modelBase != mirrorModel || in.pypi != mirrorPyPI {
		t.Fatal("mirror not applied")
	}
	in = newInstaller(Options{Dir: "/d"}, nil)
	if slices.ContainsFunc(in.env(), func(e string) bool { return strings.HasPrefix(e, "UV_PYTHON_INSTALL_MIRROR=") }) || in.modelBase != officialModel {
		t.Fatal("mirror applied without asking")
	}
}

func TestInstallFailures(t *testing.T) {
	// The self-check fails: no venv appears, venv.partial is gone.
	dir := t.TempDir()
	in := testInstaller(t, Options{Dir: dir}, &fakeRunner{failCheck: true})
	err := in.run(context.Background(), func(Progress) {})
	if err == nil || !strings.Contains(err.Error(), "检查安装") {
		t.Fatalf("err %v", err)
	}
	for _, d := range []string{"venv", "venv.partial"} {
		if _, err := os.Stat(filepath.Join(dir, d)); !os.IsNotExist(err) {
			t.Errorf("%s exists after a failed check", d)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "uv")); err != nil {
		t.Error("uv removed; retry would download it again")
	}

	// Cancelled during pip: context.Canceled, venv.partial removed.
	ctx, cancel := context.WithCancel(context.Background())
	in = testInstaller(t, Options{Dir: dir}, &fakeRunner{version: uvVersion, cancel: cancel})
	if err := in.run(ctx, func(Progress) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel gives %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "venv.partial")); !os.IsNotExist(err) {
		t.Error("venv.partial left after cancel")
	}

	// Unsupported platform.
	in = testInstaller(t, Options{Dir: dir}, &fakeRunner{})
	in.goos, in.goarch = "darwin", "amd64"
	if err := in.run(context.Background(), func(Progress) {}); !errors.Is(err, errUnsupported) {
		t.Fatalf("intel mac gives %v", err)
	}

	// A network error says to try the other source.
	in = testInstaller(t, Options{Dir: t.TempDir()}, &fakeRunner{})
	in.pypi = in.pypi + "nothing/"
	if err := in.run(context.Background(), func(Progress) {}); err == nil || !strings.Contains(err.Error(), "国内镜像") {
		t.Fatalf("network error %v", err)
	}
}

func TestPromoteReplacesOld(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "venv", "old"), 0o755)
	os.MkdirAll(filepath.Join(dir, "venv.partial", "new"), 0o755)
	if err := promote(dir, filepath.Join(dir, "venv.partial")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "venv", "new")); err != nil {
		t.Fatal("new venv not in place")
	}
	for _, d := range []string{"venv.old", "venv.partial", filepath.Join("venv", "old")} {
		if _, err := os.Stat(filepath.Join(dir, d)); !os.IsNotExist(err) {
			t.Errorf("%s left", d)
		}
	}
}
```

- [ ] **Step 3: Run, expect compile failure** — `go test ./internal/setup/`

- [ ] **Step 4: Implement `install.go`**:

```go
package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"repi/internal/tts"
)

// Options say where and how to install.
type Options struct {
	Dir    string // the Kokoro directory, tts.Kokoro.Dir
	Mirror bool   // use mirrors in China
	GPU    bool   // install the CUDA build of PyTorch
}

// Progress is what the installer is doing, for the UI.
type Progress struct {
	Step, Steps int    // 1-based step, and the number of steps
	Title       string // of the step
	Detail      string // the last output line, or bytes downloaded
}

// Install sets up the Kokoro environment in o.Dir. On success o.Dir/venv is
// complete; on failure or cancel it is left as before, and what was
// downloaded (uv, Python, the uv cache, model files) is kept for a retry.
// A cancel returns context.Canceled.
func Install(ctx context.Context, o Options, progress func(Progress)) error {
	return newInstaller(o, execRunner{}).run(ctx, progress)
}

// GPUAvailable reports whether this machine has an NVIDIA GPU and PyTorch
// has CUDA builds for it.
func GPUAvailable() bool {
	if !cudaPlatforms[runtime.GOOS+"/"+runtime.GOARCH] {
		return false
	}
	p, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return false
	}
	cmd := exec.Command(p, "-L")
	hideWindow(cmd)
	out, err := cmd.Output()
	return err == nil && strings.Contains(string(out), "GPU")
}

// EstimateSize is the rough download size, for the UI.
func EstimateSize(gpu bool) string {
	if gpu {
		return "约 3.5GB"
	}
	return "约 700MB"
}

type installer struct {
	o             Options
	r             runner
	client        *http.Client
	goos, goarch  string
	pypi          string   // simple index, with a trailing slash
	modelBase     string   // model repository, with a trailing slash
	files         []string // model files, tts.KokoroModelFiles
	progress      func(Progress)
	cur           Progress
}

func newInstaller(o Options, r runner) *installer {
	in := &installer{o: o, r: r, client: &http.Client{}, goos: runtime.GOOS, goarch: runtime.GOARCH,
		pypi: officialPyPI, modelBase: officialModel, files: tts.KokoroModelFiles()}
	if o.Mirror {
		in.pypi, in.modelBase = mirrorPyPI, mirrorModel
	}
	return in
}

func (in *installer) exe(name string) string {
	if in.goos == "windows" {
		return name + ".exe"
	}
	return name
}

func (in *installer) uvPath() string { return filepath.Join(in.o.Dir, "bin", in.exe("uv")) }

// venvPython is the interpreter of the virtual environment at dir.
func venvPython(dir, goos string) string {
	if goos == "windows" {
		return filepath.Join(dir, "Scripts", "python.exe")
	}
	return filepath.Join(dir, "bin", "python")
}

// env keeps uv's Python, cache and settings inside o.Dir.
func (in *installer) env() []string {
	env := append(os.Environ(),
		"UV_CACHE_DIR="+filepath.Join(in.o.Dir, "uv-cache"),
		"UV_PYTHON_INSTALL_DIR="+filepath.Join(in.o.Dir, "python"),
		"UV_PYTHON_PREFERENCE=only-managed",
		"UV_NO_CONFIG=1",
		"UV_NO_PROGRESS=1",
	)
	if in.o.Mirror {
		env = append(env, "UV_PYTHON_INSTALL_MIRROR="+mirrorPython)
	}
	return env
}

func (in *installer) begin(step int) {
	in.cur = Progress{Step: step, Steps: len(stepTitles), Title: stepTitles[step-1]}
	in.progress(in.cur)
}

func (in *installer) detail(s string) {
	if r := []rune(s); len(r) > 80 {
		s = "…" + string(r[len(r)-80:])
	}
	in.cur.Detail = s
	in.progress(in.cur)
}

func (in *installer) uv(ctx context.Context, args ...string) error {
	return in.r.Run(ctx, in.env(), in.detail, in.uvPath(), args...)
}

func (in *installer) run(ctx context.Context, progress func(Progress)) error {
	in.progress = progress
	tag, ok := uvTags[in.goos+"/"+in.goarch]
	if !ok {
		return errUnsupported
	}
	partial := filepath.Join(in.o.Dir, "venv.partial")
	err := in.steps(ctx, tag, partial)
	if err == nil {
		return nil
	}
	os.RemoveAll(partial)
	removeParts(filepath.Join(in.o.Dir, "model"))
	if ctx.Err() != nil {
		return context.Canceled
	}
	msg := fmt.Sprintf("「%s」失败：%v", in.cur.Title, err)
	var ne *netError
	if errors.As(err, &ne) || strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "Failed to fetch") {
		if in.o.Mirror {
			msg += "。可以取消“使用国内镜像”后重试"
		} else {
			msg += "。网络不通时可以勾选“使用国内镜像”后重试"
		}
	}
	return errors.New(msg)
}

func (in *installer) steps(ctx context.Context, tag, partial string) error {
	in.begin(1)
	if err := in.ensureUV(ctx, tag); err != nil {
		return err
	}
	in.begin(2)
	if err := os.RemoveAll(partial); err != nil {
		return err
	}
	if err := in.uv(ctx, "venv", "--relocatable", "--python", pythonVersion, partial); err != nil {
		return err
	}
	py := venvPython(partial, in.goos)
	in.begin(3)
	if err := in.uv(ctx, in.torchArgs(py)...); err != nil {
		return err
	}
	in.begin(4)
	if err := in.uv(ctx, "pip", "install", "--python", py, "--index-url", in.pypi, "kokoro=="+kokoroVersion, "soundfile", spacyModelURL); err != nil {
		return err
	}
	in.begin(5)
	err := downloadModel(ctx, in.client, in.modelBase, filepath.Join(in.o.Dir, "model"), in.files, func(i, n int, name string, done, total int64) {
		d := fmt.Sprintf("%d/%d %s %d MB", i, n, filepath.Base(name), done>>20)
		if total > 0 {
			d = fmt.Sprintf("%d/%d %s %d/%d MB", i, n, filepath.Base(name), done>>20, total>>20)
		}
		in.detail(d)
	})
	if err != nil {
		return err
	}
	in.begin(6)
	check := "import kokoro, soundfile, torch, spacy; spacy.load('en_core_web_sm')"
	if err := in.r.Run(ctx, in.env(), in.detail, py, "-c", check); err != nil {
		return err
	}
	return promote(in.o.Dir, partial)
}

// torchArgs installs the pinned PyTorch: the CPU build (or CUDA with GPU)
// from PyTorch's index or the Aliyun mirror, the plain PyPI build on macOS.
func (in *installer) torchArgs(py string) []string {
	args := []string{"pip", "install", "--python", py, "--index-url", in.pypi}
	if in.goos == "darwin" {
		return append(args, "torch=="+torchVersion)
	}
	flavor := "cpu"
	if in.o.GPU {
		flavor = cudaFlavor
	}
	if in.o.Mirror {
		args = append(args, "--find-links", mirrorTorch+flavor+"/")
	} else {
		args = append(args, "--extra-index-url", officialTorch+flavor)
	}
	return append(args, "torch=="+torchVersion+"+"+flavor)
}

// ensureUV puts the pinned uv in bin/, unless it is already there.
func (in *installer) ensureUV(ctx context.Context, tag string) error {
	exe := in.uvPath()
	if _, err := os.Stat(exe); err == nil {
		var out string
		if in.r.Run(ctx, in.env(), func(l string) { out += l }, exe, "--version") == nil && strings.HasPrefix(out, "uv "+uvVersion+" ") {
			return nil
		}
	}
	index := in.pypi + "uv/"
	page, err := getPage(ctx, in.client, index)
	if err != nil {
		return err
	}
	base, err := url.Parse(index)
	if err != nil {
		return err
	}
	link, sha, err := findWheel(page, base, uvVersion, tag)
	if err != nil {
		return err
	}
	whl := filepath.Join(in.o.Dir, "bin", "uv.whl")
	defer os.Remove(whl)
	if err := download(ctx, in.client, link, whl, sha, func(done, total int64) {
		in.detail(fmt.Sprintf("%d/%d MB", done>>20, max(total, 0)>>20))
	}); err != nil {
		return err
	}
	return extractUV(whl, in.exe("uv"), exe)
}

// promote moves the checked environment to venv, replacing an old one.
func promote(dir, partial string) error {
	venv := filepath.Join(dir, "venv")
	old := venv + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(venv); err == nil {
		if err := os.Rename(venv, old); err != nil {
			return err
		}
	}
	if err := os.Rename(partial, venv); err != nil {
		os.Rename(old, venv)
		return err
	}
	os.RemoveAll(old)
	return nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/setup/ -v`
Expected: PASS for `TestDownload`, `TestFindWheel`, `TestExtractUV`, `TestDownloadModel`, `TestInstall`, `TestTorchArgs`, `TestMirrorEnv`, `TestInstallFailures`, `TestPromoteReplacesOld`.

- [ ] **Step 6: Vet and commit**

```bash
go vet ./internal/setup/ && GOOS=windows go vet ./internal/setup/ && GOOS=darwin go vet ./internal/setup/
git add internal/setup
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat(setup): install Kokoro with uv in six steps"
```

---

### Task 6: Remember the mirror choice

**Files:**
- Modify: `internal/store/store.go` (Settings, after `DailyGoal`)
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Failing test** — append:

```go
func TestSettingsMirror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	s.SetSettings(Settings{Mirror: true})
	s2, err := Open(path)
	if err != nil || !s2.Settings().Mirror {
		t.Fatalf("mirror not kept: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/store/ -run TestSettingsMirror` → compile failure.

- [ ] **Step 3: Implement** — in `Settings` after `DailyGoal`:

```go
	// Mirror makes the one-click Kokoro setup use mirrors in China.
	Mirror bool `json:"mirror,omitempty"`
```

- [ ] **Step 4: Run** `go test ./internal/store/` → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat(store): remember the setup mirror choice"
```

---

### Task 7: Setup card in the UI

**Files:**
- Create: `setup_view.go`
- Modify: `model.go` (struct field, `noTTSErr`, `newModel`, `saveSettings`), `view.go` (call `applySetup`, show card), `voices_view.go` (show card)
- Test: `setup_view_test.go`

- [ ] **Step 1: Failing test** `setup_view_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"repi/internal/store"
	"repi/internal/tts"
)

func TestApplySetupSwitchesToKokoro(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	k := &tts.Kokoro{Dir: filepath.Join(dir, "kokoro")}
	m := &model{st: st, kokoro: k, piper: &tts.Piper{}, setupErr: noTTSErr, voice: "en_US-gone"}
	m.kokoroSetup.mirror = true

	m.applySetup() // nothing finished yet
	if m.setupErr != noTTSErr {
		t.Fatal("applied before the install finished")
	}

	py := k.Python()
	os.MkdirAll(filepath.Dir(py), 0o755)
	os.WriteFile(py, nil, 0o755)
	m.kokoroSetup.done = true
	m.applySetup()
	if m.setupErr != "" || m.voice != defaultVoice || m.voiceSel != defaultVoice {
		t.Fatalf("err %q voice %q sel %q", m.setupErr, m.voice, m.voiceSel)
	}
	if !slices.Contains(m.favorites, defaultVoice) {
		t.Fatalf("favorites %v", m.favorites)
	}
	if s := st.Settings(); s.Voice != defaultVoice || !s.Mirror {
		t.Fatalf("settings %+v", s)
	}
	if m.kokoroSetup.done {
		t.Fatal("done not reset")
	}
}
```

- [ ] **Step 2: Run** `go test -run TestApplySetupSwitchesToKokoro .` → compile failure.

- [ ] **Step 3: Model changes** in `model.go`:

Near `const defaultVoice` add:

```go
// noTTSErr is the setup error when neither piper nor Kokoro is installed.
const noTTSErr = "没有可用的 TTS：可以在下面一键安装 Kokoro"
```

In the `model` struct, next to `dic dictState`, add:

```go
	kokoroSetup setupState
```

In `newModel` replace

```go
		m.setupErr = "没有可用的 TTS：请安装 piper-tts 或 Kokoro 环境"
```

with

```go
		m.setupErr = noTTSErr
```

and after `m.dailyGoal = s.DailyGoal` add:

```go
	m.initSetup(s.Mirror)
```

In `saveSettings`, after `s.DailyGoal = m.dailyGoal` add:

```go
	s.Mirror = m.kokoroSetup.mirror
```

- [ ] **Step 4: Create `setup_view.go`**:

```go
package main

import (
	"context"
	"errors"
	"sync"

	"github.com/egoist/mygo/ui"

	"repi/internal/setup"
)

// setupState is the one-click Kokoro install, which runs on another goroutine.
type setupState struct {
	// mirror and gpu are the checkboxes; only the UI thread touches them.
	mirror, gpu bool

	mu       sync.Mutex
	gpuOK    bool // an NVIDIA GPU PyTorch has CUDA builds for
	running  bool
	done     bool // finished; applySetup takes it on the UI thread
	progress setup.Progress
	err      string
	cancel   context.CancelFunc
}

// initSetup restores the mirror choice and, while Kokoro is missing, looks
// for a GPU in the background (nvidia-smi can take a moment).
func (m *model) initSetup(mirror bool) {
	m.kokoroSetup.mirror = mirror
	if m.kokoro.Check() == nil {
		return
	}
	go func() {
		ok := setup.GPUAvailable()
		s := &m.kokoroSetup
		s.mu.Lock()
		s.gpuOK = ok
		s.mu.Unlock()
		m.invalidate()
	}()
}

func (m *model) startSetup() {
	s := &m.kokoroSetup
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	o := setup.Options{Dir: m.kokoro.Dir, Mirror: s.mirror, GPU: s.gpu && s.gpuOK}
	s.running, s.err, s.cancel, s.progress = true, "", cancel, setup.Progress{Step: 1, Steps: 6, Title: "准备"}
	s.mu.Unlock()
	m.saveSettings() // remembers the mirror choice
	go func() {
		defer cancel()
		err := setup.Install(ctx, o, func(p setup.Progress) {
			s.mu.Lock()
			s.progress = p
			s.mu.Unlock()
			m.invalidate()
		})
		s.mu.Lock()
		s.running, s.cancel = false, nil
		switch {
		case err == nil:
			s.done = true
		case !errors.Is(err, context.Canceled):
			s.err = err.Error()
		}
		s.mu.Unlock()
		m.invalidate()
	}()
}

func (m *model) cancelSetup() {
	s := &m.kokoroSetup
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// applySetup, on the UI thread, puts a finished install to use: Kokoro's
// voices appear by themselves (Check passes now); pick one if the current
// voice is gone, and offer it while reading.
func (m *model) applySetup() {
	s := &m.kokoroSetup
	s.mu.Lock()
	done := s.done
	s.done = false
	s.mu.Unlock()
	if !done {
		return
	}
	if m.setupErr == noTTSErr {
		m.setupErr = ""
	}
	if len(m.favorites) == 0 {
		m.favorites = []string{defaultVoice}
	}
	if !m.hasVoice(m.voice) {
		if m.open {
			m.applyVoiceSpeed(defaultVoice, m.speed) // also saves
			m.voiceSel = m.voice
			return
		}
		m.voice = defaultVoice
	}
	m.voiceSel = m.voice
	m.saveSettings()
}

// kokoroSetupCard offers the one-click install while Kokoro is missing.
func (m *model) kokoroSetupCard(c *ui.Context) {
	if m.kokoro.Check() == nil {
		return
	}
	t := c.Theme()
	s := &m.kokoroSetup
	s.mu.Lock()
	running, p, errText, gpuOK := s.running, s.progress, s.err, s.gpuOK
	s.mu.Unlock()
	ui.Column(c).Padding(14, 18).Gap(8).Border(1, t.Border).Children(func() {
		ui.Text(c, "安装 Kokoro 语音引擎").FontSize(16).Bold()
		ui.Textf(c, "Kokoro 是 Repi 默认的神经网络语音引擎。一键安装会下载%s（Python、PyTorch 和模型），装在 %s，不影响系统环境。",
			setup.EstimateSize(s.gpu && gpuOK), m.kokoro.Dir).TextColor(t.TextMuted).Wrap()
		if errText != "" {
			ui.Text(c, errText).TextColor(t.Danger).Wrap()
		}
		if running {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Textf(c, "第 %d/%d 步：%s", p.Step, p.Steps, p.Title)
			})
			if p.Detail != "" {
				ui.Text(c, p.Detail).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
			}
			if ui.Button(c, "取消").Clicked() {
				m.cancelSetup()
			}
			return
		}
		ui.Checkbox(c, &s.mirror, "使用国内镜像")
		if gpuOK {
			ui.Checkbox(c, &s.gpu, "GPU 加速（NVIDIA，下载约 3.5GB）")
		}
		label := "一键安装"
		if errText != "" {
			label = "重试"
		}
		if ui.PrimaryButton(c, label).Clicked() {
			m.startSetup()
		}
	})
}
```

- [ ] **Step 5: Show the card.** In `view.go` `view`, before `m.saveProgress(false)`:

```go
	m.applySetup()
```

and replace

```go
			if m.setupErr != "" {
				ui.Text(c, m.setupErr).TextColor(t.Danger)
			}
```

with

```go
			if m.setupErr != "" {
				ui.Text(c, m.setupErr).TextColor(t.Danger)
			}
			if m.setupErr == noTTSErr && !m.showVoices {
				m.kokoroSetupCard(c)
			}
```

In `voices_view.go` `voicePanel`, right after the line with `点「试听」听一下` add:

```go
	m.kokoroSetupCard(c)
```

- [ ] **Step 6: Run tests and vet**

Run: `go test ./... && go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./...`
Expected: all PASS, no vet output. (If `ui.Column(...).Border` does not exist on columns, use `.BorderWidth(1).BorderColor(t.Border)` as `lookupOverlay` does.)

- [ ] **Step 7: Commit**

```bash
git add setup_view.go setup_view_test.go model.go view.go voices_view.go
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "feat: one-click Kokoro install card"
```

---

### Task 8: End-to-end install test and README

**Files:**
- Create: `internal/setup/e2e_test.go`
- Modify: `README.md`, `README.zh-CN.md` (installation section)

- [ ] **Step 1: Write the opt-in e2e test**:

```go
package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"repi/internal/tts"
)

// TestRealInstall installs Kokoro for real and synthesizes one sentence.
// REPI_E2E_SETUP=1 runs it; REPI_E2E_MIRROR=1 uses the mirrors.
func TestRealInstall(t *testing.T) {
	if os.Getenv("REPI_E2E_SETUP") != "1" {
		t.Skip("set REPI_E2E_SETUP=1 to install for real")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	err := Install(ctx, Options{Dir: dir, Mirror: os.Getenv("REPI_E2E_MIRROR") == "1"}, func(p Progress) {
		t.Logf("%d/%d %s %s", p.Step, p.Steps, p.Title, p.Detail)
	})
	if err != nil {
		t.Fatal(err)
	}
	k := &tts.Kokoro{Dir: dir, Cache: filepath.Join(dir, "cache")}
	defer k.Close()
	if !k.LocalModel() {
		t.Fatal("model files missing")
	}
	wav, err := k.Synth(ctx, "Hello from Repi.", tts.KokoroPrefix+"af_heart", 1)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(wav); err != nil || fi.Size() < 10000 {
		t.Fatalf("wav %v %v", fi, err)
	}
}
```

- [ ] **Step 2: Run it for real on this machine** (pypi.org and huggingface.co time out here, so use the mirrors):

Run: `REPI_E2E_SETUP=1 REPI_E2E_MIRROR=1 go test ./internal/setup/ -run TestRealInstall -v -timeout 45m`
Expected: PASS. If a step fails, fix the command in `install.go` (and its unit test) before going on; record what was wrong in the commit message.

- [ ] **Step 3: README.** In `README.md`, replace the body of “### 1. Set up a TTS backend” with:

```markdown
**Kokoro-82M** (default; great quality, fast on GPU and CPU). Start Repi and click **一键安装** (Install) on the *Install Kokoro* card — on the main screen when no voice is installed, or at the top of the **Voices** panel. Repi downloads uv, Python 3.12, PyTorch, kokoro and the model (about 700 MB, about 3.5 GB with the optional NVIDIA GPU build) into its own data folder; nothing system-wide is touched. Tick **使用国内镜像** to use mirrors in China (Tsinghua PyPI, Aliyun PyTorch, NJU Python, ModelScope). Intel Macs are not supported by PyTorch any more.

<details><summary>Manual install</summary>

(the existing uv venv / pip / spacy commands, unchanged)

</details>

**piper-tts** (optional, CPU-friendly): install `piper-tts` and put English voices in `/usr/share/piper-voices`.
```

Make the same change in `README.zh-CN.md` in Chinese (keep its existing manual commands inside a `<details><summary>手动安装</summary>` block).

- [ ] **Step 4: Commit**

```bash
git add internal/setup/e2e_test.go README.md README.zh-CN.md
git -c user.name=with5 -c user.email=301201176+with5@users.noreply.github.com commit -m "test(setup): real install e2e; docs: one-click Kokoro install"
```

---

### Task 9: Build and try it

- [ ] **Step 1: Full checks**

Run: `go test ./... && go vet ./... && GOOS=windows GOARCH=amd64 go build -o /dev/null . && GOOS=darwin GOARCH=arm64 go build -o /dev/null .`
Expected: PASS, cross builds succeed.

- [ ] **Step 2: Build the binary**

Run: `go build -o repi . && ./repi --version`
Expected: version printed. Report the path `/home/grey/with5/repi/repi`.

- [ ] **Step 3: Manual UI check** — run with an empty data dir so the card shows without touching the real install:

Run: `XDG_DATA_HOME=$(mktemp -d) ./repi`
Expected: red “没有可用的 TTS” line and the install card below it; with 使用国内镜像 ticked, 一键安装 runs through the six steps; afterwards Kokoro voices appear and reading works.

- [ ] **Step 4: Verify commit identity**

Run: `git log -6 --format='%h %an <%ae> | %cn <%ce>'`
Expected: every line `with5 <301201176+with5@users.noreply.github.com>`.
