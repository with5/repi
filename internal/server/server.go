// Package server lets other devices use this computer's voices over HTTP:
// the phone lists the voices, auditions one, and has a text turned into a
// .repi pack that it then plays as if it had been exported here.
//
// Every request needs "Authorization: Bearer <token>".
//
//	GET    /api/v1/info              {"name":"repi","version":"…","format":1}
//	GET    /api/v1/voices            [{"name","label","detail","lang"}]
//	POST   /api/v1/speak             {"text","voice","speed"} → audio (Ogg Opus or WAV)
//	POST   /api/v1/jobs              {"title","text","voice","speed","translate"} → {"id"}
//	GET    /api/v1/jobs/{id}         {"id","state":"running"|"done"|"failed","done","total","error"}
//	GET    /api/v1/jobs/{id}/pack    the .repi file, once done
//	DELETE /api/v1/jobs/{id}         cancels the job and frees its file
//	GET    /api/v1/materials         [{"id","title","lang","sentences","updated","hash"}]
//	POST   /api/v1/materials/{id}/jobs  {"voice","speed","translate"} → {"id"}
//	GET    /api/v1/news/sources      [{"id","name","url"}]
//	GET    /api/v1/news?source=a,b&refresh=1  {"items":[{"source","title","url","summary","published"}],"errors":{id:msg}}
//	GET    /api/v1/article?url=…     {"title","text"}: the body of a post, ready to be read
//
// A material's pack is read in the voice and speed given, or, when they are
// left out, in those the desktop app reads that material with, so the
// sentences it has already synthesized are not made again. Its manifest
// carries "source": {"server","id","hash"}, so the phone can tell which
// materials it has and whether the text has changed since.
package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"repi/internal/feed"
	"repi/internal/pack"
	"repi/internal/text"
)

// Voice is a voice the phone may ask for.
type Voice struct {
	Name   string `json:"name"` // what to send back as "voice"
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Lang   string `json:"lang"` // "en" or "zh"
}

// Material is a material of the desktop's library.
type Material struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Lang      string    `json:"lang"`
	Sentences int       `json:"sentences"`
	Updated   time.Time `json:"updated"`
	Hash      string    `json:"hash"` // of the text, see TextHash
	Text      string    `json:"-"`
}

// Server answers the API. Voices, Synth and Dir must be set.
type Server struct {
	Token   string
	Version string
	// ID tells this computer apart from others the phone may sync with.
	ID string
	// Materials lists the desktop's materials; nil when there is no library.
	Materials func() []Material
	// Defaults are the voice and speed the desktop reads material in lang with.
	Defaults func(lang string) (voice string, speed float64)
	// News lists the blogs' posts; nil turns the news off.
	News *feed.Reader
	// Article extracts the text of a web page; nil turns it off.
	Article func(ctx context.Context, url string) (title, text string, err error)
	// Voices lists the voices that can be used now.
	Voices func() []Voice
	// Synth returns the WAV file of a sentence, synthesizing it if need be.
	Synth func(ctx context.Context, sentence, voice string, speed float64) (string, error)
	// Translate returns the Chinese of an English sentence; it may be nil.
	Translate func(ctx context.Context, sentence string) (string, bool)
	Encoder   pack.Encoder // pack.WAV when nil
	Dir       string       // where the packs being made are written
	// Keep is how long a finished pack waits to be fetched, an hour when 0.
	Keep time.Duration

	mu   sync.Mutex
	jobs map[string]*job
	busy chan struct{} // one pack at a time: they share the GPU
}

type job struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Error  string `json:"error,omitempty"`
	path   string
	cancel context.CancelFunc
	ended  time.Time
}

const (
	maxText      = 200_000 // bytes of text in one job
	maxSentences = 3000
	maxBody      = maxText + 4096
)

// NewToken makes a random token.
func NewToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// LoadToken reads the token saved at path, making and saving one first if
// there is none.
func LoadToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	t := NewToken()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return t, os.WriteFile(path, []byte(t+"\n"), 0o600)
}

// Handler is the HTTP handler of the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", s.info)
	mux.HandleFunc("GET /api/v1/voices", s.voices)
	mux.HandleFunc("POST /api/v1/speak", s.speak)
	mux.HandleFunc("POST /api/v1/jobs", s.newJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.getJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/pack", s.getPack)
	mux.HandleFunc("DELETE /api/v1/jobs/{id}", s.deleteJob)
	mux.HandleFunc("GET /api/v1/materials", s.materials)
	mux.HandleFunc("POST /api/v1/materials/{id}/jobs", s.materialJob)
	mux.HandleFunc("GET /api/v1/news/sources", s.newsSources)
	mux.HandleFunc("GET /api/v1/news", s.news)
	mux.HandleFunc("GET /api/v1/article", s.article)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			fail(w, http.StatusUnauthorized, "令牌不对")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]any{"name": "repi", "id": s.ID, "version": s.Version, "format": pack.Format})
}

func (s *Server) voices(w http.ResponseWriter, _ *http.Request) {
	v := s.Voices()
	if v == nil {
		v = []Voice{}
	}
	reply(w, v)
}

