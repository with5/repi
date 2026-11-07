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
