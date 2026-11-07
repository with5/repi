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
	// The Tsinghua mirror answers 403 to Go's default User-Agent.
	req.Header.Set("User-Agent", userAgent)
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
