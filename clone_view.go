package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"repi/internal/setup"
	"repi/internal/tts"
)

// cloneScript is what the card suggests reading aloud: about ten seconds of
// plain, varied English.
const cloneScript = "I usually wake up around seven, make a cup of coffee, and read the news for a few minutes. Then I take a short walk before I start working."

// cloneState is recording or importing a voice to clone, which runs on
// other goroutines.
type cloneState struct {
	mu       sync.Mutex
	busy     string // what is going on, "" when idle
	rec      *tts.Recording
	recStart time.Time
	err      string
	note     string
	added    *tts.CloneVoice // a new voice; applyClone takes it on the UI thread
}

func (s *cloneState) set(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
}

// cloneVoices are the cloned voices, when the engine is installed.
func (m *model) cloneVoices() []tts.CloneVoice {
	if m.clone == nil || m.clone.Check() != nil {
		return nil
	}
	return m.clone.List()
}

// ttsStatus is what a loading engine is doing, for the UI.
func (m *model) ttsStatus() string {
	if s := m.kokoro.Status(); s != "" {
		return s
	}
	if m.clone != nil {
		return m.clone.Status()
	}
	return ""
}

// startRecording opens the microphone; stopRecording (or the 30 s limit)
// ends it and the recording becomes a voice.
func (m *model) startRecording() {
	s := &m.cloneUI
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return
	}
	s.busy, s.err, s.note = "正在打开麦克风…", "", ""
	s.mu.Unlock()
	m.pl.Pause()
	m.stopAudition()
	name := m.cloneName
	go func() {
		rec, err := m.clone.Record(context.Background())
		if err != nil {
			s.set(func() { s.busy, s.err = "", err.Error() })
			m.invalidate()
			return
		}
		s.set(func() { s.rec, s.recStart, s.busy = rec, time.Now(), "录音中" })
		m.invalidate()
		tick := time.NewTicker(500 * time.Millisecond) // the elapsed time
		defer tick.Stop()
	wait:
		for {
			select {
			case <-rec.Done():
				break wait
			case <-tick.C:
				m.invalidate()
			}
		}
		path, _, err := rec.Wait()
		s.set(func() { s.rec = nil })
		if err != nil {
			s.set(func() { s.busy, s.err = "", err.Error() })
			m.invalidate()
			return
		}
		defer os.Remove(path)
		m.addVoice(name, path)
	}()
}

func (m *model) stopRecording() {
	s := &m.cloneUI
	s.mu.Lock()
	rec := s.rec
	if rec != nil {
		s.busy = "正在处理录音…"
	}
	s.mu.Unlock()
	if rec != nil {
		rec.Stop()
	}
}

// importVoice asks for an audio file and makes a voice from it.
func (m *model) importVoice() {
	s := &m.cloneUI
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return
	}
	s.busy, s.err, s.note = "选择音频文件…", "", ""
	s.mu.Unlock()
	name := m.cloneName
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  m.win,
			Title:   "选择一段自己的英文录音",
			Filters: []mygo.FileFilter{{Name: "Audio", Extensions: []string{"wav", "mp3", "flac", "ogg", "opus", "m4a"}}},
		})
		if err != nil || len(paths) == 0 {
			s.set(func() { s.busy = "" })
			m.invalidate()
			return
		}
		m.addVoice(name, paths[0])
	}()
}

// addVoice turns the audio file src into a cloned voice. It blocks.
func (m *model) addVoice(name, src string) {
	s := &m.cloneUI
	s.set(func() { s.busy = "正在处理录音…" })
	m.invalidate()
	v, err := m.clone.Add(context.Background(), name, src)
	s.set(func() {
		s.busy = ""
		if err != nil {
			s.err = err.Error()
		} else {
			s.added = &v
		}
	})
	m.invalidate()
}

// applyClone, on the UI thread, puts a new voice to use: it is starred, so
// the reader offers it, and auditioned.
func (m *model) applyClone() {
	s := &m.cloneUI
	s.mu.Lock()
	v := s.added
	s.added = nil
	if v != nil {
		s.note = fmt.Sprintf("已添加「%s」（有效录音 %.1f 秒），正在用它朗读试听文本。第一次要加载模型，稍等十几秒。", v.Name, v.Seconds)
	}
	s.mu.Unlock()
	if v == nil {
		return
	}
	name := tts.ClonePrefix + v.ID
	if !m.isFavorite(name) {
		m.toggleFavorite(name) // also saves
	}
	m.cloneName = ""
	m.audition(name)
}

// deleteVoice removes a cloned voice: from disk, the favorites, and as the
// voice in use.
func (m *model) deleteVoice(name string) {
	if a, _, _ := m.auditionState(); a == name {
		m.stopAudition()
	}
	if err := m.clone.Delete(strings.TrimPrefix(name, tts.ClonePrefix)); err != nil {
		m.cloneUI.set(func() { m.cloneUI.err = "删除失败：" + err.Error() })
		return
	}
	if i := slices.Index(m.favorites, name); i >= 0 {
		m.favorites = slices.Delete(slices.Clone(m.favorites), i, i+1)
	}
	m.repickVoices() // also saves
}

