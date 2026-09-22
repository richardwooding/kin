// Package namematch compares a person's name against the words of a catalogue
// description. Unlike the loose folding the archive scorers use, matching here
// is exact by default, because the services these callers query stem their
// search terms: asking The Gazette or Discovery for Wooding also returns Wood,
// and only an exact token tells the two apart. One edit is allowed where the
// text is optical character recognition of print.
package namematch

import (
	"strings"
	"unicode"

	"github.com/richardwooding/kin/internal/model"
)

// folded spells the Nordic and accented letters the way English-language
// indexes transcribe them, so Bjørn and Bjorn are the same word.
var folded = map[rune]string{
	'à': "A", 'á': "A", 'â': "A", 'ä': "A", 'å': "A", 'ã': "A", 'æ': "AE",
	'ç': "C", 'è': "E", 'é': "E", 'ê': "E", 'ë': "E", 'ì': "I", 'í': "I",
	'î': "I", 'ï': "I", 'ñ': "N", 'ò': "O", 'ó': "O", 'ô': "O", 'ö': "O",
	'õ': "O", 'ø': "O", 'ù': "U", 'ú': "U", 'û': "U", 'ü': "U", 'ý': "Y",
	'ÿ': "Y", 'ð': "D", 'þ': "TH", 'ß': "SS",
}

// Fold upper-cases s and replaces accented letters with their plain spelling;
// other characters are kept.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range s {
		l := unicode.ToLower(r)
		switch {
		case l >= 'a' && l <= 'z':
			b.WriteRune(l - 32)
		case folded[l] != "":
			b.WriteString(folded[l])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Tokens splits a text into upper-case runs of letters, accents folded.
func Tokens(s string) []string {
	return strings.FieldsFunc(Fold(s), func(r rune) bool { return r < 'A' || r > 'Z' })
}

// Index returns the position of the first word equal to term, or -1.
// A trailing possessive or plural s on the word is tolerated.
func Index(words []string, term string) int {
	t := Fold(strings.TrimSpace(term))
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
	t := Fold(strings.TrimSpace(term))
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
	"NORWAY": true, "NORGE": true, "SWEDEN": true, "SVERIGE": true, "DENMARK": true,
	"DANMARK": true, "FINLAND": true, "SUOMI": true, "ICELAND": true, "ISLAND": true,
	"FYLKE": true, "SOGN": true, "SOCKEN": true, "FORSAMLING": true, "HERRED": true,
	"KOMMUNE": true, "LAENS": true, "PAROCHIAL": true, "KIRKE": true, "KYRKA": true,
	"KIRKJA": true, "NORRE": true, "SONDRE": true, "NORRA": true, "SODRA": true,
	"VESTRE": true, "OSTRE": true, "STORE": true, "LILLE": true, "STORA": true, "LILLA": true,
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

// Nordic reduces a Scandinavian name to a key that ignores how it was spelt:
// Danish aa for å, C or K, doubled letters, and the patronymic endings, so
// Andersen, Anderssen and Andersson are one name, as are Christensdatter and
// Kristensdotter.
func Nordic(name string) string {
	s := strings.Join(Tokens(name), "")
	if s == "" {
		return ""
	}
	for _, end := range []struct{ from, to string }{
		{"SSON", "SEN"}, {"SSEN", "SEN"}, {"SON", "SEN"}, {"SEN", "SEN"},
		{"DOTTIR", "DATTER"}, {"DOTTER", "DATTER"}, {"DATTER", "DATTER"}, {"DOTER", "DATTER"},
	} {
		if strings.HasSuffix(s, end.from) && len(s) > len(end.from)+1 {
			s = s[:len(s)-len(end.from)] + end.to
			break
		}
	}
	if strings.HasPrefix(s, "CH") {
		s = "K" + s[2:]
	} else if strings.HasPrefix(s, "C") {
		s = "K" + s[1:]
	}
	s = strings.NewReplacer("AA", "A", "TH", "T", "PH", "F", "CK", "K", "W", "V", "Z", "S", "Q", "K", "X", "KS").Replace(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && s[i] == s[i-1] {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Patronym reports whether two Scandinavian names are spellings of one name.
func Patronym(a, b string) bool {
	ka := Nordic(a)
	return ka != "" && ka == Nordic(b)
}

// NordicIn reports whether given is one of the words of names however it is
// spelt: Kristina and Christina, Petter and Peter.
func NordicIn(given, names string) bool {
	k := Nordic(given)
	if k == "" {
		return false
	}
	for _, w := range Tokens(names) {
		if Nordic(w) == k {
			return true
		}
	}
	return false
}

// Names splits a person into a surname and their given names, from the name
// fields when both are set and otherwise from the display name. Single
// letters are dropped.
func Names(p *model.Person) (string, []string) {
	surname := strings.TrimSpace(p.Surname)
	given := strings.TrimSpace(p.Given)
	if surname == "" || given == "" {
		name := p.Name
		if i := strings.IndexByte(name, '('); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		if fields := strings.Fields(name); len(fields) > 1 {
			if surname == "" {
				surname = fields[len(fields)-1]
			}
			if given == "" {
				given = strings.Join(fields[:len(fields)-1], " ")
			}
		}
	}
	var givens []string
	for _, g := range strings.Fields(given) {
		if len([]rune(g)) > 1 {
			givens = append(givens, g)
		}
	}
	return surname, givens
}
