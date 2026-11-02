# 资料库整理：搜索、置顶、标签

日期：2026-11-02

## 目标

资料变多后，用户能快速找到想听的那一篇。手段是搜索、置顶和标签；不引入文件夹。

## 现状

- 资料存在单个 `materials.json`（`internal/store`），`Material` 有 ID、标题、正文、进度、模式、重复次数、更新时间。
- 侧边栏（`view.go` 的 `sidebar`）只列标题，按添加顺序倒序，没有搜索、分组或标签。
- 播放进度由 `saveProgress` 用 `m.cur`（当前资料的副本）调用 `store.Update` 写回。

## 数据

`store.Material` 增加两个字段，均带 `omitempty`：

- `Pinned bool` `json:"pinned,omitempty"`
- `Tags []string` `json:"tags,omitempty"`

旧的 `materials.json` 不带这两个字段，直接读取，不需要迁移。

标签没有独立存储。界面上的标签列表由所有资料的 `Tags` 统计得出（含篇数）；一个标签下没有资料时自然消失。

标签规范化（`store.NormalizeTags`）：按空格、逗号（含全角逗号）拆分，去掉开头的 `#`，去掉空串，同一篇内按不区分大小写去重，保留首次出现的写法。

## 查找（`internal/store/filter.go`，纯函数）

```go
func Filter(all []Material, query, tag string) []Hit
type Hit struct {
    Material
    Snippet string // 只命中正文时，命中的那一句；否则为空
}
func Tags(all []Material) []TagCount // 按篇数降序，同数按名字
```

规则：

- `query` 按空白拆成词，**每个词都必须命中**才算匹配。命中范围：标题、正文、标签。不区分大小写，子串匹配（中文同样适用）。
- `tag` 非空时，只保留带该标签的资料（不区分大小写）。
- 空 `query` 且空 `tag` 返回全部。
- 排序：置顶的在前；其余保持输入顺序（`store.List` 已是新的在前）。排序稳定。
- `Snippet`：所有词都没有命中标题和标签，只命中正文时，取第一个词在正文中所在的句子（用 `internal/text` 的分句），超过 60 个字符时截断加省略号。

## 界面

侧边栏自上而下：

1. 标题行：「资料」、「文件」、「新建」（保持不变）。
2. 搜索框，占位文字「搜索  /」。
3. 标签行：每个标签一个按钮，显示「名字 篇数」；选中的标签用强调色边框。再点一次取消。没有任何标签时整行不显示。
4. 列表：每项一行标题，置顶项前面一个矢量星形（`icons.go` 新增 `iconPin`，不用 Emoji），下面一行灰色小字显示标签（空则不占行）；有 `Snippet` 时再显示一行。
5. 没有匹配时显示「没有匹配的资料」。
6. 底部「音色」按钮（保持不变）。

朗读页顶部：在「大屏」旁加「置顶」/「取消置顶」按钮。

编辑器：标题下方加「标签」输入框（占位「空格或逗号分隔」），保存时规范化。

快捷键（仅在不处于编辑时生效，与现有快捷键一致）：

- `/`：聚焦搜索框。
- `Esc`：搜索框有内容时清空搜索（`ui.SearchField` 自带）。标签过滤再点一次该标签即取消。
- `P`：置顶/取消置顶当前资料。

`?` 帮助里加上这三项。

## 状态与写回

- `model` 增加 `query string` 和 `tagFilter string`（UI 线程状态，不持久化）。
- 置顶和改标签必须**同时更新 `m.cur`** 再调用 `store.Update`。否则下一次 `saveProgress` 会用旧的 `m.cur` 覆盖掉。
- `store` 增加 `SetPinned(id, bool)` 作为便捷方法，内部走 `Update` 的锁。

## 不做的事

文件夹、拖拽排序、标签颜色、标签重命名/合并、搜索高亮命中词、按进度排序。

## 测试

- `filter_test.go`：多词都要命中、只命中正文时给出 `Snippet`、标签过滤、置顶排序稳定、中文子串、空条件返回全部、`NormalizeTags`（全角逗号、`#` 前缀、去重）。
- `store_test.go`：读取不带 `pinned`/`tags` 的旧 JSON；写出后再读回字段不变；`SetPinned` 后 `saveProgress` 不会丢失置顶。
- `e2e_test.go`：用 `ui.NewTester` 在搜索框输入后，侧边栏只剩匹配项；点标签只剩该标签的资料；`P` 切换置顶后该项排到最前。
