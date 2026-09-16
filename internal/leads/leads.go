// Package leads composes search links for the ancestors whose records are
// still missing. Nothing is fetched: each lead is a URL the researcher opens
// by hand, or, for sites whose terms forbid pre-filled searches, the search
// page plus the values to type.
package leads

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
)

//go:embed leads.html
var tpl string

// Options selects the people to compose leads for.
type Options struct {
	Root        string   // ancestors of this person are examined
	ProbableIDs []string // people whose link to their parents is unproven
	Only        string   // one person id instead of the frontier (optional)
	MaxGen      int      // generations above root to examine; 0 means 20
}

// Lead is one search: a pre-filled URL, or a search page with a hint of what to type or run.
type Lead struct {
	Label string
	URL   string
	Hint  string
}

// Group is the leads for one service.
type Group struct {
	Service string
	Leads   []Lead
}

// Entry is one person on the research frontier with their leads.
type Entry struct {
	Person *model.Person
	Gen    int
	Why    []string
	Groups []Group
}

// Build returns the frontier of root's ancestry: everyone missing a parent,
// plus the probable ids, each with leads composed from their names, dates and places.
func Build(g *model.Graph, opts Options) []Entry {
	maxGen := opts.MaxGen
	if maxGen <= 0 {
		maxGen = 20
	}
	why := map[string][]string{}
	gen := map[string]int{}
	add := func(id string, g0 int, reason string) {
		id = g.Resolve(id)
		if g.Persons[id] == nil {
			return
		}
		if _, ok := gen[id]; !ok {
			gen[id] = g0
		}
		why[id] = append(why[id], reason)
	}
	if opts.Only != "" {
		add(opts.Only, 0, "requested")
	} else {
		for id, d := range graph.Ancestors(g, g.Resolve(opts.Root)) {
			p := g.Persons[id]
			if p == nil || d > maxGen {
				continue
			}
			if p.Father == "" {
				add(id, d, "no father")
			}
			if p.Mother == "" {
				add(id, d, "no mother")
			}
		}
		for _, id := range opts.ProbableIDs {
			id = g.Resolve(id)
			if d, ok := graph.Ancestors(g, g.Resolve(opts.Root))[id]; ok {
				add(id, d, "probable link to parents")
			}
		}
	}
	kids := map[string][]*model.Person{}
	for _, p := range g.Persons {
		for _, par := range []string{p.Father, p.Mother} {
			if par != "" {
				kids[g.Resolve(par)] = append(kids[g.Resolve(par)], p)
			}
		}
	}
	out := make([]Entry, 0, len(why))
	for id, reasons := range why {
		p := g.Persons[id]
		out = append(out, Entry{Person: p, Gen: gen[id], Why: model.Uniq(reasons), Groups: groups(p, placesOf(g, p, kids[id]))})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Gen != out[j].Gen {
			return out[i].Gen < out[j].Gen
		}
		return out[i].Person.Name < out[j].Person.Name
	})
	return out
}

type region uint

const (
	cornwall region = 1 << iota
	england
	southAfrica
	ireland
)

