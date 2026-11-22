// Package pack writes a material as a .repi file for the phone app: a zip
// holding manifest.json and one audio file per sentence. Everything the phone
// needs is computed here, so it only plays: the sentences, their translations
// and when each word has been said.
//
// manifest.json (Format 1):
//
//	{
//	  "format": 1,
//	  "title": "…", "lang": "en" | "zh",
//	  "voice": "kokoro:af_heart", "speed": 1,
//	  "created": "2026-11-21T10:00:00Z",
//	  "gapMs": 350,                           // silence at the end of each audio file
//	  "source": {"server": "…", "id": 12, "hash": "…"}, // only from repi serve, see Origin
//	  "sentences": [{
//	    "text": "Hello, world.",
//	    "translation": "你好，世界。",          // omitted when there is none
//	    "audio": "audio/0001.opus",
//	    "durationMs": 1830,
//	    "tokens": [{"text": "Hello,", "endMs": 620},
//	               {"text": "world.", "space": true, "endMs": 1830}]
//	  }]
//	}
//
// A token's endMs is when it has been said, from the start of its audio file;
// the token being said at t is the first one whose endMs is after t.
// durationMs is the length of the speech file before the gap was added.
package pack

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"repi/internal/align"
)

// Format is the version of manifest.json. The phone refuses a newer one.
const Format = 1

// Gap is the silence added to the end of every sentence's audio, so the
// phone, playing them back to back, pauses between sentences as the desktop
// player does.
const Gap = 350 * time.Millisecond

// Ext is the extension of a pack file.
const Ext = ".repi"

// Manifest is manifest.json.
type Manifest struct {
	Format    int        `json:"format"`
	Title     string     `json:"title"`
	Lang      string     `json:"lang"`
	Voice     string     `json:"voice"`
	Speed     float64    `json:"speed"`
	Created   time.Time  `json:"created"`
	GapMs     int64      `json:"gapMs"`
	Source    *Origin    `json:"source,omitempty"`
	Sentences []Sentence `json:"sentences"`
}

// Origin is the desktop material a pack was made of, when repi serve made
// it for the phone: the server's ID, the material's ID and the hash of its
// text, so the phone can tell when the desktop's text has changed.
type Origin struct {
	Server string `json:"server"`
	ID     int    `json:"id"`
	Hash   string `json:"hash"`
}

// Sentence is one sentence and its audio.
type Sentence struct {
	Text        string  `json:"text"`
	Translation string  `json:"translation,omitempty"`
	Audio       string  `json:"audio"`
	DurationMs  int64   `json:"durationMs"`
	Tokens      []Token `json:"tokens"`
}

// Token is a word as shown on screen (a character in Chinese).
type Token struct {
	Text  string `json:"text"`
	Space bool   `json:"space,omitempty"`
	EndMs int64  `json:"endMs"`
}

// Source is the material to pack.
type Source struct {
	Title, Lang, Voice string
	Speed              float64
	Sentences          []string
	Origin             *Origin // nil for an export
	// Audio returns the WAV file of sentence i, synthesizing it if need be.
	Audio func(ctx context.Context, i int) (string, error)
	// Translation returns the Chinese of a sentence, if there is one. It may
	// be nil.
	Translation func(ctx context.Context, sentence string) (string, bool)
}

// Encoder turns a WAV file into the audio stored in the pack, with gap of
// silence added to its end.
type Encoder interface {
	Ext() string // with the dot
	Encode(ctx context.Context, wav string, gap time.Duration, w io.Writer) error
}

// WAV stores the WAV files, padded with silence.
type WAV struct{}

func (WAV) Ext() string { return ".wav" }

func (WAV) Encode(_ context.Context, wav string, gap time.Duration, w io.Writer) error {
	b, err := os.ReadFile(wav)
	if err != nil {
		return err
	}
	_, err = w.Write(padWAV(b, gap))
	return err
}

// padWAV appends gap of silence to a PCM WAV whose data chunk comes last, as
// the TTS engines write them. Any other file is returned as it is.
func padWAV(b []byte, gap time.Duration) []byte {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return b
	}
	var byteRate, align int
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:off+8]))
		body := off + 8
		switch {
		case id == "fmt " && body+16 <= len(b):
			byteRate = int(binary.LittleEndian.Uint32(b[body+8:]))
			align = int(binary.LittleEndian.Uint16(b[body+12:]))
		case id == "data" && body+size == len(b) && byteRate > 0 && align > 0:
			pad := int(gap.Seconds()*float64(byteRate)) / align * align
			out := make([]byte, len(b)+pad) // zeros: silence in PCM and float
			copy(out, b)
			binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
			binary.LittleEndian.PutUint32(out[off+4:], uint32(size+pad))
			return out
		}
		off = body + size + size%2
	}
	return b
}

