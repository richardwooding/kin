package news

import (
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
	"github.com/richardwooding/kin/internal/place"
)

// Keep is the least score worth showing.
const Keep = 4

// Family is what the graph knows around the person being scored.
type Family struct {
	Places    string
	Region    place.Region
	Relatives []Relative
}

// Relative is a parent, spouse or child who may be named in a notice about
// the person. A spouse is recognised by given name; a parent or child, whose
// given names are the commonest in the parish, only with their surname too.
type Relative struct{ Role, Given, Surname string }

// deathWords are the words of a death or burial notice in the languages of
// these papers, accents folded: død, døde, afgik ved døden, begravet (Danish
// and Norwegian), död, avled, avliden, begraven (Swedish), kuoli, kuollut
// (Finnish), gestorben, verstorben (German), and the English. Words of
// widowhood are left out: a widow is alive, and lists of them are common.
var deathWords = map[string]bool{
	"DOD": true, "DODE": true, "DODEN": true, "DODT": true, "AFGIK": true, "AFGAAET": true,
	"BEGRAVET": true, "BEGRAVEN": true, "BEGRAVELSE": true, "JORDEFAERD": true,
	"AVLED": true, "AVLIDEN": true, "AVLIDNE": true,
	"KUOLI": true, "KUOLLUT": true, "GESTORBEN": true, "VERSTORBEN": true,
	"DECEASED": true, "DIED": true, "DEATH": true, "BURIED": true,
}

const near = 12 // words either side of the name that count as the notice about them

// Score rates how likely a hit is to be about p. The paper must date from
// p's sixteenth year to sixty years after their death. Where the source gives
// the words around the name, the surname must be among them, spelt exactly,
// with one OCR error, or as a variant of the same patronym in a Nordic
// region, and the first given name just before or after it; a source that
// gives only the name has already matched the phrase. A hit backed by
// neither a place, a relative nor a death is held below Keep: Ole Olsen is
// in every paper in Norway.
func Score(p *model.Person, h Hit, f Family, context bool) (int, []string) {
	surname, givens := namematch.Names(p)
	if surname == "" || len(givens) == 0 {
		return 0, nil
	}
	by, dy := year(p.Birth), year(p.Death)
	switch {
	case h.Year == 0:
	case by > 0 && h.Year < by+16:
		return 0, nil
	case dy > 0 && h.Year > dy+60:
		return 0, nil
	case dy == 0 && by > 0 && h.Year > by+100:
		return 0, nil
	}
	sc, why := 3, []string{"named " + givens[0] + " " + surname}
	words := namematch.Tokens(h.Text)
	at := -1
	if context {
		at = nameAt(words, surname, givens[0], f.Region.Nordic())
		if at < 0 {
			return 0, nil
		}
		lo, hi := at-near, at+near
		if lo < 0 {
			lo = 0
		}
		if hi > len(words) {
			hi = len(words)
		}
		words = words[lo:hi]
	}

	backed := false
	atDeath := dy > 0 && h.Year >= dy && h.Year <= dy+2
	if atDeath {
		sc++
		why = append(why, "paper of "+strconv.Itoa(h.Year)+", at the death")
	}
	if context {
		if atDeath {
			for _, w := range words {
				if deathWords[w] {
					sc += 2
					backed = true
					why = append(why, "death notice ("+strings.ToLower(w)+")")
					break
				}
			}
		}
		text := strings.Join(words, " ")
		named := 0
		for _, rel := range f.Relatives {
			if named == 2 || !namematch.NordicIn(rel.Given, text) || namematch.NordicIn(rel.Given, givens[0]) {
				continue
			}
			if rel.Role != "spouse" && (rel.Surname == "" || !namematch.NordicIn(rel.Surname, text)) {
				continue
			}
			sc += 2
			named++
			backed = true
			why = append(why, rel.Role+" "+rel.Given)
		}
	}
	mine := place.Distinct(f.Places)
	for _, pl := range []struct{ what, text string }{{"place", strings.Join(words, " ")}, {"paper of", h.Place + " " + h.Paper}} {
		if !context && pl.what == "place" {
			continue
		}
		if w := shared(mine, pl.text); w != "" {
			sc += 2
			backed = true
			why = append(why, pl.what+" "+w)
			break
		}
	}
	if !backed && sc >= Keep {
		sc = Keep - 1
		why = append(why, "name and date only")
	}
	return sc, why
}

// nameAt finds the surname among words with the given name up to three
// words before it (Hans Chr. Jensen) or just after it (Jensen, Hans), and
// returns the surname's position, or -1.
func nameAt(words []string, surname, given string, nordic bool) int {
	sur := namematch.Fold(surname)
	for i, w := range words {
		if !(namematch.Exact([]string{w}, sur) || (len(sur) >= 6 && namematch.Lev1(w, sur)) || (nordic && namematch.Patronym(w, sur))) {
			continue
		}
		lo, hi := i-3, i+2
		if lo < 0 {
			lo = 0
		}
		if hi > len(words) {
			hi = len(words)
		}
		if namematch.NordicIn(given, strings.Join(words[lo:hi], " ")) {
			return i
		}
	}
	return -1
}

func shared(mine []string, text string) string {
	for _, w := range namematch.Tokens(text) {
		for _, m := range mine {
			if w == m {
				return w
			}
		}
	}
	return ""
}

func year(s string) int {
	y, _ := strconv.Atoi(model.Year(s))
	return y
}
