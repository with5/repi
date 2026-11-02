// Package dict is an offline English–Chinese dictionary built from ECDICT.
// The CSV is converted once into one sorted, indexed file that is memory
// mapped, so a lookup is a binary search and costs no memory to keep around.
package dict

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

const magic = "RPDICT1\n"

// Entry is one dictionary headword.
type Entry struct {
	Word     string
	Phonetic string
	Meanings []string // one per line, e.g. "n. 赛跑, 流出"
	Tags     []string // exam labels, e.g. "高考", "四级"
	Exchange string   // raw ECDICT exchange field, "p:went/d:gone/0:go/1:p"
}

// Form is a word form of a headword: "过去式", "went".
type Form struct{ Label, Word string }

// Result is what a lookup of a written word finds.
type Result struct {
	Entry
	Lemma    *Entry // the base form, when the word is an inflection of it
	Relation string // "过去式", "过去分词、过去式"; set with Lemma
	Forms    []Form // inflections of the word itself, when it is a base form
}

// Dict is an opened dictionary file.
type Dict struct {
	data    []byte
	count   int
	offsets []byte // count little-endian uint32, relative to base
	base    int
}

// Open maps the dictionary file at path.
func Open(path string) (*Dict, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(fi.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil { // not mappable: read it instead
		if data, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	}
	if len(data) < len(magic)+4 || string(data[:len(magic)]) != magic {
		return nil, errors.New("词典文件格式不对，请重新下载")
	}
	n := int(binary.LittleEndian.Uint32(data[len(magic):]))
	start := len(magic) + 4
	if start+4*n > len(data) {
		return nil, errors.New("词典文件不完整，请重新下载")
	}
	return &Dict{data: data, count: n, offsets: data[start : start+4*n], base: start + 4*n}, nil
}

// Len is the number of headwords.
func (d *Dict) Len() int { return d.count }

// record returns the fields of the i-th record: key, word, phonetic,
// translation, tag, exchange.
func (d *Dict) record(i int) (f [6]string) {
	off := d.base + int(binary.LittleEndian.Uint32(d.offsets[4*i:]))
	b := d.data[off:]
	for k := range f {
		n, w := binary.Uvarint(b)
		if w <= 0 || w+int(n) > len(b) {
			return f
		}
		f[k] = string(b[w : w+int(n)])
		b = b[w+int(n):]
	}
	return f
}

func (d *Dict) get(key string) (Entry, bool) {
	i := sort.Search(d.count, func(i int) bool {
		off := d.base + int(binary.LittleEndian.Uint32(d.offsets[4*i:]))
		n, w := binary.Uvarint(d.data[off:])
		return string(d.data[off+w:off+w+int(n)]) >= key
	})
	if i >= d.count {
		return Entry{}, false
	}
	f := d.record(i)
	if f[0] != key {
		return Entry{}, false
	}
	return Entry{Word: f[1], Phonetic: strings.ReplaceAll(f[2], "ә", "ə"), Meanings: splitLines(f[3]), Tags: tagLabels(f[4]), Exchange: f[5]}, true
}

// Lookup finds a word as written on screen: punctuation around it and a
// trailing 's are ignored, case does not matter.
func (d *Dict) Lookup(written string) (Result, bool) {
	key := Normalize(written)
	if key == "" {
		return Result{}, false
	}
	e, ok := d.get(key)
	if !ok {
		return Result{}, false
	}
	r := Result{Entry: e}
	ex := parseExchange(e.Exchange)
	if lemma := ex["0"]; lemma != "" && !strings.EqualFold(lemma, key) {
		if le, ok := d.get(strings.ToLower(lemma)); ok {
			r.Lemma = &le
			r.Relation = relation(ex["1"])
		}
	} else {
		r.Forms = forms(ex)
	}
	return r, true
}

// Normalize turns a token as written ("“Don't,") into a dictionary key.
func Normalize(w string) string {
	w = strings.NewReplacer("’", "'", "‘", "'").Replace(w)
	w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	w = strings.ToLower(w)
	if s := strings.TrimSuffix(w, "'s"); s != "" && s != w {
		if _, ok := map[string]bool{"it": true, "he": true, "she": true, "that": true, "what": true, "there": true, "let": true, "who": true, "here": true}[s]; !ok {
			w = s
		}
	}
	return w
}

var tagNames = map[string]string{
	"zk": "中考", "gk": "高考", "cet4": "四级", "cet6": "六级",
	"ky": "考研", "toefl": "托福", "ielts": "雅思", "gre": "GRE",
}

func tagLabels(tag string) []string {
	var out []string
	for _, t := range strings.Fields(tag) {
		if l, ok := tagNames[t]; ok {
			out = append(out, l)
		}
	}
	return out
}

// splitLines splits an ECDICT text field, whose line breaks are written as
// a backslash and an n.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func parseExchange(s string) map[string]string {
	m := map[string]string{}
	for _, p := range strings.Split(s, "/") {
		if k, v, ok := strings.Cut(p, ":"); ok {
			m[k] = v
		}
	}
	return m
}

var formLabels = []Form{
	{"p", "过去式"}, {"d", "过去分词"}, {"i", "现在分词"}, {"3", "第三人称单数"},
	{"s", "复数"}, {"r", "比较级"}, {"t", "最高级"},
}

// relation names how a word relates to its base form, from the codes of
// ECDICT's "1:" field ("pd" is past tense and past participle).
func relation(codes string) string {
	var out []string
	for _, c := range codes {
		for _, f := range formLabels {
			if f.Label == string(c) {
				out = append(out, f.Word)
			}
		}
	}
	return strings.Join(out, "、")
}

func forms(ex map[string]string) []Form {
	var out []Form
	for _, f := range formLabels {
		if w := ex[f.Label]; w != "" {
			out = append(out, Form{f.Word, w})
		}
	}
	return out
}

// header of the written file: magic, count, offsets, then the records.
func writeFile(recs []rec) []byte {
	var data bytes.Buffer
	offs := make([]byte, 4*len(recs))
	var tmp [binary.MaxVarintLen64]byte
	for i, r := range recs {
		binary.LittleEndian.PutUint32(offs[4*i:], uint32(data.Len()))
		for _, s := range [6]string{r.key, r.word, r.phonetic, r.trans, r.tag, r.exchange} {
			data.Write(tmp[:binary.PutUvarint(tmp[:], uint64(len(s)))])
			data.WriteString(s)
		}
	}
	var out bytes.Buffer
	out.WriteString(magic)
	binary.Write(&out, binary.LittleEndian, uint32(len(recs)))
	out.Write(offs)
	out.Write(data.Bytes())
	return out.Bytes()
}

type rec struct{ key, word, phonetic, trans, tag, exchange string }
