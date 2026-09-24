// Package tree renders one person's pedigree (direct ancestors, couple by
// couple) as a self-contained pan-and-zoom HTML page.
package tree

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/viz"
)

//go:embed template.html
var template string

// Options controls what the page draws.
type Options struct {
	Root         string   // person whose ancestors are drawn
	Reader       string   // relationship labels are relative to this person (default Root)
	MaxGen       int      // generations above Root to draw; 0 means 20
	ProbableIDs  []string // ids from which upward the line is only probable
	RecordsPath  string   // optional records json
	DashboardURL string   // absolute URL of the published dashboard (optional)
	Site         viz.Site // Title and Eyebrow are used
	Client       bool     // the tree is for a client or relative: leave out the internal research note text
}

// Node is one person on the pedigree.
type Node struct {
	ID          string   `json:"id"`
	Numbers     []int    `json:"n"`   // every Ahnentafel number the person occurs under, ascending; n[0] is drawn
	Gen         int      `json:"gen"` // generation of n[0]; the root is 0
	Name        string   `json:"name"`
	Given       string   `json:"given,omitempty"`
	Surname     string   `json:"surname,omitempty"`
	Gender      string   `json:"gender,omitempty"`
	Birth       string   `json:"birth,omitempty"`
	Death       string   `json:"death,omitempty"`
	BirthPlace  string   `json:"birthPlace,omitempty"`
	DeathPlace  string   `json:"deathPlace,omitempty"`
	Occupations []string `json:"occupations,omitempty"`
	Father      string   `json:"father,omitempty"` // resolved id; may be absent from Nodes (dangling or beyond MaxGen)
	Mother      string   `json:"mother,omitempty"`
	Spouses     []string `json:"spouses,omitempty"`  // spouses present in the graph
	Children    []string `json:"children,omitempty"` // by birth; ids resolve to Nodes or People
	Relation    string   `json:"rel"`                // "self", a gendered label, or "" when unrelated to Reader
	Status      string   `json:"status"`             // probable | record | gravestone | tree
	Sources     []string `json:"sources,omitempty"`
	WikiTree    string   `json:"wikitree,omitempty"`
	URL         string   `json:"url,omitempty"`
	Living      bool     `json:"living,omitempty"`
	Note        string   `json:"note,omitempty"`
	Records     []int    `json:"records,omitempty"` // indexes into Payload.Records
}

// Brief is a person referenced from a node (other spouse, child) who is not on the line.
type Brief struct {
	Name     string `json:"name"`
	Birth    string `json:"birth,omitempty"`
	Death    string `json:"death,omitempty"`
	Gender   string `json:"gender,omitempty"`
	WikiTree string `json:"wikitree,omitempty"`
	URL      string `json:"url,omitempty"`
}

// Pedigree is the walked ancestry of Root.
type Pedigree struct {
	Root        string
	Reader      string
	Nodes       []Node           // breadth-first, ascending Ahnentafel number
	Siblings    []Node           // Root's own full siblings, birth order; not on the ancestor line so they carry no Numbers
	People      map[string]Brief // spouses and children who are not Nodes
	Generations int              // deepest generation present
	Dangling    int              // parent ids named on a node but absent from the graph
	Truncated   int              // parents present in the graph but beyond MaxGen
}

type siteText struct {
	Title   string `json:"title"`
	Eyebrow string `json:"eyebrow"`
}

// Payload is the JSON handed to the page.
type Payload struct {
	Root         string            `json:"root"`
	Reader       string            `json:"reader"`
	ReaderName   string            `json:"readerName"`
	Generated    string            `json:"generated"`
	MaxGen       int               `json:"maxGen"`
	Generations  int               `json:"generations"`
	Dangling     int               `json:"dangling"`
	Truncated    int               `json:"truncated"`
	DashboardURL string            `json:"dashboardUrl,omitempty"`
	Site         siteText          `json:"site"`
	Nodes        []Node            `json:"nodes"`
	Siblings     []Node            `json:"siblings,omitempty"`
	People       map[string]Brief  `json:"people"`
	Records      []json.RawMessage `json:"records"`
}