// deleteVoiceButton asks for a second click before deleting a cloned voice.
func (m *model) deleteVoiceButton(c *ui.Context, v voiceInfo) {
	if m.confirmVoc == v.Name {
		if ui.Button(c.Key("del"), "确认删除").Label("确认删除 " + v.Label).TextColor(c.Theme().Danger).Clicked() {
			m.confirmVoc = ""
			m.deleteVoice(v.Name)
		}
		return
	}
	if ui.Button(c.Key("del"), "删除").Label("删除 " + v.Label).Clicked() {
		m.confirmVoc = v.Name
	}
}

// cloneCard offers the voice-cloning engine once Kokoro is installed, and
// then records or imports the user's voice.
func (m *model) cloneCard(c *ui.Context) {
	if m.clone == nil || m.kokoro.Check() != nil {
		return // Kokoro's card comes first; the engine reuses its PyTorch
	}
	m.applyClone()
	if m.clone.Check() != nil {
		m.cloneSetupCard(c)
		return
	}
	t := c.Theme()
	s := &m.cloneUI
	s.mu.Lock()
	busy, rec, start, errText, note := s.busy, s.rec, s.recStart, s.err, s.note
	s.mu.Unlock()
	ui.Column(c).Padding(14, 18).Gap(8).Border(1, t.Border).Children(func() {
		ui.Text(c, "用我的声音朗读").FontSize(16).Bold()
		ui.Text(c, "录 8–15 秒英文，Repi 就能模仿你的音色。找个安静的地方，离麦克风一拳远，用平时说话的语速。可以读这段：").
			FontSize(12).TextColor(t.TextMuted).Wrap()
		ui.Text(c, cloneScript).FontSize(13).Wrap()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &m.cloneName).Placeholder("我的声音").Label("名字").Width(200)
			switch {
			case rec != nil:
				secs := int(time.Since(start).Seconds())
				if ui.PrimaryButton(c, fmt.Sprintf("■ 停止（%d 秒）", secs)).Label("停止录音").Clicked() {
					m.stopRecording()
				}
			default:
				if ui.PrimaryButton(c, "● 录音").Label("开始录音").Disabled(busy != "").Clicked() {
					m.startRecording()
				}
				if ui.Button(c, "导入音频…").Disabled(busy != "").Clicked() {
					m.importVoice()
				}
			}
		})
		if busy != "" && rec == nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, busy).FontSize(12).TextColor(t.TextMuted)
			})
		}
		if rec != nil {
			ui.Text(c, "最长 30 秒，录够 10 秒左右就可以停了。").FontSize(12).TextColor(t.TextMuted)
		}
		if errText != "" {
			ui.Text(c, errText).FontSize(12).TextColor(t.Danger).Wrap()
		}
		if note != "" {
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
		ui.Text(c, "录音只保存在本机。生成的语音带有 Chatterbox 的隐形水印。请只克隆你自己或得到同意的人的声音。").
			FontSize(11).TextColor(t.TextMuted).Wrap()
	})
}

// cloneSetupCard offers the one-click install of the voice-cloning engine.
func (m *model) cloneSetupCard(c *ui.Context) {
	m.checkGPU()
	t := c.Theme()
	s := &m.cloneSetup
	s.mu.Lock()
	running, p, errText, done := s.running, s.progress, s.err, s.done
	s.done = false
	s.mu.Unlock()
	k := &m.kokoroSetup
	k.mu.Lock()
	gpuOK := k.gpuOK
	k.mu.Unlock()
	if done {
		m.invalidate() // Check passes now; the next frame shows the recorder
	}
	ui.Column(c).Padding(14, 18).Gap(8).Border(1, t.Border).Children(func() {
		ui.Text(c, "安装声音克隆（Chatterbox）").FontSize(16).Bold()
		ui.Textf(c, "录 10 秒左右自己的声音，Repi 就能用你的声音朗读。需要再下载%s（模型和依赖，PyTorch 复用 Kokoro 的，不再下载），装好后占用磁盘最多约 9GB，位置在 %s。",
			setup.EstimateCloneSize(), m.clone.Dir).TextColor(t.TextMuted).Wrap()
		if !gpuOK {
			ui.Text(c, "没有检测到 NVIDIA GPU：可以用，但生成会慢很多，一句话可能要十几秒。").FontSize(12).TextColor(t.Warning).Wrap()
		}
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
			if ui.Button(c.Key("cancel-clone"), "取消").Clicked() {
				m.cancelSetup(s)
			}
			return
		}
		ui.Checkbox(c.Key("clone-mirror"), &k.mirror, "使用国内镜像")
		label := "安装声音克隆"
		if errText != "" {
			label = "重试"
		}
		if ui.PrimaryButton(c, label).Clicked() {
			m.startCloneSetup()
		}
	})
}
