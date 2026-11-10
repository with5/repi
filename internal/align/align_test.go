package align

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestWeight(t *testing.T) {
	cases := map[string]float64{
		"a": 1, "the": 1, "make": 1, "table": 2, "container": 3, "containers": 3,
		"orchestration": 4, "open-source": 3, "Kubernetes": 4, "deployable": 3.5,
		"3": 1.2, "2024": 4.8, "—": 0, "Mr.": 1, "HTTP": 2,
	}
	for w, want := range cases {
		if got := weight(w); math.Abs(got-want) > 0.51 { // an estimate: within half a syllable
			t.Errorf("weight(%q) = %v, want about %v", w, got, want)
		}
	}
}

func TestTokensPauses(t *testing.T) {
	tk := Tokens("Well, it works. Really!")
	if len(tk) != 4 {
		t.Fatalf("tokens = %v", tk)
	}
	if tk[0].Pause == 0 || tk[2].Pause == 0 {
		t.Errorf("no stop after \"Well,\" or \"works.\": %v", tk)
	}
	if tk[3].Pause != 0 {
		t.Errorf("stop after the last word: %v", tk[3])
	}
}

func TestSpansAndIndex(t *testing.T) {
	tk := []Token{{Text: "a", Weight: 1}, {Text: "bb", Weight: 2}, {Text: "c", Weight: 1}}
	ends := Spans(tk, time.Second, 5*time.Second) // 1 s per syllable
	want := []time.Duration{2 * time.Second, 4 * time.Second, 5 * time.Second}
	for i := range want {
		if ends[i] != want[i] {
			t.Fatalf("ends = %v, want %v", ends, want)
		}
	}
	for pos, idx := range map[time.Duration]int{0: 0, 1999 * time.Millisecond: 0, 2 * time.Second: 1, 3500 * time.Millisecond: 1, 4 * time.Second: 2, 9 * time.Second: 2} {
		if got := Index(ends, pos); got != idx {
			t.Errorf("Index(%v) = %d, want %d", pos, got, idx)
		}
	}
	if Index(nil, 0) != -1 {
		t.Error("no tokens should give -1")
	}
}

// wav16 makes a mono 16-bit WAV: silence, a tone, silence.
func wav16(rate int, lead, tone, tail float64) []byte {
	n := func(s float64) int { return int(s * float64(rate)) }
	frames := n(lead) + n(tone) + n(tail)
	data := make([]byte, frames*2)
	for i := n(lead); i < n(lead)+n(tone); i++ {
		v := int16(8000 * math.Sin(2*math.Pi*220*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	return append(h, data...)
}

func TestMeasureWAV(t *testing.T) {
	sp, err := MeasureWAV(wav16(22050, 0.5, 1.0, 0.7))
	if err != nil {
		t.Fatal(err)
	}
	near := func(got, want time.Duration) bool {
		d := got - want
		return d > -80*time.Millisecond && d < 80*time.Millisecond
	}
	if !near(sp.Start, 500*time.Millisecond) || !near(sp.End, 1500*time.Millisecond) || !near(sp.Total, 2200*time.Millisecond) {
		t.Errorf("speech = %+v, want about 0.5 s .. 1.5 s of 2.2 s", sp)
	}
	if _, err := MeasureWAV([]byte("nope")); err == nil {
		t.Error("garbage accepted")
	}
}

// A silence found in the audio anchors the stop after a comma: the word
// before it ends where the silence begins, and the next starts where it ends.
func TestAlignAnchorsToSilence(t *testing.T) {
	tk := Tokens("One two, three four") // a stop after "two,"
	sp := Speech{Start: 0, End: 4 * time.Second, Total: 4 * time.Second,
		Gaps: []Gap{{Start: 1500 * time.Millisecond, End: 2000 * time.Millisecond}}}
	ends := Align(tk, sp)
	if len(ends) != 4 {
		t.Fatalf("ends = %v", ends)
	}
	if ends[1] != 2000*time.Millisecond { // "two," stays highlighted through its stop
		t.Errorf("end of \"two,\" = %v, want 2s (the end of the silence)", ends[1])
	}
	if ends[0] <= 0 || ends[0] >= 1500*time.Millisecond {
		t.Errorf("end of \"One\" = %v, want inside the first segment", ends[0])
	}
	if ends[3] != 4*time.Second || ends[2] <= 2000*time.Millisecond || ends[2] >= ends[3] {
		t.Errorf("last words: %v", ends[2:])
	}
	for i := 1; i < len(ends); i++ {
		if ends[i] < ends[i-1] {
			t.Errorf("ends go back: %v", ends)
		}
	}
	// no silence near the stop (far away): the estimate is kept
	far := Speech{Start: 0, End: 4 * time.Second, Total: 4 * time.Second, Gaps: []Gap{{Start: 3900 * time.Millisecond, End: 4000 * time.Millisecond}}}
	got, want := Align(tk, far), Spans(tk, 0, 4*time.Second)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("unmatched stop changed the estimate: %v vs %v", got, want)
			break
		}
	}
}

func TestTokensChinese(t *testing.T) {
	type tk struct {
		text  string
		space bool
		pause float64
	}
	got := Tokens("他说：“我用 iPhone，拍了 3.14 张。”好")
	want := []tk{
		{"他", false, 0}, {"说：", false, commaPause}, {"“我", false, 0}, {"用", false, 0},
		{"iPhone，", true, commaPause}, {"拍", false, 0}, {"了", false, 0},
		{"3.14", true, 0}, {"张。”", true, stopPause}, {"好", false, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Text != w.text || got[i].Space != w.space || got[i].Pause != w.pause {
			t.Errorf("token %d = %+v, want %+v", i, got[i], w)
		}
	}
	if got[0].Weight != 1 || got[4].Weight != 2 {
		t.Errorf("weights %v %v, want 1 and 2", got[0].Weight, got[4].Weight)
	}
}

func TestTokensEnglishSpace(t *testing.T) {
	got := Tokens("Hello there, world.")
	if len(got) != 3 || got[0].Space || !got[1].Space || !got[2].Space {
		t.Errorf("got %+v", got)
	}
}