// request is the body of speak and newJob.
type request struct {
	Title     string  `json:"title"`
	Text      string  `json:"text"`
	Voice     string  `json:"voice"`
	Speed     float64 `json:"speed"`
	Translate bool    `json:"translate"`
}

// parse reads the request and checks the voice can speak its text.
func (s *Server) parse(w http.ResponseWriter, r *http.Request) (request, string, bool) {
	req, ok := decode(w, r)
	if !ok {
		return req, "", false
	}
	lang, ok := s.check(w, &req)
	return req, lang, ok
}

func decode(w http.ResponseWriter, r *http.Request) (request, bool) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "请求格式不对："+err.Error())
		return req, false
	}
	return req, true
}

// check fills in what req leaves out and checks the voice can speak its
// text; it returns the text's language.
func (s *Server) check(w http.ResponseWriter, req *request) (string, bool) {
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		fail(w, http.StatusBadRequest, "没有文字")
		return "", false
	}
	if len(req.Text) > maxText {
		fail(w, http.StatusRequestEntityTooLarge, "文字太长")
		return "", false
	}
	if req.Speed <= 0 {
		req.Speed = 1
	}
	req.Speed = min(max(req.Speed, 0.5), 2)
	lang := "en"
	if text.IsChinese(req.Text) {
		lang = "zh"
	}
	var found *Voice
	var first *Voice
	voices := s.Voices()
	for i, v := range voices {
		if v.Name == req.Voice {
			found = &voices[i]
		}
		if first == nil && v.Lang == lang {
			first = &voices[i]
		}
	}
	switch {
	case req.Voice == "" && first != nil:
		req.Voice = first.Name
	case req.Voice == "":
		fail(w, http.StatusBadRequest, "电脑上没有能读"+langName(lang)+"的音色")
		return "", false
	case found == nil:
		fail(w, http.StatusBadRequest, fmt.Sprintf("电脑上没有音色 %q", req.Voice))
		return "", false
	case found.Lang != lang:
		fail(w, http.StatusBadRequest, fmt.Sprintf("%s 不能读%s", found.Label, langName(lang)))
		return "", false
	}
	return lang, true
}

func langName(lang string) string {
	if lang == "zh" {
		return "中文"
	}
	return "英语"
}

// speak returns the audio of a short text, for auditioning a voice.
func (s *Server) speak(w http.ResponseWriter, r *http.Request) {
	req, _, ok := s.parse(w, r)
	if !ok {
		return
	}
	sents := text.Split(req.Text)
	if len(sents) == 0 {
		fail(w, http.StatusBadRequest, "没有句子")
		return
	}
	// one sentence: an audition, not a material
	wav, err := s.Synth(r.Context(), sents[0], req.Voice, req.Speed)
	if err != nil {
		fail(w, http.StatusInternalServerError, "合成失败："+err.Error())
		return
	}
	enc := s.encoder()
	ctype := "audio/wav"
	if enc.Ext() == ".opus" {
		ctype = "audio/ogg"
	}
	w.Header().Set("Content-Type", ctype)
	if err := enc.Encode(r.Context(), wav, 0, w); err != nil {
		// headers are gone; all that is left is to cut the reply short
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) encoder() pack.Encoder {
	if s.Encoder == nil {
		return pack.WAV{}
	}
	return s.Encoder
}

// newJob starts making a pack of a text in the background.
func (s *Server) newJob(w http.ResponseWriter, r *http.Request) {
	req, lang, ok := s.parse(w, r)
	if !ok {
		return
	}
	s.start(w, req, lang, nil)
}

func (s *Server) materials(w http.ResponseWriter, _ *http.Request) {
	out := []Material{}
	if s.Materials != nil {
		out = append(out, s.Materials()...)
	}
	reply(w, out)
}

// materialJob starts making a pack of one of the desktop's materials.
func (s *Server) materialJob(w http.ResponseWriter, r *http.Request) {
	req, ok := decode(w, r)
	if !ok {
		return
	}
	var mat *Material
	if s.Materials != nil {
		for _, m := range s.Materials() {
			if fmt.Sprint(m.ID) == r.PathValue("id") {
				mat = &m
				break
			}
		}
	}
	if mat == nil {
		fail(w, http.StatusNotFound, "电脑上没有这篇材料了")
		return
	}
	req.Title, req.Text = mat.Title, mat.Text
	if s.Defaults != nil {
		voice, speed := s.Defaults(mat.Lang)
		if req.Voice == "" && s.hasVoice(voice, mat.Lang) {
			req.Voice = voice
		}
		if req.Speed <= 0 {
			req.Speed = speed
		}
	}
	lang, ok := s.check(w, &req)
	if !ok {
		return
	}
	s.start(w, req, lang, &pack.Origin{Server: s.ID, ID: mat.ID, Hash: mat.Hash})
}

func (s *Server) hasVoice(name, lang string) bool {
	for _, v := range s.Voices() {
		if v.Name == name && v.Lang == lang {
			return true
		}
	}
	return false
}

func (s *Server) newsSources(w http.ResponseWriter, _ *http.Request) {
	out := []feed.Source{}
	if s.News != nil {
		out = append(out, s.News.Sources...)
	}
	reply(w, out)
}

func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	if s.News == nil {
		reply(w, feed.Result{Items: []feed.Item{}})
		return
	}
	var ids []string
	if v := r.URL.Query().Get("source"); v != "" {
		ids = strings.Split(v, ",")
	}
	reply(w, s.News.Latest(r.Context(), ids, r.URL.Query().Get("refresh") == "1"))
}

