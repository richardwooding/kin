package tna

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
)

// Keep is the score below which a catalogue entry is noise.
const Keep = 4

// ScoreOpts describe the search a record came from.
type ScoreOpts struct {
	Places   string // the person's own or their family's places
	Common   bool   // the query returned a flood of same-name records
	Frontier bool   // a search of the archives that hold records elsewhere
}

// willOf matches the way the wills and death duty registers are described:
// "Will of John Wooding, Cooper of Portsmouth, Hampshire".
var willOf = regexp.MustCompile(`(?i)^(?:abstract of )?(?:the )?(?:will|administration|probate)s? of ([^,.]+)`)

// residence matches the place a will names its testator as living at, which
// is the strongest thing in the entry for telling namesakes apart.
var residence = regexp.MustCompile(`(?i)\bof ([A-Z][^,.]*?)\s*,\s*([A-Z][^,.]*)`)

// yearRange reads the covering dates, which run from a single year to a span
// in brackets: "1763", "09 October 1773", "[1853-1872]".
var yearRange = regexp.MustCompile(`\b(1[5-9]\d\d|20\d\d)\b`)

// ScorePerson rates a catalogue entry against a person and says why.
//
// The catalogue stems its search terms, so a search for Wooding returns Wood
// and a search for Nuns returns Nun. The surname must therefore match exactly,
// with no loose folding, before anything else is counted.
func ScorePerson(p *model.Person, r Record, o ScoreOpts) (int, []string) {
	surname, givens := names(p)
	if surname == "" {
		return 0, nil
	}
	words := namematch.Tokens(r.Text())
	if !namematch.Exact(words, surname) {
		return 0, nil
	}
	score, why := 0, []string{"surname"}
	first := ""
	if len(givens) > 0 {
		first = givens[0]
	}
	switch {
	case first != "" && namematch.Exact(words, first):
		score += 2
		why = []string{"given name"}
		if namematch.Near(words, surname, first, 2) {
			score++
			why = []string{"full name"}
		}
	case initials(words, surname, givens):
		score++
		why = []string{"surname and initials"}
	}
	for _, g := range givens[1:] {
		if namematch.Near(words, surname, g, 4) {
			score++
			why = append(why, "further given names")
			break
		}
	}

	named := ""
	if m := willOf.FindStringSubmatch(r.Description); m != nil && first != "" {
		named = m[1]
		toks := namematch.Tokens(named)
		if namematch.Exact(toks, surname) && namematch.Exact(toks, first) {
			score += 2
			why = append(why, "the record is in their name")
		}
	}

	var placed string
	mine := namematch.PlaceTokens(o.Places)
	for _, tok := range mine {
		if namematch.Exact(words, tok) {
			placed = tok
			score += 2
			why = append(why, "place "+capitalise(tok))
			break
		}
	}
	// A will says where its testator lived. When that is somewhere else
	// entirely, the entry is a namesake's however well the name matches.
	if placed == "" && named != "" && len(mine) > 0 {
		if m := residence.FindStringSubmatch(r.Description); m != nil {
			return 0, nil
		}
	}
	// a family's own papers in a record office are a lead to the whole family,
	// so they are judged by the parish rather than by one person's dates
	family := o.Frontier && placed != "" && isFamilyCollection(r, surname, placed)
	if family {
		score += 2
		why = append(why, "a family's papers from that parish")
	}

	by, dy := year(p.Birth), year(p.Death)
	if from, to := dates(r.Dates); from > 0 && !family {
		switch {
		case by > 0 && to < by, dy > 0 && from > dy+60:
			return 0, nil // the record closed before they were born, or long after their estate
		case by > 0 && from <= dy+5 && (dy == 0 || to >= by):
			score += 2
			why = append(why, "dates fit their life")
		}
	}

	if o.Common && onlyNames(why) && score > 2 {
		score = 2
	}
	return score, why
}

// isFamilyCollection reports whether a record is one family's own papers,
// named for them and their parish, rather than a session roll or a register
// that merely mentions the parish among hundreds of others. The test is the
// record's own title: "Knuckey family of Stithians." names the family, while
// "Sessions held at Truro" names an administrative series.
func isFamilyCollection(r Record, surname, parish string) bool {
	title := r.Title
	if title == "" {
		title = r.Description
	}
	if len(title) > 120 {
		return false // a whole roll transcribed into the title is not a family's papers
	}
	words := namematch.Tokens(title)
	if !namematch.Exact(words, surname) || !namematch.Exact(words, parish) {
		return false
	}
	low := strings.ToLower(title)
	for _, w := range []string{"family", "papers", "deeds", "estate", "collection", "archive", "of "} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func onlyNames(why []string) bool {
	for _, w := range why {
		switch w {
		case "surname", "given name", "full name", "surname and initials", "further given names":
		default:
			return false
		}
	}
	return true
}

// dates reads the first and last year of a covering-dates string.
func dates(s string) (from, to int) {
	years := yearRange.FindAllString(s, -1)
	if len(years) == 0 {
		return 0, 0
	}
	from, _ = strconv.Atoi(years[0])
	to, _ = strconv.Atoi(years[len(years)-1])
	if to < from {
		to = from
	}
	return from, to
}

// initials reports whether every given name appears only as its letter, next
// to the surname, as a medal card or register writes it.
func initials(words []string, surname string, givens []string) bool {
	if len(givens) == 0 {
		return false
	}
	for _, g := range givens {
		if !namematch.Near(words, surname, g[:1], 4) {
			return false
		}
	}
	return true
}

// names splits a person into a surname and their given names.
func names(p *model.Person) (string, []string) {
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
		if len(g) > 1 {
			givens = append(givens, g)
		}
	}
	return surname, givens
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return s[:1] + strings.ToLower(s[1:])
}

func year(s string) int {
	y, _ := strconv.Atoi(model.Year(s))
	return y
}
