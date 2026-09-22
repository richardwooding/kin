package riksarkivet

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
	Places  string
	Father  *model.Person
	Mother  *model.Person
	Spouses []*model.Person
}

// Score rates how likely an entry is to be about p. A birth entry must give
// p's first given name and a birth year within two of p's; a marriage entry
// must name p, surname and first given name, as groom or bride. A match on
// names and dates alone, with neither a parent, a spouse nor the parish from
// the graph to back it, is held below Keep, since a handful of patronymics
// name half of Skåne; so is an entry naming a parent other than the graph's.
// A patronymic agreeing with the entry's own father is not backing, since
// every Nils has sons called Nilsson.
func Score(p *model.Person, r Record, f Family) (int, []string) {
	switch r.Type {
	case Birth:
		return scoreBirth(p, r, f)
	case Marriage:
		return scoreMarriage(p, r, f)
	}
	return 0, nil
}

func scoreBirth(p *model.Person, r Record, f Family) (int, []string) {
	surname, givens := namematch.Names(p)
	if len(givens) == 0 || !namematch.NordicIn(givens[0], r.Child) {
		return 0, nil
	}
	sc, why := 2, []string{"given name " + givens[0]}
	if len(givens) > 1 && allGiven(givens, r.Child) {
		sc++
		why = append(why, "all given names")
	}
	by := year(p.Birth)
	switch d := abs(by - r.Year()); {
	case by == 0 || r.Year() == 0:
		return 0, nil
	case d > 2:
		return 0, nil
	case d == 0:
		sc += 2
		why = append(why, "born "+strconv.Itoa(by))
		if len(p.Birth) >= 10 && p.Birth[:10] == r.Date {
			sc++
			why = append(why, "birth date")
		}
	case d == 1:
		sc++
		why = append(why, "born within a year")
	}

	backed, contradicted := false, false
	fs, fg := split(r.Father)
	switch {
	case surname != "" && fg != "" && patronymOf(surname, fg):
		sc += 2
		why = append(why, "patronymic from father "+fg)
	case surname != "" && fs != "" && namematch.Patronym(surname, fs):
		sc++
		why = append(why, "father's surname")
	}
	for _, par := range []struct {
		role   string
		p      *model.Person
		record string
	}{{"father", f.Father, r.Father}, {"mother", f.Mother, r.Mother}} {
		if par.p == nil || par.record == "" {
			continue
		}
		ps, pg := namematch.Names(par.p)
		rs, rg := split(par.record)
		switch {
		case len(pg) == 0 || rg == "":
		case namematch.NordicIn(pg[0], rg):
			sc += 2
			backed = true
			why = append(why, par.role+" "+pg[0])
			if ps != "" && rs != "" && namematch.Patronym(ps, rs) {
				sc++
				why = append(why, par.role+"'s surname")
			}
		default:
			contradicted = true
			why = append(why, par.role+" "+rg+", not "+pg[0])
		}
	}
	if parishIn(r.Parish, f.Places) {
		sc += 2
		backed = true
		why = append(why, "parish "+r.Parish)
	}
	if (!backed || contradicted) && sc >= Keep {
		sc = Keep - 1
		if !backed {
			why = append(why, "names and dates only")
		}
	}
	return sc, why
}

func scoreMarriage(p *model.Person, r Record, f Family) (int, []string) {
	surname, givens := namematch.Names(p)
	if surname == "" || len(givens) == 0 {
		return 0, nil
	}
	sides := []struct{ self, other string }{{r.Groom, r.Bride}, {r.Bride, r.Groom}}
	switch strings.ToLower(p.Gender) {
	case "male", "m":
		sides = sides[:1]
	case "female", "f":
		sides = sides[1:]
	}
	var self, other string
	for _, s := range sides {
		ss, sg := split(s.self)
		if ss != "" && namematch.Patronym(surname, ss) && namematch.NordicIn(givens[0], sg) {
			self, other = s.self, s.other
			break
		}
	}
	if self == "" {
		return 0, nil
	}
	if by := year(p.Birth); by > 0 && r.Year() > 0 && (r.Year() < by+14 || r.Year() > by+70) {
		return 0, nil
	}
	sc, why := 3, []string{"named " + self}
	backed := false
	osur, og := split(other)
	for _, sp := range f.Spouses {
		ss, sg := namematch.Names(sp)
		if len(sg) > 0 && namematch.NordicIn(sg[0], og) {
			sc += 2
			backed = true
			why = append(why, "spouse "+sg[0])
			if ss != "" && osur != "" && namematch.Patronym(ss, osur) {
				sc++
				why = append(why, "spouse's surname")
			}
			break
		}
	}
	if parishIn(r.Parish, f.Places) {
		sc += 2
		backed = true
		why = append(why, "parish "+r.Parish)
	}
	if !backed && sc >= Keep {
		sc = Keep - 1
		why = append(why, "names only")
	}
	return sc, why
}

// split reads a register name, "Surname, Given names".
func split(name string) (surname, given string) {
	if i := strings.IndexByte(name, ','); i >= 0 {
		return strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+1:])
	}
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return "", name
	}
	return fields[len(fields)-1], strings.Join(fields[:len(fields)-1], " ")
}

func allGiven(givens []string, names string) bool {
	for _, g := range givens {
		if !namematch.NordicIn(g, names) {
			return false
		}
	}
	return true
}

// patronymOf reports whether surname is formed from the father's first given
// name, as Nilsson or Nilsdotter from Nils.
func patronymOf(surname, fatherGiven string) bool {
	first := strings.Fields(fatherGiven)
	if len(first) == 0 {
		return false
	}
	return namematch.Patronym(surname, first[0]+"sson") || namematch.Patronym(surname, first[0]+"sdotter")
}

func parishIn(parish, places string) bool {
	if parish == "" {
		return false
	}
	mine := place.Distinct(places)
	for _, w := range namematch.Tokens(parish) {
		for _, m := range mine {
			if w == m {
				return true
			}
		}
	}
	return false
}

func year(s string) int {
	y, _ := strconv.Atoi(model.Year(s))
	return y
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
