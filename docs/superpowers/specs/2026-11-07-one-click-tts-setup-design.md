# 一键安装 Kokoro 语音引擎

日期：2026-11-07

## 目标

用户在 Repi 里点一个按钮，就能装好 Kokoro-82M 所需的全部依赖（Python、PyTorch、kokoro、spaCy 英文模型）和模型文件，不需要预装 Python、uv 或任何命令行操作。Linux、macOS、Windows 用同一套流程。

默认直连官方源；勾选“使用国内镜像”后改走国内源。

## 不做的事

- piper-tts 的一键安装
- 卸载、重装、升级依赖版本的按钮
- 自动检测网络并切换镜像（只按用户勾选）
- 安装进度精确到字节的总进度条（按步骤显示）

## 现状

- `tts.Kokoro.Dir` 是 `store.DataDir()/kokoro`，`Check()` 只看 `venv/bin/python`（Windows 为 `venv\Scripts\python.exe`）是否存在。
- worker（`kokoro_worker.py`）用 `KModel(repo_id=REPO)` 和 `KPipeline` 在首次使用时从 HuggingFace 下载模型和音色。国内直连 huggingface.co 经常超时，`hf-mirror.com` 目前对这些文件也只是 308 重定向回 huggingface.co。
- 启动时如果 piper 和 Kokoro 都不可用，`view.go` 用红字显示 `setupErr`。
- 词典下载（`dict_view.go`）已经有“按钮 → goroutine 下载 → 状态文字 → `m.win.Update` 刷新”的模式，本功能沿用。

## 网络实测（2026-11-07，国内网络）

| 资源 | 官方源 | 国内源 |
| --- | --- | --- |
| uv wheel | pypi.org 超时 | 清华 `pypi.tuna.tsinghua.edu.cn/simple/uv/` 可用，各平台 wheel 齐全 |
| Python 本体 | GitHub 可用 | 南大 `mirror.nju.edu.cn/github-release/astral-sh/python-build-standalone/` 可用 |
| PyTorch CPU | `download.pytorch.org/whl/cpu` 可用 | 阿里云 `mirrors.aliyun.com/pytorch-wheels/cpu/`（find-links 格式）可用 |
| spaCy `en_core_web_sm` | GitHub Releases 可用且快 | 无可用镜像（不在 PyPI，ModelScope 也没有），始终走 GitHub |
| Kokoro-82M 模型与音色 | huggingface.co 超时 | ModelScope `AI-ModelScope/Kokoro-82M` 可用 |

## 方案

Repi（Go）下载独立的 `uv` 二进制，再用 uv 建 venv、装包。uv 会自己下载 Python，所以用户什么都不用预装。模型文件由 Go 直接下载到本地目录，worker 改为优先加载本地文件，从而不再依赖 HuggingFace 的运行时下载。

## 目录布局

```
DataDir()/kokoro/
  bin/uv[.exe]          安装用的 uv
  venv/                 装好的环境（现有路径不变）
  venv.partial/         安装中的环境，完成后改名为 venv
  model/                config.json、kokoro-v1_0.pth、voices/*.pt
  worker.py             （现有）
```

uv 的下载缓存用 `UV_CACHE_DIR=DataDir()/kokoro/uv-cache`，uv 管理的 Python 用 `UV_PYTHON_INSTALL_DIR=DataDir()/kokoro/python`，都不碰用户全局环境。

## 安装包（`internal/setup`）

不依赖 UI，对外接口：

```go
type Options struct {
	Dir    string // DataDir()/kokoro
	Mirror bool   // 使用国内镜像
	GPU    bool   // 安装 CUDA 版 PyTorch
}

type Progress struct {
	Step, Steps int    // 当前第几步、共几步
	Title       string // "安装依赖"
	Detail      string // 最后一行日志或 "123/327 MB"
}

func Install(ctx context.Context, o Options, progress func(Progress)) error
func HasNvidiaGPU() bool // exec.LookPath("nvidia-smi") 且运行成功
```

