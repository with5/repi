# 资料库整理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给资料库加搜索、置顶和标签（规格：`docs/superpowers/specs/2026-11-02-library-organization-design.md`）。

**Architecture:** 查找逻辑是 `internal/store` 里的纯函数（`Filter`、`Tags`、`NormalizeTags`），不碰 UI。`model` 只多存搜索词和标签过滤；侧边栏每帧用 `Filter` 算出要显示的列表。置顶和标签写回时同步更新 `m.cur`，免得 `saveProgress` 用旧副本覆盖。

**Tech Stack:** Go 1.27、mygo `ui`（`SearchField`、`ui.Tester` 做界面测试）。

---

## 文件

- 改 `internal/store/store.go`：`Material` 加 `Pinned`、`Tags`；加 `SetPinned`。
- 新 `internal/store/filter.go`：`Hit`、`TagCount`、`Filter`、`Tags`、`NormalizeTags`。
- 新 `internal/store/filter_test.go`；改 `internal/store/store_test.go`。
- 改 `icons.go`：`iconPin`。
- 改 `model.go`：`query`、`tagFilter`、`draftTags`、`focusSearch` 字段；`saveDraft` 存标签；`togglePin`。
- 新 `library_view.go`：侧边栏的搜索框、标签行和列表（从 `view.go` 的 `sidebar` 里拆出来）。
- 改 `view.go`：快捷键 `/`、`P`；侧边栏调用新函数；朗读页「置顶」按钮；编辑器「标签」输入框；帮助里加三项。
- 改 `e2e_test.go`：界面测试。

## Task 1: 数据字段、`NormalizeTags`、`SetPinned`

- [ ] **Step 1: 写失败的测试** 追加到 `internal/store/store_test.go`：

```go
func TestOldFileHasNoNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	old := `{"settings":{},"materials":[{"id":1,"title":"A","text":"One.","index":0,"mode":0,"repeat":3,"updated":"2026-01-01T00:00:00Z"}],"next":1}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := s.Get(1)
	if m.Pinned || len(m.Tags) != 0 || m.Title != "A" {
		t.Errorf("old material = %+v", m)
	}
}

func TestPinAndTagsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	a := s.Add(Material{Title: "A", Text: "One.", Tags: []string{"work"}})
	s.SetPinned(a.ID, true)
	s2, _ := Open(path)
	m, _ := s2.Get(a.ID)
	if !m.Pinned || len(m.Tags) != 1 || m.Tags[0] != "work" {
		t.Errorf("after reload = %+v", m)
	}
	s2.SetPinned(a.ID, false)
	if m, _ := s2.Get(a.ID); m.Pinned {
		t.Error("still pinned")
	}
}
```

同时在文件顶部的 import 里加 `"os"`。

- [ ] **Step 2: 运行确认失败** `go test ./internal/store/` → 编译失败：`Pinned`、`Tags`、`SetPinned` 未定义。

- [ ] **Step 3: 实现** 在 `store.go` 的 `Material` 里加：

```go
	Pinned bool     `json:"pinned,omitempty"`
	Tags   []string `json:"tags,omitempty"`
```

在 `Delete` 之前加：

```go
// SetPinned pins or unpins the material with id and saves.
func (s *Store) SetPinned(id int, pinned bool) {
	s.mu.Lock()
	for _, m := range s.f.Materials {
		if m.ID == id {
			m.Pinned = pinned
		}
	}
	s.mu.Unlock()
	s.Save()
}
```

- [ ] **Step 4: 运行确认通过** `go test ./internal/store/` → PASS。

## Task 2: `Filter`、`Tags`、`NormalizeTags`

- [ ] **Step 1: 写失败的测试** 新建 `internal/store/filter_test.go`：

```go
package store

import (
	"reflect"
	"testing"
)

func titles(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Title)
	}
	return out
}

