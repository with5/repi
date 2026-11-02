package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// Omarchy keeps the active theme's palette in colors.toml and replaces the
// file when the theme changes, so the app reads it again every moment it is
// cheap to and follows the desktop without a restart.

const fallbackAccent = "#FFD60A"

// omarchyColorsPath is where Omarchy publishes the current theme.
func omarchyColorsPath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "omarchy", "current", "theme", "colors.toml")
}

// parseColors reads the flat `key = "value"` lines of colors.toml.
func parseColors(src string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(src, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if strings.HasPrefix(k, "#") || v == "" {
			continue
		}
		if i := strings.Index(v[1:], "\""); strings.HasPrefix(v, "\"") && i >= 0 {
			v = v[1 : i+1]
		}
		out[k] = v
	}
	return out
}

// blend lays fg at alpha over bg: Omarchy fills controls with the foreground
// at a few percent, which needs to be an opaque color to compare and draw.
func blend(bg, fg ui.Color, alpha float32) ui.Color {
	mix := func(b, f uint8) uint8 { return uint8(float32(b)*(1-alpha) + float32(f)*alpha + 0.5) }
	return ui.RGB(mix(bg.R, fg.R), mix(bg.G, fg.G), mix(bg.B, fg.B))
}

// hexOr parses a hex color, or returns def when it isn't one.
func hexOr(s, def string) ui.Color {
	if len(s) != 7 || s[0] != '#' {
		s = def
	}
	if _, err := strconv.ParseUint(s[1:], 16, 32); err != nil {
		s = def
	}
	return ui.Hex(s)
}

// omarchyTheme builds the mygo theme from a colors.toml. The look is
// Omarchy's: flat, square, hairline borders, controls filled with the
// foreground at 4/8/22 percent for rest/hover/pressed, the accent kept for
// what is current. Unknown or missing colors fall back to a dark palette.
func omarchyTheme(src, font string) *ui.Theme {
	c := parseColors(src)
	dark := c["mode"] != "light"
	bgDef, fgDef := "#101913", "#a1af9c"
	if !dark {
		bgDef, fgDef = "#f5f5f5", "#333333"
	}
	bg := hexOr(c["background"], bgDef)
	fg := hexOr(c["foreground"], fgDef)
	accent := hexOr(c["accent"], fallbackAccent)
	muted := blend(bg, fg, 0.65)
	if len(c["dark_foreground"]) == 7 {
		muted = hexOr(c["dark_foreground"], "#777777")
	}

	t := &ui.Theme{
		Dark:           dark,
		Background:     bg,
		Surface:        blend(bg, fg, 0.04),
		SurfaceHover:   blend(bg, fg, 0.08),
		SurfacePressed: blend(bg, fg, 0.22),
		Border:         blend(bg, fg, 0.4),
		Text:           fg,
		TextMuted:      muted,
		Accent:         accent,
		AccentHover:    blend(accent, fg, 0.15),
		AccentPressed:  blend(accent, bg, 0.2),
		AccentText:     bg,
		Danger:         hexOr(c["red"], "#c87a5c"),
		Warning:        hexOr(c["yellow"], "#c4a64e"),
		Success:        hexOr(c["green"], "#6aae52"),
		Selection:      accent.Alpha(0.35),
		Focus:          fg.Alpha(0.6),
		Scrollbar:      fg.Alpha(0.25),
		ScrollbarWidth: 6,
		Radius:         0, // Omarchy's windows are square unless the theme rounds them
		Spacing:        3,
		FontSize:       13,
		Font:           font,
	}
	return t
}

// omarchyFont is the monospace font the desktop uses, which Omarchy's
// terminals show; the Nerd Font it ships is the fallback.
func omarchyFont() string {
	if out, err := exec.Command("omarchy", "font", "current").Output(); err == nil {
		if f := strings.TrimSpace(string(out)); f != "" {
			return f
		}
	}
	return "CaskaydiaMono Nerd Font"
}

// themeSource keeps the theme in use and reloads it when colors.toml changes.
type themeSource struct {
	cur  atomic.Pointer[ui.Theme]
	raw  string
	font string
}

func newThemeSource() *themeSource {
	s := &themeSource{font: omarchyFont()}
	s.reload()
	return s
}

// reload reads colors.toml; it reports whether the theme changed.
func (s *themeSource) reload() bool {
	b, _ := os.ReadFile(omarchyColorsPath())
	if src := string(b); src != s.raw || s.cur.Load() == nil {
		s.raw = src
		s.cur.Store(omarchyTheme(src, s.font))
		return true
	}
	return false
}

// Theme is the theme to build the frame with.
func (s *themeSource) Theme() *ui.Theme { return s.cur.Load() }

// watch looks for a theme switch every second and calls changed when there
// is one. A theme switch replaces the file and symlinks, which a file watch
// would have to re-arm each time; reading a small file is simpler and cheap.
func (s *themeSource) watch(changed func()) {
	go func() {
		for range time.Tick(time.Second) {
			if s.reload() {
				changed()
			}
		}
	}()
}