// Build walks the pedigree of opts.Root. Every ancestor is emitted once, under
// the smallest Ahnentafel number it occurs at; later occurrences are recorded
// in Numbers and not expanded again. recs are the raw record citations whose
// "person" field is matched to nodes.
func Build(g *model.Graph, opts Options, recs []json.RawMessage) (*Pedigree, error) {
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
	probable := map[string]bool{}
	for _, id := range opts.ProbableIDs {
		for anc := range graph.Ancestors(g, g.Resolve(id)) {
			probable[anc] = true
		}
	}
	kids := childIndex(g)
	recPerson := recordPersons(g, recs)

	ped := &Pedigree{Root: root, Reader: reader, People: map[string]Brief{}}
	index := map[string]int{}
	type item struct {
		id string
		n  int
	}
	queue := []item{{root, 1}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		gen := bits.Len(uint(it.n)) - 1
		p := g.Persons[it.id]
		if gen > maxGen {
			if p != nil {
				ped.Truncated++
			}
			continue
		}
		if p == nil {
			ped.Dangling++
			continue
		}
		if i, dup := index[it.id]; dup {
			ped.Nodes[i].Numbers = append(ped.Nodes[i].Numbers, it.n)
			continue
		}
		index[it.id] = len(ped.Nodes)
		n := buildNode(g, p, reader, probable, kids, recPerson, opts.Client)
		n.Numbers = []int{it.n}
		n.Gen = gen
		if gen > ped.Generations {
			ped.Generations = gen
		}
		ped.Nodes = append(ped.Nodes, n)
		if n.Father != "" {
			queue = append(queue, item{n.Father, it.n * 2})
		}
		if n.Mother != "" {
			queue = append(queue, item{n.Mother, it.n*2 + 1})
		}
	}
	for _, n := range ped.Nodes {
		for _, id := range append(append([]string{}, n.Spouses...), n.Children...) {
			if _, on := index[id]; on {
				continue
			}
			if _, done := ped.People[id]; done {
				continue
			}
			if q := g.Persons[id]; q != nil {
				ped.People[id] = Brief{Name: q.Name, Birth: q.Birth, Death: q.Death, Gender: q.Gender, WikiTree: q.WikiTree, URL: q.URL}
			}
		}
	}
	ped.Siblings = siblingNodes(g, root, index, probable, kids, recPerson, reader, opts.Client)
	return ped, nil
}

// buildNode fills in every Node field that does not depend on the person's
// place in the Ahnentafel walk (Numbers, Gen); callers set those themselves.
func buildNode(g *model.Graph, p *model.Person, reader string, probable map[string]bool, kids map[string][]string, recPerson []string, client bool) Node {
	n := Node{
		ID: p.ID, Name: p.Name, Given: p.Given, Surname: p.Surname,
		Gender: p.Gender, Birth: p.Birth, Death: p.Death, BirthPlace: p.BirthPlace, DeathPlace: p.DeathPlace,
		Occupations: p.Occupations, Sources: p.Sources, WikiTree: p.WikiTree, URL: p.URL, Living: p.Living,
		Note: p.Note, Status: status(p, probable), Children: kids[p.ID],
	}
	if client {
		n.Note = ""
	}
	if p.Father != "" {
		n.Father = g.Resolve(p.Father)
	}
	if p.Mother != "" {
		n.Mother = g.Resolve(p.Mother)
	}
	for _, s := range p.Spouses {
		if s = g.Resolve(s); g.Persons[s] != nil {
			n.Spouses = append(n.Spouses, s)
		}
	}
	n.Spouses = model.Uniq(n.Spouses)
	if p.ID == reader {
		n.Relation = "self"
	} else if rel, ok := graph.Relationship(g, reader, p.ID); ok {
		n.Relation = graph.Gendered(rel.Label, p.Gender)
	}
	for i, rp := range recPerson {
		if rp == p.ID {
			n.Records = append(n.Records, i)
		}
	}
	return n
}

