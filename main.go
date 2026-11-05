// repi plays English study material aloud with a local TTS model, sentence by
// sentence, over and over, so the ear gets used to it.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"repi/internal/player"
)

func main() {
	audio.SetLatency(audio.LatencyPlayback) // long listening: ride out hiccups
	m := newModel(player.SoundCard{})
	mygo.App.OnQuit(func() {
		m.saveProgress(true)
		m.pl.Close()
		m.kokoro.Close()
	})
	mygo.App.WhenReady(func() {
		m.win = mygo.NewWindow(mygo.WindowOptions{
			Title:   "Repi · 磨耳朵",
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
