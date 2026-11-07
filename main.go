// repi plays English study material aloud with a local TTS model, sentence by
// sentence, over and over, so the ear gets used to it.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"repi/internal/player"
)

// version is this release; the git tag of a release is "v" + version.
const version = "0.3.1"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("repi", version)
		return
	}
	audio.SetLatency(audio.LatencyPlayback) // long listening: ride out hiccups
	m := newModel(player.SoundCard{})
	mygo.App.OnQuit(func() {
		m.saveProgress(true)
		m.pl.Close()
		m.st.Save()
		m.kokoro.Close()
	})
	mygo.App.WhenReady(func() {
		m.win = mygo.NewWindow(mygo.WindowOptions{
			Title:   "Repi " + version + " · 磨耳朵",
			Width:   1000,
			Height:  720,
			Content: ui.View(m.view),
		})
		m.theme.watch(m.invalidate)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
