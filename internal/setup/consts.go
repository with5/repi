// Package setup installs the Kokoro environment: uv, a Python virtual
// environment with PyTorch and kokoro, and the model files; and, next to it,
// the Chatterbox voice-cloning engine.
package setup

import "errors"

const (
	userAgent = "repi-setup (+https://github.com/with5/repi)"

	uvVersion     = "0.13.7"
	pythonVersion = "3.12"
	torchVersion  = "2.15.0"
	cudaFlavor    = "cu132"
	kokoroVersion = "0.9.4"
	spacyModelURL = "https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl"

	officialPyPI  = "https://pypi.org/simple/"
	mirrorPyPI    = "https://pypi.tuna.tsinghua.edu.cn/simple/"
	officialTorch = "https://download.pytorch.org/whl/"          // + flavor, an index
	mirrorTorch   = "https://mirrors.aliyun.com/pytorch-wheels/" // + flavor + "/", find-links
	mirrorPython  = "https://mirror.nju.edu.cn/github-release/astral-sh/python-build-standalone"
	officialModel = "https://huggingface.co/hexgrad/Kokoro-82M/resolve/main/"
	vcRedistURL   = "https://aka.ms/vs/17/release/vc_redist." // + "x64.exe" or "arm64.exe"
	mirrorModel   = "https://www.modelscope.cn/models/AI-ModelScope/Kokoro-82M/resolve/master/"

	// The voice-cloning engine. Chatterbox pins torch 2.6, which has no
	// kernels for new GPUs, so it is installed without dependencies on top of
	// Kokoro's PyTorch, with the dependencies listed in cloneDeps.
	chatterboxVersion = "0.1.7"
	cloneModelRev     = "5bb1f6ee58e50c3b8d408bc82a6d3740c2db6e18"
	officialClone     = "https://huggingface.co/ResembleAI/chatterbox/resolve/" + cloneModelRev + "/"
	mirrorClone       = "https://hf-mirror.com/ResembleAI/chatterbox/resolve/" + cloneModelRev + "/"
	// torchaudio stopped following PyTorch's versions; 2.11.1 needs torch>=2.11.
	torchaudioVersion = "2.11.1"
)

// cloneNoDeps are installed without their dependencies: their pins (old
// torch, gradio, a dev tool) are not needed for inference.
var cloneNoDeps = []string{"chatterbox-tts==" + chatterboxVersion, "s3tokenizer==0.3.0"}

// cloneDeps are what Chatterbox and s3tokenizer need, plus the tools for
// recording and preparing the reference voice.
var cloneDeps = []string{
	"numpy>=1.24,<2", "librosa==0.11.0", "transformers==5.2.0", "diffusers==0.29.0",
	"resemble-perth==1.0.1", "conformer==0.3.2", "safetensors==0.5.3", "omegaconf==2.4.0",
	"onnx", "einops", "tqdm", "soundfile", "pyloudnorm==0.2.0", "sounddevice",
}

// uvTags are the platform tags of the uv wheels, by GOOS/GOARCH. Intel Macs
// are missing on purpose: PyTorch has no macOS x86_64 wheels any more.
var uvTags = map[string]string{
	"linux/amd64":   "manylinux_2_17_x86_64",
	"linux/arm64":   "manylinux_2_17_aarch64",
	"darwin/arm64":  "macosx_11_0_arm64",
	"windows/amd64": "win_amd64",
	"windows/arm64": "win_arm64",
}

// cudaPlatforms have CUDA builds of PyTorch.
var cudaPlatforms = map[string]bool{"linux/amd64": true, "linux/arm64": true, "windows/amd64": true}

var stepTitles = []string{"获取 uv", "准备 Python", "安装 PyTorch", "安装 Kokoro", "下载模型", "检查安装"}

var cloneStepTitles = []string{"获取 uv", "准备 Python", "安装 PyTorch", "安装 Chatterbox", "下载模型", "检查安装"}

var errUnsupported = errors.New("这个系统不支持一键安装，请参照 README 手动安装 Kokoro")
