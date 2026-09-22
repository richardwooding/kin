// Package leads composes search links for the ancestors whose records are
// still missing. Nothing is fetched: each lead is a URL the researcher opens
// by hand, or, for sites whose terms forbid pre-filled searches, the search
// page plus the values to type.
package leads

import (
	_ "embed"
	"encoding/json"
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
	Root        string                // ancestors of this person are examined
	ProbableIDs []string              // people whose link to their parents is unproven
	Only        string                // one person id instead of the frontier (optional)
	MaxGen      int                   // generations above root to examine; 0 means 20
	Upstream    []string              // id prefixes whose parentless people are another site's ends, not ours (e.g. "wt:")
	Searched    map[string][]Searched // searches already made, by person id
}

// Searched records one search already made for a person, so the same lead
// is not offered again without saying so.
type Searched struct {
	Person  string `json:"person"`
	Service string `json:"service"`
	When    string `json:"when,omitempty"`
	Note    string `json:"note,omitempty"`
}

// LoadSearched reads a json list of Searched entries and indexes it by person id.
func LoadSearched(path string) (map[string][]Searched, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []Searched
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := map[string][]Searched{}
	for _, x := range list {
		out[x.Person] = append(out[x.Person], x)
	}
	return out, nil
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
	Person   *model.Person
	Gen      int
	Why      []string
	Groups   []Group
	Searched []Searched
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
	upstream := func(id string) bool {
		for _, pre := range opts.Upstream {
			if pre != "" && strings.HasPrefix(id, pre) {
				return true
			}
		}
		return false
	}
	if opts.Only != "" {
		add(opts.Only, 0, "requested")
	} else {
		for id, d := range graph.Ancestors(g, g.Resolve(opts.Root)) {
			p := g.Persons[id]
			if p == nil || d > maxGen || upstream(id) {
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
		var spouse *model.Person
		if len(p.Spouses) > 0 {
			spouse = g.Persons[g.Resolve(p.Spouses[0])]
		}
		out = append(out, Entry{Person: p, Gen: gen[id], Why: model.Uniq(reasons),
			Groups: groups(p, place.PlacesOf(g, p, kids[id]), spouse, kids[id]), Searched: opts.Searched[id]})
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

// firstName is the first given name, the form the parish indexes want.
func firstName(p *model.Person) string {
	given := strings.TrimSpace(p.Given)
	if given == "" {
		given = strings.TrimSpace(strings.TrimSuffix(p.Name, p.Surname))
	}
	if i := strings.IndexByte(given, ' '); i > 0 {
		return given[:i]
	}
	return given
}

// kidYears is the span of the children's birth years, or zeros.
func kidYears(kids []*model.Person) (lo, hi int) {
	for _, k := range kids {
		if y := yearOf(k.Birth); y > 0 {
			if lo == 0 || y < lo {
				lo = y
			}
			if y > hi {
				hi = y
			}
		}
	}
	return lo, hi
}

// cornishParish is the parish to search: the person's own Cornish place, else a child's.
func cornishParish(p *model.Person, kids []*model.Person) string {
	for _, pl := range []string{p.BirthPlace, p.DeathPlace} {
		if place.Of(pl)&place.Cornwall != 0 {
			return place.Parish(pl)
		}
	}
	for _, k := range kids {
		if place.Of(k.BirthPlace)&place.Cornwall != 0 {
			return place.Parish(k.BirthPlace)
		}
	}
	return ""
}

func groups(p *model.Person, places string, spouse *model.Person, kids []*model.Person) []Group {
	given := strings.TrimSpace(p.Given)
	if given == "" {
		given = strings.TrimSpace(strings.TrimSuffix(p.Name, p.Surname))
	}
	first := firstName(p)
	surname := p.Surname
	by, dy := yearOf(p.Birth), yearOf(p.Death)
	r := place.Of(places)
	depot := place.Depot(places)
	klo, khi := kidYears(kids)
	var out []Group

	if r&place.Cornwall != 0 {
		parish := cornishParish(p, kids)
		var ls []Lead
		if by > 0 {
			ls = append(ls, Lead{Label: "baptisms " + span(by-5, by+3), URL: opc("baptisms", parish, first, surname, by-5, by+3, nil)})
		} else {
			ls = append(ls, Lead{Label: "baptisms, any year", URL: opc("baptisms", parish, first, surname, 0, 0, nil)})
		}
		mfrom, mto := by+15, by+45
		if klo > 0 {
			mfrom, mto = klo-15, khi
		}
		if by == 0 && klo == 0 {
			mfrom, mto = 0, 0
		}
		mlabel := "marriages"
		var mq url.Values
		if spouse != nil {
			mlabel = "marriage to " + spouse.Name
			mq = url.Values{"forename2": {firstName(spouse)}, "surname2": {spouse.Surname}}
		}
		if mfrom > 0 {
			mlabel += " " + span(mfrom, mto)
		} else {
			mlabel += ", any year"
		}
		ls = append(ls, Lead{Label: mlabel, URL: opc("marriages", parish, first, surname, mfrom, mto, mq)})
		if spouse != nil || len(kids) > 0 {
			// every child of the couple: the OPC baptism search by both parents' forenames
			father, mother, family := first, "", surname
			if spouse != nil {
				mother = firstName(spouse)
			}
			if p.Gender == "female" {
				father, mother = mother, first
				family = ""
				if spouse != nil {
					family = spouse.Surname
				} else if len(kids) > 0 {
					family = kids[0].Surname
				}
			}
			cfrom, cto := klo-3, khi+3
			if klo == 0 {
				cfrom, cto = by+18, by+50
				if by == 0 {
					cfrom, cto = 0, 0
				}
			}
			q := url.Values{}
			if father != "" {
				q.Set("forename2", father)
			}
			if mother != "" {
				q.Set("forename3", mother)
			}
			clabel := "children of the couple, baptisms"
			if cfrom > 0 {
				clabel += " " + span(cfrom, cto)
			}
			ls = append(ls, Lead{Label: clabel, URL: opc("baptisms", parish, "", family, cfrom, cto, q)})
		}
		if dy > 0 {
			ls = append(ls, Lead{Label: "burials " + span(dy-1, dy+1), URL: opc("burials", parish, first, surname, dy-1, dy+1, nil)})
		} else if by > 0 {
			ls = append(ls, Lead{Label: "burials " + span(by+20, by+100), URL: opc("burials", parish, first, surname, by+20, by+100, nil)})
		}
		out = append(out, Group{Service: "Cornwall OPC", Leads: ls})
	}

	fs := []Lead{{Label: "records by name" + dateLabel(by), URL: familySearch(given, surname, by, p.BirthPlace, "")}}
	if r&place.England != 0 && by > 0 {
		for _, c := range []struct {
			year int
			id   string
		}{{1851, "2563939"}, {1861, "1493747"}, {1881, "2562194"}} {
			if by <= c.year && (dy == 0 || dy >= c.year) {
				fs = append(fs, Lead{Label: fmt.Sprintf("%d census of England and Wales", c.year), URL: familySearch(given, surname, by, "", c.id)})
			}
		}
	}
	if r&place.SouthAfrica != 0 {
		fs = append(fs, Lead{Label: "Dutch Reformed Church registers, Cape Town archives" + dateLabel(by), URL: familySearch(given, surname, by, "", "1478678")})
		if depot == "TAB" {
			fs = append(fs, Lead{Label: "Hervormde Kerk registers, Pretoria archive" + dateLabel(by), URL: familySearch(given, surname, by, "", "2155416")})
		}
		if dy > 0 {
			probate := map[string][2]string{
				"KAB": {"2517051", "Cape probate records"},
				"TAB": {"2520237", "Transvaal probate records"},
				"VAB": {"3040532", "Orange Free State probate records"},
			}
			if c, ok := probate[depot]; ok {
				q := url.Values{"q.givenName": {given}, "q.surname": {surname}, "q.deathLikeDate.from": {strconv.Itoa(dy)}, "q.deathLikeDate.to": {strconv.Itoa(dy + 1)}, "f.collectionId": {c[0]}}
				fs = append(fs, Lead{Label: c[1] + ", death " + span(dy, dy+1), URL: "https://www.familysearch.org/search/record/results?" + q.Encode()})
			}
		}
	}
	for _, c := range []struct {
		r     place.Region
		id    string
		label string
	}{
		{place.Norway, "1467014", "Norway baptisms 1634-1927"},
		{place.Sweden, "1520594", "Sweden baptisms 1611-1920"},
		{place.Denmark, "1778463", "Denmark baptisms 1618-1923"},
		{place.Finland, "1778464", "Finland baptisms 1657-1890"},
	} {
		if r&c.r != 0 {
			fs = append(fs, Lead{Label: c.label + dateLabel(by), URL: familySearch(given, surname, by, "", c.id)})
		}
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
		na := fmt.Sprintf("kin naairs -db %s -q %s", depot, shellQuote(strings.ToUpper(surname+" "+first)))
		if dy > 0 {
			na += fmt.Sprintf(" -from %d -to %d", dy-1, dy+3)
		}
		out = append(out, Group{Service: "South Africa", Leads: []Lead{
			{Label: "NAAIRS archives index", Hint: na},
			{Label: "eGGSA gravestones", Hint: "kin eggsa graves -surname " + shellQuote(surname)},
		}})
	}
	out = append(out, nordic(r, first, surname, by, dy)...)
	if g, ok := newspapers(r, first, surname, by, dy); ok {
		out = append(out, g)
	}
	if r&place.Ireland != 0 {
		out = append(out, Group{Service: "Ireland", Leads: []Lead{{Label: "civil and church records", URL: "https://www.irishgenealogy.ie/en/", Hint: fmt.Sprintf("surname %s, first name %s%s", surname, first, dateLabel(by))}}})
	}
	return out
}

// nordic gives the Nordic archives. Only Riksarkivet's open API and a local
// copy of Link-Lives may be searched by kin; Digitalarkivet, the Swedish
// census search, histreg, DDD, HisKi and Íslendingabók are links for the user
// to open, pre-filled only where the site takes its search in the address.
func nordic(r place.Region, first, surname string, by, dy int) []Group {
	typed := fmt.Sprintf("surname %s, first name %s%s", surname, first, dateLabel(by))
	var out []Group
	if r&place.Norway != 0 {
		q := url.Values{"firstname": {first}, "lastname": {surname}}
		if by > 0 {
			q.Set("birth_year_from", strconv.Itoa(by-2))
			q.Set("birth_year_to", strconv.Itoa(by+2))
		}
		out = append(out, Group{Service: "Norway", Leads: []Lead{
			{Label: "Digitalarkivet censuses and church books", URL: "https://www.digitalarkivet.no/en/search/persons/advanced?" + q.Encode()},
			{Label: "Historisk befolkningsregister (type by hand)", URL: "https://histreg.no/", Hint: typed},
		}})
	}
	if r&place.Sweden != 0 {
		q := url.Values{"Fornamn": {first}, "Efternamn": {surname}}
		if by > 0 {
			q.Set("Fodelsear", strconv.Itoa(by))
		}
		ra := "kin riksarkivet -name " + shellQuote(first+" "+surname)
		if by > 0 {
			ra += fmt.Sprintf(" -from %d -to %d", by-2, by+2)
		}
		out = append(out, Group{Service: "Sweden", Leads: []Lead{
			{Label: "Riksarkivet censuses 1860-1930", URL: "https://sok.riksarkivet.se/folkrakningar?" + q.Encode()},
			{Label: "Riksarkivet birth registers", Hint: ra},
		}})
	}
	if r&place.Denmark != 0 {
		out = append(out, Group{Service: "Denmark", Leads: []Lead{
			{Label: "Link-Lives censuses and burials", Hint: "kin linklives sweep, once release 2 is downloaded from https://digidata.rigsarkivet.dk/aflevering/14001"},
			{Label: "Link-Lives life courses (type by hand)", URL: "https://link-lives.dk/", Hint: typed},
			{Label: "Danish Demographic Database (type by hand)", URL: "https://ddd.dda.dk/ddd_en.htm", Hint: typed},
			{Label: "Danish Family Search (type by hand)", URL: "https://www.danishfamilysearch.com/", Hint: typed},
		}})
	}
	if r&place.Finland != 0 {
		out = append(out, Group{Service: "Finland", Leads: []Lead{
			{Label: "HisKi parish registers (type by hand)", URL: "https://hiski.genealogia.fi/hiski?en", Hint: typed + "; choose the parish first"},
			{Label: "SukuHaku, for members of the Genealogical Society of Finland", URL: "https://www.genealogia.fi/en/webservices/sukuhaku/", Hint: typed},
		}})
	}
	if r&place.Iceland != 0 {
		out = append(out, Group{Service: "Iceland", Leads: []Lead{
			{Label: "Íslendingabók (needs an Icelandic kennitala login)", URL: "https://www.islendingabok.is/", Hint: typed},
		}})
	}
	return out
}

// newspapers gives the historical newspapers of the person's countries over
// their adult life. The British Newspaper Archive forbids programs, and Welsh
// Newspapers Online, Irish Newspaper Archives, Svenska dagstidningar,
// timarit.is and Digi turn them away, so those are pages to open; Norway and
// Denmark to 1880 are searched by kin news.
func newspapers(r place.Region, first, surname string, by, dy int) (Group, bool) {
	from, to := 0, 0
	switch {
	case by > 0 && dy > 0:
		from, to = by+16, dy+2
	case by > 0:
		from, to = by+16, by+100
	case dy > 0:
		from, to = dy-60, dy+2
	}
	name := first + " " + surname
	typed := fmt.Sprintf("the phrase %q", name)
	years := ""
	if from > 0 {
		years = " " + span(from, to)
		typed += ", years " + span(from, to)
	}
	var ls []Lead
	if r.UK() {
		ls = append(ls, Lead{Label: "British Newspaper Archive (type by hand)", URL: "https://www.britishnewspaperarchive.co.uk/search", Hint: typed})
	}
	if r&place.Wales != 0 {
		ls = append(ls, Lead{Label: "Welsh Newspapers Online (type by hand)", URL: "https://newspapers.library.wales/", Hint: typed})
	}
	if r&place.Ireland != 0 {
		ls = append(ls, Lead{Label: "Irish Newspaper Archives (type by hand)", URL: "https://www.irishnewsarchive.com/", Hint: typed})
	}
	hint := func(src string, from, to int) string {
		h := "kin news -source " + src + " -q " + shellQuote(name)
		if from > 0 {
			h += fmt.Sprintf(" -from %d -to %d", from, to)
		}
		return h
	}
	if r&place.Norway != 0 {
		q := url.Values{"q": {`"` + name + `"`}, "mediatype": {"aviser"}}
		ls = append(ls, Lead{Label: "Norwegian newspapers" + years, URL: "https://www.nb.no/search?" + q.Encode(), Hint: hint("nb", from, to)})
	}
	if r&place.Denmark != 0 {
		l := Lead{Label: "Danish newspapers in Mediestream" + years, URL: "https://www2.statsbiblioteket.dk/mediestream/avis/search/" + url.PathEscape(`"`+name+`"`)}
		if from == 0 || from <= 1880 {
			top := to
			if top == 0 || top > 1880 {
				top = 1880
			}
			l.Hint = hint("kb", from, top) + " (the labs API ends in 1880)"
		}
		ls = append(ls, l)
	}
	if r&place.Sweden != 0 {
		q := url.Values{"q": {`"` + name + `"`}}
		if from > 0 {
			q.Set("from", fmt.Sprintf("%d-01-01", from))
			q.Set("to", fmt.Sprintf("%d-12-31", to))
		}
		ls = append(ls, Lead{Label: "Svenska dagstidningar" + years, URL: "https://tidningar.kb.se/search?" + q.Encode()})
	}
	if r&place.Finland != 0 {
		ls = append(ls, Lead{Label: "Digi, National Library of Finland (type by hand)", URL: "https://digi.kansalliskirjasto.fi/search", Hint: typed})
	}
	if r&place.Iceland != 0 {
		ls = append(ls, Lead{Label: "timarit.is (type by hand)", URL: "https://timarit.is/", Hint: typed})
	}
	return Group{Service: "Newspapers", Leads: ls}, len(ls) > 0
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

// opc composes a Cornwall OPC search: parish plus its neighbours when known,
// years when known, and any extra fields (forename2/forename3 are the
// parents in a baptism search and the spouse in a marriage search).
func opc(table, parish, first, surname string, from, to int, extra url.Values) string {
	q := url.Values{"surname1": {surname}, "t": {table}, "bf": {"Search"}}
	if first != "" {
		q.Set("forename1", first)
	}
	if parish != "" {
		q.Set("parish", parish)
		q.Set("nearby", "1")
	}
	if from > 0 {
		q.Set("year_from", strconv.Itoa(from))
		q.Set("year_to", strconv.Itoa(to))
	}
	for k, v := range extra {
		q[k] = v
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
		for _, x := range e.Searched {
			fmt.Fprintf(w, "  already searched: %s", x.Service)
			if x.When != "" {
				fmt.Fprintf(w, " (%s)", x.When)
			}
			if x.Note != "" {
				fmt.Fprintf(w, ": %s", x.Note)
			}
			fmt.Fprintln(w)
		}
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