// placesOf returns the person's own birth and death places or, when both are
// blank, the places of their spouses and children, so that a parent known
// only from a child's baptism still gets the leads for that parish's country.
func placesOf(g *model.Graph, p *model.Person, kids []*model.Person) string {
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

// regionsOf reads a place text; a person born in England who died at the
// Cape belongs to both, and Cornwall implies England.
func regionsOf(places string) region {
	s := strings.ToLower(places)
	var r region
	if strings.Contains(s, "cornwall") {
		r |= cornwall | england
	}
	if containsAny(s, "england", "wales", "surrey", "middlesex", "london", "kent", "lancashire", "devon") {
		r |= england
	}
	if containsAny(s, "south africa", "cape", "transvaal", "natal", "free state", "orange river", "griqualand") {
		r |= southAfrica
	}
	if strings.Contains(s, "ireland") {
		r |= ireland
	}
	return r
}

// depot names the National Archives repository for the places a person lived.
func depot(places string) string {
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

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func yearOf(s string) int {
	y, _ := strconv.Atoi(model.Year(s))
	return y
}

func span(a, b int) string { return fmt.Sprintf("%d to %d", a, b) }

func groups(p *model.Person, places string) []Group {
	given := strings.TrimSpace(p.Given)
	if given == "" {
		given = strings.TrimSpace(strings.TrimSuffix(p.Name, p.Surname))
	}
	first := given
	if i := strings.IndexByte(given, ' '); i > 0 {
		first = given[:i]
	}
	surname := p.Surname
	by, dy := yearOf(p.Birth), yearOf(p.Death)
	r := regionsOf(places)
	var out []Group

	if r&cornwall != 0 {
		var ls []Lead
		if by > 0 {
			ls = append(ls, Lead{Label: "baptisms " + span(by-5, by+3), URL: opc("baptisms", first, surname, by-5, by+3)})
			ls = append(ls, Lead{Label: "marriages " + span(by+15, by+45), URL: opc("marriages", first, surname, by+15, by+45)})
		} else {
			ls = append(ls, Lead{Label: "baptisms, any year", URL: opc("baptisms", first, surname, 0, 0)})
			ls = append(ls, Lead{Label: "marriages, any year", URL: opc("marriages", first, surname, 0, 0)})
		}
		if dy > 0 {
			ls = append(ls, Lead{Label: "burials " + span(dy-1, dy+1), URL: opc("burials", first, surname, dy-1, dy+1)})
		}
		out = append(out, Group{Service: "Cornwall OPC", Leads: ls})
	}

	fs := []Lead{{Label: "records by name" + dateLabel(by), URL: familySearch(given, surname, by, p.BirthPlace, "")}}
	if r&england != 0 && by > 0 && by <= 1881 && (dy == 0 || dy >= 1881) {
		fs = append(fs, Lead{Label: "1881 census of England and Wales", URL: familySearch(given, surname, by, "", "2562194")})
	}
	if r&southAfrica != 0 && dy > 0 && depot(places) == "KAB" {
		q := url.Values{"q.givenName": {given}, "q.surname": {surname}, "q.deathLikeDate.from": {strconv.Itoa(dy)}, "q.deathLikeDate.to": {strconv.Itoa(dy + 1)}, "f.collectionId": {"2517051"}}
		fs = append(fs, Lead{Label: "Cape probate records, death " + span(dy, dy+1), URL: "https://www.familysearch.org/search/record/results?" + q.Encode()})
	}
	out = append(out, Group{Service: "FamilySearch", Leads: fs})

	wt := "kin wikitree search -last " + shellQuote(surname) + " -first " + shellQuote(first)
	if by > 0 {
		wt += fmt.Sprintf(" -birth %d -spread 3", by)
	}
	out = append(out, Group{Service: "WikiTree", Leads: []Lead{{Label: "profiles by name", Hint: wt}}})

	if r&england != 0 {
		years := "any years"
		if by > 0 {
			years = span(by-5, by+3)
		}
		typed := fmt.Sprintf("surname %s, first name %s, %s", surname, first, years)
		ls := []Lead{{Label: "FreeREG parish registers (type by hand)", URL: "https://www.freereg.org.uk/search_queries/new", Hint: typed}}
		if by <= 1911 && (dy == 0 || dy >= 1841) {
			ls = append(ls, Lead{Label: "FreeCEN census (type by hand)", URL: "https://www.freecen.org.uk/search_queries/new", Hint: typed})
		}
		if by >= 1837 || dy >= 1837 {
			ls = append(ls, Lead{Label: "FreeBMD civil index (type by hand)", URL: "https://www.freebmd.org.uk/cgi/search.pl", Hint: typed})
		}
		ls = append(ls, Lead{Label: "National Archives Discovery", URL: "https://discovery.nationalarchives.gov.uk/results/r?_q=" + url.QueryEscape(fmt.Sprintf("%q", surname+", "+first))})
		out = append(out, Group{Service: "England and Wales", Leads: ls})
	}

	if r&southAfrica != 0 {
		na := fmt.Sprintf("kin naairs -db %s -q %s", depot(places), shellQuote(strings.ToUpper(surname+" "+first)))
		if dy > 0 {
			na += fmt.Sprintf(" -from %d -to %d", dy-1, dy+3)
		}
		out = append(out, Group{Service: "South Africa", Leads: []Lead{
			{Label: "NAAIRS archives index", Hint: na},
			{Label: "eGGSA gravestones", Hint: "kin eggsa graves -surname " + shellQuote(surname)},
		}})
	}
	if r&ireland != 0 {
		out = append(out, Group{Service: "Ireland", Leads: []Lead{{Label: "civil and church records", URL: "https://www.irishgenealogy.ie/en/", Hint: fmt.Sprintf("surname %s, first name %s%s", surname, first, dateLabel(by))}}})
	}
	return out
}

func dateLabel(by int) string {
	if by == 0 {
		return ""
	}
	return ", born " + span(by-3, by+3)
}

func opc(table, first, surname string, from, to int) string {
	q := url.Values{"forename1": {first}, "surname1": {surname}, "t": {table}, "bf": {"Search"}}
	if from > 0 {
		q.Set("year_from", strconv.Itoa(from))
		q.Set("year_to", strconv.Itoa(to))
	}
	return "https://www.cornwall-opc-database.org/search-database/" + table + "/index.php?" + q.Encode()
}

func familySearch(given, surname string, by int, place, collection string) string {
	q := url.Values{"q.givenName": {given}, "q.surname": {surname}}
	if by > 0 {
		q.Set("q.birthLikeDate.from", strconv.Itoa(by-3))
		q.Set("q.birthLikeDate.to", strconv.Itoa(by+3))
	}
	if place != "" {
		q.Set("q.anyPlace", place)
	}
	if collection != "" {
		q.Set("f.collectionId", collection)
	}
	return "https://www.familysearch.org/search/record/results?" + q.Encode()
}

func shellQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " '\"") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

// WriteText prints the entries as indented plain text.
func WriteText(w io.Writer, entries []Entry) {
	for _, e := range entries {
		p := e.Person
		fmt.Fprintf(w, "\n[gen %d] %s (%s)", e.Gen, p.Name, p.ID)
		if p.Birth != "" || p.BirthPlace != "" {
			fmt.Fprintf(w, "  b. %s %s", p.Birth, p.BirthPlace)
		}
		if p.Death != "" || p.DeathPlace != "" {
			fmt.Fprintf(w, "  d. %s %s", p.Death, p.DeathPlace)
		}
		fmt.Fprintf(w, "\n  %s\n", strings.Join(e.Why, "; "))
		for _, gr := range e.Groups {
			fmt.Fprintf(w, "  %s\n", gr.Service)
			for _, l := range gr.Leads {
				switch {
				case l.URL != "" && l.Hint != "":
					fmt.Fprintf(w, "    %s: %s\n      type: %s\n", l.Label, l.URL, l.Hint)
				case l.URL != "":
					fmt.Fprintf(w, "    %s: %s\n", l.Label, l.URL)
				default:
					fmt.Fprintf(w, "    %s: %s\n", l.Label, l.Hint)
				}
			}
		}
	}
}

// Render writes the entries as a self-contained HTML page.
func Render(entries []Entry, title string, path string) error {
	t, err := template.New("leads").Parse(tpl)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, map[string]any{"Title": title, "Entries": entries})
}
