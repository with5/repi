// Package text splits study material, English or Chinese, into sentences.
package text

import (
	"strings"
	"unicode"
)

const (
	cjkStops = "。！？"
	stops    = ".?!…" + cjkStops
	// closers stay with the sentence they close.
	closers = `"'”’)]」』）》】`
)

// abbreviations whose trailing period does not end a sentence.
var abbreviations = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true, "sr": true, "jr": true,
	"st": true, "vs": true, "etc": true, "inc": true, "ltd": true, "co": true,
	"e.g": true, "i.e": true, "no": true, "fig": true, "approx": true, "u.s": true,
}

// Split cuts s into sentences: at . ? ! (and their runs, closing quotes and
// brackets) followed by space, at Chinese 。！？ wherever they are, and at
// line breaks. ? and ! end a sentence before a Chinese character too.
// Abbreviations such as "Mr." and decimals such as "3.14" do not end a
// sentence. Empty results are dropped and whitespace is collapsed.
func Split(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		out = append(out, splitLine(line)...)
	}
	return out
}

func splitLine(line string) []string {
	rs := []rune(line)
	var out []string
	start := 0
	flush := func(end int) {
		if t := strings.Join(strings.Fields(string(rs[start:end])), " "); hasLetterOrDigit(t) {
			out = append(out, t)
		}
		start = end
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if !strings.ContainsRune(stops, r) {
			continue
		}
		j := i + 1
		for j < len(rs) && strings.ContainsRune(stops, rs[j]) {
			j++
		}
		cjk := strings.ContainsAny(string(rs[i:j]), cjkStops)
		for j < len(rs) && strings.ContainsRune(closers, rs[j]) {
			j++
		}
		if !cjk && j < len(rs) && !unicode.IsSpace(rs[j]) && !(r != '.' && r != '…' && IsHan(rs[j])) {
			i = j - 1 // "3.14", "e.g.x": not a boundary
			continue
		}
		if r == '.' && j == i+1 && isAbbreviation(rs, i) {
			continue
		}
		flush(j)
		i = j - 1
	}
	flush(len(rs))
	return out
}

// isAbbreviation reports whether the word ending at the period rs[dot] is
// a known abbreviation or a single capital initial ("J. K. Rowling").
func isAbbreviation(rs []rune, dot int) bool {
	k := dot
	for k > 0 && !unicode.IsSpace(rs[k-1]) {
		k--
	}
	w := strings.ToLower(strings.Trim(string(rs[k:dot]), `"'“‘([`))
	if abbreviations[w] {
		return true
	}
	return len([]rune(w)) == 1 && unicode.IsUpper(rs[k])
}

// IsHan reports whether r is a Chinese character.
func IsHan(r rune) bool { return unicode.Is(unicode.Han, r) }

// IsChinese reports whether s is Chinese material: it has Chinese
// characters, at least as many as English words (a character says about as
// much as a word), so English words inside Chinese keep it Chinese and a
// Chinese gloss in English text does not.
func IsChinese(s string) bool {
	han, words := 0, 0
	inWord := false
	for _, r := range s {
		latin := r < 0x250 && unicode.IsLetter(r)
		if latin && !inWord {
			words++
		}
		inWord = latin
		if IsHan(r) {
			han++
		}
	}
	return han > 0 && han >= words
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
