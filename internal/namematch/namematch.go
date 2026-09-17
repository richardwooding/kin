// Package namematch compares a person's name against the words of a catalogue
// description. Unlike the loose folding the archive scorers use, matching here
// is exact by default, because the services these callers query stem their
// search terms: asking The Gazette or Discovery for Wooding also returns Wood,
// and only an exact token tells the two apart. One edit is allowed where the
// text is optical character recognition of print.
package namematch

import "strings"

// Tokens splits a text into upper-case runs of letters.
func Tokens(s string) []string {
	var out []string
	var cur []rune
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			cur = append(cur, r-32)
		case r >= 'A' && r <= 'Z':
			cur = append(cur, r)
		default:
			if len(cur) > 0 {
				out = append(out, string(cur))
				cur = nil
			}
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// Index returns the position of the first word equal to term, or -1.
// A trailing possessive or plural s on the word is tolerated.
func Index(words []string, term string) int {
	t := strings.ToUpper(strings.TrimSpace(term))
	if t == "" {
		return -1
	}
	for i, w := range words {
		if w == t || (len(w) == len(t)+1 && w[len(w)-1] == 'S' && w[:len(w)-1] == t) {
			return i
		}
	}
	return -1
}

// Exact reports whether term appears as a whole word.
func Exact(words []string, term string) bool { return Index(words, term) >= 0 }

// OCR is Exact, or a single edit away from a word, for names long enough that
// one scanning error still leaves the name recognisable.
func OCR(words []string, term string) bool {
	if Exact(words, term) {
		return true
	}
	t := strings.ToUpper(strings.TrimSpace(term))
	if len(t) < 6 {
		return false
	}
	for _, w := range words {
		if Lev1(w, t) {
			return true
		}
	}
	return false
}

// Lev1 reports whether a and b are one substitution, insertion or deletion apart.
func Lev1(a, b string) bool {
	if a == b {
		return true
	}
	switch {
	case len(a) == len(b):
		diff := 0
		for i := range a {
			if a[i] != b[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		return diff == 1
	case len(a) == len(b)+1:
		return oneMore(a, b)
	case len(b) == len(a)+1:
		return oneMore(b, a)
	}
	return false
}

// oneMore reports whether long is short with one extra letter.
func oneMore(long, short string) bool {
	i := 0
	for i < len(short) && long[i] == short[i] {
		i++
	}
	return long[i+1:] == short[i:]
}

// Near reports whether words a and b both appear within span words of each
// other, in either order.
func Near(words []string, a, b string, span int) bool {
	ia, ib := Index(words, a), Index(words, b)
	if ia < 0 || ib < 0 {
		return false
	}
	d := ia - ib
	if d < 0 {
		d = -d
	}
	return d <= span
}

// dropped are the county and country words that say nothing about which
// parish or town a record belongs to.
var dropped = map[string]bool{
	"ENGLAND": true, "WALES": true, "SCOTLAND": true, "IRELAND": true, "BRITAIN": true,
	"UNITED": true, "KINGDOM": true, "COUNTY": true, "SHIRE": true, "DISTRICT": true,
	"CORNWALL": true, "DEVON": true, "SURREY": true, "MIDDLESEX": true, "KENT": true,
	"HAMPSHIRE": true, "LANCASHIRE": true, "YORKSHIRE": true, "SOUTH": true, "AFRICA": true,
	"CAPE": true, "COLONY": true, "PROVINCE": true, "THEN": true, "NEAR": true,
}

// PlaceTokens returns the distinctive words of a place text: the parish or
// town names, without the counties and countries that match everything.
func PlaceTokens(places string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range Tokens(places) {
		if len(w) < 4 || dropped[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}
