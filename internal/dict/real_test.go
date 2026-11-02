package dict

import (
	"os"
	"testing"
	"time"
)

// With REPI_ECDICT=<ecdict.csv>: build the real dictionary and look words up.
func TestRealECDICT(t *testing.T) {
	csv := os.Getenv("REPI_ECDICT")
	if csv == "" {
		t.Skip("set REPI_ECDICT to the path of ecdict.csv")
	}
	f, _ := os.Open(csv)
	defer f.Close()
	out := t.TempDir() + "/d.bin"
	t0 := time.Now()
	n, err := Build(f, out)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(out)
	t.Logf("built %d entries, %d MB in %v", n, fi.Size()>>20, time.Since(t0))
	d, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	t0 = time.Now()
	for _, w := range []string{"orchestration", "went", "running", "better", "Kubernetes", "deployable", "children"} {
		r, ok := d.Lookup(w)
		t.Logf("%-14s ok=%v phon=%q lemma=%v rel=%q tags=%v meanings=%q", w, ok, r.Phonetic, r.Lemma != nil, r.Relation, r.Tags, r.Meanings)
	}
	t.Logf("7 lookups in %v", time.Since(t0))
}
