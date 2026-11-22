package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"repi/internal/article"
	"repi/internal/feed"
	"repi/internal/pack"
	"repi/internal/server"
	"repi/internal/store"
	"repi/internal/text"
	"repi/internal/translate"
	"repi/internal/tts"
)

// defaultPort is where `repi serve` listens unless told otherwise.
const defaultPort = 7849

// serve runs `repi serve`: no window, only the API that lets the phone use
// this computer's voices. It shares the audio cache with the desktop app.
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", fmt.Sprintf(":%d", defaultPort), "address to listen on")
	newToken := fs.Bool("new-token", false, "replace the saved token with a new one")
	if err := fs.Parse(args); err != nil {
		return err
	}

	data := store.DataDir()
	tokenFile := filepath.Join(data, "server-token")
	if *newToken {
		os.Remove(tokenFile)
	}
	token, err := server.LoadToken(tokenFile)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	id, err := server.LoadToken(filepath.Join(data, "server-id"))
	if err != nil {
		return fmt.Errorf("server id: %w", err)
	}

	audioDir := filepath.Join(store.CacheDir(), "audio")
	m := &model{
		piper:  &tts.Piper{Cache: audioDir, Voices: tts.ListVoices(tts.DefaultVoiceDirs...)},
		kokoro: &tts.Kokoro{Dir: filepath.Join(data, "kokoro"), Cache: audioDir},
		clone:  &tts.Clone{Dir: filepath.Join(data, "clone"), Voices: filepath.Join(data, "voices"), Cache: audioDir},
	}
	m.synth = &tts.Router{Piper: m.piper, Kokoro: m.kokoro, Clone: m.clone}
	m.pool = &tts.Pool{S: m.synth}
	defer m.kokoro.Close()
	defer m.clone.Close()
	var mu sync.Mutex // guards m, which requests share
	tr := &translate.Client{Dir: filepath.Join(store.CacheDir(), "translate")}

	srv := &server.Server{
		Token:     token,
		Version:   version,
		ID:        id,
		Materials: libraryMaterials,
		Defaults:  readingDefaults,
		News:      &feed.Reader{Sources: feed.Sources},
		Article: func(ctx context.Context, u string) (string, string, error) {
			a, err := article.Fetch(ctx, u)
			return a.Title, a.Text, err
		},
		Voices: func() []server.Voice {
			mu.Lock()
			defer mu.Unlock()
			m.checkZh() // the desktop may have installed Chinese meanwhile
			var out []server.Voice
			for _, v := range m.voiceList() {
				out = append(out, server.Voice{Name: v.Name, Label: v.Label, Detail: v.Detail, Lang: tts.VoiceLang(v.Name)})
			}
			return out
		},
		Synth: m.pool.Get,
		Translate: func(ctx context.Context, s string) (string, bool) {
			if t, ok := tr.Cached(s); ok {
				return t, true
			}
			t, err := tr.Translate(ctx, s)
			return t, err == nil && t != ""
		},
		Encoder: pack.BestEncoder(),
		Dir:     filepath.Join(store.CacheDir(), "server"),
	}
	defer srv.Close()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	n := len(srv.Voices())
	fmt.Printf("Repi %s 语音合成服务已启动，%d 个音色可用\n\n", version, n)
	fmt.Println("在手机 Repi 的「电脑合成」里填：")
	for _, ip := range lanIPs() {
		fmt.Printf("  地址  http://%s:%d\n", ip, port)
	}
	fmt.Printf("  令牌  %s\n\n", token)
	fmt.Println("手机用 USB 连着时，也可以先运行 adb reverse tcp:" + fmt.Sprint(port) + " tcp:" + fmt.Sprint(port) +
		"，地址填 http://127.0.0.1:" + fmt.Sprint(port))
	if n == 0 {
		fmt.Println("\n注意：现在没有可用的音色，先在桌面版 Repi 里安装 Kokoro。")
	}

	hs := &http.Server{
		Handler:           logRequests(srv.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sctx)
	}()
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// lanIPs are this computer's IPv4 addresses other devices may reach.
func lanIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			out = append(out, n.IP.String())
		}
	}
	if len(out) == 0 {
		out = []string{"<本机 IP>"}
	}
	return out
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		h.ServeHTTP(w, r)
		if r.Method != http.MethodGet || !isPoll(r.URL.Path) {
			fmt.Printf("%s %s %s %s\n", t.Format("15:04:05"), r.RemoteAddr, r.Method, r.URL.Path)
		}
	})
}

// isPoll is a job's progress, which the phone asks for every second.
func isPoll(p string) bool {
	d, f := filepath.Split(p)
	return filepath.Base(filepath.Clean(d)) == "jobs" && f != ""
}

// libraryMaterials lists the desktop's materials, newest first. The file is
// read on every call, so what the desktop app saved meanwhile shows up.
func libraryMaterials() []server.Material {
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		return nil
	}
	var out []server.Material
	for _, m := range st.List() {
		lang := "en"
		if text.IsChinese(m.Text) {
			lang = "zh"
		}
		out = append(out, server.Material{
			ID: m.ID, Title: m.Title, Lang: lang, Sentences: len(text.Split(m.Text)),
			Updated: m.Updated, Hash: server.TextHash(m.Text), Text: m.Text,
		})
	}
	return out
}

// readingDefaults are the voice and speed the desktop app reads lang with.
func readingDefaults(lang string) (string, float64) {
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		return "", 1
	}
	s := st.Settings()
	voice := s.Voice
	if voice == "" {
		voice = defaultVoice
	}
	if lang == "zh" {
		voice = s.ZhVoice
		if voice == "" {
			voice = defaultZhVoice
		}
	}
	if s.Speed <= 0 {
		s.Speed = 1
	}
	return voice, s.Speed
}
