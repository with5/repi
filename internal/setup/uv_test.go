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