func (s *Server) article(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if s.Article == nil {
		fail(w, http.StatusNotFound, "电脑上没有开启文章提取")
		return
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		fail(w, http.StatusBadRequest, "链接无效")
		return
	}
	title, body, err := s.Article(r.Context(), u)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	reply(w, map[string]string{"title": title, "text": body})
}

// TextHash is the hash of a material's text that tells the phone whether
// its copy is still the same text.
func TextHash(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:8])
}

// start makes the pack of req in the background and replies with the job.
func (s *Server) start(w http.ResponseWriter, req request, lang string, origin *pack.Origin) {
	sents := text.Split(req.Text)
	if len(sents) == 0 {
		fail(w, http.StatusBadRequest, "没有句子")
		return
	}
	if len(sents) > maxSentences {
		fail(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("句子太多（%d 句，最多 %d）", len(sents), maxSentences))
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = firstWords(sents[0], 36)
	}
	src := pack.Source{Title: title, Lang: lang, Voice: req.Voice, Speed: req.Speed, Sentences: sents, Origin: origin}
	src.Audio = func(ctx context.Context, i int) (string, error) {
		return s.Synth(ctx, sents[i], req.Voice, req.Speed)
	}
	if req.Translate {
		src.Translation = s.Translate
	}

	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ID: NewToken(), Title: title, State: "running", Total: len(sents), cancel: cancel}
	s.mu.Lock()
	s.sweepLocked()
	if s.jobs == nil {
		s.jobs = map[string]*job{}
		s.busy = make(chan struct{}, 1)
	}
	s.jobs[j.ID] = j
	busy := s.busy
	s.mu.Unlock()

	go func() {
		defer cancel()
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		case <-ctx.Done():
			s.finish(j, "", ctx.Err())
			return
		}
		path, err := s.write(ctx, src, func(d, _ int) {
			s.mu.Lock()
			j.Done = d
			s.mu.Unlock()
		})
		s.finish(j, path, err)
	}()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	reply(w, s.snapshot(j))
}

func (s *Server) write(ctx context.Context, src pack.Source, progress func(d, n int)) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.Dir, "job-*"+pack.Ext)
	if err != nil {
		return "", err
	}
	err = pack.Write(ctx, f, src, s.encoder(), progress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (s *Server) finish(j *job, path string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j.ended = time.Now()
	switch {
	case err != nil:
		j.State, j.Error = "failed", err.Error()
		if errors.Is(err, context.Canceled) {
			j.Error = "已取消"
		}
	default:
		j.State, j.path = "done", path
	}
	if _, ok := s.jobs[j.ID]; !ok && path != "" { // deleted while running
		os.Remove(path)
	}
}

// sweepLocked forgets the jobs that ended long ago, and their files.
func (s *Server) sweepLocked() {
	keep := s.Keep
	if keep == 0 {
		keep = time.Hour
	}
	for id, j := range s.jobs {
		if !j.ended.IsZero() && time.Since(j.ended) > keep {
			if j.path != "" {
				os.Remove(j.path)
			}
			delete(s.jobs, id)
		}
	}
}

func (s *Server) lookup(r *http.Request) *job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[r.PathValue("id")]
}

func (s *Server) snapshot(j *job) job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *j
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j := s.lookup(r)
	if j == nil {
		fail(w, http.StatusNotFound, "没有这个任务")
		return
	}
	reply(w, s.snapshot(j))
}

func (s *Server) getPack(w http.ResponseWriter, r *http.Request) {
	j := s.lookup(r)
	if j == nil {
		fail(w, http.StatusNotFound, "没有这个任务")
		return
	}
	snap := s.snapshot(j)
	if snap.State != "done" {
		fail(w, http.StatusConflict, "还没做好")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.repi")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", urlEscape(pack.FileName(snap.Title))))
	http.ServeFile(w, r, snap.path)
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	j := s.jobs[r.PathValue("id")]
	if j != nil {
		delete(s.jobs, j.ID)
		j.cancel()
		if j.path != "" {
			os.Remove(j.path)
		}
	}
	s.mu.Unlock()
	if j == nil {
		fail(w, http.StatusNotFound, "没有这个任务")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Close cancels the jobs and removes their files.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.jobs {
		j.cancel()
		if j.path != "" {
			os.Remove(j.path)
		}
		delete(s.jobs, id)
	}
}

func reply(w http.ResponseWriter, v any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	reply(w, map[string]string{"error": msg})
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// firstWords is the start of s, at most n runes, cut at a space if there is
// one near the end.
func firstWords(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}
