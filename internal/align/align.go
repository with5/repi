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

// Token is a word with the punctuation stuck to it, as shown on screen.
type Token struct {
	Text   string
	Weight float64 // how long it takes to say, in syllables
	Pause  float64 // the stop after it, in syllables
}

// Pause lengths, in syllables, after a mid-sentence punctuation mark.
const (
	commaPause = 0.7
	stopPause  = 1.4 // . ? ! inside a sentence ("Wait... what?")
	dashPause  = 0.6
)

// Tokens splits a sentence at spaces.
func Tokens(sentence string) []Token {
	var out []Token
	for _, w := range strings.Fields(sentence) {
		t := Token{Text: w, Weight: weight(w)}
		switch last := lastRune(w); {
		case last == ',' || last == ';' || last == ':':
			t.Pause = commaPause
		case last == '.' || last == '?' || last == '!' || last == '…':
			t.Pause = stopPause
		case last == '—' || last == '–' || w == "-" || w == "--":
			t.Pause = dashPause
		}
		out = append(out, t)
	}
	if n := len(out); n > 0 {
		out[n-1].Pause = 0 // the end is trimmed away with the silence
	}
	return out
}

func lastRune(s string) rune {
	r := []rune(strings.TrimRight(s, `"'”’)]`))
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

// weight estimates the syllables of a written word: groups of vowels, less a
// silent final "e". Digits are said digit by digit; a token with no letters
// (a dash) takes no time.
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
