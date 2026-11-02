package store

import (
	"reflect"
	"strings"
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
	if got := titles(Filter(sample, "pod containers", "")); !reflect.DeepEqual(got, []string{"Pods"}) {
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
	if got := titles(Filter(sample, "", "WORK")); !reflect.DeepEqual(got, []string{"Pods", "通义千问"}) {
		t.Errorf("got %v", got)
	}
}

func TestFilterPinnedFirstStable(t *testing.T) {
	if got := titles(Filter(sample, "", "")); !reflect.DeepEqual(got, []string{"AI engineers", "Pods", "通义千问"}) {
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
	long := Material{Title: "T", Text: "word " + strings.Repeat("x", 100) + "."}
	hits := Filter([]Material{long}, "word", "")
	if n := len([]rune(hits[0].Snippet)); n > 61 {
		t.Errorf("snippet has %d runes", n)
	}
}

func TestTagsCounted(t *testing.T) {
	want := []TagCount{{"work", 2}, {"news", 1}, {"study", 1}}
	if got := Tags(sample); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags("#work, study，Work  #news;")
	if want := []string{"work", "study", "news"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if NormalizeTags("  ") != nil {
		t.Error("blank input should give nil")
	}
}
