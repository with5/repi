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
	Dir    string // the Kokoro directory, tts.Kokoro.Dir; also holds uv, Python and the uv cache
	Mirror bool   // use mirrors in China
	GPU    bool   // install the CUDA build of PyTorch
	// Clone, when set, installs the Chatterbox voice-cloning engine in this
	// directory (tts.Clone.Dir) instead of Kokoro. It uses Dir's uv, Python
	// and uv cache, and the PyTorch build Kokoro has, so PyTorch comes from
	// the cache.
	Clone string
	// Zh, when set, adds Chinese support to the Kokoro environment in Dir,
	// which was installed without it: misaki's Chinese G2P and the Mandarin
	// voices. New installs have it already.
	Zh bool
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

// EstimateCloneSize is the rough download size of the voice-cloning engine
// once Kokoro is installed, for the UI.
func EstimateCloneSize() string { return "约 3.5GB" }

// EstimateZhSize is the rough download size of Chinese support, for the UI.
func EstimateZhSize() string { return "约 30MB" }

// EstimateSize is the rough download size, for the UI.
func EstimateSize(gpu bool) string {
	if gpu {
		return "约 3.5GB"
	}
	return "约 700MB"
}

type installer struct {
	o            Options
	r            runner
	client       *http.Client
	goos, goarch string
	pypi         string   // simple index, with a trailing slash
	modelBase    string   // model repository, with a trailing slash
	files        []string // model files, tts.KokoroModelFiles
	titles       []string // of the steps
	target       string   // where venv/ and model/ go: o.Dir, or o.Clone
	torch        string   // a PyTorch version to match, "2.15.0+cu132"; "" for torchVersion
	vcRedist     string   // Microsoft VC++ redistributable, for Windows
	progress     func(Progress)
	cur          Progress
}

func newInstaller(o Options, r runner) *installer {
	in := &installer{o: o, r: r, client: &http.Client{}, goos: runtime.GOOS, goarch: runtime.GOARCH,
		pypi: officialPyPI, modelBase: officialModel, titles: stepTitles, target: o.Dir,
		files: append(tts.KokoroModelFiles(), tts.KokoroZhFiles()...)}
	if o.Mirror {
		in.pypi, in.modelBase = mirrorPyPI, mirrorModel
	}
	if o.Zh {
		in.files, in.titles = tts.KokoroZhFiles(), zhStepTitles
	}
	if o.Clone != "" {
		in.modelBase, in.files, in.titles, in.target = officialClone, tts.CloneModelFiles, cloneStepTitles, o.Clone
		if o.Mirror {
			in.modelBase = mirrorClone
		}
		in.torch = installedTorch(filepath.Join(o.Dir, "venv"), in.goos)
	}
	in.vcRedist = vcRedistURL + "x64.exe"
	if in.goarch == "arm64" {
		in.vcRedist = vcRedistURL + "arm64.exe"
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

// installedTorch is the PyTorch version in the virtual environment at venv,
// "2.15.0+cu132", or "" when there is none.
func installedTorch(venv, goos string) string {
	pattern := filepath.Join(venv, "lib", "python*", "site-packages", "torch-*.dist-info")
	if goos == "windows" {
		pattern = filepath.Join(venv, "Lib", "site-packages", "torch-*.dist-info")
	}
	m, _ := filepath.Glob(pattern)
	if len(m) != 1 {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m[0]), "torch-"), ".dist-info")
}

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
	in.cur = Progress{Step: step, Steps: len(in.titles), Title: in.titles[step-1]}
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
	if err := os.MkdirAll(in.target, 0o755); err != nil {
		return err
	}
	partial := filepath.Join(in.target, "venv.partial")
	var err error
	if in.o.Zh {
		err = in.zhSteps(ctx, tag)
	} else {
		err = in.steps(ctx, tag, partial)
	}
	if err == nil {
		return nil
	}
	if !in.o.Zh {
		os.RemoveAll(partial)
	}
	removeParts(filepath.Join(in.target, "model"))
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
	if err := in.installEngine(ctx, py); err != nil {
		return err
	}
	in.begin(5)
	if err := in.download(ctx); err != nil {
		return err
	}
	in.begin(6)
	check := "import kokoro, soundfile, torch, spacy; spacy.load('en_core_web_sm'); " + zhCheck
	if in.o.Clone != "" {
		check = "import torch, torchaudio, librosa, soundfile, pyloudnorm; from chatterbox.tts import ChatterboxTTS"
	}
	err := in.r.Run(ctx, in.env(), in.detail, py, "-c", check)
	if err != nil && needsVCRuntime(in.goos, err) {
		if err := in.installVCRuntime(ctx); err != nil {
			return err
		}
		err = in.r.Run(ctx, in.env(), in.detail, py, "-c", check)
	}
	if err != nil {
		return err
	}
	return promote(in.target, partial)
}

// zhCheck imports what Kokoro needs to read Chinese.
const zhCheck = "import jieba, pypinyin, cn2an; from misaki import zh"

