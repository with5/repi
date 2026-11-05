<div align="center">

<img src="assets/logo.svg" alt="Repi" width="420">

### Loop English aloud, sentence by sentence — until it clicks.

A local-first listening trainer. Paste any English text, and Repi reads it back with a neural TTS model, on repeat, with a word-by-word karaoke view, an offline dictionary and instant translation.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux-FCC624?style=flat-square&logo=linux&logoColor=black)](#installation)
[![TTS](https://img.shields.io/badge/TTS-Kokoro--82M-4a9a68?style=flat-square)](https://huggingface.co/hexgrad/Kokoro-82M)
[![Offline](https://img.shields.io/badge/works-offline-success?style=flat-square)](#why-repi)
[![Built with MyGo](https://img.shields.io/badge/built%20with-MyGo-8A2BE2?style=flat-square)](https://github.com/egoist/mygo)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen?style=flat-square)](https://github.com/with5/repi/pulls)
[![Stars](https://img.shields.io/github/stars/with5/repi?style=flat-square&logo=github)](https://github.com/with5/repi/stargazers)

**English** · [简体中文](README.zh-CN.md)

</div>

---

## Why Repi

Listening skill comes from hearing the same material many times, not from hearing a lot of different material once. Repi turns any text into a tireless, patient reader that you can leave running in the background.

- 🔁 **Loop modes** — whole-article loop, or repeat each sentence *N* times before moving on.
- 🗣️ **Natural voices** — 27 Kokoro-82M voices (US/UK, male/female), generated locally on GPU or CPU. [piper-tts](https://github.com/rhasspy/piper) is supported as a lightweight fallback.
- 🎤 **Karaoke big screen** — large type, the word being spoken is highlighted, unread words are dimmed.
- 📖 **Click any word to look it up** — playback pauses, a card shows IPA, Chinese definitions, exam tags (CET-4, IELTS…) and word forms. Inflections like `went` / `running` resolve to their lemma. Powered by an offline [ECDICT](https://github.com/skywind3000/ECDICT) index.
- 🌐 **Inline translation** — toggle a Chinese line under the current sentence; each sentence is translated once and cached.
- 🙈 **Hide text** — listen only, no peeking.
- ⚡ **Fast and light** — a single native binary written in Go, no Electron, no webview. The model is loaded once and kept resident.
- 🎨 **Follows your theme** — live-syncs with the current [Omarchy](https://omarchy.org) theme; falls back to a dark default elsewhere.
- 🔒 **Local-first** — audio, materials and progress all stay on your machine.

## Installation

### Requirements

- Linux
- [Go](https://go.dev/dl/) 1.27+
- At least one TTS backend (see below)

### 1. Set up a TTS backend

**Kokoro-82M** (default; great quality, fast on GPU and CPU). The ~330 MB model is downloaded on first use.

```sh
mkdir -p ~/.local/share/repi/kokoro && cd ~/.local/share/repi/kokoro
uv venv --python 3.12 venv
VIRTUAL_ENV=$PWD/venv uv pip install -U kokoro soundfile pip
venv/bin/python -m spacy download en_core_web_sm
```

**piper-tts** (optional, CPU-friendly): install `piper-tts` and put English voices in `/usr/share/piper-voices`.

### 2. Build and install Repi

```sh
git clone https://github.com/with5/repi.git
cd repi
./bin/install          # installs to ~/.local, no sudo needed
```

This builds a stripped release binary to `~/.local/bin/repi` and adds a launcher entry and icon. Set `PREFIX=/some/path` to install elsewhere.

Just want to try Repi?

```sh
go build -o repi . && ./repi
```

## Usage

1. **New** opens the editor (pre-filled from your clipboard); **File** imports a `.txt` / `.md`.
2. **Save & open** — reading starts immediately. Click any sentence to start from there.
3. Press **F** for the big screen, then click a word to look it up.

| Key | Action |
| --- | --- |
| `Space` | Play / pause |
| `←` `→` | Previous / next sentence |
| `F` | Big-screen (karaoke) mode |
| `T` | Toggle translation (big screen) |
| `Esc` | Close the dictionary card |
| `?` | Show all shortcuts |
| `Q` | Quit |

The **Voices** panel lets you audition every voice (with your own sample sentence) and star favorites; the voice dropdown in the reader shows only starred voices.

### Offline dictionary

On first lookup, click **Download dictionary** on the card (66 MB download, 28 MB indexed). After that, no network is needed.

### Where data lives

| What | Path |
| --- | --- |
| Sentence audio cache | `~/.cache/repi/audio` |
| Translation cache | `~/.cache/repi/translate` |
| Materials & progress | `~/.local/share/repi/materials.json` |
| Dictionary | `~/.local/share/repi/dict/` |
| Kokoro environment | `~/.local/share/repi/kokoro/` |

## How it works

Text is split into sentences (aware of `Mr.`, `e.g.`, decimals and quotes). Each sentence is synthesized once into a WAV, cached by `hash(voice + speed + text)`, and played back by a small state machine that handles loops, repeats and seeking. Already-synthesized sentences start playing while the rest are still being generated. Translations use the free MyMemory API (no key required).

```
main.go           app entry
internal/
  text/           sentence splitting
  tts/            Kokoro / piper backends + cache
  player/         playback state machine + audio output
  align/          word-level timing estimation
  dict/           offline ECDICT lookup
  translate/      sentence translation + cache
  store/          materials library
```

## Development

```sh
go test ./...
```

Design notes live in [`docs/superpowers/specs`](docs/superpowers/specs).

## Contributing

Issues and pull requests are welcome. If you're planning something big, open an issue first so we can talk it through.

## Acknowledgements

[MyGo](https://github.com/egoist/mygo) · [Kokoro-82M](https://huggingface.co/hexgrad/Kokoro-82M) · [piper](https://github.com/rhasspy/piper) · [ECDICT](https://github.com/skywind3000/ECDICT) · [MyMemory](https://mymemory.translated.net)

## License

No license has been declared yet. Add a `LICENSE` file before accepting outside contributions.
