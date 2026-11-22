package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"repi/internal/pack"
)

// exportState is the export of a material to a .repi file for the phone.
type exportState struct {
	mu     sync.Mutex
	id     int // material being or last exported
	busy   bool
	done   int
	total  int
	path   string // written file, once done
	err    string
	cancel context.CancelFunc
}

func (s *exportState) set(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
}

// exportPack asks where to save the open material as a pack, then
// synthesizes what is missing and writes it in the background.
func (m *model) exportPack() {
	if !m.open || !m.canRead() {
		return
	}
	s := &m.export
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	m.mu.Lock()
	src := pack.Source{
		Title: m.cur.Title, Lang: m.lang(), Voice: m.voice, Speed: m.speed,
		Sentences: m.sentences,
	}
	m.mu.Unlock()
	id, withTrans, tr := m.cur.ID, m.showTrans, m.tr
	src.Audio = func(ctx context.Context, i int) (string, error) {
		return m.pool.Get(ctx, src.Sentences[i], src.Voice, src.Speed)
	}
	src.Translation = func(ctx context.Context, sentence string) (string, bool) {
		if t, ok := tr.Cached(sentence); ok {
			return t, true
		}
		if !withTrans { // only ask the web when translation is on
			return "", false
		}
		t, err := tr.Translate(ctx, sentence)
		return t, err == nil && t != ""
	}

	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            m.win,
			Title:             "导出到手机",
			DefaultPath:       filepath.Join(exportDir(), pack.FileName(src.Title)),
			Filters:           []mygo.FileFilter{{Name: "Repi", Extensions: []string{"repi"}}},
			CreateDirectories: true,
		})
		if err != nil || path == "" {
			return
		}
		if filepath.Ext(path) == "" {
			path += pack.Ext
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.set(func() {
			s.id, s.busy, s.done, s.total = id, true, 0, len(src.Sentences)
			s.path, s.err, s.cancel = "", "", cancel
		})
		m.invalidate()
		err = writePack(ctx, path, src, func(d, n int) {
			s.set(func() { s.done, s.total = d, n })
			m.invalidate()
		})
		cancel()
		s.set(func() {
			s.busy, s.cancel = false, nil
			switch {
			case err == context.Canceled:
			case err != nil:
				s.err = "导出失败：" + err.Error()
			default:
				s.path = path
			}
		})
		m.invalidate()
	}()
}

// writePack writes the pack next to path and renames it there when complete,
// so a failed export leaves no broken file.
func writePack(ctx context.Context, path string, src pack.Source, progress func(d, n int)) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".repi-export-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op after the rename
	err = pack.Write(ctx, f, src, pack.BestEncoder(), progress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// exportDir is where the save dialog starts: Downloads when there is one.
func exportDir() string {
	home, _ := os.UserHomeDir()
	if d := filepath.Join(home, "Downloads"); isDir(d) {
		return d
	}
	return home
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// exportButton is the header button, which shows the export's progress.
func (m *model) exportButton(c *ui.Context) {
	s := &m.export
	s.mu.Lock()
	busy, mine, done, total := s.busy, s.id == m.cur.ID, s.done, s.total
	s.mu.Unlock()
	if busy {
		if mine {
			ui.Textf(c, "导出中 %d/%d", done, total).TextColor(c.Theme().TextMuted).FontSize(12)
		}
		if ui.Button(c, "取消导出").Clicked() {
			s.set(func() {
				if s.cancel != nil {
					s.cancel()
				}
			})
		}
		return
	}
	if ui.Button(c, "导出到手机").Tooltip("生成 .repi 文件，传到手机上用 Repi 收听").Disabled(!m.canRead()).Clicked() {
		m.exportPack()
	}
}

// exportNote is the line under the header after an export of this material.
func (m *model) exportNote(c *ui.Context) {
	s := &m.export
	s.mu.Lock()
	mine, path, errText := s.id == m.cur.ID && !s.busy, s.path, s.err
	s.mu.Unlock()
	if !mine || (path == "" && errText == "") {
		return
	}
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		if errText != "" {
			ui.Text(c, errText).TextColor(t.Danger).FontSize(12).Grow(1).Wrap()
		} else {
			ui.Text(c, "已导出："+path+"，把它发到手机，用 Repi 打开即可").TextColor(t.TextMuted).FontSize(12).Grow(1).Wrap()
		}
		if ui.Button(c, "知道了").Clicked() {
			s.set(func() { s.path, s.err = "", "" })
		}
	})
}
