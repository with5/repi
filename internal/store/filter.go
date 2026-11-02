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