// Opus encodes speech to Ogg Opus with ffmpeg, about a tenth of the WAV.
type Opus struct{ FFmpeg string }

func (Opus) Ext() string { return ".opus" }

func (o Opus) Encode(ctx context.Context, wav string, gap time.Duration, w io.Writer) error {
	cmd := exec.CommandContext(ctx, o.FFmpeg, "-hide_banner", "-loglevel", "error",
		"-i", wav, "-af", fmt.Sprintf("apad=pad_dur=%.3f", gap.Seconds()),
		"-ac", "1", "-c:a", "libopus", "-b:a", "32k", "-application", "voip",
		"-f", "ogg", "pipe:1")
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg: %s", msg)
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}

// BestEncoder is Opus when an ffmpeg with libopus is on PATH, else WAV.
func BestEncoder() Encoder {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return WAV{}
	}
	out, err := exec.Command(bin, "-hide_banner", "-encoders").Output()
	if err != nil || !strings.Contains(string(out), "libopus") {
		return WAV{}
	}
	return Opus{FFmpeg: bin}
}

// ErrEmpty is returned for a material without sentences.
var ErrEmpty = errors.New("没有可导出的句子")

// Write packs src into w. progress, when not nil, is called after each
// sentence with how many are done.
func Write(ctx context.Context, w io.Writer, src Source, enc Encoder, progress func(done, total int)) error {
	n := len(src.Sentences)
	if n == 0 {
		return ErrEmpty
	}
	zw := zip.NewWriter(w)
	man := Manifest{
		Format: Format, Title: src.Title, Lang: src.Lang, Voice: src.Voice,
		Speed: src.Speed, Created: time.Now().UTC().Truncate(time.Second),
		GapMs: Gap.Milliseconds(), Source: src.Origin,
	}
	// Opus is already compressed; a WAV gains from deflate.
	method := zip.Store
	if _, ok := enc.(WAV); ok {
		method = zip.Deflate
	}
	for i, text := range src.Sentences {
		if err := ctx.Err(); err != nil {
			return err
		}
		wav, err := src.Audio(ctx, i)
		if err != nil {
			return fmt.Errorf("第 %d 句合成失败：%w", i+1, err)
		}
		sp, err := align.SpeechOf(wav)
		if err != nil {
			return fmt.Errorf("第 %d 句的音频无法读取：%w", i+1, err)
		}
		name := fmt.Sprintf("audio/%04d%s", i+1, enc.Ext())
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: man.Created})
		if err != nil {
			return err
		}
		if err := enc.Encode(ctx, wav, Gap, fw); err != nil {
			return fmt.Errorf("第 %d 句编码失败：%w", i+1, err)
		}
		s := Sentence{Text: text, Audio: name, DurationMs: sp.Total.Milliseconds(), Tokens: Tokens(text, sp)}
		if src.Translation != nil && src.Lang != "zh" {
			if tr, ok := src.Translation(ctx, text); ok {
				s.Translation = tr
			}
		}
		man.Sentences = append(man.Sentences, s)
		if progress != nil {
			progress(i+1, n)
		}
	}
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Deflate, Modified: man.Created})
	if err != nil {
		return err
	}
	enc2 := json.NewEncoder(fw)
	enc2.SetEscapeHTML(false)
	if err := enc2.Encode(man); err != nil {
		return err
	}
	return zw.Close()
}

// Tokens are the words of sentence with when each has been said, as the big
// screen of the desktop times them.
func Tokens(sentence string, sp align.Speech) []Token {
	toks := align.Tokens(sentence)
	ends := align.Align(toks, sp)
	out := make([]Token, len(toks))
	for i, t := range toks {
		out[i] = Token{Text: t.Text, Space: t.Space, EndMs: ends[i].Milliseconds()}
	}
	return out
}

// FileName is a file name for a pack of the material titled title.
func FileName(title string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r < ' ', strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "repi"
	}
	return name + Ext
}
