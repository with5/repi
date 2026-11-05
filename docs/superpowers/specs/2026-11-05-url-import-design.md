# 通过 URL 导入文章

日期：2026-11-05

## 目标

用户给一个文章链接，Repi 抓取网页、提取标题和正文，填进现有编辑器，由用户确认后保存。

范围是服务端直接返回正文的普通新闻和博客页面。

## 不做的事

- JS 渲染的页面、需要登录或付费的页面（提取失败时提示改用粘贴）
- 第三方提取服务：抓取只在本机进行，只访问用户给的链接
- 重复导入检测
- 记录来源 URL（`store.Material` 不加字段）
- 判断文章语言、自动翻译、自动打标签

## 现状

- 侧边栏（`view.go` 的 `sidebar`）有“文件”和“新建”两个按钮。
- `importFile` 在 goroutine 里读文件，再通过 `m.win.Update` 回到 UI 线程，调用 `startEdit` 并设置 `draftTitle`。URL 导入沿用这个做法。
- `saveDraft` 负责保存，存储层不需要改动。
- `text.Split` 除了按标点断句，**还会在每个换行处断句**。所以提取结果里，一个段落必须只占一行，否则网页源码中的换行会把一句话切成两半。

## 依赖

新增 `codeberg.org/readeck/go-readability/v2`（v2.1.3），它是 Mozilla Readability 的 Go 移植版。

`golang.org/x/net` 从间接依赖变为直接依赖（用到 `html` 和 `html/charset`）。

## 提取包（`internal/article`）

不依赖 UI，对外接口：

```go
type Article struct {
	Title string
	Text  string
}

func Fetch(ctx context.Context, rawURL string) (Article, error)
```

### 抓取

1. 去掉首尾空白。没有协议时补 `https://`。协议不是 http 或 https、或者没有主机名时，返回错误。
2. 发送 GET 请求，整个过程（含读取响应）超时 30 秒。重定向沿用 `net/http` 的默认规则。
3. User-Agent 固定为 `Mozilla/5.0 (compatible; Repi)`，如实标明身份，不伪装成浏览器。被网站拒绝时提示改用粘贴。
4. 状态码不是 2xx 时返回错误。
5. `Content-Type` 必须是 `text/html` 或 `application/xhtml+xml`，缺失时也接受。
6. 响应超过 5 MB 时返回错误，不做截断，以免静默得到半篇文章。
7. 用 `charset.NewReader` 按响应声明的编码转成 UTF-8。

### 提取

用 `readability.FromReader` 解析，传入重定向后的最终 URL。

- **标题：** `Article.Title()`，合并连续空白。
- **正文：** 遍历 `Article.Node` 生成纯文本，不使用 `RenderText`，这样才能控制换行。

正文的转换规则：

- `p`、`h1`–`h6`、`li`、`blockquote`、`dd`、`dt` 等块级元素各输出一行，块内空白合并成一个空格，块之间空一行。
- 块级元素相互嵌套时（如 `li` 里的 `p`、`blockquote` 里的 `p`），只按最内层的块输出，同一段文字不重复。
- 不在任何块级元素内的直接文本，自成一行。
- 连续两个 `<br>` 视为分段（旧式页面用它代替 `<p>`），单个 `<br>` 视为空格。
- 跳过 `pre`、`code` 块、`table`、`figure`、`figcaption`、`img`、`svg`、`video`、`audio`、`script`、`style`。段内的行内 `code` 保留文字。
- 链接只保留文字，不保留地址。
- 第一行与标题相同（忽略大小写和首尾空白）时删除，避免标题被读两遍。

### 错误

用哨兵错误区分需要特别提示的情况，其余错误用中文包装后返回。