// download fetches the model files into target/model.
func (in *installer) download(ctx context.Context) error {
	return downloadModel(ctx, in.client, in.modelBase, filepath.Join(in.target, "model"), in.files, func(i, n int, name string, done, total int64) {
		d := fmt.Sprintf("%d/%d %s %d MB", i, n, filepath.Base(name), done>>20)
		if total > 0 {
			d = fmt.Sprintf("%d/%d %s %d/%d MB", i, n, filepath.Base(name), done>>20, total>>20)
		}
		in.detail(d)
	})
}

// zhSteps add Chinese support to the installed Kokoro environment, in place:
// what they add is new, so a failure leaves English as it was.
func (in *installer) zhSteps(ctx context.Context, tag string) error {
	py := venvPython(filepath.Join(in.o.Dir, "venv"), in.goos)
	if _, err := os.Stat(py); err != nil {
		return errors.New("还没有安装 Kokoro")
	}
	in.begin(1)
	if err := in.ensureUV(ctx, tag); err != nil {
		return err
	}
	in.begin(2)
	if err := in.uv(ctx, "pip", "install", "--python", py, "--index-url", in.pypi, "misaki[zh]=="+misakiVersion); err != nil {
		return err
	}
	in.begin(3)
	if err := in.download(ctx); err != nil {
		return err
	}
	in.begin(4)
	return in.r.Run(ctx, in.env(), in.detail, py, "-c", zhCheck)
}

// installEngine installs kokoro, or Chatterbox: first the packages whose
// pins are ignored, then their dependencies together with the PyTorch
// already installed, so that the resolver keeps it.
func (in *installer) installEngine(ctx context.Context, py string) error {
	if in.o.Clone == "" {
		return in.uv(ctx, "pip", "install", "--python", py, "--index-url", in.pypi, "kokoro=="+kokoroVersion, "misaki[en,zh]=="+misakiVersion, "soundfile", spacyModelURL)
	}
	if err := in.uv(ctx, append([]string{"pip", "install", "--python", py, "--index-url", in.pypi, "--no-deps"}, cloneNoDeps...)...); err != nil {
		return err
	}
	args := in.torchArgs(py)
	args = append(args, "torchaudio=="+torchaudioVersion)
	return in.uv(ctx, append(args, cloneDeps...)...)
}

// needsVCRuntime reports whether the self-check failed because the Microsoft
// Visual C++ runtime PyTorch needs on Windows is missing or too old. PyTorch
// then names vc_redist in its error; WinError 126 is the DLL load failure.
func needsVCRuntime(goos string, err error) bool {
	if goos != "windows" {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "vc_redist") || strings.Contains(s, "WinError 126")
}

// installVCRuntime downloads Microsoft's VC++ redistributable and runs it.
// It asks for administrator rights itself (one UAC prompt).
func (in *installer) installVCRuntime(ctx context.Context) error {
	in.detail("正在下载 Microsoft VC++ 运行库…")
	exe := filepath.Join(in.o.Dir, "bin", "vc_redist.exe")
	defer os.Remove(exe)
	if err := download(ctx, in.client, in.vcRedist, exe, "", func(done, total int64) {
		in.detail(fmt.Sprintf("正在下载 Microsoft VC++ 运行库 %d/%d MB", done>>20, max(total, 0)>>20))
	}); err != nil {
		return err
	}
	in.detail("正在安装 Microsoft VC++ 运行库，请在弹出的窗口里允许…")
	err := in.r.Run(ctx, in.env(), in.detail, exe, "/install", "/passive", "/norestart")
	var ec interface{ ExitCode() int }
	if err != nil && errors.As(err, &ec) {
		switch ec.ExitCode() {
		case 3010, 1638: // installed, reboot pending; a newer one is there
			return nil
		case 1602, 1223: // the user said no
			return errors.New("没有安装 Microsoft VC++ 运行库（授权被取消）。PyTorch 需要它，请重试并允许")
		}
	}
	if err != nil {
		return fmt.Errorf("安装 Microsoft VC++ 运行库失败：%w。也可以手动安装 %s 后重试", err, in.vcRedist)
	}
	return nil
}

// torchArgs installs the pinned PyTorch: the CPU build (or CUDA with GPU)
// from PyTorch's index or the Aliyun mirror, the plain PyPI build on macOS.
func (in *installer) torchArgs(py string) []string {
	args := []string{"pip", "install", "--python", py, "--index-url", in.pypi}
	version, flavor := torchVersion, "cpu"
	if in.o.GPU {
		flavor = cudaFlavor
	}
	if in.torch != "" { // the build Kokoro has, which is in the uv cache
		v, f, ok := strings.Cut(in.torch, "+")
		version = v
		if ok {
			flavor = f
		}
	}
	if in.goos == "darwin" {
		return append(args, "torch=="+version)
	}
	if in.o.Mirror {
		args = append(args, "--find-links", mirrorTorch+flavor+"/")
	} else {
		args = append(args, "--extra-index-url", officialTorch+flavor)
	}
	return append(args, "torch=="+version+"+"+flavor)
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
