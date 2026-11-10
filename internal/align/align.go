// Package align estimates which word of a sentence is being spoken at a given
// time. TTS engines give no word timestamps, so the time of the speech is
// shared among the words by their length in syllables, with a longer stop
// after commas and the like. It is an estimate, good to within a word or so.
package align

import (
	"strings"
	"time"
	"unicode"
)

// Token is a word with the punctuation stuck to it, as shown on screen. In
// Chinese a token is one character.
type Token struct {
	Text   string
	Weight float64 // how long it takes to say, in syllables
	Pause  float64 // the stop after it, in syllables
	Space  bool    // a space parts it from the token before
}

// Pause lengths, in syllables, after a mid-sentence punctuation mark.
const (
	commaPause = 0.7
	stopPause  = 1.4 // . ? ! inside a sentence ("Wait... what?")
	dashPause  = 0.6
)

// Tokens splits a sentence at spaces; a sentence with Chinese characters
// into characters, with English words and numbers kept whole.
func Tokens(sentence string) []Token {
	var out []Token
	if strings.IndexFunc(sentence, isHan) >= 0 {
		out = hanTokens(sentence)
	} else {
		for i, w := range strings.Fields(sentence) {
			out = append(out, Token{Text: w, Weight: weight(w), Space: i > 0})
		}
	}
	for i := range out {
		out[i].Pause = pauseAfter(out[i].Text)
	}
	if n := len(out); n > 0 {
		out[n-1].Pause = 0 // the end is trimmed away with the silence
	}
	return out
}

func isHan(r rune) bool { return unicode.Is(unicode.Han, r) }

// pauseAfter is the stop the punctuation at the end of a token makes.
func pauseAfter(w string) float64 {
	switch last := lastRune(w); {
	case strings.ContainsRune(",;:，、；：", last):
		return commaPause
	case strings.ContainsRune(".?!…。！？", last):
		return stopPause
	case last == '—' || last == '–' || w == "-" || w == "--":
		return dashPause
	}
	return 0
}

// openers stick to the token after them, other punctuation to the one before.
const openers = "“‘「『（《【([<"

// hanTokens makes a token of each Chinese character and of each run of
// other letters and digits ("iPhone", "3.14"). Punctuation sticks to its
// token; a space only marks the next token.
func hanTokens(s string) []Token {
	var out []Token
	var word []rune // the run being read
	prefix, space := "", false
	add := func(text string) {
		out = append(out, Token{Text: prefix + text, Weight: weight(text), Space: space && len(out) > 0})
		prefix, space = "", false
	}
	flush := func() {
		if len(word) > 0 {
			add(string(word))
			word = nil
		}
	}
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			flush()
			space = true
		case isHan(r):
			flush()
			add(string(r))
		case strings.ContainsRune(openers, r):
			flush()
			prefix += string(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || len(word) > 0 && !isCJKPunct(r):
			word = append(word, r) // "don't", "3.14", "e-mail" and the like
		case len(out) > 0 || len(word) > 0:
			flush()
			out[len(out)-1].Text += string(r) // "，" "。" "”" after a token
		default:
			prefix += string(r) // punctuation before any token
		}
	}
	flush()
	if prefix != "" && len(out) > 0 {
		out[len(out)-1].Text += prefix
	}
	return out
}

// isCJKPunct reports whether r is full-width punctuation, which ends a run
// of Latin letters.
func isCJKPunct(r rune) bool {
	return r >= 0x3000 && r <= 0x303F || r >= 0xFF00 && r <= 0xFF65 || strings.ContainsRune("“”‘’—…·", r)
}

