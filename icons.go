package main

import "github.com/egoist/mygo/ui"

// Flat shapes for the transport buttons. They are drawn in the color of the
// text, so they follow the Omarchy theme like everything else.
var (
	iconPlay  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M4 2.5v11l9-5.5z"/></svg>`))
	iconPause = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M3.5 2.5h3.5v11H3.5zM9 2.5h3.5v11H9z"/></svg>`))
	iconPrev  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M3 2.5h2v11H3zM13 2.5v11L6 8z"/></svg>`))
	iconNext  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M11 2.5h2v11h-2zM3 2.5v11L10 8z"/></svg>`))
)

// iconPin marks a pinned material in the library.
var iconPin = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M8 1.5l2.1 4.4 4.9.6-3.6 3.4.9 4.8L8 12.3l-4.3 2.4.9-4.8L1 6.5l4.9-.6z"/></svg>`))
