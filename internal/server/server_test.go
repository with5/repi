package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// silentWAV writes a 16 kHz mono PCM WAV of d of silence.
func silentWAV(t *testing.T, path string, d time.Duration) {
	n := int(d.Seconds() * 16000 * 2)
	b := make([]byte, 44+n)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTest(t *testing.T) (*Server, *httptest.Server, *[]string) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "s.wav")
	silentWAV(t, wav, time.Second)
	var said []string
	s := &Server{
		Token: "secret",
		Voices: func() []Voice {
			return []Voice{{"kokoro:af_heart", "Heart", "", "en"}, {"kokoro:zf_xiaoxiao", "Xiaoxiao", "", "zh"}}
		},
		Synth: func(_ context.Context, sentence, voice string, _ float64) (string, error) {
			said = append(said, voice+"|"+sentence)
			return wav, nil
		},
		Dir: filepath.Join(dir, "jobs"),
	}
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	t.Cleanup(s.Close)
	return s, hs, &said
}

func do(t *testing.T, hs *httptest.Server, method, path, token string, body any) *http.Response {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, hs.URL+path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestToken(t *testing.T) {
	_, hs, _ := newTest(t)
	for _, tok := range []string{"", "wrong"} {
		if res := do(t, hs, "GET", "/api/v1/voices", tok, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("token %q: %d", tok, res.StatusCode)
		}
	}
	res := do(t, hs, "GET", "/api/v1/voices", "secret", nil)
	var v []Voice
	json.NewDecoder(res.Body).Decode(&v)
	if res.StatusCode != 200 || len(v) != 2 || v[1].Lang != "zh" {
		t.Fatalf("%d %+v", res.StatusCode, v)
	}
}

func TestSpeakChecksLanguage(t *testing.T) {
	_, hs, said := newTest(t)
	res := do(t, hs, "POST", "/api/v1/speak", "secret", request{Text: "你好。", Voice: "kokoro:af_heart"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("English voice reading Chinese: %d", res.StatusCode)
	}
	res = do(t, hs, "POST", "/api/v1/speak", "secret", request{Text: "你好。再见。"})
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "audio/wav" || !bytes.HasPrefix(b, []byte("RIFF")) {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if len(*said) != 1 || (*said)[0] != "kokoro:zf_xiaoxiao|你好。" {
		t.Fatalf("said %q", *said)
	}
}

func TestJob(t *testing.T) {
	_, hs, said := newTest(t)
	res := do(t, hs, "POST", "/api/v1/jobs", "secret", request{Text: "Hello there. How are you?", Voice: "kokoro:af_heart"})
	var j job
	json.NewDecoder(res.Body).Decode(&j)
	if res.StatusCode != http.StatusAccepted || j.ID == "" || j.Total != 2 {
		t.Fatalf("%d %+v", res.StatusCode, j)
	}
	deadline := time.Now().Add(5 * time.Second)
	for j.State == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		json.NewDecoder(do(t, hs, "GET", "/api/v1/jobs/"+j.ID, "secret", nil).Body).Decode(&j)
	}
	if j.State != "done" || j.Done != 2 {
		t.Fatalf("%+v", j)
	}
	res = do(t, hs, "GET", "/api/v1/jobs/"+j.ID+"/pack", "secret", nil)
	b, _ := io.ReadAll(res.Body)
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "audio/0001.wav,audio/0002.wav,manifest.json" {
		t.Fatalf("pack holds %v", names)
	}
	if len(*said) != 2 {
		t.Fatalf("said %q", *said)
	}
	if res := do(t, hs, "DELETE", "/api/v1/jobs/"+j.ID, "secret", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if res := do(t, hs, "GET", "/api/v1/jobs/"+j.ID, "secret", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: %d", res.StatusCode)
	}
}

func TestUnknownVoice(t *testing.T) {
	_, hs, _ := newTest(t)
	res := do(t, hs, "POST", "/api/v1/jobs", "secret", request{Text: "Hi.", Voice: "nope"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("%d", res.StatusCode)
	}
}

func TestMaterialJob(t *testing.T) {
	s, hs, said := newTest(t)
	s.ID = "pc1"
	s.Materials = func() []Material {
		return []Material{{ID: 7, Title: "Story", Lang: "en", Sentences: 1, Hash: TextHash("One line."), Text: "One line."}}
	}
	s.Defaults = func(lang string) (string, float64) { return "kokoro:af_heart", 1.1 }

	res := do(t, hs, "GET", "/api/v1/materials", "secret", nil)
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(b), "One line.") || !strings.Contains(string(b), `"id":7`) {
		t.Fatalf("list: %d %s", res.StatusCode, b)
	}
	if res := do(t, hs, "POST", "/api/v1/materials/8/jobs", "secret", map[string]any{}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing material: %d", res.StatusCode)
	}
	res = do(t, hs, "POST", "/api/v1/materials/7/jobs", "secret", map[string]any{})
	var j job
	json.NewDecoder(res.Body).Decode(&j)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("%d %+v", res.StatusCode, j)
	}
	deadline := time.Now().Add(5 * time.Second)
	for j.State == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		json.NewDecoder(do(t, hs, "GET", "/api/v1/jobs/"+j.ID, "secret", nil).Body).Decode(&j)
	}
	if j.State != "done" {
		t.Fatalf("%+v", j)
	}
	res = do(t, hs, "GET", "/api/v1/jobs/"+j.ID+"/pack", "secret", nil)
	b, _ = io.ReadAll(res.Body)
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var man struct {
		Title  string
		Speed  float64
		Source struct {
			Server string
			ID     int
			Hash   string
		}
	}
	for _, f := range z.File {
		if f.Name == "manifest.json" {
			r, _ := f.Open()
			json.NewDecoder(r).Decode(&man)
			r.Close()
		}
	}
	if man.Title != "Story" || man.Speed != 1.1 || man.Source.Server != "pc1" || man.Source.ID != 7 || man.Source.Hash != TextHash("One line.") {
		t.Fatalf("manifest %+v", man)
	}
	if len(*said) != 1 || (*said)[0] != "kokoro:af_heart|One line." {
		t.Fatalf("said %q", *said)
	}
}
