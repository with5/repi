package pack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeWAV writes a 16-bit mono WAV: lead seconds of silence, a tone, a tail.
func writeWAV(t *testing.T, path string, lead, tone, tail float64) {
	t.Helper()
	const rate = 24000
	n := func(s float64) int { return int(s * rate) }
	frames := n(lead) + n(tone) + n(tail)
	data := make([]byte, frames*2)
	for i := n(lead); i < n(lead)+n(tone); i++ {
		v := int16(8000 * math.Sin(2*math.Pi*220*float64(i)/rate))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], rate)
	binary.LittleEndian.PutUint32(h[28:], rate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func source(t *testing.T, sents ...string) Source {
	dir := t.TempDir()
	var paths []string
	for i := range sents {
		p := filepath.Join(dir, string(rune('a'+i))+".wav")
		writeWAV(t, p, 0.1, 1.0+float64(i)*0.5, 0.1)
		paths = append(paths, p)
	}
	return Source{
		Title: "Test", Lang: "en", Voice: "kokoro:af_heart", Speed: 1,
		Sentences: sents,
		Audio:     func(_ context.Context, i int) (string, error) { return paths[i], nil },
		Translation: func(_ context.Context, s string) (string, bool) {
			if s == "Hello there." {
				return "你好。", true
			}
			return "", false
		},
	}
}

func readPack(t *testing.T, b []byte) (Manifest, map[string][]byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	var m Manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m, files
}

func TestWriteWAV(t *testing.T) {
	src := source(t, "Hello there.", "How are you today?")
	var buf bytes.Buffer
	var steps []int
	err := Write(context.Background(), &buf, src, WAV{}, func(d, n int) { steps = append(steps, d) })
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[1] != 2 {
		t.Errorf("progress = %v", steps)
	}
	m, files := readPack(t, buf.Bytes())
	if m.Format != Format || m.Title != "Test" || m.Lang != "en" || len(m.Sentences) != 2 {
		t.Fatalf("manifest = %+v", m)
	}
	s0, s1 := m.Sentences[0], m.Sentences[1]
	if s0.Translation != "你好。" || s1.Translation != "" {
		t.Errorf("translations = %q, %q", s0.Translation, s1.Translation)
	}
	if s0.Audio != "audio/0001.wav" || len(files[s0.Audio]) == 0 {
		t.Errorf("audio %q missing", s0.Audio)
	}
	if s0.DurationMs < 1150 || s0.DurationMs > 1250 {
		t.Errorf("duration = %d ms", s0.DurationMs)
	}
	if m.GapMs != Gap.Milliseconds() {
		t.Errorf("gapMs = %d", m.GapMs)
	}
	// 1.2 s of 16-bit mono at 24 kHz, plus the gap
	if want := 44 + (28800+int(Gap.Seconds()*24000))*2; len(files[s0.Audio]) != want {
		t.Errorf("padded WAV is %d bytes, want %d", len(files[s0.Audio]), want)
	}
	if len(s1.Tokens) != 4 || s1.Tokens[0].Space || !s1.Tokens[1].Space {
		t.Fatalf("tokens = %+v", s1.Tokens)
	}
	prev := int64(0)
	for _, tk := range s1.Tokens {
		if tk.EndMs < prev || tk.EndMs > s1.DurationMs {
			t.Errorf("token %q ends at %d (after %d, of %d)", tk.Text, tk.EndMs, prev, s1.DurationMs)
		}
		prev = tk.EndMs
	}
}

func TestWriteChineseSkipsTranslation(t *testing.T) {
	src := source(t, "Hello there.")
	src.Lang = "zh"
	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, src, WAV{}, nil); err != nil {
		t.Fatal(err)
	}
	m, _ := readPack(t, buf.Bytes())
	if m.Sentences[0].Translation != "" {
		t.Error("Chinese material got a translation")
	}
}

func TestWriteErrors(t *testing.T) {
	if err := Write(context.Background(), io.Discard, Source{}, WAV{}, nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty: %v", err)
	}
	src := source(t, "One.")
	src.Audio = func(context.Context, int) (string, error) { return "", errors.New("boom") }
	if err := Write(context.Background(), io.Discard, src, WAV{}, nil); err == nil {
		t.Error("synthesis failure not reported")
	}
}

func TestOpus(t *testing.T) {
	enc, ok := BestEncoder().(Opus)
	if !ok {
		t.Skip("no ffmpeg with libopus")
	}
	src := source(t, "Hello there.")
	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, src, enc, nil); err != nil {
		t.Fatal(err)
	}
	m, files := readPack(t, buf.Bytes())
	b := files[m.Sentences[0].Audio]
	if m.Sentences[0].Audio != "audio/0001.opus" || !bytes.HasPrefix(b, []byte("OggS")) {
		t.Errorf("audio %q is not Ogg (%d bytes)", m.Sentences[0].Audio, len(b))
	}
}

func TestFileName(t *testing.T) {
	for in, want := range map[string]string{
		"A/B: c?":  "A_B_ c_.repi",
		"  ":       "repi.repi",
		"英语听力 第一课": "英语听力 第一课.repi",
		"..hidden": "hidden.repi",
	} {
		if got := FileName(in); got != want {
			t.Errorf("FileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPadWAVLeavesOthers(t *testing.T) {
	junk := []byte("not a wav file at all")
	if got := padWAV(junk, Gap); !bytes.Equal(got, junk) {
		t.Error("non-WAV changed")
	}
}
