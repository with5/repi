# repi — 英文磨耳朵

用本地 TTS（piper-tts）把英文资料一句句朗读，并一直循环，直到听熟。

## 运行

依赖：Go 1.27，以及至少一个 TTS 后端：

- **Kokoro-82M**（默认，音质好、很快，GPU/CPU 都行）：Python 子进程常驻，模型只加载一次。
  安装环境（模型约 330MB，首次使用时自动下载）：

      mkdir -p ~/.local/share/repi/kokoro && cd ~/.local/share/repi/kokoro
      uv venv --python 3.12 venv
      VIRTUAL_ENV=$PWD/venv uv pip install -U kokoro soundfile pip
      venv/bin/python -m spacy download en_core_web_sm

  27 个英语音色（美音/英音，男声/女声），在下拉框里以 `kokoro:af_heart` 等标识；语速是 Kokoro 的原生参数。
- **piper-tts**（备选，CPU 即可）：`/usr/share/piper-voices` 里的英文音色。

    go build -o repi . && ./repi

## 用法

- 「新建」：打开编辑器（自动带入剪贴板文字）；「文件」：从 .txt/.md 导入。
- 「保存并打开」后立即开始朗读；点任意一句从该句开始。
- 「音色」面板：逐个试听（可改试听句子），点星标收藏；朗读界面的音色下拉框只显示收藏的音色。
- 「大屏」（快捷键 F）：大字显示当前句，按播放时间估算正在读的词，当前词高亮、未读到的词变淡，只在换词时刷新。
- 大屏里**点任意单词**查词：朗读自动暂停，卡片显示音标、中文释义、考试标签（高考/四级/雅思…）和词形；`went`、`running` 这类变形会指向原形并一并显示原形释义。Esc 或点空白处关闭，之后继续朗读。
  词典是离线的 ECDICT：第一次查词时卡片上点「下载词典」（下载 66MB，建好索引后占 28MB，放在 `~/.local/share/repi/dict/`），之后完全不用联网。
- 大屏顶栏的「翻译」开关（快捷键 `T`）：当前句下方显示中文，切句时自动更新。走 MyMemory 免费接口，不需要 key；每句只请求一次，译文缓存在 `~/.cache/repi/translate`。匿名使用每天有字数额度，用完会提示，可稍后点「重试」。
- 循环方式：整篇循环 / 逐句重复（每句 N 次）。
- 空格 播放/暂停，← → 切句，`?` 显示全部快捷键，`Q` 退出，「隐藏文字」只听不看。
- 外观跟随 Omarchy 当前主题（读取 `~/.local/state/omarchy/current/theme/colors.toml`，切换主题后约 1 秒内自动更新），字体用系统等宽字体；没有 Omarchy 时用深色默认配色。
- 句子音频缓存在 `~/.cache/repi/audio`，资料和进度在 `~/.local/share/repi/materials.json`。
