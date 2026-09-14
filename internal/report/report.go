// Package report renders a printable ancestry report (direct line, Ahnentafel,
// siblings, sources) for one person in the graph.
package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
)

//go:embed report.html
var tpl string

// Record mirrors an entry in seed/records.json.
type Record struct {
	Person     string `json:"person"`
	Names      string `json:"names"`
	Event      string `json:"event"`
	Date       string `json:"date"`
	Place      string `json:"place"`
	Detail     string `json:"detail"`
	Collection string `json:"collection"`
	URL        string `json:"url"`
	Image      string `json:"image"`
}

type Row struct {
	Ahnentafel int
	Generation string // relation to the reader (grandmother, great-grandfather…)
	Person     *model.Person
	Status     string // proven | probable | recorded
	Sources    []Source
}

type Source struct{ Label, URL string }

type Family struct {
	Parents  string
	Children []*model.Person
}

type Data struct {
	Title, Subtitle, Reader, Generated string
	Subject                            *model.Person
	Line                               []Row
	Families                           []Family
	Records                            []Record
	Notes                              []string
	Probable                           map[string]bool
}

// Options control the report.
type Options struct {
	Root        string // person id whose ancestry is reported
	Reader      string // person id the relationship labels are computed from
	Title       string
	Subtitle    string
	MaxGen      int      // generations above Root to include
	ProbableIDs []string // ids from which upward the line is only probable
	Notes       []string
	RecordsPath string
}

// Render writes the HTML report for opts to path.
func Render(g *model.Graph, opts Options, path string) error {
	root := g.Persons[opts.Root]
	if root == nil {
		return fmt.Errorf("no person %s", opts.Root)
	}
	if opts.MaxGen <= 0 {
		opts.MaxGen = 8
	}
	probable := map[string]bool{}
	for _, id := range opts.ProbableIDs {
		for anc := range graph.Ancestors(g, id) {
			probable[anc] = true
		}
	}
	var recs []Record
	if opts.RecordsPath != "" {
		if b, err := os.ReadFile(opts.RecordsPath); err == nil {
			_ = json.Unmarshal(b, &recs)
		}
	}
	// Ahnentafel walk
	type item struct {
		id string
		n  int
	}
	rows := map[int]Row{}
	queue := []item{{opts.Root, 1}}
	inLine := map[string]bool{}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		p := g.Persons[it.id]
		if p == nil {
			continue
		}
		gen := 0
		for n := it.n; n > 1; n /= 2 {
			gen++
		}
		if gen > opts.MaxGen {
			continue
		}
		inLine[p.ID] = true
		r := Row{Ahnentafel: it.n, Person: p, Status: status(p, probable)}
		if rel, ok := graph.Relationship(g, opts.Reader, p.ID); ok {
			r.Generation = graph.Gendered(rel.Label, p.Gender)
		}
		if p.ID == opts.Reader {
			r.Generation = "self"
		}
		for _, rc := range recs {
			if concerns(g, rc, p) {
				r.Sources = append(r.Sources, Source{Label: shortEvent(rc.Event) + ", " + model.Year(rc.Date), URL: rc.URL})
			}
		}
		if p.WikiTree != "" {
			r.Sources = append(r.Sources, Source{Label: "WikiTree " + p.WikiTree, URL: "https://www.wikitree.com/wiki/" + p.WikiTree})
		}
		rows[it.n] = r
		if p.Father != "" {
			queue = append(queue, item{p.Father, it.n * 2})
		}
		if p.Mother != "" {
			queue = append(queue, item{p.Mother, it.n*2 + 1})
		}
	}
	nums := make([]int, 0, len(rows))
	for n := range rows {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	d := Data{Title: opts.Title, Subtitle: opts.Subtitle, Subject: root, Probable: probable, Notes: opts.Notes,
		Generated: time.Now().Format("2 January 2006")}
	if rd := g.Persons[opts.Reader]; rd != nil {
		d.Reader = rd.Name
	}
	for _, n := range nums {
		d.Line = append(d.Line, rows[n])
	}
	// families: children of every couple in the line (siblings of line members)
	seenFam := map[string]bool{}
	for _, n := range nums {
		p := rows[n].Person
		if p.Father == "" && p.Mother == "" {
			continue
		}
		key := p.Father + "|" + p.Mother
		if seenFam[key] {
			continue
		}
		seenFam[key] = true
		var kids []*model.Person
		for _, q := range g.Persons {
			if (p.Father != "" && q.Father == p.Father) || (p.Mother != "" && q.Mother == p.Mother) {
				kids = append(kids, q)
			}
		}
		sort.Slice(kids, func(i, j int) bool { return kids[i].Birth < kids[j].Birth })
		var names []string
		for _, pid := range []string{p.Father, p.Mother} {
			if par := g.Persons[pid]; par != nil {
				names = append(names, par.Name)
			}
		}
		d.Families = append(d.Families, Family{Parents: strings.Join(names, " and "), Children: kids})
	}
	// records for anyone on the line or in one of its families
	relevant := map[string]bool{}
	for id := range inLine {
		relevant[id] = true
	}
	for _, fam := range d.Families {
		for _, c := range fam.Children {
			relevant[c.ID] = true
		}
	}
	for _, rc := range recs {
		for id := range relevant {
			if concerns(g, rc, g.Persons[id]) {
				d.Records = append(d.Records, rc)
				break
			}
		}
	}
	sort.Slice(d.Records, func(i, j int) bool { return d.Records[i].Date < d.Records[j].Date })
	funcs := template.FuncMap{
		"year": model.Year,
		"date": func(s string) string {
			s = strings.TrimSpace(s)
			if s == "" || strings.EqualFold(s, "unknown") {
				return ""
			}
			if t, err := time.Parse("2006-01-02", s); err == nil {
				return t.Format("2 Jan 2006")
			}
			if t, err := time.Parse("2006-01", s); err == nil {
				return t.Format("Jan 2006")
			}
			return s
		},
		"place": func(s string) string {
			return strings.TrimSuffix(strings.TrimSuffix(s, ", South Africa"), ", England")
		},
		"cap": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
	}
	t, err := template.New("report").Funcs(funcs).Parse(tpl)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, d)
}

// concerns reports whether a record is about p: filed under p's id (or an id
// that merged into it), or filed under p's spouse with p named in it.
func concerns(g *model.Graph, rc Record, p *model.Person) bool {
	if p == nil {
		return false
	}
	target := g.Resolve(rc.Person)
	if target == p.ID {
		return true
	}
	for _, sp := range p.Spouses {
		if target == sp && p.Given != "" && strings.Contains(rc.Names, strings.Fields(p.Given)[0]) {
			return true
		}
	}
	return false
}

func shortEvent(e string) string {
	if i := strings.Index(e, ":"); i > 0 {
		return strings.TrimSpace(e[:i])
	}
	if i := strings.Index(e, " ("); i > 0 {
		return e[:i]
	}
	return e
}

func status(p *model.Person, probable map[string]bool) string {
	if probable[p.ID] {
		return "probable"
	}
	for _, s := range p.Sources {
		if s == "familysearch" || s == "user" {
			return "proven"
		}
	}
	return "recorded"
}