var sample = []Material{
	{ID: 3, Title: "Pods", Text: "A pod is small. It runs containers.", Tags: []string{"work"}},
	{ID: 2, Title: "AI engineers", Text: "They build models.", Tags: []string{"study"}, Pinned: true},
	{ID: 1, Title: "通义千问", Text: "由阿里巴巴开发。", Tags: []string{"news", "Work"}},
}

func TestFilterAllTermsMustMatch(t *testing.T) {
	got := titles(Filter(sample, "pod containers", ""))
	if !reflect.DeepEqual(got, []string{"Pods"}) {
		t.Errorf("got %v", got)
	}
	if got := Filter(sample, "pod models", ""); len(got) != 0 {
		t.Errorf("terms from different materials matched: %v", titles(got))
	}
}

func TestFilterChinese(t *testing.T) {
	if got := titles(Filter(sample, "阿里", "")); !reflect.DeepEqual(got, []string{"通义千问"}) {
		t.Errorf("got %v", got)
	}
}

func TestFilterTagCaseInsensitive(t *testing.T) {
	got := titles(Filter(sample, "", "WORK"))
	if !reflect.DeepEqual(got, []string{"AI engineers", "Pods", "通义千问"}[1:]) {
		t.Errorf("got %v", got)
	}
}

func TestFilterPinnedFirstStable(t *testing.T) {
	got := titles(Filter(sample, "", ""))
	if !reflect.DeepEqual(got, []string{"AI engineers", "Pods", "通义千问"}) {
		t.Errorf("got %v", got)
	}
}

func TestFilterSnippetOnlyForBodyMatches(t *testing.T) {
	hits := Filter(sample, "runs", "")
	if len(hits) != 1 || hits[0].Snippet != "It runs containers." {
		t.Errorf("hits = %+v", hits)
	}
	if hits := Filter(sample, "pods", ""); len(hits) != 1 || hits[0].Snippet != "" {
		t.Errorf("title match has a snippet: %+v", hits)
	}
}

