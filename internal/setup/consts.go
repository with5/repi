// Package setup installs the Kokoro environment: uv, a Python virtual
// environment with PyTorch and kokoro, and the model files.
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
	mirrorModel   = "https://www.modelscope.cn/models/AI-ModelScope/Kokoro-82M/resolve/master/"
)

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

var errUnsupported = errors.New("这个系统不支持一键安装，请参照 README 手动安装 Kokoro")