func lastRune(s string) rune {
	r := []rune(strings.TrimRight(s, `"'”’)]」』）》】`))
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

// weight estimates the syllables of a written word: groups of vowels, less a
// silent final "e". A Chinese character is one syllable. Digits are said
// digit by digit; a token with no letters (a dash) takes no time.
func weight(w string) float64 {
	total := 0.0
	for _, part := range strings.FieldsFunc(w, func(r rune) bool { return r == '-' || r == '—' || r == '–' || r == '/' }) {
		total += partWeight(part)
	}
	return total
}

func partWeight(s string) float64 {
	s = strings.ToLower(strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
	if s == "" {
		return 0
	}
	if r := []rune(s); len(r) == 1 && isHan(r[0]) {
		return 1
	}
	syl, digits, letters := 0, 0, 0
	inVowel := false
	rs := []rune(s)
	for _, r := range rs {
		switch {
		case unicode.IsDigit(r):
			digits++
			inVowel = false
		case unicode.IsLetter(r):
			letters++
			if strings.ContainsRune("aeiouy", r) {
				if !inVowel {
					syl++
				}
				inVowel = true
			} else {
				inVowel = false
			}
		}
	}
	// silent final e: "make", "time"; not "the", "be", or "-le" as in "table"
	if n := len(rs); n > 2 && rs[n-1] == 'e' && !strings.ContainsRune("aeiouy", rs[n-2]) && !(rs[n-2] == 'l' && n > 3 && !strings.ContainsRune("aeiouy", rs[n-3])) && syl > 1 {
		syl--
	}
	if letters > 0 && syl == 0 {
		syl = 1 // "mr", "nth", acronyms
		if letters > 3 {
			syl = letters / 2 // spelled out: "HTTP"
		}
	}
	return float64(syl) + 1.2*float64(digits)
}

// Spans returns when each token has been said, as the end time of every token,
// given that the speech runs from start to end. A token's span includes the
// stop after it.
func Spans(tokens []Token, start, end time.Duration) []time.Duration {
	total := 0.0
	for _, t := range tokens {
		total += t.Weight + t.Pause
	}
	ends := make([]time.Duration, len(tokens))
	if total == 0 || end <= start {
		for i := range ends {
			ends[i] = end
		}
		return ends
	}
	unit := float64(end-start) / total
	acc := 0.0
	for i, t := range tokens {
		acc += t.Weight + t.Pause
		ends[i] = start + time.Duration(acc*unit)
	}
	return ends
}

// Index returns the token being said at pos: the first one whose span has not
// ended. Before the speech it is the first, after it the last.
func Index(ends []time.Duration, pos time.Duration) int {
	for i, e := range ends {
		if pos < e {
			return i
		}
	}
	return len(ends) - 1
}

// Align returns when each token has been said, like Spans, but anchors the
// stops after commas and the like to the silences found in the audio: each such
// token is matched, in order, with the nearest silence to where the estimate
// puts it. Between the anchors the words share the time by their syllables. A
// stop with no silence near it keeps its estimate.
func Align(tokens []Token, sp Speech) []time.Duration {
	first := Spans(tokens, sp.Start, sp.End)
	if len(sp.Gaps) == 0 || len(tokens) < 2 {
		return first
	}
	total := 0.0
	for _, t := range tokens {
		total += t.Weight + t.Pause
	}
	unit := float64(sp.End-sp.Start) / total
	tol := (sp.End-sp.Start)/6 + 300*time.Millisecond

	// match[i] is the silence after token i, if one was found.
	match := map[int]Gap{}
	next := 0
	for i, t := range tokens[:len(tokens)-1] {
		if t.Pause == 0 {
			continue
		}
		expect := first[i] - time.Duration(t.Pause*unit/2)
		best, bestD := -1, tol
		for g := next; g < len(sp.Gaps); g++ {
			d := (sp.Gaps[g].Start+sp.Gaps[g].End)/2 - expect
			if d < 0 {
				d = -d
			}
			if d < bestD {
				best, bestD = g, d
			}
		}
		if best >= 0 {
			match[i] = sp.Gaps[best]
			next = best + 1
		}
	}

	ends := make([]time.Duration, len(tokens))
	from, a := sp.Start, 0 // start of the segment, its first token
	for i := range tokens {
		g, anchored := match[i]
		if !anchored && i != len(tokens)-1 {
			continue
		}
		to := sp.End // the speech of the segment ends at the silence, or the end
		if anchored {
			to = g.Start
		}
		w := 0.0
		for k := a; k <= i; k++ {
			w += tokens[k].Weight
			if k < i || !anchored {
				w += tokens[k].Pause // stops without a silence of their own
			}
		}
		acc := 0.0
		for k := a; k <= i; k++ {
			acc += tokens[k].Weight
			if k < i || !anchored {
				acc += tokens[k].Pause
			}
			if w > 0 {
				ends[k] = from + time.Duration(float64(to-from)*acc/w)
			} else {
				ends[k] = to
			}
		}
		if anchored {
			ends[i] = g.End // the word stays highlighted through its stop
			from = g.End
		}
		a = i + 1
	}
	return ends
}
