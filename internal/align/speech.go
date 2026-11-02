package align

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"sync"
	"time"
)

// Gap is a stop inside the speech.
type Gap struct{ Start, End time.Duration }

// Speech is where the voice is in a WAV file, without the silence at both
// ends, and the stops inside it.
type Speech struct {
	Start, End, Total time.Duration
	Gaps              []Gap
}

// minGap is the shortest silence taken for a stop between words.
const minGap = 100 * time.Millisecond

var (
	speechMu    sync.Mutex
	speechCache = map[string]Speech{}
)

// SpeechOf measures the WAV file at path (16-bit PCM or 32-bit float). The
// results are kept. On a file it cannot read it returns an error.
func SpeechOf(path string) (Speech, error) {
	speechMu.Lock()
	sp, ok := speechCache[path]
	speechMu.Unlock()
	if ok {
		return sp, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Speech{}, err
	}
	sp, err = MeasureWAV(b)
	if err != nil {
		return Speech{}, err
	}
	speechMu.Lock()
	speechCache[path] = sp
	speechMu.Unlock()
	return sp, nil
}

// MeasureWAV finds the speech in the bytes of a WAV file: the first and last
// 10 ms windows louder than a fraction of the loudest one, widened by 30 ms.
func MeasureWAV(b []byte) (Speech, error) {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return Speech{}, errors.New("not a WAV file")
	}
	var format, channels, bits, rate int
	var data []byte
	for i := 12; i+8 <= len(b); {
		id, size := string(b[i:i+4]), int(binary.LittleEndian.Uint32(b[i+4:i+8]))
		body := b[i+8:]
		if size > len(body) || size < 0 {
			size = len(body) // a streamed file may claim more than it has
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return Speech{}, errors.New("short fmt chunk")
			}
			format = int(binary.LittleEndian.Uint16(body[0:2]))
			channels = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits = int(binary.LittleEndian.Uint16(body[14:16]))
		case "data":
			data = body[:size]
		}
		i += 8 + size + size&1
	}
	if channels < 1 || rate < 1 || data == nil {
		return Speech{}, errors.New("incomplete WAV file")
	}
	var sample func(frame int) float64 // mean absolute amplitude of a frame
	frameBytes := channels * bits / 8
	switch {
	case format == 1 && bits == 16:
		sample = func(f int) float64 {
			var sum float64
			for c := range channels {
				sum += math.Abs(float64(int16(binary.LittleEndian.Uint16(data[f*frameBytes+c*2:]))) / 32768)
			}
			return sum / float64(channels)
		}
	case format == 3 && bits == 32:
		sample = func(f int) float64 {
			var sum float64
			for c := range channels {
				sum += math.Abs(float64(math.Float32frombits(binary.LittleEndian.Uint32(data[f*frameBytes+c*4:]))))
			}
			return sum / float64(channels)
		}
	default:
		return Speech{}, errors.New("unsupported WAV format")
	}
	frames := len(data) / frameBytes
	total := time.Duration(float64(frames) / float64(rate) * float64(time.Second))
	win := max(rate/100, 1) // 10 ms
	n := frames / win
	if n == 0 {
		return Speech{Start: 0, End: total, Total: total}, nil
	}
	level := make([]float64, n)
	loudest := 0.0
	for w := range n {
		var sum float64
		for f := w * win; f < (w+1)*win; f++ {
			sum += sample(f)
		}
		level[w] = sum / float64(win)
		loudest = max(loudest, level[w])
	}
	threshold := max(loudest*0.06, 0.002)
	first, last := -1, -1
	for w, l := range level {
		if l >= threshold {
			if first < 0 {
				first = w
			}
			last = w
		}
	}
	if first < 0 {
		return Speech{Start: 0, End: total, Total: total}, nil
	}
	const pad = 30 * time.Millisecond
	start := max(time.Duration(first)*10*time.Millisecond-pad, 0)
	end := min(time.Duration(last+1)*10*time.Millisecond+pad, total)
	sp := Speech{Start: start, End: end, Total: total}
	quiet := loudest * 0.05
	for w := first; w <= last; {
		if level[w] >= quiet {
			w++
			continue
		}
		v := w
		for v <= last && level[v] < quiet {
			v++
		}
		if d := time.Duration(v-w) * 10 * time.Millisecond; d >= minGap {
			sp.Gaps = append(sp.Gaps, Gap{Start: time.Duration(w) * 10 * time.Millisecond, End: time.Duration(v) * 10 * time.Millisecond})
		}
		w = v
	}
	return sp, nil
}
