package store

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir holds the library, dictionary and TTS environment. On Linux it is
// ~/.local/share/repi (XDG_DATA_HOME aware, unchanged from before).
func DataDir() string {
	return filepath.Join(baseDir("XDG_DATA_HOME", ".local/share", "Library/Application Support", "LOCALAPPDATA"), "repi")
}

// CacheDir holds regenerable files: synthesized audio and translations.
func CacheDir() string {
	return filepath.Join(baseDir("XDG_CACHE_HOME", ".cache", "Library/Caches", "LOCALAPPDATA"), "repi")
}

func baseDir(xdgEnv, linuxRel, macRel, winEnv string) string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, macRel)
	case "windows":
		if d := os.Getenv(winEnv); d != "" {
			return d
		}
		return filepath.Join(home, "AppData", "Local")
	}
	if d := os.Getenv(xdgEnv); d != "" {
		return d
	}
	return filepath.Join(home, linuxRel)
}
