// Package place reads the place strings on a person and says which countries
// their records belong to, so the search and sweep commands know which
// archives are worth asking. A person with no place of their own takes the
// places of their spouses and children, because a parent known only from a
// child's baptism still belongs to that parish's country.
package place

import (
	"strings"

	"github.com/richardwooding/kin/internal/model"
)

// Region is a set of countries a person's records may lie in.
type Region uint

const (
	Cornwall Region = 1 << iota
	England
	SouthAfrica
	Ireland
	Scotland
)

// Of reads a place text; a person born in England who died at the Cape
// belongs to both, and Cornwall implies England.
func Of(places string) Region {
	s := strings.ToLower(places)
	var r Region
	if strings.Contains(s, "cornwall") {
		r |= Cornwall | England
	}
	if containsAny(s, "england", "wales", "surrey", "middlesex", "london", "kent", "lancashire", "devon") {
		r |= England
	}
	if containsAny(s, "south africa", "cape", "transvaal", "natal", "free state", "orange river", "griqualand") {
		r |= SouthAfrica
	}
	if strings.Contains(s, "ireland") {
		r |= Ireland
	}
	if containsAny(s, "scotland", "edinburgh", "glasgow", "lanark", "fife", "aberdeen") {
		r |= Scotland
	}
	return r
}

// UK reports whether the region covers any part of the United Kingdom or
// Ireland, whose records are in the British catalogues and gazettes.
func (r Region) UK() bool { return r&(England|Scotland|Ireland) != 0 }

// Edition names the gazette that carried the official notices of a region.
func (r Region) Edition() string {
	switch {
	case r&England != 0:
		return "London" // the London Gazette also covers Wales
	case r&Scotland != 0:
		return "Edinburgh"
	case r&Ireland != 0:
		return "Belfast"
	}
	return ""
}

// PlacesOf returns the person's own birth and death places or, when both are
// blank, the places of their spouses and children.
func PlacesOf(g *model.Graph, p *model.Person, kids []*model.Person) string {
	if p.BirthPlace != "" || p.DeathPlace != "" {
		return p.BirthPlace + " | " + p.DeathPlace
	}
	var parts []string
	for _, sp := range p.Spouses {
		if q := g.Persons[g.Resolve(sp)]; q != nil {
			parts = append(parts, q.BirthPlace, q.DeathPlace)
		}
	}
	for _, k := range kids {
		parts = append(parts, k.BirthPlace, k.DeathPlace)
	}
	return strings.Join(parts, " | ")
}

// Kids indexes the graph by parent id, for PlacesOf.
func Kids(g *model.Graph) map[string][]*model.Person {
	kids := map[string][]*model.Person{}
	for _, p := range g.Persons {
		for _, par := range []string{p.Father, p.Mother} {
			if par != "" {
				kids[g.Resolve(par)] = append(kids[g.Resolve(par)], p)
			}
		}
	}
	return kids
}

// Depot names the National Archives of South Africa repository for the places
// a person lived.
func Depot(places string) string {
	s := strings.ToLower(places)
	switch {
	case containsAny(s, "transvaal", "johannesburg", "pretoria", "gauteng"):
		return "TAB"
	case containsAny(s, "natal"):
		return "NAB"
	case containsAny(s, "free state", "orange"):
		return "VAB"
	case containsAny(s, "cape", "wynberg", "mossel bay", "kimberley", "griqualand"):
		return "KAB"
	}
	return "RSA"
}

// Parish is the first part of a place string, the parish, town or farm that
// distinguishes it from every other place in the same county.
func Parish(place string) string {
	if i := strings.IndexByte(place, ','); i > 0 {
		place = place[:i]
	}
	return strings.TrimSpace(place)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
