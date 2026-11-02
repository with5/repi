package dict

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio
go,gəu,,"vi. 走, 去\nn. 进行",,,,zk gk cet4,1,1,p:went/d:gone/i:going/3:goes,,
went,went,,go的过去式,,,,,0,0,0:go/1:p,,
gone,gɔn,,"a. 离去的\nv. go的过去分词",,,,,0,0,0:go/1:d,,
Apple,æpl,,n. 苹果（大写）,,,,,0,0,s:Apples,,
apple,'æpәl,,n. 苹果,,,,cet6,0,0,s:apples,,
nothing,nʌθiŋ,,,,,,,0,0,,,
ice cream,,,冰淇淋,,,,,0,0,,,
`

func build(t *testing.T) *Dict {
	t.Helper()
	out := filepath.Join(t.TempDir(), "d.bin")
	n, err := Build(strings.NewReader(sample), out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 { // no meaning and phrases left out, the apples share one
		t.Fatalf("entries = %d, want 4", n)
	}
	d, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLookupBase(t *testing.T) {
	d := build(t)
	r, ok := d.Lookup("“Go,")
	if !ok || r.Word != "go" || r.Phonetic != "gəu" {
		t.Fatalf("go: %+v %v", r, ok)
	}
	if len(r.Meanings) != 2 || r.Meanings[1] != "n. 进行" {
		t.Errorf("meanings = %q", r.Meanings)
	}
	if strings.Join(r.Tags, " ") != "中考 高考 四级" {
		t.Errorf("tags = %q", r.Tags)
	}
	if len(r.Forms) != 4 || r.Forms[0] != (Form{"过去式", "went"}) {
		t.Errorf("forms = %+v", r.Forms)
	}
	if r.Lemma != nil {
		t.Error("a base form has no lemma")
	}
}

func TestLookupInflection(t *testing.T) {
	d := build(t)
	r, ok := d.Lookup("Went")
	if !ok || r.Lemma == nil || r.Lemma.Word != "go" || r.Relation != "过去式" {
		t.Fatalf("went: %+v %v", r, ok)
	}
	if r, _ := d.Lookup("gone."); r.Relation != "过去分词" {
		t.Errorf("gone relation = %q", r.Relation)
	}
}

func TestLookupMisses(t *testing.T) {
	d := build(t)
	for _, w := range []string{"", "...", "zzzz", "nothing", "ice"} {
		if _, ok := d.Lookup(w); ok {
			t.Errorf("%q found", w)
		}
	}
	if r, ok := d.Lookup("APPLE's"); !ok || r.Word != "apple" || r.Meanings[0] != "n. 苹果" || r.Phonetic != "'æpəl" {
		t.Errorf("apple: %+v %v", r, ok)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"“Hello,": "hello", "don’t": "don't", "it's": "it's", "Tom's": "tom",
		"well-known.": "well-known", "(42)": "42", "—": "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "sub", "d.bin")
	var steps []string
	if err := Download(context.Background(), srv.URL, out, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	d, err := Open(out)
	if err != nil || d.Len() != 4 {
		t.Fatalf("open: %v", err)
	}
	if len(steps) == 0 || steps[len(steps)-1] != "正在建立索引…" {
		t.Errorf("progress = %q", steps)
	}
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	if err := Download(context.Background(), bad.URL, out+"2", func(string) {}); err == nil {
		t.Error("404 not reported")
	}
}
