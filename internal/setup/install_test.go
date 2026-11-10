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
	vcMissing bool   // the self-check fails like PyTorch without the VC++ runtime
	vcExit    int    // exit code of vc_redist
	vcRan     bool
}

type exitErr int

func (e exitErr) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

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
	case strings.HasSuffix(name, "vc_redist.exe"):
		f.vcRan = true
		if f.vcExit != 0 {
			return exitErr(f.vcExit)
		}
	case len(args) > 0 && args[0] == "-c":
		if f.vcMissing && !f.vcRan {
			return errors.New("OSError: [WinError 126] ... Microsoft Visual C++ Redistributable is not installed ... vc_redist.x64.exe")
		}
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

func TestInstallVCRuntime(t *testing.T) {
	windows := func(r *fakeRunner) (*installer, string) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
		os.WriteFile(filepath.Join(dir, "bin", "uv.exe"), nil, 0o755) // r.version makes it current
		r.version = uvVersion
		in := testInstaller(t, Options{Dir: dir}, r)
		in.goos = "windows"
		in.vcRedist = strings.TrimSuffix(in.modelBase, "/") + "/vc_redist.x64.exe"
		return in, dir
	}
	for _, code := range []int{0, 3010, 1638} {
		r := &fakeRunner{vcMissing: true, vcExit: code}
		in, dir := windows(r)
		if err := in.run(context.Background(), func(Progress) {}); err != nil {
			t.Fatalf("exit %d: %v", code, err)
		}
		if !r.vcRan {
			t.Fatal("vc_redist not run")
		}
		if got := r.calls[len(r.calls)-2].args; !slices.Equal(got, []string{"/install", "/passive", "/norestart"}) {
			t.Errorf("vc_redist args %v", got)
		}
		if _, err := os.Stat(filepath.Join(dir, "venv")); err != nil {
			t.Fatal("venv not promoted after the runtime was installed")
		}
		if _, err := os.Stat(filepath.Join(dir, "bin", "vc_redist.exe")); !os.IsNotExist(err) {
			t.Error("vc_redist.exe left behind")
		}
	}

	r := &fakeRunner{vcMissing: true, vcExit: 1602}
	in, dir := windows(r)
	if err := in.run(context.Background(), func(Progress) {}); err == nil || !strings.Contains(err.Error(), "授权被取消") {
		t.Fatalf("declined UAC gives %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "venv")); !os.IsNotExist(err) {
		t.Fatal("venv promoted without the runtime")
	}

	// Without the VC++ hint, or off Windows, a failed check is not retried.
	if needsVCRuntime("linux", errors.New("vc_redist")) || needsVCRuntime("windows", errors.New("No module named kokoro")) {
		t.Fatal("needsVCRuntime too eager")
	}
	r = &fakeRunner{failCheck: true}
	in, _ = windows(r)
	if err := in.run(context.Background(), func(Progress) {}); err == nil || r.vcRan {
		t.Fatalf("err %v, vc_redist ran %v", err, r.vcRan)
	}
}

// The voice-cloning engine goes in its own directory but uses Kokoro's uv,
// Python, uv cache and PyTorch build; Chatterbox's own pins are skipped.
func TestInstallClone(t *testing.T) {
	dir := t.TempDir()
	kokoro, clone := filepath.Join(dir, "kokoro"), filepath.Join(dir, "clone")
	os.MkdirAll(filepath.Join(kokoro, "venv", "lib", "python3.12", "site-packages", "torch-2.15.0+cu132.dist-info"), 0o755)
	r := &fakeRunner{}
	var seen []Progress
	in := testInstaller(t, Options{Dir: kokoro, Clone: clone}, r)
	in.files = []string{"t3_cfg.safetensors", "conds.pt"}
	if err := in.run(context.Background(), func(p Progress) { seen = append(seen, p) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(venvPython(filepath.Join(clone, "venv"), "linux")); err != nil {
		t.Fatal("clone venv not promoted")
	}
	if _, err := os.Stat(filepath.Join(kokoro, "venv.partial")); !os.IsNotExist(err) {
		t.Fatal("touched Kokoro's directory")
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "model", "conds.pt")); string(b) != "weights" {
		t.Fatal("model not downloaded")
	}
	if _, err := os.Stat(filepath.Join(kokoro, "bin", "uv")); err != nil {
		t.Fatal("uv not in Kokoro's bin")
	}
	for _, c := range r.calls {
		if !slices.Contains(c.env, "UV_CACHE_DIR="+filepath.Join(kokoro, "uv-cache")) {
			t.Errorf("%v does not use Kokoro's uv cache", c.args)
		}
	}
	if slices.ContainsFunc(seen, func(p Progress) bool { return p.Title == "安装 Kokoro" }) {
		t.Error("shows Kokoro's steps")
	}
	// [0] venv, [1] torch, [2] chatterbox --no-deps, [3] deps with torch, [4] self-check
	if torch := r.calls[1].args; !slices.Contains(torch, "torch==2.15.0+cu132") {
		t.Errorf("torch does not match Kokoro's: %v", torch)
	}
	if nd := r.calls[2].args; !slices.Contains(nd, "--no-deps") || !slices.Contains(nd, "chatterbox-tts=="+chatterboxVersion) {
		t.Errorf("chatterbox args %v", nd)
	}
	deps := r.calls[3].args
	for _, w := range []string{"torch==2.15.0+cu132", "torchaudio==" + torchaudioVersion, "transformers==5.2.0", "sounddevice"} {
		if !slices.Contains(deps, w) {
			t.Errorf("deps %v lack %s", deps, w)
		}
	}
	if slices.Contains(deps, "--no-deps") {
		t.Error("deps installed without their own dependencies")
	}
	if check := r.calls[4].args; !strings.Contains(check[1], "chatterbox") {
		t.Errorf("self-check %v", check)
	}
}

func TestCloneSources(t *testing.T) {
	in := newInstaller(Options{Dir: "/k", Clone: "/c", Mirror: true}, nil)
	if in.modelBase != mirrorClone || in.target != "/c" || in.torch != "" {
		t.Fatalf("mirror %q target %q torch %q", in.modelBase, in.target, in.torch)
	}
	if in = newInstaller(Options{Dir: "/k", Clone: "/c"}, nil); in.modelBase != officialClone {
		t.Fatalf("model %q", in.modelBase)
	}
	if !strings.Contains(officialClone, cloneModelRev) {
		t.Fatal("model revision not pinned")
	}
	// no Kokoro venv found: fall back to the GPU choice
	in = newInstaller(Options{Dir: "/k", Clone: "/c", GPU: true}, nil)
	in.goos = "linux"
	if got := in.torchArgs("/p"); !slices.Contains(got, "torch==2.15.0+cu132") {
		t.Fatalf("torch args %v", got)
	}
}
