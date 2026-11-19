package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/dict"
	"repi/internal/store"
)

// A word looked up on the big screen is saved by its base form, with the
// sentence it was met in; the word book lists it and a review grades it.
func TestWordBook(t *testing.T) {
	m, tt := bigModel(t)
	if _, err := dict.Build(strings.NewReader(dictSample), m.dic.path); err != nil {
		t.Fatal(err)
	}
	m.dic.reopen()
	tt.Frame()
	if err := tt.Click("went"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "went 是 go 的过去式")
	if err := tt.Click("加入生词本"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("go 已在生词本里") {
		t.Fatalf("not marked as saved: %q", tt.Texts())
	}
	ws := m.st.Words()
	if len(ws) != 1 || ws[0].Word != "go" || ws[0].Form != "went" || ws[0].Sentence != "We went home." ||
		ws[0].Material != m.cur.ID || len(ws[0].Meanings) != 2 {
		t.Fatalf("saved %+v", ws)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	if !tt.HasText("生词 1") {
		t.Errorf("sidebar does not count the due word: %q", tt.Texts())
	}
	if err := tt.Click("生词本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"go", "vi. 走, 去", "待复习", "开始复习（1）"} {
		if !tt.HasText(want) {
			t.Errorf("list lacks %q: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("开始复习"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("复习生词") || tt.HasText("vi. 走, 去") {
		t.Fatalf("review shows the meaning before Space: %q", tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if !tt.HasText("vi. 走, 去") || !tt.HasText("1 天") {
		t.Fatalf("Space did not reveal the meaning: %q", tt.Texts())
	}
	if m.pl.State().Playing {
		t.Error("Space in the review started the reading")
	}

	// Forgotten: it comes back in the same session.
	tt.Key(0, ui.Key1)
	tt.Frame()
	if !tt.HasText("复习生词") || tt.HasText("vi. 走, 去") {
		t.Fatalf("forgotten word not shown again: %q", tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	tt.Key(0, ui.Key3)
	tt.Frame()
	if !tt.HasText("今天的生词复习完了") {
		t.Fatalf("session did not end: %q", tt.Texts())
	}
	w := m.st.Words()[0]
	if w.Lapses != 1 || w.Reps != 1 || !w.Due.After(time.Now()) {
		t.Errorf("schedule after review: %+v", w)
	}

	again, err := store.Open(store.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := again.DueCount(time.Now()); got != 0 || len(again.Words()) != 1 {
		t.Errorf("on disk: due %d, words %d", got, len(again.Words()))
	}

	if err := tt.Click("回到生词本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("明天复习") || !tt.HasText("今天没有要复习的") {
		t.Errorf("list after review: %q", tt.Texts())
	}
	if err := tt.Click("删除 go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(m.st.Words()) != 0 || !tt.HasText("生词本还是空的") {
		t.Errorf("not deleted: %q", tt.Texts())
	}
}

func TestSentenceMarksWholeWord(t *testing.T) {
	w := store.Word{Word: "go", Sentence: "A good go at it."}
	re := wordRE
	defer func() { wordRE = re }()
	tt := ui.NewTester(func(c *ui.Context) {
		sentenceText(c, w, ui.Color{}, ui.Color{})
	}, 400, 100)
	tt.Frame()
	if !tt.HasText("A good go at it.") {
		t.Fatalf("texts: %q", tt.Texts())
	}
	if loc := wordRE["go"].FindStringIndex(w.Sentence); loc == nil || loc[0] != 7 {
		t.Errorf("matched %v, want the standalone go at 7", loc)
	}
}
