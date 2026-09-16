// Package model holds the shared person/graph types every source writes into.
package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Person is one node in the kinship graph. IDs are namespaced by source:
// seed: (hand-entered), fs: (hand-entered from FamilySearch), wd:Q… (Wikidata),
// wt:<WikiTree-Id> (WikiTree), eggsa:<hash> (eGGSA gravestones).
// A new field must be added to Merge and classified in Redact.
type Person struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Given       string   `json:"given,omitempty"`
	Surname     string   `json:"surname,omitempty"`
	Gender      string   `json:"gender,omitempty"`
	Birth       string   `json:"birth,omitempty"`
	Death       string   `json:"death,omitempty"`
	BirthPlace  string   `json:"birthPlace,omitempty"`
	DeathPlace  string   `json:"deathPlace,omitempty"`
	Occupations []string `json:"occupations,omitempty"`
	Citizenship []string `json:"citizenship,omitempty"`
	Description string   `json:"description,omitempty"`
	Wikidata    string   `json:"wikidata,omitempty"`
	WikiTree    string   `json:"wikitree,omitempty"`
	URL         string   `json:"url,omitempty"`
	Father      string   `json:"father,omitempty"`
	Mother      string   `json:"mother,omitempty"`
	Spouses     []string `json:"spouses,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	Living      bool     `json:"living,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// Graph is a set of persons keyed by ID. Parent links live on the child.
type Graph struct {
	Persons map[string]*Person `json:"persons"`
	Aliases map[string]string  `json:"aliases,omitempty"` // merged id -> surviving id
}

func NewGraph() *Graph { return &Graph{Persons: map[string]*Person{}, Aliases: map[string]string{}} }

// Resolve follows merge aliases to the surviving id.
func (g *Graph) Resolve(id string) string {
	for i := 0; i < 8; i++ {
		t, ok := g.Aliases[id]
		if !ok {
			return id
		}
		id = t
	}
	return id
}

// Add inserts p or merges it into an existing person with the same ID.
func (g *Graph) Add(p *Person) *Person {
	if p.ID == "" {
		return p
	}
	if e, ok := g.Persons[p.ID]; ok {
		e.Merge(p)
		return e
	}
	g.Persons[p.ID] = p
	return p
}

// Merge fills empty fields of p from o and unions list fields.
func (p *Person) Merge(o *Person) {
	fill := func(dst *string, src string) {
		if (*dst == "" || strings.EqualFold(*dst, "unknown")) && src != "" && !strings.EqualFold(src, "unknown") {
			*dst = src
		}
	}
	fill(&p.Name, o.Name)
	fill(&p.Given, o.Given)
	fill(&p.Surname, o.Surname)
	fill(&p.Gender, o.Gender)
	fill(&p.Birth, o.Birth)
	fill(&p.Death, o.Death)
	fill(&p.BirthPlace, o.BirthPlace)
	fill(&p.DeathPlace, o.DeathPlace)
	fill(&p.Description, o.Description)
	fill(&p.Wikidata, o.Wikidata)
	fill(&p.WikiTree, o.WikiTree)
	fill(&p.URL, o.URL)
	fillRef(&p.Father, o.Father)
	fillRef(&p.Mother, o.Mother)
	fill(&p.Note, o.Note)
	p.Occupations = Uniq(append(p.Occupations, o.Occupations...))
	p.Citizenship = Uniq(append(p.Citizenship, o.Citizenship...))
	p.Spouses = Uniq(append(p.Spouses, o.Spouses...))
	p.Sources = Uniq(append(p.Sources, o.Sources...))
	p.Living = p.Living || o.Living
}

// Redact returns a copy of p that keeps only identity and family structure:
// names, gender, parent and spouse links, source tags, WikiTree and Wikidata
// ids and the living flag. Dates, places, occupations, notes and URLs are
// dropped. Built as an allow-list so that a field added later is dropped
// until it is classified here.
func (p *Person) Redact() *Person {
	return &Person{
		ID:       p.ID,
		Name:     p.Name,
		Given:    p.Given,
		Surname:  p.Surname,
		Gender:   p.Gender,
		Wikidata: p.Wikidata,
		WikiTree: p.WikiTree,
		Father:   p.Father,
		Mother:   p.Mother,
		Spouses:  append([]string(nil), p.Spouses...),
		Sources:  append([]string(nil), p.Sources...),
		Living:   p.Living,
	}
}

// Redacted returns a copy of g in which every living person is reduced to
// Redact, and the number of persons so reduced. Other persons and the
// aliases are copied unchanged; g is not modified.
func (g *Graph) Redacted() (*Graph, int) {
	out := NewGraph()
	n := 0
	for id, p := range g.Persons {
		if p.Living {
			out.Persons[id] = p.Redact()
			n++
			continue
		}
		c := *p
		c.Occupations = append([]string(nil), p.Occupations...)
		c.Citizenship = append([]string(nil), p.Citizenship...)
		c.Spouses = append([]string(nil), p.Spouses...)
		c.Sources = append([]string(nil), p.Sources...)
		out.Persons[id] = &c
	}
	for k, v := range g.Aliases {
		out.Aliases[k] = v
	}
	return out, n
}

// fillRef fills a parent reference, letting a resolved id replace an
// unresolved placeholder such as "wt:id:12345".
func fillRef(dst *string, src string) {
	if src == "" {
		return
	}
	if *dst == "" || (IsPlaceholder(*dst) && !IsPlaceholder(src)) {
		*dst = src
	}
}

// IsPlaceholder reports whether id is an unresolved numeric WikiTree reference.
func IsPlaceholder(id string) bool { return strings.HasPrefix(id, "wt:id:") }

// Uniq returns s without duplicates or empties, preserving order.
func Uniq(s []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(s))
	for _, v := range s {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Sorted returns persons ordered by surname, name, birth.
func (g *Graph) Sorted() []*Person {
	out := make([]*Person, 0, len(g.Persons))
	for _, p := range g.Persons {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Surname != out[j].Surname {
			return out[i].Surname < out[j].Surname
		}
		if out[i].Birth != out[j].Birth {
			return out[i].Birth < out[j].Birth
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func Load(path string) (*Graph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g := NewGraph()
	if err := json.Unmarshal(b, g); err != nil {
		return nil, err
	}
	if g.Persons == nil {
		g.Persons = map[string]*Person{}
	}
	if g.Aliases == nil {
		g.Aliases = map[string]string{}
	}
	for id, p := range g.Persons {
		if p.ID == "" {
			p.ID = id
		}
	}
	return g, nil
}

// Save writes g as indented JSON, creating the parent directory if needed.
func (g *Graph) Save(path string) error {
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, b, 0o644)
}

// Year extracts a 4-digit year from a date string like 1979-09-04, 1979, 1970s.
func Year(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		y := s[:4]
		for _, c := range y {
			if c < '0' || c > '9' {
				return ""
			}
		}
		if y == "0000" {
			return ""
		}
		return y
	}
	return ""
}
