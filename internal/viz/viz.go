// Package viz renders the kinship graph into a self-contained HTML page.
package viz

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
)

//go:embed template.html
var template string

// Site holds the page text that belongs to one family rather than to the
// toolkit: the title, an optional flags column for the regional-records table
// and extra rows for the "Where to order copies" table. Load it from a JSON
// file with -site; every key is optional and falls back to DefaultSite.
type Site struct {
	Title        string        `json:"title"`                  // <title> and <h1>
	Eyebrow      string        `json:"eyebrow"`                // small line above the heading
	FlagsNote    string        `json:"flagsNote,omitempty"`    // sentence explaining the flags
	Flags        []Flag        `json:"flags,omitempty"`        // pills shown in the regional-records table
	OrderingRows []OrderingRow `json:"orderingRows,omitempty"` // rows added to "Where to order copies"
	Links        []Link        `json:"links,omitempty"`        // extra links in the header row, e.g. the printable reports
}

// Link is one extra header link; Href is used as written, so a relative path
// suits pages published together.
type Link struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

// Flag marks people in the regional-records table: everyone descended from
// DescendantsOf, or everyone whose given name matches NamePattern.
type Flag struct {
	Label         string `json:"label"`
	DescendantsOf string `json:"descendantsOf,omitempty"` // person id
	NamePattern   string `json:"namePattern,omitempty"`   // case-insensitive regular expression
	Style         string `json:"style,omitempty"`         // "ok", "prob" or empty
}

// OrderingRow is one row of the "Where to order copies" table.
type OrderingRow struct {
	Prefix  string `json:"prefix"`
	Holding string `json:"holding"`
	How     string `json:"how"`
}

// DefaultSite is what the page shows when no site file is given.
func DefaultSite() Site {
	return Site{
		Title:   "Kin",
		Eyebrow: "Family history · public records",
		OrderingRows: []OrderingRow{{
			Prefix:  "Estates after the index",
			Holding: "Master of the High Court for the district where the person died. Estate numbers appear in the newspaper estate notices and on the Master's records.",
			How:     "A child or grandchild may request a copy of the death notice and liquidation account from the Master's office, quoting name and date of death; identity documents are asked for.",
		}},
	}
}

// LoadSite reads a site JSON file over the defaults. An empty path returns
// the defaults; keys present in the file replace the corresponding default.
func LoadSite(path string) (Site, error) {
	s := DefaultSite()
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	for _, f := range s.Flags {
		if f.NamePattern != "" {
			if _, err := regexp.Compile("(?i)" + f.NamePattern); err != nil {
				return s, fmt.Errorf("%s: flag %q: %w", path, f.Label, err)
			}
		}
	}
	for _, l := range s.Links {
		if l.Label == "" || l.Href == "" {
			return s, fmt.Errorf("%s: link %q: label and href are required", path, l.Label)
		}
	}
	return s, nil
}

// Options controls what the page shows.
type Options struct {
	Seed        string   // person id the page is about; relationships are computed from here
	NoticesPath string   // JSON list of newspaper notices from `kin eggsa papers` (optional)
	RecordsPath string   // JSON list of hand-collected record citations (optional)
	ProbableIDs []string // ids from which upward the line is only probable
	Site        Site
	TreeURL     string // absolute URL of the published family tree page, linked from the header (optional)
	MapURL      string // absolute URL of the published map page (optional)
}

// Payload is the JSON handed to the page. It carries only the people the page
// draws (the seed's ancestors, everyone within two steps of them, the regional
// record rows and the ancestor chains those rows are flagged through), with
// only the fields the page reads; Counts describes the whole graph.
type Payload struct {
	Seed       string            `json:"seed"`
	Generated  string            `json:"generated"`
	Persons    []Person          `json:"persons"`
	Components map[string]int    `json:"components"` // connected component of each shipped person
	Relations  map[string]string `json:"relations"`  // relationship label of each shipped person to the seed
	Family     []string          `json:"family"`     // surnames of the seed and the first three generations
	Regional   []string          `json:"regional"`   // ids shown in the regional-records table
	Counts     Counts            `json:"counts"`
	Notices    []json.RawMessage `json:"notices"`
	Records    []json.RawMessage `json:"records"`
	Probable   []string          `json:"probable"`
	Site       Site              `json:"site"`
	TreeURL    string            `json:"treeUrl,omitempty"`
	MapURL     string            `json:"mapUrl,omitempty"`
}

