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
	"time"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/place"
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
	kids := place.Kids(g)
	out := make([]Entry, 0, len(why))
	for id, reasons := range why {
		p := g.Persons[id]
		out = append(out, Entry{Person: p, Gen: gen[id], Why: model.Uniq(reasons), Groups: groups(p, place.PlacesOf(g, p, kids[id]))})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Gen != out[j].Gen {
			return out[i].Gen < out[j].Gen
		}
		return out[i].Person.Name < out[j].Person.Name
	})
	return out
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
	r := place.Of(places)
	var out []Group

	if r&place.Cornwall != 0 {
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
	if r&place.England != 0 && by > 0 && by <= 1881 && (dy == 0 || dy >= 1881) {
		fs = append(fs, Lead{Label: "1881 census of England and Wales", URL: familySearch(given, surname, by, "", "2562194")})
	}
	if r&place.SouthAfrica != 0 && dy > 0 && place.Depot(places) == "KAB" {
		q := url.Values{"q.givenName": {given}, "q.surname": {surname}, "q.deathLikeDate.from": {strconv.Itoa(dy)}, "q.deathLikeDate.to": {strconv.Itoa(dy + 1)}, "f.collectionId": {"2517051"}}
		fs = append(fs, Lead{Label: "Cape probate records, death " + span(dy, dy+1), URL: "https://www.familysearch.org/search/record/results?" + q.Encode()})
	}
	out = append(out, Group{Service: "FamilySearch", Leads: fs})

	wt := "kin wikitree search -last " + shellQuote(surname) + " -first " + shellQuote(first)
	if by > 0 {
		wt += fmt.Sprintf(" -birth %d -spread 3", by)
	}
	out = append(out, Group{Service: "WikiTree", Leads: []Lead{{Label: "profiles by name", Hint: wt}}})

	if r&place.England != 0 {
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

	if r.UK() {
		label := "official notices"
		if by > 0 || dy > 0 {
			label += " " + span(gazetteFrom(by), gazetteTo(by, dy))
		}
		ls := []Lead{{Label: label, URL: gazette(surname, first, by, dy, r.Edition())}}
		if r&place.Ireland != 0 && r&(place.England|place.Scotland) == 0 {
			ls[0].Hint = "the Belfast Gazette begins in 1921 and the Dublin Gazette ends in 1922, so earlier Irish notices are elsewhere"
		}
		out = append(out, Group{Service: "The Gazette", Leads: ls})
	}

	if r&place.SouthAfrica != 0 {
		na := fmt.Sprintf("kin naairs -db %s -q %s", place.Depot(places), shellQuote(strings.ToUpper(surname+" "+first)))
		if dy > 0 {
			na += fmt.Sprintf(" -from %d -to %d", dy-1, dy+3)
		}
		out = append(out, Group{Service: "South Africa", Leads: []Lead{
			{Label: "NAAIRS archives index", Hint: na},
			{Label: "eGGSA gravestones", Hint: "kin eggsa graves -surname " + shellQuote(surname)},
		}})
	}
	if r&place.Ireland != 0 {
		out = append(out, Group{Service: "Ireland", Leads: []Lead{{Label: "civil and church records", URL: "https://www.irishgenealogy.ie/en/", Hint: fmt.Sprintf("surname %s, first name %s%s", surname, first, dateLabel(by))}}})
	}
	return out
}

// gazetteFrom and gazetteTo bound a search of the official notices by the
// years a person could have been named in one: from majority to a few years
// after death, when the estate notices appear.
func gazetteFrom(by int) int {
	if by > 0 {
		return by + 16
	}
	return 1665
}

func gazetteTo(by, dy int) int {
	switch {
	case dy > 0:
		return dy + 3
	case by > 0:
		return by + 100
	}
	return time.Now().Year()
}

// gazette links to The Gazette's own search page, not to the JSON feed.
func gazette(surname, first string, by, dy int, edition string) string {
	q := url.Values{"text": {fmt.Sprintf("%q", surname+", "+first)}}
	q.Set("start-publish-date", fmt.Sprintf("%d-01-01", gazetteFrom(by)))
	q.Set("end-publish-date", fmt.Sprintf("%d-12-31", gazetteTo(by, dy)))
	if edition != "" {
		q.Set("edition", edition)
	}
	return "https://www.thegazette.co.uk/all-notices/notice?" + q.Encode()
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
