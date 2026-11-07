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