// Person is the part of a model.Person the page reads. URL is left out when it
// is the WikiTree profile, which the page derives from WikiTree.
type Person struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Given      string   `json:"given,omitempty"`
	Surname    string   `json:"surname,omitempty"`
	Birth      string   `json:"birth,omitempty"`
	Death      string   `json:"death,omitempty"`
	BirthPlace string   `json:"birthPlace,omitempty"`
	DeathPlace string   `json:"deathPlace,omitempty"`
	WikiTree   string   `json:"wikitree,omitempty"`
	URL        string   `json:"url,omitempty"`
	Father     string   `json:"father,omitempty"`
	Mother     string   `json:"mother,omitempty"`
	Spouses    []string `json:"spouses,omitempty"`
	Sources    []string `json:"sources,omitempty"`
}

// Counts summarises the whole graph for the tiles and the footer.
type Counts struct {
	Persons int            `json:"persons"` // everyone in the graph
	Network int            `json:"network"` // everyone in the seed's connected component
	Sources map[string]int `json:"sources"` // people per source name
}

// Hops is how far from a direct ancestor the family network reaches.
const Hops = 2

// regionalPlace matches a birthplace or place of death in South Africa, past or present.
var regionalPlace = regexp.MustCompile(`(?i)south africa|cape colony|cape province|cape town|transvaal|natal|orange free|gauteng|springs|johannesburg|pretoria|durban|boksburg|benoni|germiston|brakpan|witwatersrand|ceres|wynberg|maitland|paarl|durbanville|port elizabeth|kenwyn|roodepoort|maraisburg|krugersdorp|plettenberg|mossel bay|stellenbosch|drakenstein|caep|kaap`)

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// shown picks the people the page draws and the regional rows among them.
func shown(g *model.Graph, seed string) (keep map[string]bool, family, regional []string) {
	keep = map[string]bool{}
	anc := graph.Ancestors(g, seed) // includes seed at 0
	for id := range anc {
		if g.Persons[id] != nil {
			keep[id] = true
		}
	}
	// the network: everyone within Hops steps of an ancestor by birth or marriage
	adj := map[string][]string{}
	for id, p := range g.Persons {
		for _, o := range append([]string{p.Father, p.Mother}, p.Spouses...) {
			if o != "" && g.Persons[o] != nil {
				adj[id] = append(adj[id], o)
				adj[o] = append(adj[o], id)
			}
		}
	}
	dist := map[string]int{}
	var queue []string
	for id := range keep {
		if id != seed {
			dist[id] = 0
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if dist[cur] >= Hops {
			continue
		}
		for _, nb := range adj[cur] {
			if _, seen := dist[nb]; !seen {
				dist[nb] = dist[cur] + 1
				queue = append(queue, nb)
				keep[nb] = true
			}
		}
	}
	// family surnames: the first three generations nearest first (father before
	// mother within a generation, then by birth), then the seed's own
	type gp struct {
		p   *model.Person
		gen int
	}
	var near []gp
	seen := map[string]bool{seed: true}
	for queue := []gp{{g.Persons[seed], 0}}; len(queue) > 0; queue = queue[1:] {
		cur := queue[0]
		if cur.p == nil || cur.gen >= 3 {
			continue
		}
		for _, id := range []string{cur.p.Father, cur.p.Mother} {
			if p := g.Persons[id]; p != nil && !seen[id] {
				seen[id] = true
				near = append(near, gp{p, cur.gen + 1})
				queue = append(queue, gp{p, cur.gen + 1})
			}
		}
	}
	birth := func(p *model.Person) string {
		if p.Birth == "" {
			return "9999"
		}
		return p.Birth
	}
	sort.SliceStable(near, func(i, j int) bool {
		if near[i].gen != near[j].gen {
			return near[i].gen < near[j].gen
		}
		return birth(near[i].p) < birth(near[j].p)
	})
	names := map[string]bool{}
	add := func(surname string) {
		if n := strings.ToLower(surname); n != "" && !names[n] {
			names[n] = true
			family = append(family, strings.ToUpper(n[:1])+n[1:])
		}
	}
	for _, x := range near {
		add(x.p.Surname)
	}
	if p := g.Persons[seed]; p != nil {
		add(p.Surname)
	}
	// regional rows: family surnames born, buried or recorded in the region, plus their ancestor chains for the flags
	for _, p := range g.Sorted() {
		if p.ID == seed || strings.HasPrefix(p.ID, "seed:") || !names[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(p.Surname), "s"))] && !names[strings.ToLower(strings.TrimSpace(p.Surname))] {
			continue
		}
		if !regionalPlace.MatchString(p.BirthPlace) && !regionalPlace.MatchString(p.DeathPlace) && !has(p.Sources, "eggsa") {
			continue
		}
		regional = append(regional, p.ID)
		keep[p.ID] = true
		for id := range graph.Ancestors(g, p.ID) {
			if g.Persons[id] != nil {
				keep[id] = true
			}
		}
	}
	return keep, family, regional
}

