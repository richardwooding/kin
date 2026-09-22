package riksarkivet

import (
	"context"
	"sort"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
	"github.com/richardwooding/kin/internal/place"
)

// Candidate is one register entry scored against a person.
type Candidate struct {
	Record
	Score int      `json:"score"`
	Why   []string `json:"why"`
}

// SweepResult holds the scored entries for one person.
type SweepResult struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Birth  string      `json:"birth,omitempty"`
	Death  string      `json:"death,omitempty"`
	Places string      `json:"places,omitempty"`
	Query  []string    `json:"query"`
	Total  int         `json:"total"`
	Capped bool        `json:"capped,omitempty"`
	Hits   []Candidate `json:"hits"`
	Err    string      `json:"err,omitempty"`
}

// SweepOptions select the people to sweep.
type SweepOptions struct {
	Root        string
	ProbableIDs []string
	MaxGen      int // 0 means 20
	Min         int // keep hits scoring at least this; Keep by default
	Log         func(string, ...any)
}

const (
	maxHits = 15
	details = 5 // full entries fetched per person, for the archive reference
)

// People lists the Swedish ancestors.
func People(g *model.Graph, opts SweepOptions) []string {
	return place.Ancestors(g, opts.Root, opts.ProbableIDs, opts.MaxGen, func(r place.Region) bool { return r&place.Sweden != 0 })
}

// Plan lists the queries for one person: their baptism by their own name and
// by their father's, within two years of their birth, and their marriage
// between fifteen and sixty years after it, or in the sixty years before
// their death when the birth is not known.
func Plan(g *model.Graph, p *model.Person) []Query {
	surname, givens := namematch.Names(p)
	if surname == "" || len(givens) == 0 {
		return nil
	}
	name := givens[0] + " " + surname
	by, dy := year(p.Birth), year(p.Death)
	var qs []Query
	if by > 0 {
		qs = append(qs, Query{Name: name, Type: Birth, YearMin: by - 2, YearMax: by + 2})
		if f := g.Persons[g.Resolve(p.Father)]; p.Father != "" && f != nil {
			if fs, fg := namematch.Names(f); fs != "" && len(fg) > 0 && fg[0]+" "+fs != name {
				qs = append(qs, Query{Name: fg[0] + " " + fs, Type: Birth, YearMin: by - 2, YearMax: by + 2})
			}
		}
	}
	switch {
	case by > 0:
		qs = append(qs, Query{Name: name, Type: Marriage, YearMin: by + 15, YearMax: by + 60})
	case dy > 0:
		qs = append(qs, Query{Name: name, Type: Marriage, YearMin: dy - 60, YearMax: dy})
	}
	return qs
}

// FamilyOf gathers the parents and spouses the graph gives p.
func FamilyOf(g *model.Graph, p *model.Person, places string) Family {
	f := Family{Places: places}
	if p.Father != "" {
		f.Father = g.Persons[g.Resolve(p.Father)]
	}
	if p.Mother != "" {
		f.Mother = g.Persons[g.Resolve(p.Mother)]
	}
	for _, s := range p.Spouses {
		if sp := g.Persons[g.Resolve(s)]; sp != nil {
			f.Spouses = append(f.Spouses, sp)
		}
	}
	return f
}

// cached is one query's answer, kept so siblings sharing a father cost the
// archive one request between them.
type cached struct {
	recs  []Record
	total int
}

// SweepPerson runs one person's queries, scores the entries, and reads the
// full entry of the best few.
func (c *Client) SweepPerson(ctx context.Context, g *model.Graph, p *model.Person, places string, opts SweepOptions, memo map[string]cached) SweepResult {
	res := SweepResult{ID: p.ID, Name: p.Name, Birth: p.Birth, Death: p.Death, Places: places, Hits: []Candidate{}}
	min := opts.Min
	if min == 0 {
		min = Keep
	}
	fam := FamilyOf(g, p, places)
	best := map[string]Candidate{}
	for _, q := range Plan(g, p) {
		u := q.URL()
		res.Query = append(res.Query, u)
		got, ok := memo[u]
		if !ok {
			recs, total, err := c.Search(ctx, q)
			if err != nil {
				res.Err = err.Error()
				return res
			}
			got = cached{recs: recs, total: total}
			if memo != nil {
				memo[u] = got
			}
		}
		res.Total += got.total
		if got.total > len(got.recs) {
			res.Capped = true
		}
		for _, r := range got.recs {
			if sc, why := Score(p, r, fam); sc >= min && sc > best[r.ID].Score {
				best[r.ID] = Candidate{Record: r, Score: sc, Why: why}
			}
		}
	}
	for _, h := range best {
		res.Hits = append(res.Hits, h)
	}
	sort.Slice(res.Hits, func(i, j int) bool {
		if res.Hits[i].Score != res.Hits[j].Score {
			return res.Hits[i].Score > res.Hits[j].Score
		}
		return res.Hits[i].ID < res.Hits[j].ID
	})
	if len(res.Hits) > maxHits {
		res.Hits = res.Hits[:maxHits]
	}
	for i := range res.Hits {
		if i >= details {
			break
		}
		if err := c.Detail(ctx, &res.Hits[i].Record); err != nil && opts.Log != nil {
			opts.Log("%s: entry %s: %v", p.Name, res.Hits[i].ID, err)
		}
	}
	return res
}

// Sweep runs every person in turn, handing each result to each as it is made.
func (c *Client) Sweep(ctx context.Context, g *model.Graph, ids []string, opts SweepOptions, each func(SweepResult)) {
	kids := place.Kids(g)
	memo := map[string]cached{}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		rid := g.Resolve(id)
		p := g.Persons[rid]
		if p == nil {
			continue
		}
		each(c.SweepPerson(ctx, g, p, place.PlacesOf(g, p, kids[rid]), opts, memo))
	}
}
