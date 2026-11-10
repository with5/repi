<div align="center">

<img src="assets/logo.svg" alt="Repi" width="420">

### 把英文一句句循环朗读，直到听熟。

本地优先的英文磨耳朵工具：粘贴任意英文，Repi 用神经网络 TTS 一遍遍读给你听，配逐词高亮大屏、离线词典和即时翻译。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux-FCC624?style=flat-square&logo=linux&logoColor=black)](#安装)
[![TTS](https://img.shields.io/badge/TTS-Kokoro--82M-4a9a68?style=flat-square)](https://huggingface.co/hexgrad/Kokoro-82M)
[![Offline](https://img.shields.io/badge/离线可用-success?style=flat-square)](#特性)
[![Built with MyGo](https://img.shields.io/badge/built%20with-MyGo-8A2BE2?style=flat-square)](https://github.com/egoist/mygo)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen?style=flat-square)](https://github.com/with5/repi/pulls)
[![Stars](https://img.shields.io/github/stars/with5/repi?style=flat-square&logo=github)](https://github.com/with5/repi/stargazers)

[English](README.md) · **简体中文**

</div>

---

## 特性

听力靠的是同一份材料反复听，而不是一遍听很多不同的材料。Repi 把任何文本变成一个不知疲倦的朗读者，挂在后台就行。

- 🔁 **循环方式**：整篇循环，或每句重复 N 次再下一句。
- 🗣️ **自然音色**：27 个 Kokoro-82M 英语音色（美音/英音、男声/女声），本地 GPU/CPU 生成；也支持 [piper-tts](https://github.com/rhasspy/piper) 作为轻量备选。
- 🀄 **也能读中文**：粘贴中文资料，Repi 用 Kokoro 的 8 个普通话音色朗读，大屏逐字高亮。英文资料和中文资料各自记住自己的音色。
- 🎙️ **用你自己的声音**：录 10 秒左右（或导入一段录音），Repi 就用这个声音朗读，由 [Chatterbox](https://github.com/resemble-ai/chatterbox) 在本地克隆。
- 🎤 **卡拉 OK 大屏**：大字显示，当前读到的词高亮，未读到的词变淡。
- 📖 **点词即查**：朗读自动暂停，卡片显示音标、中文释义、考试标签（高考/四级/雅思…）和词形；`went`、`running` 等变形会指向原形。基于离线 [ECDICT](https://github.com/skywind3000/ECDICT)。
- 🌐 **句子翻译**：当前句下方显示中文，每句只请求一次并缓存。
- 🙈 **隐藏文字**：只听不看。
- ⚡ **轻快**：Go 编写的单个原生程序，没有 Electron、没有 webview；模型只加载一次并常驻。
- 🎨 **跟随主题**：实时跟随 [Omarchy](https://omarchy.org) 当前主题，没有时使用深色默认配色。
- 🔒 **本地优先**：音频、资料、进度都留在你的机器上。

## 安装

### 依赖

- Linux
- [Go](https://go.dev/dl/) 1.27+
- 至少一个 TTS 后端（见下）

### 1. 准备 TTS 后端

**Kokoro-82M**（默认，音质好、GPU/CPU 都很快）。打开 Repi，在「安装 Kokoro 语音引擎」卡片上点 **一键安装**。没有可用音色时它出现在主界面，音色面板顶部也有。Repi 会把 uv、Python 3.12、PyTorch、kokoro 和模型下载到自己的数据目录（约 700MB，选 NVIDIA GPU 加速版约 3.5GB），不改动系统环境。勾选 **使用国内镜像** 改走国内源（清华 PyPI、阿里云 PyTorch、南大 Python、ModelScope）。Intel Mac 不支持，PyTorch 已不再提供对应版本。

<details><summary>手动安装</summary>

```sh
mkdir -p ~/.local/share/repi/kokoro && cd ~/.local/share/repi/kokoro
uv venv --python 3.12 venv
VIRTUAL_ENV=$PWD/venv uv pip install -U kokoro "misaki[zh]" soundfile pip
venv/bin/python -m spacy download en_core_web_sm
```

</details>

**中文朗读**：新装的 Kokoro 自带。旧版 Repi 装的 Kokoro，会在音色面板和每篇中文资料上显示 **安装中文朗读** 卡片：它往现有环境里补装 misaki 的中文分词（jieba、pypinyin）和 8 个普通话音色，约 30MB，不影响英文朗读。中文字数不少于英文单词数的资料算作中文：按 。！？ 分句，用你选的中文音色朗读，夹在其中的英文单词按英文发音（普通话音色读英文仍带口音）。查词只对其中的英文单词有效，翻译开关会隐藏。

**声音克隆**（可选，需要先装好 Kokoro）。在音色面板里 Kokoro 卡片下方，点 **安装声音克隆**。它会把 [Chatterbox](https://github.com/resemble-ai/chatterbox)（MIT 许可）装在 Kokoro 旁边，复用 Kokoro 的 uv、Python 和 PyTorch，所以只需下载约 3.5GB（主要是模型），装好后最多占用约 9GB 磁盘。之后在安静的房间录 8–15 秒英文，或导入一个音频文件（WAV、FLAC、MP3、OGG），新音色会自动收藏并试听。强烈建议使用 NVIDIA GPU：Chatterbox 需要约 4GB 显存，用 CPU 生成一句可能要十几秒。录音依赖 PortAudio，Windows 和 macOS 的安装包已自带，Linux 需要从发行版安装（`portaudio`）。生成的语音带有 Chatterbox 的隐形水印。请只克隆你自己或得到同意的人的声音。

**piper-tts**（可选，CPU 即可）：安装 `piper-tts`，并把英文音色放在 `/usr/share/piper-voices`。

### 2. 构建并安装 Repi

```sh
git clone https://github.com/with5/repi.git
cd repi
./bin/install          # 安装到 ~/.local，无需 sudo
```

会把精简的发布版二进制装到 `~/.local/bin/repi`，并添加应用启动器图标。用 `PREFIX=/some/path` 可改安装位置。

只想试一下 Repi：

```sh
go build -o repi . && ./repi
```

## 使用

1. 点「新增」选择方式：「导入」读取 `.txt` / `.md`；「URL」从链接抓取文章，自动填好标题和正文供你检查；「粘贴」打开编辑器并带入剪贴板文字。需要 JS 或登录的页面无法抓取，请改用粘贴。
2. 「保存并打开」后立即开始朗读，点任意一句从该句开始。
3. 按 **F** 进入大屏，点任意单词查词。

| 快捷键 | 功能 |
| --- | --- |
| `空格` | 播放 / 暂停 |
| `←` `→` | 上一句 / 下一句 |
| `F` | 大屏（逐词高亮） |
| `T` | 开关翻译（大屏） |
| `Esc` | 关闭词典卡片 |
| `?` | 显示全部快捷键 |
| `Q` | 退出 |

「音色」面板可逐个试听（可改试听句子）并点星标收藏；朗读界面的音色下拉框只显示收藏的音色。

### 离线词典

第一次查词时，在卡片上点「下载词典」（下载 66MB，建好索引后占 28MB），之后完全不用联网。

### 数据位置

| 内容 | 路径 |
| --- | --- |
| 句子音频缓存 | `~/.cache/repi/audio` |
| 翻译缓存 | `~/.cache/repi/translate` |
| 资料与进度 | `~/.local/share/repi/materials.json` |
| 词典 | `~/.local/share/repi/dict/` |
| Kokoro 环境 | `~/.local/share/repi/kokoro/` |
| 声音克隆环境 | `~/.local/share/repi/clone/` |
| 你的录音 | `~/.local/share/repi/voices/` |

## 工作原理

文章先切句（处理 `Mr.`、`e.g.`、小数、引号），每句合成一次 wav，以 `hash(音色 + 语速 + 文本)` 为键缓存，由一个小状态机负责循环、重复和跳转。已合成的句子会先播放，其余在后台继续生成。翻译使用 MyMemory 免费接口，不需要 key。

```
main.go           程序入口
internal/
  text/           切句
  tts/            Kokoro / Chatterbox / piper 后端 + 缓存
  player/         播放状态机 + 音频输出
  align/          词级时间估算
  dict/           离线 ECDICT 查词
  translate/      句子翻译 + 缓存
  store/          资料库
```

## 开发

```sh
go test ./...
```

设计文档见 [`docs/superpowers/specs`](docs/superpowers/specs)。

## 参与贡献

欢迎提 Issue 和 PR；较大的改动请先开 Issue 讨论。

## 致谢

[MyGo](https://github.com/egoist/mygo) · [Kokoro-82M](https://huggingface.co/hexgrad/Kokoro-82M) · [Chatterbox](https://github.com/resemble-ai/chatterbox) · [piper](https://github.com/rhasspy/piper) · [ECDICT](https://github.com/skywind3000/ECDICT) · [MyMemory](https://mymemory.translated.net)

## 社区认可与友情链接

[LINUX DO - 新的理想型社区](https://linux.do)

## 许可证

暂未声明许可证。接受外部贡献前请先添加 `LICENSE` 文件。
