// Package geomap renders the ancestors' birth and death places on a
// self-contained pan-and-zoom world map. The world outline is embedded, so
// the page needs no tile server, and the places are geocoded once through
// package geo and cached.
package geomap

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/geo"
	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/viz"
)

//go:embed template.html
var template string

// world is Natural Earth's 1:50m countries and land as TopoJSON, from the
// world-atlas package (public domain data, ISC-licensed packaging).
//
//go:embed world-50m.json
var world string

// Options controls the map.
type Options struct {
	Root         string
	Reader       string
	MaxGen       int
	CachePath    string // geocode cache json (read and written)
	PlacesPath   string // manual overrides json (optional)
	Offline      bool   // never call the geocoder
	DashboardURL string
	TreeURL      string
	Site         viz.Site
	Log          func(string, ...any)
}

// Person is one ancestor on the map.
type Person struct {
	ID         string `json:"id"`
	N          int    `json:"n"`    // smallest Ahnentafel number
	Gen        int    `json:"gen"`  // generation above the root
	Line       int    `json:"line"` // 0..3 = the root's grandparents' lines, -1 = root and parents
	Name       string `json:"name"`
	Birth      string `json:"birth,omitempty"`
	Death      string `json:"death,omitempty"`
	BirthPlace string `json:"birthPlace,omitempty"`
	DeathPlace string `json:"deathPlace,omitempty"`
	BirthAt    int    `json:"birthAt"` // index into Places, -1 when unknown
	DeathAt    int    `json:"deathAt"`
	Relation   string `json:"rel"`
	Status     string `json:"status"`
	WikiTree   string `json:"wikitree,omitempty"`
	URL        string `json:"url,omitempty"`
}

// Place is one map pin: every place string that resolved to the same point.
type Place struct {
	Lat       float64  `json:"lat"`
	Lon       float64  `json:"lon"`
	Label     string   `json:"label"`            // the name as the records write it for a town; the resolved area otherwise
	Modern    string   `json:"modern,omitempty"` // what the gazetteer calls the place today, when that differs
	Names     []string `json:"names"`            // the place strings as written in the graph
	Precision string   `json:"precision"`
	Births    []string `json:"births"` // person ids
	Deaths    []string `json:"deaths"`
}

// Payload is the JSON handed to the page.
type Payload struct {
	Root         string   `json:"root"`
	ReaderName   string   `json:"readerName"`
	Generated    string   `json:"generated"`
	Lines        []string `json:"lines"` // names of the four grandparents, in Ahnentafel order 4..7
	Persons      []Person `json:"persons"`
	Places       []Place  `json:"places"`
	Unplaced     []string `json:"unplaced"` // place strings that did not resolve
	DashboardURL string   `json:"dashboardUrl,omitempty"`
	TreeURL      string   `json:"treeUrl,omitempty"`
	Site         struct {
		Title   string `json:"title"`
		Eyebrow string `json:"eyebrow"`
	} `json:"site"`
}

func status(p *model.Person) string {
	for _, s := range p.Sources {
		if s == "familysearch" || s == "user" {
			return "record"
		}
	}
	for _, s := range p.Sources {
		if s == "eggsa" {
			return "gravestone"
		}
	}
	return "tree"
}