### 步骤

1. **获取 uv**：请求 simple 索引页（官方 `https://pypi.org/simple/uv/`，镜像用清华），找到固定版本 `uvVersion` 中匹配当前平台标签的 wheel（见下表），下载后解出 `uv`/`uv.exe` 放到 `bin/`，Unix 上加可执行位。已存在且 `uv --version` 输出与 `uvVersion` 一致时跳过。
2. **建 venv**：删除残留的 `venv.partial`，执行 `uv venv --relocatable --python 3.12 venv.partial`。镜像模式下设置 `UV_PYTHON_INSTALL_MIRROR=https://mirror.nju.edu.cn/github-release/astral-sh/python-build-standalone`（已确认 uv 0.13.7 所需的 20261026 发布在南大镜像上）。
3. **装 PyTorch**：
   - Linux、Windows CPU：官方 `--index-url https://download.pytorch.org/whl/cpu`；镜像 `--find-links https://mirrors.aliyun.com/pytorch-wheels/cpu/`，加上 PyPI 镜像作为其它包的索引。
   - GPU（只在 Linux、Windows 上提供）：官方 CUDA 索引 `download.pytorch.org/whl/cu132`；镜像用阿里云 `pytorch-wheels/cu132/`。只在 linux/amd64、linux/arm64、windows/amd64 上提供。
   - macOS：直接从 PyPI 装（PyPI 的 macOS torch 就是 CPU/MPS 版）。
   - torch 版本固定为 `torchVersion`。
4. **装 kokoro**：`uv pip install kokoro==<kokoroVersion> soundfile`，再装 spaCy 模型的 wheel（GitHub Releases 上固定版本的 URL）。镜像模式下 `UV_INDEX_URL` 指向清华 PyPI。
5. **下载模型**：Go 直接下载 `config.json`、`kokoro-v1_0.pth` 和 `tts.KokoroVoices` 里每个音色的 `voices/<id>.pt` 到 `model/`。官方源 `https://huggingface.co/hexgrad/Kokoro-82M/resolve/main/<path>`，镜像源 `https://www.modelscope.cn/models/AI-ModelScope/Kokoro-82M/resolve/master/<path>`。先写 `.part` 文件，下完再改名；已存在的完整文件跳过，所以重试不会重下。显示“已下载 / 总大小 MB”。
6. **完成**：用 `venv.partial` 里的 python 执行 `import kokoro, torch, spacy; spacy.load('en_core_web_sm')` 做自检，通过后把 `venv.partial` 改名为 `venv`（如果已有旧的 `venv`，先改名为 `venv.old` 再删除）。

`Check()` 不改：`venv` 只会在自检通过后出现，所以装到一半不会被误判为已安装。

### 平台标签

| GOOS/GOARCH | uv wheel 标签 |
| --- | --- |
| linux/amd64 | `manylinux_2_17_x86_64` |
| linux/arm64 | `manylinux_2_17_aarch64` |
| darwin/arm64 | `macosx_11_0_arm64` |
| windows/amd64 | `win_amd64` |
| windows/arm64 | `win_arm64` |

其它平台直接报“不支持一键安装，请参照 README 手动安装”。Intel Mac（darwin/amd64）也不支持：PyTorch 2.3 起不再发布 macOS x86_64 的 wheel。

### 执行命令

所有外部命令都通过一个 `runner` 接口执行，便于测试时替换：

```go
type runner interface {
	Run(ctx context.Context, env []string, name string, args ...string) error // 每行输出回调给 progress 的 Detail
}
```

真实实现用 `exec.CommandContext`，Windows 上调用 `hideWindow` 不弹黑窗口（把 `internal/tts` 里的 `hideWindow` 挪到可共享的位置，或在 `setup` 里复制一份）。取消时 `ctx` 结束，子进程被杀。

### 版本固定

