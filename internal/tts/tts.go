// Package tts turns sentences into speech with the local piper-tts binary and
// caches the WAV files on disk.
package tts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Synth makes the audio of one sentence and returns the path of a WAV file.
type Synth interface {
	Synth(ctx context.Context, sentence, voice string, speed float64) (string, error)
}

// Voice is a piper voice model on disk.
type Voice struct {
	Name  string // "en_US-amy-medium"
	Model string // path of the .onnx file
}

// DefaultVoiceDirs are searched for piper voices.
var DefaultVoiceDirs = []string{"/usr/share/piper-voices/en", os.ExpandEnv("$HOME/.local/share/piper-voices")}

// ListVoices finds *.onnx voice models under dirs, sorted by name.
func ListVoices(dirs ...string) []Voice {
	var out []Voice
	seen := map[string]bool{}
	for _, d := range dirs {
		filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && strings.HasSuffix(p, ".onnx") {
				name := strings.TrimSuffix(filepath.Base(p), ".onnx")
				if !seen[name] {
					seen[name] = true
					out = append(out, Voice{Name: name, Model: p})
				}
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Piper runs piper-tts, one process per sentence, caching results in Cache.
type Piper struct {
	Bin    string // path of piper-tts, "piper-tts" if empty
	Cache  string // directory of cached WAV files
	Voices []Voice
}

// Check reports why Piper cannot work, or nil.
func (p *Piper) Check() error {
	if _, err := exec.LookPath(p.bin()); err != nil {
		return fmt.Errorf("找不到 %s，请安装 piper-tts", p.bin())
	}
	if len(p.Voices) == 0 {
		return errors.New("没有找到 piper 音色（/usr/share/piper-voices）")
	}
	return nil
}

func (p *Piper) bin() string {
	if p.Bin == "" {
		return "piper-tts"
	}
	return p.Bin
}

// CachePath returns where the audio of a sentence is cached.
func (p *Piper) CachePath(sentence, voice string, speed float64) string {
	return filepath.Join(p.Cache, CacheKey(sentence, voice, speed)+".wav")
}

// CacheKey is the cache file name stem for a sentence, voice and speed.
func CacheKey(sentence, voice string, speed float64) string {
	h := sha256.Sum256([]byte(voice + "\x00" + strconv.FormatFloat(speed, 'f', 2, 64) + "\x00" + sentence))
	return hex.EncodeToString(h[:16])
}

// Cached reports whether the sentence is already synthesized.
func (p *Piper) Cached(sentence, voice string, speed float64) bool {
	_, err := os.Stat(p.CachePath(sentence, voice, speed))
	return err == nil
}

// Synth implements Synth. speed 1 is normal, 1.2 is 20% faster.
func (p *Piper) Synth(ctx context.Context, sentence, voice string, speed float64) (string, error) {
	out := p.CachePath(sentence, voice, speed)
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	var model string
	for _, v := range p.Voices {
		if v.Name == voice {
			model = v.Model
		}
	}
	if model == "" {
		return "", fmt.Errorf("未知音色 %q", voice)
	}
	if err := os.MkdirAll(p.Cache, 0o755); err != nil {
		return "", err
	}
	if speed <= 0 {
		speed = 1
	}
	tmp := out + ".tmp"
	cmd := exec.CommandContext(ctx, p.bin(), "-q", "-m", model, "-f", tmp,
		"--length_scale", strconv.FormatFloat(1/speed, 'f', 3, 64), "--sentence_silence", "0.1")
	cmd.Stdin = strings.NewReader(sentence + "\n")
	if b, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("piper-tts: %w: %s", err, strings.TrimSpace(string(b)))
	}
	if err := os.Rename(tmp, out); err != nil {
		return "", err
	}
	return out, nil
}