// Build geocodes and assembles the payload.
func Build(ctx context.Context, g *model.Graph, opts Options) (*Payload, error) {
	logf := opts.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	root := g.Resolve(opts.Root)
	if g.Persons[root] == nil {
		return nil, fmt.Errorf("root %q is not in the graph", opts.Root)
	}
	reader := root
	if opts.Reader != "" {
		reader = g.Resolve(opts.Reader)
	}
	maxGen := opts.MaxGen
	if maxGen <= 0 {
		maxGen = 20
	}
	// Ahnentafel walk, first occurrence only
	type item struct {
		id string
		n  int
	}
	seen := map[string]int{}
	var persons []Person
	queue := []item{{root, 1}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		gen := bits.Len(uint(it.n)) - 1
		p := g.Persons[it.id]
		if p == nil || gen > maxGen {
			continue
		}
		if _, dup := seen[it.id]; dup {
			continue
		}
		seen[it.id] = it.n
		line := -1
		if it.n >= 4 {
			m := it.n
			for m >= 8 {
				m /= 2
			}
			line = m - 4
		}
		per := Person{ID: p.ID, N: it.n, Gen: gen, Line: line, Name: p.Name, Birth: p.Birth, Death: p.Death,
			BirthPlace: p.BirthPlace, DeathPlace: p.DeathPlace, BirthAt: -1, DeathAt: -1, Status: status(p), WikiTree: p.WikiTree, URL: p.URL}
		if p.ID == reader {
			per.Relation = "self"
		} else if rel, ok := graph.Relationship(g, reader, p.ID); ok {
			per.Relation = graph.Gendered(rel.Label, p.Gender)
		}
		persons = append(persons, per)
		if p.Father != "" {
			queue = append(queue, item{g.Resolve(p.Father), it.n * 2})
		}
		if p.Mother != "" {
			queue = append(queue, item{g.Resolve(p.Mother), it.n*2 + 1})
		}
	}
	// geocode
	cache := geo.LoadCache(opts.CachePath)
	if opts.PlacesPath != "" {
		if err := cache.LoadOverrides(opts.PlacesPath); err != nil {
			logf("geo: overrides: %v", err)
		}
	}
	names := map[string]bool{}
	for _, per := range persons {
		if per.BirthPlace != "" {
			names[per.BirthPlace] = true
		}
		if per.DeathPlace != "" {
			names[per.DeathPlace] = true
		}
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	logf("geo: %d distinct places", len(list))
	points := geo.New().ResolveAll(ctx, cache, list, opts.Offline, logf)
	_ = cache.Save()
	// group by rounded coordinate
	pl := &Payload{Root: root, Generated: time.Now().Format("2 January 2006"), DashboardURL: opts.DashboardURL, TreeURL: opts.TreeURL}
	pl.Site.Title, pl.Site.Eyebrow = opts.Site.Title, opts.Site.Eyebrow
	if p := g.Persons[reader]; p != nil {
		pl.ReaderName = p.Name
	}
	key := func(pt geo.Point) string { return fmt.Sprintf("%.3f,%.3f", pt.Lat, pt.Lon) }
	index := map[string]int{}
	placeOf := func(name string) int {
		pt, ok := points[name]
		if !ok || pt.Failed {
			return -1
		}
		k := key(pt)
		if i, ok := index[k]; ok {
			p := &pl.Places[i]
			p.Names = model.Uniq(append(p.Names, name))
			if rank(pt.Precision) < rank(p.Precision) {
				p.Precision, p.Label = pt.Precision, pt.Label
			}
			return i
		}
		index[k] = len(pl.Places)
		pl.Places = append(pl.Places, Place{Lat: round4(pt.Lat), Lon: round4(pt.Lon), Label: pt.Label, Names: []string{name}, Precision: pt.Precision, Births: []string{}, Deaths: []string{}})
		return index[k]
	}
	defer func() {
		for i := range pl.Places {
			p := &pl.Places[i]
			modern := shortLabel(p.Label)
			p.Label = modern
			if p.Precision != "town" {
				continue
			}
			if h := historicName(p.Names); h != "" && !strings.EqualFold(h, firstPart(modern)) {
				p.Label, p.Modern = h, modern
			}
		}
	}()
	unplaced := map[string]bool{}
	for i := range persons {
		per := &persons[i]
		if per.BirthPlace != "" {
			if per.BirthAt = placeOf(per.BirthPlace); per.BirthAt >= 0 {
				pl.Places[per.BirthAt].Births = append(pl.Places[per.BirthAt].Births, per.ID)
			} else {
				unplaced[per.BirthPlace] = true
			}
		}
		if per.DeathPlace != "" {
			if per.DeathAt = placeOf(per.DeathPlace); per.DeathAt >= 0 {
				pl.Places[per.DeathAt].Deaths = append(pl.Places[per.DeathAt].Deaths, per.ID)
			} else {
				unplaced[per.DeathPlace] = true
			}
		}
	}
	pl.Unplaced = []string{}
	for n := range unplaced {
		pl.Unplaced = append(pl.Unplaced, n)
	}
	sort.Strings(pl.Unplaced)
	pl.Persons = persons
	pl.Lines = make([]string, 4)
	for _, per := range persons {
		if per.N >= 4 && per.N <= 7 {
			pl.Lines[per.N-4] = per.Name
		}
	}
	for i, n := range pl.Lines {
		if n == "" {
			pl.Lines[i] = []string{"paternal grandfather", "paternal grandmother", "maternal grandfather", "maternal grandmother"}[i]
		}
	}
	return pl, nil
}

func rank(p string) int {
	switch p {
	case "town":
		return 0
	case "region":
		return 1
	}
	return 2
}

func round4(f float64) float64 { return math.Round(f*1e4) / 1e4 }

var parens = regexp.MustCompile(`\([^)]*\)`)

// firstPart returns the text before the first comma, without parentheses.
func firstPart(s string) string {
	s = parens.ReplaceAllString(s, "")
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// historicName picks the name the records themselves use for a place: the
// most frequent leading part of the strings that resolved there, skipping
// street addresses. Ties go to the shorter, then the alphabetically earlier.
func historicName(names []string) string {
	count := map[string]int{}
	spelling := map[string]string{}
	for _, n := range names {
		parts := strings.Split(parens.ReplaceAllString(n, ""), ",")
		var h string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" || (part[0] >= '0' && part[0] <= '9') {
				continue // "48 Eighth Avenue"
			}
			h = part
			break
		}
		if h == "" {
			continue
		}
		k := strings.ToLower(h)
		count[k]++
		if _, ok := spelling[k]; !ok {
			spelling[k] = h
		}
	}
	best := ""
	for k := range count {
		if best == "" || count[k] > count[best] || (count[k] == count[best] && (len(k) < len(best) || (len(k) == len(best) && k < best))) {
			best = k
		}
	}
	return spelling[best]
}

// shortLabel trims a Nominatim display name to its first three parts.
func shortLabel(s string) string {
	parts := strings.Split(s, ",")
	if len(parts) > 3 {
		parts = append(parts[:2], parts[len(parts)-1])
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ", ")
}

// Render writes the page.
func Render(ctx context.Context, g *model.Graph, opts Options, path string) error {
	pl, err := Build(ctx, g, opts)
	if err != nil {
		return err
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return err
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "</", "<\\/") }
	html := strings.Replace(template, "/*__DATA__*/", esc(string(b)), 1)
	html = strings.Replace(html, "/*__WORLD__*/", esc(world), 1)
	return os.WriteFile(path, []byte(html), 0o644)
}
