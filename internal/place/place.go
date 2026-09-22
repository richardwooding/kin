// Package place reads the place strings on a person and says which countries
// their records belong to, so the search and sweep commands know which
// archives are worth asking. A person with no place of their own takes the
// places of their spouses and children, because a parent known only from a
// child's baptism still belongs to that parish's country.
package place

import (
	"sort"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
)

// Region is a set of countries a person's records may lie in.
type Region uint

const (
	Cornwall Region = 1 << iota
	England
	SouthAfrica
	Ireland
	Scotland
	Norway
	Sweden
	Denmark
	Finland
	Iceland
)

// nordic lists, with accents folded, the country, county and city names that
// place a record in each Nordic country. They are matched as whole words
// because many are short (Ribe, Fyn, Vasa, Åbo). Viborg is the Danish amt;
// the Finnish Viipuri goes by its Finnish name.
var nordic = []struct {
	r     Region
	names []string
}{
	{Norway, []string{
		"norway", "norge", "noreg", "norwegen", "akershus", "hedmark", "hedemarken",
		"oppland", "kristians amt", "christians amt", "buskerud", "vestfold", "jarlsberg",
		"telemark", "bratsberg", "aust agder", "vest agder", "nedenes", "lister og mandal",
		"rogaland", "stavanger", "hordaland", "sondre bergenhus", "nordre bergenhus",
		"sogn og fjordane", "more og romsdal", "romsdal", "romsdals amt", "trondelag",
		"sor trondelag", "nord trondelag", "sondre trondhjem", "nordre trondhjem", "trondheim",
		"trondhjem", "nordland", "nordlands amt", "troms", "tromso", "finnmark", "finmarken",
		"smaalenene", "ostfold", "christiania", "kristiania", "oslo",
	}},
	{Sweden, []string{
		"sweden", "sverige", "schweden", "stockholm", "uppsala", "upsala", "uppland",
		"sodermanland", "ostergotland", "jonkoping", "kronoberg", "kalmar", "gotland",
		"blekinge", "kristianstad", "malmohus", "malmo", "skane", "scania", "halland",
		"goteborg", "gothenburg", "bohus", "bohuslan", "alvsborg", "skaraborg",
		"vastergotland", "varmland", "orebro", "narke", "vastmanland", "kopparberg",
		"dalarna", "dalecarlia", "gavleborg", "halsingland", "vasternorrland", "jamtland",
		"vasterbotten", "norrbotten", "smaland", "medelpad", "angermanland", "harjedalen",
	}},
	{Denmark, []string{
		"denmark", "danmark", "danemark", "kobenhavn", "kjobenhavn", "copenhagen",
		"frederiksborg", "holbaek", "soro", "praesto", "bornholm", "maribo", "lolland",
		"falster", "odense", "svendborg", "fyn", "fyen", "funen", "sjaelland", "zealand",
		"jylland", "jutland", "hjorring", "thisted", "aalborg", "viborg", "randers",
		"aarhus", "arhus", "skanderborg", "vejle", "ringkobing", "ribe", "sonderjylland",
	}},
	{Finland, []string{
		"finland", "suomi", "finnland", "uusimaa", "nyland", "turku", "abo", "turun",
		"vaasa", "vasa", "oulu", "uleaborg", "hame", "hameenlinna", "tavastehus", "viipuri",
		"kuopio", "mikkeli", "pori", "bjorneborg", "ahvenanmaa", "aland", "helsinki",
		"helsingfors", "satakunta", "pohjanmaa", "ostrobothnia", "karjala", "karelia",
	}},
	{Iceland, []string{"iceland", "island", "islandia", "reykjavik", "akureyri"}},
}

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
	return r | nordicOf(places)
}

// nordicOf matches the nordic names against the folded words of a place,
// allowing the genitive s of Kristianstads län.
// Island is Iceland only when written Ísland, since the English word is
// common, and London's Denmark Hill and Denmark Street are not Denmark.
func nordicOf(places string) Region {
	text := " " + strings.ToLower(strings.Join(namematch.Tokens(places), " ")) + " "
	text = strings.NewReplacer(" denmark hill ", " ", " denmark street ", " ").Replace(text)
	if !strings.Contains(strings.ToLower(places), "ísland") {
		text = strings.ReplaceAll(text, " island ", " ")
	}
	var r Region
	for _, c := range nordic {
		for _, n := range c.names {
			if strings.Contains(text, " "+n+" ") || strings.Contains(text, " "+n+"s ") {
				r |= c.r
				break
			}
		}
	}
	return r
}

// UK reports whether the region covers any part of the United Kingdom or
// Ireland, whose records are in the British catalogues and gazettes.
func (r Region) UK() bool { return r&(England|Scotland|Ireland) != 0 }

// Nordic reports whether the region covers any of the Nordic countries.
func (r Region) Nordic() bool { return r&(Norway|Sweden|Denmark|Finland|Iceland) != 0 }

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

// Ancestors lists the ancestors of root within maxGen generations (20 when
// zero), plus any probable ids above root, whose places fall in a region that
// in accepts. The living are left out, and so is anyone born since 1920 with
// no death, who may still be living. Nearer generations come first.
func Ancestors(g *model.Graph, root string, probable []string, maxGen int, in func(Region) bool) []string {
	if maxGen <= 0 {
		maxGen = 20
	}
	root = g.Resolve(root)
	anc := graph.Ancestors(g, root)
	kids := Kids(g)
	gen := map[string]int{}
	for id, d := range anc {
		if id != root && d <= maxGen {
			gen[id] = d
		}
	}
	for _, id := range probable {
		id = g.Resolve(id)
		if d, ok := anc[id]; ok && id != root {
			gen[id] = d
		}
	}
	var out []string
	for id := range gen {
		p := g.Persons[id]
		if p == nil || p.Living || !in(Of(PlacesOf(g, p, kids[id]))) {
			continue
		}
		by, _ := strconv.Atoi(model.Year(p.Birth))
		if model.Year(p.Death) == "" && by >= 1920 {
			continue
		}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		if gen[out[i]] != gen[out[j]] {
			return gen[out[i]] < gen[out[j]]
		}
		return out[i] < out[j]
	})
	return out
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