// Render writes the page for g to path.
func Render(g *model.Graph, opts Options, path string) error {
	keep, family, regional := shown(g, opts.Seed)
	comps := graph.Components(g)
	counts := Counts{Persons: len(g.Persons), Sources: map[string]int{}}
	for id, p := range g.Persons {
		if comps[id] == comps[opts.Seed] {
			counts.Network++
		}
		for _, s := range p.Sources {
			counts.Sources[s]++
		}
	}
	pl := Payload{Seed: opts.Seed, Components: map[string]int{}, Relations: map[string]string{}, Family: family, Regional: regional, Counts: counts,
		Site: opts.Site, TreeURL: opts.TreeURL, MapURL: opts.MapURL}
	for _, p := range g.Sorted() {
		if !keep[p.ID] {
			continue
		}
		v := Person{ID: p.ID, Name: p.Name, Given: p.Given, Surname: p.Surname, Birth: p.Birth, Death: p.Death, BirthPlace: p.BirthPlace, DeathPlace: p.DeathPlace,
			WikiTree: p.WikiTree, URL: p.URL, Father: p.Father, Mother: p.Mother, Spouses: p.Spouses, Sources: p.Sources}
		if p.WikiTree != "" && p.URL == "https://www.wikitree.com/wiki/"+p.WikiTree {
			v.URL = ""
		}
		pl.Persons = append(pl.Persons, v)
		pl.Components[p.ID] = comps[p.ID]
		if p.ID != opts.Seed {
			if r, ok := graph.Relationship(g, opts.Seed, p.ID); ok {
				pl.Relations[p.ID] = r.Label
			}
		}
	}
	for _, id := range opts.ProbableIDs {
		id = g.Resolve(id)
		for anc := range graph.Ancestors(g, id) {
			pl.Probable = append(pl.Probable, anc)
		}
	}
	if opts.NoticesPath != "" {
		if b, err := os.ReadFile(opts.NoticesPath); err == nil {
			_ = json.Unmarshal(b, &pl.Notices)
		}
	}
	if opts.RecordsPath != "" {
		if b, err := os.ReadFile(opts.RecordsPath); err == nil {
			_ = json.Unmarshal(b, &pl.Records)
		}
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return err
	}
	// keep the JSON safe inside a <script> block
	s := strings.ReplaceAll(string(b), "</", "<\\/")
	html := strings.Replace(template, "/*__DATA__*/", s, 1)
	return os.WriteFile(path, []byte(html), 0o644)
}