都是 `setup` 包里的常量（2026-11-08 确认官方源和镜像都有）：uv 0.13.7、Python 3.12、torch 2.15.0（`+cpu` / `+cu132`）、kokoro 0.9.4、`en_core_web_sm` 3.8.0。

## worker 改动

`kokoro_worker.py` 读取环境变量 `REPI_KOKORO_MODEL`（模型目录）：

- 目录里有 `config.json` 和 `kokoro-v1_0.pth` 时，用 `KModel(repo_id=REPO, config=..., model=...)` 加载本地文件；合成时把音色参数换成 `model/voices/<voice>.pt` 的路径（`KPipeline` 对以 `.pt` 结尾的音色直接读文件）。
- 否则保持现有行为，从 HuggingFace 下载。手动安装的老用户不受影响。

本地模型完整时（`tts.KokoroModelFiles()` 列出的文件都在 `<Dir>/model`），`tts.Kokoro.start` 设置 `REPI_KOKORO_MODEL=<Dir>/model` 和 `HF_HUB_OFFLINE=1`，防止 kokoro 在后台联网；否则两者都不设。

## 设置

`store.Settings` 新增 `Mirror bool`（`json:"mirror,omitempty"`），点“一键安装”时保存，作为下次安装卡片里复选框的初始值。默认 `false`。

## UI

新增 `setup_view.go`，状态放在 `model.setup`（结构与 `dictState` 类似：`running`、`progress`、`err`、`cancel`）。

安装卡片内容：

- 说明：“Kokoro 是 Repi 默认的神经网络语音引擎。一键安装会下载约 X MB（Python、PyTorch 和模型），装在 `<Dir>`，不影响系统环境。”（X 按是否 GPU 估算显示）
- 复选框“使用国内镜像”。
- 复选框“GPU 加速（NVIDIA，下载约 3.5GB）”，只在 `HasNvidiaGPU()` 为真时显示。
- 主按钮“一键安装”。
- 运行中：“第 3/6 步：安装 PyTorch”，下面是 `Detail`，按钮变成“取消”。
- 失败：红字错误 + “重试”按钮；错误信息里提示可以试试切换镜像。

出现位置：

1. 主界面 `setupErr` 那一行下面（没有任何可用 TTS 时）。
2. 音色面板顶部，在 Kokoro 未安装时显示（已有 piper 的用户也能装上 Kokoro）。

安装成功后，在 UI 线程上：

- 清空 `setupErr`（如果它是“没有可用的 TTS”那条）。
- 如果当前音色不可用，切到 `defaultVoice`（`kokoro:af_heart`），并刷新 `voiceSel`。
- 触发重绘；之后的朗读会正常启动 worker。

## 错误处理

- 每一步的错误都带上步骤名：“安装 PyTorch 失败：<最后几行输出>”。
- 网络错误（HTTP 状态码不是 2xx、超时）提示“下载失败，可以勾选/取消‘使用国内镜像’后重试”。
- 取消：结束子进程，删除 `venv.partial` 和 `.part` 文件，保留 `bin/uv`、`uv-cache`、`python` 和已下完的模型文件，下次重试更快。
- 磁盘空间不检查，交给错误信息说明。

## 测试

单元测试（`internal/setup`）：

- 从 simple 索引 HTML 里挑出指定版本、指定平台标签的 wheel URL（包括带 `#sha256=` 片段和相对链接的情况）。
- 从 wheel（zip）里解出 uv 二进制。
- 用假 runner 检查各平台 × 镜像开关 × GPU 开关下每一步的命令行参数和环境变量。
- 模型下载用 `httptest.Server`：跳过已存在的文件、`.part` 改名、HTTP 错误。
- 自检失败时不改名，`venv` 不出现；取消时清理 `venv.partial`。

端到端测试：设置 `REPI_E2E_SETUP=1` 时，在临时目录里完整安装一次，再用该环境合成一句话。默认跳过。

文档：README 的安装一节改为“在 Repi 里点一键安装”，原来的手动步骤保留在“手动安装”小节；中文 README 同步。
