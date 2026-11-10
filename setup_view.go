package main

import (
	"context"
	"errors"
	"sync"

	"github.com/egoist/mygo/ui"

	"repi/internal/setup"
)

// setupState is a one-click install (Kokoro, or the voice-cloning engine),
// which runs on another goroutine.
type setupState struct {
	// mirror and gpu are the checkboxes; only the UI thread touches them,
	// and gpuAsked, set once the GPU check has started.
	mirror, gpu, gpuAsked bool

	mu       sync.Mutex
	gpuOK    bool // an NVIDIA GPU PyTorch has CUDA builds for
	running  bool
	done     bool // finished; applySetup takes it on the UI thread
	progress setup.Progress
	err      string
	cancel   context.CancelFunc
}

// initSetup restores the mirror choice.
func (m *model) initSetup(mirror bool) { m.kokoroSetup.mirror = mirror }

// checkGPU looks for a GPU in the background the first time the card is
// shown (nvidia-smi can take a moment).
func (m *model) checkGPU() {
	if m.kokoroSetup.gpuAsked {
		return
	}
	m.kokoroSetup.gpuAsked = true
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
	k := &m.kokoroSetup
	m.startInstall(k, setup.Options{Dir: m.kokoro.Dir, Mirror: k.mirror, GPU: k.gpu && k.gpuOK})
}

// startCloneSetup installs the voice-cloning engine next to Kokoro. The
// mirror choice is Kokoro's checkbox, shared by both cards.
func (m *model) startCloneSetup() {
	m.startInstall(&m.cloneSetup, setup.Options{Dir: m.kokoro.Dir, Clone: m.clone.Dir, Mirror: m.kokoroSetup.mirror})
}

// startInstall runs the install o in the background, reporting into s.
func (m *model) startInstall(s *setupState, o setup.Options) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	steps := 6
	if o.Zh {
		steps = 4
	}
	s.running, s.err, s.cancel, s.progress = true, "", cancel, setup.Progress{Step: 1, Steps: steps, Title: "准备"}
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

func (m *model) cancelSetup(s *setupState) {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// applySetup, on the UI thread, puts a finished install to use: Kokoro's
// voices, or its Mandarin ones, appear by themselves (Check and checkZh pass
// now); star one of each language so the reader offers it, and pick them
// where the voice in use is gone.
func (m *model) applySetup() {
	kokoro, zh := m.kokoroSetup.take(), m.zhSetup.take()
	if !kokoro && !zh {
		return
	}
	if kokoro && m.setupErr == noTTSErr {
		m.setupErr = ""
	}
	m.checkZh() // a new Kokoro reads Chinese as well
	if kokoro {
		m.favorite(defaultVoice)
	}
	if m.zhOK {
		m.favorite(defaultZhVoice)
	}
	m.repickVoices() // also saves
}

// take reports whether the install finished since it was last asked.
func (s *setupState) take() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	done := s.done
	s.done = false
	return done
}

// startZhSetup adds Chinese support to the installed Kokoro.
func (m *model) startZhSetup() {
	m.startInstall(&m.zhSetup, setup.Options{Dir: m.kokoro.Dir, Zh: true, Mirror: m.kokoroSetup.mirror})
}

// zhSetupCard offers Chinese support while Kokoro, installed before Repi
// read Chinese, cannot speak it.
func (m *model) zhSetupCard(c *ui.Context) {
	if m.kokoro.Check() != nil || m.zhOK {
		return
	}
	t := c.Theme()
	s := &m.zhSetup
	s.mu.Lock()
	running, p, errText := s.running, s.progress, s.err
	s.mu.Unlock()
	ui.Column(c.Key("zh-setup")).Padding(14, 18).Gap(8).Border(1, t.Border).Children(func() {
		ui.Text(c, "安装中文朗读").FontSize(16).Bold()
		ui.Textf(c, "让 Kokoro 朗读中文资料：再下载%s（中文分词和 8 个普通话音色），装进现有的 Kokoro 环境，英文朗读不受影响。",
			setup.EstimateZhSize()).TextColor(t.TextMuted).Wrap()
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
			if ui.Button(c.Key("cancel-zh"), "取消").Clicked() {
				m.cancelSetup(s)
			}
			return
		}
		ui.Checkbox(c.Key("zh-mirror"), &m.kokoroSetup.mirror, "使用国内镜像")
		label := "安装中文朗读"
		if errText != "" {
			label = "重试"
		}
		if ui.PrimaryButton(c, label).Clicked() {
			m.startZhSetup()
		}
	})
}

// kokoroSetupCard offers the one-click install while Kokoro is missing.
func (m *model) kokoroSetupCard(c *ui.Context) {
	if m.kokoro.Check() == nil {
		return
	}
	m.checkGPU()
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
				m.cancelSetup(s)
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
