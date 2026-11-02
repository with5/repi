package dict

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CSVURL is where ECDICT's full CSV (about 66 MB) is downloaded from.
const CSVURL = "https://raw.githubusercontent.com/skywind3000/ECDICT/master/ecdict.csv"

// Build converts an ECDICT CSV into a dictionary file at out. Entries
// without a Chinese meaning and phrases are left out.
func Build(r io.Reader, out string) (int, error) {
	cr := csv.NewReader(r)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return 0, err
	}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	for _, need := range []string{"word", "phonetic", "translation", "tag", "exchange"} {
		if _, ok := col[need]; !ok {
			return 0, fmt.Errorf("CSV 缺少 %q 列", need)
		}
	}
	field := func(row []string, name string) string {
		if i := col[name]; i < len(row) {
			return row[i]
		}
		return ""
	}
	best := map[string]rec{}
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		word := strings.TrimSpace(field(row, "word"))
		trans := strings.TrimSpace(field(row, "translation"))
		key := strings.ToLower(word)
		if word == "" || trans == "" || len(key) > 40 || strings.ContainsAny(key, " \t") {
			continue
		}
		n := rec{key, word, field(row, "phonetic"), trans, field(row, "tag"), field(row, "exchange")}
		// "Apple" and "apple" share a key: keep the lowercase headword.
		if old, ok := best[key]; ok && (old.word == key || n.word != key) {
			continue
		}
		best[key] = n
	}
	recs := make([]rec, 0, len(best))
	for _, r := range best {
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].key < recs[j].key })
	if len(recs) == 0 {
		return 0, errors.New("CSV 里没有词条")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return 0, err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, writeFile(recs), 0o644); err != nil {
		return 0, err
	}
	return len(recs), os.Rename(tmp, out)
}

// Download fetches the CSV from url and builds the dictionary file at out,
// calling progress with a short description of what it is doing.
func Download(ctx context.Context, url, out string, progress func(string)) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载词典失败：HTTP %d", resp.StatusCode)
	}
	csvPath := out + ".csv"
	f, err := os.Create(csvPath)
	if err != nil {
		return err
	}
	defer os.Remove(csvPath)
	total, done, last := resp.ContentLength, int64(0), time.Time{}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			done += int64(n)
			if time.Since(last) > 200*time.Millisecond {
				last = time.Now()
				if total > 0 {
					progress(fmt.Sprintf("正在下载词典 %d/%d MB", done>>20, total>>20))
				} else {
					progress(fmt.Sprintf("正在下载词典 %d MB", done>>20))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	progress("正在建立索引…")
	in, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = Build(in, out)
	return err
}
