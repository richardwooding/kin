package gazette

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
)

// Keep is the score below which a notice is noise. The feed stems its search
// terms, so a search for one surname returns its shorter relatives; only the
// scoring here decides what is really about the person.
const Keep = 4

// Score rates a notice against a person and says why. places is the person's
// own or their family's places; common says the query returned so many
// notices that a bare name match means little.
func Score(p *model.Person, places string, n Notice, common bool) (int, []string) {
	surname, givens := names(p)
	if surname == "" {
		return 0, nil
	}
	all := namematch.Tokens(n.Title + " " + n.Text)
	at := namematch.Index(all, surname)
	if at < 0 && n.Scanned() && namematch.OCR(all, surname) {
		at = ocrIndex(all, surname)
	}
	if at < 0 {
		return 0, nil
	}
	// A page of an old issue holds dozens of unrelated notices, so everything
	// but the name is judged in the words around the name, not on the page.
	words := all
	if n.Scanned() {
		words = window(all, at, 18)
	}

	score, why := 0, []string{"surname"}
	first := ""
	if len(givens) > 0 {
		first = givens[0]
	}
	switch {
	case first != "" && namematch.Near(words, surname, first, 4):
		score += 3
		why = []string{"full name"}
	case first != "" && namematch.Exact(words, first):
		score++
		why = []string{"surname and given name apart"}
	default:
		// initials, as the notices write them: NUNS, L. A.
		if initials(words, surname, givens) {
			score++
			why = []string{"surname and initials"}
		}
	}
	for _, g := range givens[1:] {
		if namematch.Near(words, surname, g, 5) {
			score++
			why = append(why, "further given names")
			break
		}
	}

	for _, tok := range namematch.PlaceTokens(places) {
		if tok == strings.ToUpper(n.Edition) {
			continue // every page of the London Gazette says London
		}
		if namematch.Exact(words, tok) {
			score += 2
			why = append(why, "place "+capitalise(tok))
			break
		}
	}

	by, dy := year(p.Birth), year(p.Death)
	if n.Year > 0 {
		switch {
		case by > 0 && n.Year < by+16, dy > 0 && n.Year > dy+60:
			return 0, nil // before they could be named, or long after the estate was wound up
		case dy > 0 && n.Year >= dy-1 && n.Year <= dy+3:
			score += 3
			why = append(why, "year of death")
		case by > 0 && (dy == 0 || n.Year <= dy):
			score++
			why = append(why, "in their lifetime")
		}
	}

	if deceased(words, n) && first != "" && namematch.Near(words, surname, first, 4) {
		score += 2
		why = append(why, "deceased estates notice")
	}
	for _, occ := range p.Occupations {
		if tokens := namematch.Tokens(occ); len(tokens) > 0 && namematch.Exact(words, tokens[0]) && len(tokens[0]) > 4 {
			score++
			why = append(why, "occupation")
			break
		}
	}

	if common && onlyNames(why) && score > 2 {
		score = 2 // a flood of same-name notices: a name alone proves nothing
	}
	return score, why
}

// deceased reports whether this notice, and not some neighbour on the same
// printed page, settles an estate.
func deceased(words []string, n Notice) bool {
	if n.Code == "2903" {
		return true
	}
	for _, w := range words {
		if w == "DECEASED" {
			return true
		}
	}
	return false
}

// window returns the words around position at.
func window(words []string, at, span int) []string {
	lo, hi := at-span, at+span+1
	if lo < 0 {
		lo = 0
	}
	if hi > len(words) {
		hi = len(words)
	}
	return words[lo:hi]
}

// ocrIndex finds the word one edit away from term.
func ocrIndex(words []string, term string) int {
	t := strings.ToUpper(term)
	for i, w := range words {
		if namematch.Lev1(w, t) {
			return i
		}
	}
	return -1
}

func onlyNames(why []string) bool {
	for _, w := range why {
		switch w {
		case "surname", "full name", "surname and given name apart", "surname and initials", "further given names",
			"in their lifetime": // a sixty-year window is no evidence at all
		default:
			return false
		}
	}
	return true
}

// initials reports whether every given name appears only as its letter, next
// to the surname, as the notices abbreviate them.
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

// capitalise turns an upper-case place token back into a readable name.
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

func date(y int, end bool) string {
	if y <= 0 {
		return ""
	}
	if end {
		return fmt.Sprintf("%d-12-31", y)
	}
	return fmt.Sprintf("%d-01-01", y)
}
