<div align="center">

<img src="assets/logo.svg" alt="Repi" width="420">

### Loop English aloud, sentence by sentence — until it clicks.

A local-first listening trainer. Paste any English text, and Repi reads it back with a neural TTS model, on repeat, with a word-by-word karaoke view, an offline dictionary and instant translation.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-FCC624?style=flat-square)](#installation)
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
- 🎙️ **Your own voice** — record about 10 seconds (or import a clip) and Repi reads in that voice, cloned locally with [Chatterbox](https://github.com/resemble-ai/chatterbox).
- 🎤 **Karaoke big screen** — large type, the word being spoken is highlighted, unread words are dimmed.
- 📖 **Click any word to look it up** — playback pauses, a card shows IPA, Chinese definitions, exam tags (CET-4, IELTS…) and word forms. Inflections like `went` / `running` resolve to their lemma. Powered by an offline [ECDICT](https://github.com/skywind3000/ECDICT) index.
- 🌐 **Inline translation** — toggle a Chinese line under the current sentence; each sentence is translated once and cached.
- 🙈 **Hide text** — listen only, no peeking.
- ⚡ **Fast and light** — a single native binary written in Go, no Electron, no webview. The model is loaded once and kept resident.
- 🎨 **Follows your theme** — live-syncs with the current [Omarchy](https://omarchy.org) theme; falls back to a dark default elsewhere.
- 🔒 **Local-first** — audio, materials and progress all stay on your machine.

## Installation

### Requirements

- Linux (primary), macOS and Windows (experimental: cross-compiled, not yet tested on real hardware). Prebuilt binaries are attached to each [release](https://github.com/with5/repi/releases); on Windows the Kokoro venv lives under `%LOCALAPPDATA%\repi\kokoro`, on macOS under `~/Library/Application Support/repi/kokoro`.
- [Go](https://go.dev/dl/) 1.27+
- At least one TTS backend (see below)

### 1. Set up a TTS backend

**Kokoro-82M** (default; great quality, fast on GPU and CPU). Start Repi and click **一键安装** (Install) on the *Install Kokoro* card — it is on the main screen when no voice is installed, and at the top of the **Voices** panel. Repi downloads uv, Python 3.12, PyTorch, kokoro and the model (about 700 MB, about 3.5 GB with the optional NVIDIA GPU build) into its own data folder; nothing system-wide is touched. Tick **使用国内镜像** to use mirrors in China (Tsinghua PyPI, Aliyun PyTorch, NJU Python, ModelScope). Intel Macs are not supported, as PyTorch no longer ships for them.

<details><summary>Manual install</summary>

```sh
mkdir -p ~/.local/share/repi/kokoro && cd ~/.local/share/repi/kokoro
uv venv --python 3.12 venv
VIRTUAL_ENV=$PWD/venv uv pip install -U kokoro soundfile pip
venv/bin/python -m spacy download en_core_web_sm
```

</details>

**Voice cloning** (optional, needs Kokoro first). In the **Voices** panel, click **安装声音克隆** (Install voice cloning) on the card below Kokoro's. It installs [Chatterbox](https://github.com/resemble-ai/chatterbox) (MIT) next to Kokoro and reuses Kokoro's uv, Python and PyTorch build, so it downloads about 3.5 GB (mostly the model) and takes up to about 9 GB of disk. Then record 8–15 seconds of English in a quiet room, or import an audio file (WAV, FLAC, MP3, OGG). The new voice is starred and auditioned at once. An NVIDIA GPU is strongly recommended: Chatterbox needs about 4 GB of VRAM, and on CPU a sentence can take ten seconds or more. Recording uses PortAudio, which the Windows and macOS wheels bundle; on Linux install it from your distribution (`portaudio`). Generated audio carries Chatterbox's inaudible watermark. Only clone your own voice, or one you have permission to use.

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

1. **新增** (Add) opens a menu: **导入** (Import) reads a `.txt` / `.md`; **URL** fetches an article from a link and fills in its title and body for you to review; **粘贴** (Paste) opens the editor, pre-filled from your clipboard. Pages that need JavaScript or a login cannot be fetched, so paste those instead.
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
| Voice-cloning environment | `~/.local/share/repi/clone/` |
| Your voice recordings | `~/.local/share/repi/voices/` |

## How it works

Text is split into sentences (aware of `Mr.`, `e.g.`, decimals and quotes). Each sentence is synthesized once into a WAV, cached by `hash(voice + speed + text)`, and played back by a small state machine that handles loops, repeats and seeking. Already-synthesized sentences start playing while the rest are still being generated. Translations use the free MyMemory API (no key required).

```
main.go           app entry
internal/
  text/           sentence splitting
  tts/            Kokoro / Chatterbox / piper backends + cache
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

[MyGo](https://github.com/egoist/mygo) · [Kokoro-82M](https://huggingface.co/hexgrad/Kokoro-82M) · [Chatterbox](https://github.com/resemble-ai/chatterbox) · [piper](https://github.com/rhasspy/piper) · [ECDICT](https://github.com/skywind3000/ECDICT) · [MyMemory](https://mymemory.translated.net)

## Community & Friends

[LINUX DO - A New Ideal Community](https://linux.do)

## License

No license has been declared yet. Add a `LICENSE` file before accepting outside contributions.