// siblingNodes returns root's own full siblings (other children of its
// parents), birth order, skipping anyone already drawn as an ancestor.
func siblingNodes(g *model.Graph, root string, index map[string]int, probable map[string]bool, kids map[string][]string, recPerson []string, reader string, client bool) []Node {
	rp := g.Persons[root]
	if rp == nil {
		return nil
	}
	seen := map[string]bool{root: true}
	var ids []string
	for _, par := range []string{rp.Father, rp.Mother} {
		if par == "" {
			continue
		}
		for _, id := range kids[g.Resolve(par)] {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := g.Persons[ids[i]], g.Persons[ids[j]]
		if a.Birth != b.Birth {
			return a.Birth < b.Birth
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	var out []Node
	for _, id := range ids {
		if _, on := index[id]; on {
			continue
		}
		p := g.Persons[id]
		if p == nil {
			continue
		}
		n := buildNode(g, p, reader, probable, kids, recPerson, client)
		n.Gen = 0
		out = append(out, n)
	}
	return out
}

// status follows the dashboard's rule: probable, then a person known from a
// record (FamilySearch or the seed), then a gravestone, else a tree entry.
func status(p *model.Person, probable map[string]bool) string {
	if probable[p.ID] {
		return "probable"
	}
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

// childIndex maps every parent id to its children, sorted by birth, name, id.
func childIndex(g *model.Graph) map[string][]string {
	out := map[string][]string{}
	for _, q := range g.Persons {
		for _, par := range []string{q.Father, q.Mother} {
			if par == "" {
				continue
			}
			par = g.Resolve(par)
			out[par] = append(out[par], q.ID)
		}
	}
	for par, ids := range out {
		sort.Slice(ids, func(i, j int) bool {
			a, b := g.Persons[ids[i]], g.Persons[ids[j]]
			if a.Birth != b.Birth {
				return a.Birth < b.Birth
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.ID < b.ID
		})
		out[par] = model.Uniq(ids)
	}
	return out
}

// recordPersons returns, for each raw record, the resolved id it concerns.
func recordPersons(g *model.Graph, recs []json.RawMessage) []string {
	out := make([]string, len(recs))
	for i, raw := range recs {
		var r struct {
			Person string `json:"person"`
		}
		if json.Unmarshal(raw, &r) == nil && r.Person != "" {
			out[i] = g.Resolve(r.Person)
		}
	}
	return out
}

func loadRecords(path string) []json.RawMessage {
	var recs []json.RawMessage
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &recs)
		}
	}
	if recs == nil {
		recs = []json.RawMessage{}
	}
	return recs
}

// Render writes the page for opts to path.
func Render(g *model.Graph, opts Options, path string) error {
	recs := loadRecords(opts.RecordsPath)
	ped, err := Build(g, opts, recs)
	if err != nil {
		return err
	}
	maxGen := opts.MaxGen
	if maxGen <= 0 {
		maxGen = 20
	}
	readerName := ped.Reader
	if p := g.Persons[ped.Reader]; p != nil && p.Name != "" {
		readerName = p.Name
	}
	pl := Payload{
		Root: ped.Root, Reader: ped.Reader, ReaderName: readerName,
		Generated: time.Now().Format("2 January 2006"), MaxGen: maxGen,
		Generations: ped.Generations, Dangling: ped.Dangling, Truncated: ped.Truncated,
		DashboardURL: opts.DashboardURL, Site: siteText{Title: opts.Site.Title, Eyebrow: opts.Site.Eyebrow},
		Nodes: ped.Nodes, Siblings: ped.Siblings, People: ped.People, Records: recs,
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