| 情况 | 提示 |
|---|---|
| 链接无效 | 链接无效，请输入 http 或 https 地址 |
| 网络失败或超时 | 无法访问该网页：<原因> |
| HTTP 403、405、429 | 网站拒绝了这次抓取（HTTP <状态码>），可以复制正文后用“新增”里的“粘贴” |
| 其他非 2xx 状态码 | 网站返回错误（HTTP <状态码>） |
| 不是 HTML | 这个链接不是网页 |
| 超过 5 MB | 网页太大，无法导入 |
| 正文不足 200 个字符（`ErrNoContent`） | 没有提取到正文。这个页面可能需要 JS 或登录，可以复制正文后用“新增”里的“粘贴” |

200 个字符的门槛用于拦下 JS 空壳页（如 “Please enable JavaScript”）和导航页。代价是极短的文章也会被拒绝，这类文章可以用粘贴导入。

## 界面

侧边栏原有的“文件”和“新建”两个按钮合并为一个“新增”按钮（`Label` 为“新增资料”），点击后用 `PopoverMenuButton` 弹出跟随主题的菜单：

- **导入**：原“文件”，读取 `.txt` / `.md`
- **URL**：本功能的入口
- **粘贴**：原“新建”，打开编辑器并带入剪贴板文字

选择 URL 后打开 `ui.Modal`（用法同 `dict_view.go`、`view.go` 里的现有弹窗），内容包括：

- 多行输入框（3 到 6 行，长链接自动折行），自动聚焦。回车直接导入，不换行；粘贴进来的换行符在解析链接时去掉。剪贴板内容是单个 http 或 https 链接时自动填入，否则留空。
- “导入”按钮和“取消”按钮。输入为空或正在抓取时，“导入”不可用。
- 抓取中显示“正在抓取…”，失败时在输入框下方显示错误信息，弹窗保持打开，可以改链接重试。

状态放在 `model` 上，只在 UI 线程读写：

```go
type urlImport struct {
	open     bool
	input    string
	fetching bool
	err      string
	cancel   context.CancelFunc
	gen      int // 每次开始或取消抓取时递增
}
```

## 数据流

1. 点击“导入”：递增 `gen`，创建可取消的 context，设置 `fetching`，启动 goroutine 调用 `article.Fetch`。
2. goroutine 结束后通过 `m.win.Update` 回到 UI 线程，`gen` 已变化就丢弃结果。
3. 成功：关闭弹窗，调用 `startEdit(store.Material{}, art.Text)`，再设置 `draftTitle = art.Title`。
4. 失败：写入 `err`，清除 `fetching`。
5. 点击“取消”或用其他方式关闭弹窗：调用 `cancel`，递增 `gen`。

用户在编辑器里检查、修改后，点“保存并打开”走现有的 `saveDraft`。导入不会直接保存或开始朗读。

与“粘贴”和“导入”一样，导入成功会替换编辑器里尚未保存的草稿。

## 测试

`internal/article`（`httptest` 加内联 HTML 样本）：

- 提取标题和正文，导航、页脚、“订阅”之类的杂质不进入正文
- 每个段落只占一行：源码里带硬换行的段落，经 `text.Split` 后不会被切开
- 嵌套块不重复，代码块和表格被跳过，与标题相同的首行被删除
- 补全协议、拒绝非 http(s) 协议
- 非 2xx、非 HTML、超过 5 MB、正文不足 200 字符时返回对应错误
- context 取消后返回错误
- 非 UTF-8 页面（如 ISO-8859-1）能正确转码

`model` 层（`e2e_test.go`）：

- 导入成功后进入编辑器，标题和正文已填好，保存后生成资料
- 导入失败时不进入编辑器，错误信息保留
- 取消后才返回的结果被丢弃
- 侧边栏只显示“新增”，菜单里有三个选项
- 选择 URL 或粘贴后，键盘焦点在输入框或编辑器里

剪贴板链接判断写成纯函数，单独测试。

## 文档

`README.md` 和 `README.zh-CN.md` 的用法部分各加一句，说明可以用“链接”从 URL 导入。

## 交付

测试通过后，在项目根目录执行 `go build -o repi .` 生成二进制。