func TestFilterSnippetTruncates(t *testing.T) {
	long := Material{Title: "T", Text: "word " + string(make([]rune, 0)) + repeat("x", 100) + "."}
	hits := Filter([]Material{long}, "word", "")
	if n := len([]rune(hits[0].Snippet)); n > 61 {
		t.Errorf("snippet has %d runes", n)
	}
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func TestTagsCounted(t *testing.T) {
	got := Tags(sample)
	want := []TagCount{{"work", 2}, {"news", 1}, {"study", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags("#work, study，Work  #news;")
	want := []string{"work", "study", "news"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if NormalizeTags("  ") != nil {
		t.Error("blank input should give nil")
	}
}
```

注意 `TestFilterTagCaseInsensitive` 的期望是置顶优先，所以带 `work` 的是 `Pods` 和 `通义千问`（`AI engineers` 没有 `work`）。把那一行期望改成 `[]string{"Pods", "通义千问"}`，不要用切片截取。

- [ ] **Step 2: 运行确认失败** `go test ./internal/store/` → `Filter` 等未定义。

- [ ] **Step 3: 实现** 新建 `internal/store/filter.go`：

```go
package store

import (
	"sort"
	"strings"
	"unicode"

	"repi/internal/text"
)

// Hit is a material that matched a search, with the sentence that matched
// when only the text did.
type Hit struct {
	Material
	Snippet string
}

// TagCount is a tag and how many materials carry it.
type TagCount struct {
	Name  string
	Count int
}

const snippetRunes = 60

// Filter returns the materials matching every word of query (in the title,
// text or tags, ignoring case) and carrying tag if one is given. Pinned
// materials come first; otherwise the order of all is kept.
func Filter(all []Material, query, tag string) []Hit {
	terms := strings.Fields(strings.ToLower(query))
	var pinned, rest []Hit
	for _, m := range all {
		if tag != "" && !hasTag(m, tag) {
			continue
		}
		head := strings.ToLower(m.Title + "\n" + strings.Join(m.Tags, "\n"))
		body := strings.ToLower(m.Text)
		ok, inHead := true, false
		for _, t := range terms {
			h := strings.Contains(head, t)
			if !h && !strings.Contains(body, t) {
				ok = false
				break
			}
			inHead = inHead || h
		}
		if !ok {
			continue
		}
		hit := Hit{Material: m}
		if len(terms) > 0 && !inHead {
			hit.Snippet = snippetOf(m.Text, terms[0])
		}
		if m.Pinned {
			pinned = append(pinned, hit)
		} else {
			rest = append(rest, hit)
		}
	}
	return append(pinned, rest...)
}

func hasTag(m Material, tag string) bool {
	for _, t := range m.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// snippetOf is the first sentence of s containing term, cut to a short line.
func snippetOf(s, term string) string {
	for _, sent := range text.Split(s) {
		if strings.Contains(strings.ToLower(sent), term) {
			if r := []rune(sent); len(r) > snippetRunes {
				return string(r[:snippetRunes]) + "…"
			}
			return sent
		}
	}
	return ""
}

// Tags counts the tags of all, most used first and by name among equals.
// Tags differing only in case are one, shown as first written.
func Tags(all []Material) []TagCount {
	counts := map[string]*TagCount{}
	var order []string
	for _, m := range all {
		for _, t := range m.Tags {
			k := strings.ToLower(t)
			if counts[k] == nil {
				counts[k] = &TagCount{Name: t}
				order = append(order, k)
			}
			counts[k].Count++
		}
	}
	out := make([]TagCount, 0, len(order))
	for _, k := range order {
		out = append(out, *counts[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// NormalizeTags splits s at spaces and commas, drops a leading # and empty
// pieces, and removes repeats ignoring case, keeping the first spelling.
func NormalizeTags(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，;；、", r)
	})
	var out []string
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimLeft(p, "#＃")
		k := strings.ToLower(p)
		if p == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}
```

`TestTagsCounted` 里 `Work` 与 `work` 合并为 `work` 2 篇（首次写法 `work`，因为 `Pods` 先出现）。

- [ ] **Step 4: 运行确认通过** `go test ./internal/store/` → PASS。

## Task 3: 模型状态、置顶和标签写回

- [ ] **Step 1: 写失败的测试** 追加到 `e2e_test.go`：

```go
// Pinning changes the material the player will save next, so playback
// progress does not write the old, unpinned copy over it.
func TestPinSurvivesProgressSave(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	defer m.pl.Close()
	m.draftText = "One. Two. Three."
	m.saveDraft()
	m.togglePin()
	m.pl.Seek(2)
	m.saveProgress(true)
	got, _ := m.st.Get(m.cur.ID)
	if !got.Pinned || got.Index != 2 {
		t.Errorf("stored = %+v", got)
	}
}

func TestSaveDraftStoresTags(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	defer m.pl.Close()
	m.draftText, m.draftTags = "One.", "#work, study"
	m.saveDraft()
	if got := m.cur.Tags; len(got) != 2 || got[0] != "work" || got[1] != "study" {
		t.Errorf("tags = %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败** `go test -run 'TestPin|TestSaveDraftStoresTags' .` → `togglePin`、`draftTags` 未定义。

- [ ] **Step 3: 实现** `model.go`：

在 `showHelp bool` 一行后加字段：

```go
	query       string // search box
	tagFilter   string // tag chosen in the sidebar
	draftTags   string // tags being edited, as typed
	focusSearch bool   // give the search box the focus in the next frame
```

`saveDraft` 里两处：更新分支 `mat.Title, mat.Text = title, body` 后加 `mat.Tags = store.NormalizeTags(m.draftTags)`；新建分支改成 `store.Material{Title: title, Text: body, Tags: store.NormalizeTags(m.draftTags)}`。

在 `firstWords` 之前加：

```go
// togglePin pins or unpins the open material. It changes m.cur as well,
// which saveProgress writes back whole.
func (m *model) togglePin() {
	if !m.open {
		return
	}
	m.cur.Pinned = !m.cur.Pinned
	m.st.SetPinned(m.cur.ID, m.cur.Pinned)
	m.refresh()
}
```

`view.go` 的 `startEdit` 里加 `m.draftTags = strings.Join(mat.Tags, " ")`（文件顶部 import 加 `"strings"`）。

- [ ] **Step 4: 运行确认通过** `go test -run 'TestPin|TestSaveDraftStoresTags' .` → PASS。

## Task 4: 侧边栏、朗读页、编辑器和快捷键

- [ ] **Step 1: 写失败的界面测试** 追加到 `e2e_test.go`（`ui` 已导入，另需 `"strings"` 与 `"github.com/egoist/mygo/ui"` 已在文件里的话不重复）：

```go
func addMaterial(m *model, title, body, tags string) {
	m.draftTitle, m.draftText, m.draftTags = title, body, tags
	m.saveDraft()
}

func TestSidebarSearchAndTags(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	defer m.pl.Close()
	addMaterial(m, "Pods", "A pod runs containers.", "work")
	addMaterial(m, "Models", "They build models.", "study")
	tt := ui.NewTester(m.view, 1000, 640)
	tt.Frame()
	if !tt.HasText("Pods") || !tt.HasText("Models") {
		t.Fatalf("both should show: %v", tt.Texts())
	}

	m.query = "containers"
	tt.Frame()
	if !tt.HasText("Pods") || tt.HasText("Models") {
		t.Errorf("search: %v", tt.Texts())
	}

	m.query, m.tagFilter = "", "study"
	tt.Frame()
	if tt.HasText("Pods") || !tt.HasText("Models") {
		t.Errorf("tag filter: %v", tt.Texts())
	}

	m.query, m.tagFilter = "zzz", ""
	tt.Frame()
	if !tt.HasText("没有匹配的资料") {
		t.Errorf("empty state: %v", tt.Texts())
	}
}

func TestPinShortcutAndTyping(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	defer m.pl.Close()
	addMaterial(m, "Pods", "A pod runs containers.", "")
	tt := ui.NewTester(m.view, 1000, 640)
	tt.Frame()
	tt.Key(0, ui.KeyP)
	tt.Frame()
	if !m.cur.Pinned {
		t.Error("P did not pin")
	}
	// Typing the same letters into the search box must not trigger them.
	tt.Key(0, ui.KeySlash)
	tt.Frame()
	tt.Type("pq")
	tt.Frame()
	if m.query != "pq" || !m.cur.Pinned {
		t.Errorf("query %q pinned %v", m.query, m.cur.Pinned)
	}
}
```

- [ ] **Step 2: 运行确认失败** `go test -run 'TestSidebarSearch|TestPinShortcut' .` → 失败（无搜索框，无「没有匹配的资料」）。

- [ ] **Step 3: 实现**

`icons.go` 追加：

```go
var iconPin = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M8 1.5l2.1 4.4 4.9.6-3.6 3.4.9 4.8L8 12.3l-4.3 2.4.9-4.8L1 6.5l4.9-.6z"/></svg>`))
```

新建 `library_view.go`：

```go
package main

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"repi/internal/store"
)

// library is the sidebar's list: the search box, the tags, and the materials
// matching them. It returns the ID the user chose, or "" if none changed.
func (m *model) library(c *ui.Context) (chosen string) {
	t := c.Theme()
	sel := ""
	if m.open {
		sel = strconv.Itoa(m.cur.ID)
	}

	search := ui.SearchField(c, &m.query).Placeholder("搜索  /").Label("搜索资料").Margin(0, 16)
	if m.focusSearch {
		search.Focus()
		m.focusSearch = false
	}

	tags := store.Tags(m.materials)
	if m.tagFilter != "" && !hasTagCount(tags, m.tagFilter) {
		m.tagFilter = "" // the last material with it is gone
	}
	if len(tags) > 0 {
		ui.Row(c).Wrap().Gap(4).Padding(0, 16).Children(func() {
			for _, tg := range tags {
				on := tg.Name == m.tagFilter
				b := ui.Button(c.Key("tag:"+tg.Name), fmt.Sprintf("%s %d", tg.Name, tg.Count)).Label("标签 " + tg.Name).FontSize(11)
				if on {
					b.BorderColor(t.Accent).TextColor(t.Accent)
				}
				if b.Clicked() {
					if on {
						m.tagFilter = ""
					} else {
						m.tagFilter = tg.Name
					}
				}
			}
		})
	}

	hits := store.Filter(m.materials, m.query, m.tagFilter)
	chosen = sel
	if len(hits) == 0 {
		ui.Text(c, "没有匹配的资料").TextColor(t.TextMuted).FontSize(12).Padding(8, 16)
		return chosen
	}
	ui.Sidebar(c, &chosen, func() {
		for _, h := range hits {
			ui.SidebarItem(c.Key(h.ID), strconv.Itoa(h.ID), nil, h.Title)
		}
	}).Grow(1).Label("资料列表")
	return chosen
}

func hasTagCount(tags []store.TagCount, name string) bool {
	for _, t := range tags {
		if t.Name == name {
			return true
		}
	}
	return false
}
```

注意：`ui.SidebarItem` 只显示标题；标签、置顶星和摘要要放进列表项。先用上面的最小版本让测试通过，再在 Step 5 把列表项换成自绘行。

`view.go`：

1. 快捷键块（`!m.editing` 里，`?` 之后）加：

```go
		if c.Shortcut(0, ui.KeySlash) {
			m.focusSearch = true
		}
		if c.Shortcut(0, ui.KeyP) {
			m.togglePin()
		}
```

2. `sidebar` 里删掉 `sel`、`chosen`、`ui.Sidebar(...)` 这段，换成 `chosen := m.library(c)`，其后的 `if chosen != sel` 逻辑保留，`sel` 在那里重新算：`sel := ""; if m.open { sel = strconv.Itoa(m.cur.ID) }`。

3. `reader` 顶部那一行里「大屏」按钮前加：

```go
		pin := "置顶"
		if m.cur.Pinned {
			pin = "取消置顶"
		}
		if ui.Button(c, pin).Clicked() {
			m.togglePin()
		}
```

4. `editor` 里标题输入后加：

```go
	ui.TextInput(c, &m.draftTags).Placeholder("空格或逗号分隔").Label("标签")
```

5. `helpOverlay` 的快捷键表加 `{"/", "搜索资料"}`、`{"P", "置顶 / 取消置顶"}`。

- [ ] **Step 4: 运行确认通过** `go vet ./... && go test ./...` → PASS。如果 `Margin`、`Placeholder` 等方法在 `SearchField` 上不存在，按编译错误换成 `Padding` 或包一层 `ui.Row`。

- [ ] **Step 5: 自绘列表项** 把 `ui.Sidebar` 换成可滚动的列表：每项是一个 `ui.Column`，第一行 `iconPin`（仅置顶）加标题，第二行标签（灰色 11 号），第三行 `Snippet`（灰色 11 号，仅有时）。当前项与悬停沿用朗读页句子行的样式（2px 强调色左边框加 `t.Text.Alpha(0.08)`）。点击返回该项 ID 给 `chosen`。跑 `go test ./...`，再用临时测试渲染一帧 PNG 看实际样子。

- [ ] **Step 6: 构建并安装** `go vet ./... && go test ./... && bin/install`。

## 自查

- 规格里的 `Filter`/`Tags`/`NormalizeTags`：Task 2。数据字段和 `SetPinned`：Task 1。`m.cur` 同步：Task 3。侧边栏、按钮、编辑器、快捷键、帮助：Task 4。`Esc` 清空搜索：`ui.SearchField` 自带；标签过滤随「清空」按钮不一并清除，行为是标签再点一次取消，这点与规格里「`Esc` 同时取消标签过滤」不同，已在规格里改为只清空搜索。
- 目录不是 git 仓库，计划里不含提交步骤。
