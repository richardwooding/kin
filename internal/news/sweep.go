package news

import (
	"context"
	"sort"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
	"github.com/richardwooding/kin/internal/place"
)

// Candidate is one hit scored against a person.
type Candidate struct {
	Hit
	Score int      `json:"score"`
	Why   []string `json:"why"`
}

// SweepResult holds the scored hits for one person.
type SweepResult struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Birth  string      `json:"birth,omitempty"`
	Death  string      `json:"death,omitempty"`
	Places string      `json:"places,omitempty"`
	Query  []string    `json:"query"`
	Total  int         `json:"total"`
	Hits   []Candidate `json:"hits"`
	Err    string      `json:"err,omitempty"`
}

// SweepOptions select the people to sweep.
type SweepOptions struct {
	Root        string
	ProbableIDs []string
	MaxGen      int // 0 means 20
	Min         int // keep hits scoring at least this; Keep by default
	Max         int // hits read per query; 50 when zero
}

const maxHits = 15

// People lists the ancestors in a region one of the sources covers.
func People(g *model.Graph, srcs []Source, opts SweepOptions) []string {
	return place.Ancestors(g, opts.Root, opts.ProbableIDs, opts.MaxGen, func(r place.Region) bool {
		for _, s := range srcs {
			if r != 0 && s.Covers(r) {
				return true
			}
		}
		return false
	})
}

// Plan lists the queries each source gets for p: their name as a phrase
// from their sixteenth year to two years after death, or to their hundredth
// year when the death is not known, and again from the year of death to two
// years after, since the archives return the oldest hits first and a death
// notice is the likeliest find. Each is clipped to the years the source may
// show; a source whose region p is not in gets none.
func Plan(p *model.Person, places string, src Source, max int) []Query {
	surname, givens := namematch.Names(p)
	if surname == "" || len(givens) == 0 || !src.Covers(place.Of(places)) {
		return nil
	}
	phrase := givens[0] + " " + surname
	by, dy := year(p.Birth), year(p.Death)
	var spans [][2]int
	switch {
	case by > 0 && dy > 0:
		spans = [][2]int{{by + 16, dy + 2}, {dy, dy + 2}}
	case by > 0:
		spans = [][2]int{{by + 16, by + 100}}
	case dy > 0:
		spans = [][2]int{{dy - 60, dy + 2}, {dy, dy + 2}}
	}
	var qs []Query
	for _, sp := range spans {
		if from, to, ok := src.Window(sp[0], sp[1]); ok {
			qs = append(qs, Query{Phrase: phrase, From: from, To: to, Max: max})
		}
	}
	return qs
}

// FamilyOf gathers the given names of p's parents, spouses and children.
func FamilyOf(g *model.Graph, p *model.Person, places string, kids []*model.Person) Family {
	f := Family{Places: places, Region: place.Of(places)}
	add := func(role, id string) {
		if q := g.Persons[g.Resolve(id)]; id != "" && q != nil {
			if sn, gs := namematch.Names(q); len(gs) > 0 {
				f.Relatives = append(f.Relatives, Relative{Role: role, Given: gs[0], Surname: sn})
			}
		}
	}
	for _, s := range p.Spouses {
		add("spouse", s)
	}
	add("father", p.Father)
	add("mother", p.Mother)
	for _, k := range kids {
		add("child", k.ID)
	}
	return f
}

type cached struct {
	hits  []Hit
	total int
}

// SweepPerson runs p's query against each source and scores the hits.
func SweepPerson(ctx context.Context, g *model.Graph, p *model.Person, places string, kids []*model.Person, srcs []Source, opts SweepOptions, memo map[string]cached) SweepResult {
	res := SweepResult{ID: p.ID, Name: p.Name, Birth: p.Birth, Death: p.Death, Places: places, Hits: []Candidate{}}
	min := opts.Min
	if min == 0 {
		min = Keep
	}
	fam := FamilyOf(g, p, places, kids)
	seen := map[string]bool{}
	for _, src := range srcs {
		for _, q := range Plan(p, places, src, opts.Max) {
			res.Err = sweepQuery(ctx, p, src, q, fam, min, memo, seen, &res)
			if res.Err != "" {
				return res
			}
		}
	}
	sort.SliceStable(res.Hits, func(i, j int) bool {
		if res.Hits[i].Score != res.Hits[j].Score {
			return res.Hits[i].Score > res.Hits[j].Score
		}
		return res.Hits[i].Date < res.Hits[j].Date
	})
	if len(res.Hits) > maxHits {
		res.Hits = res.Hits[:maxHits]
	}
	return res
}

// sweepQuery runs one query, scoring its hits into res, and returns an error
// text if it failed. A page two queries both find is kept once.
func sweepQuery(ctx context.Context, p *model.Person, src Source, q Query, fam Family, min int, memo map[string]cached, seen map[string]bool, res *SweepResult) string {
	u := src.URL(q)
	res.Query = append(res.Query, u)
	got, ok := memo[u]
	if !ok {
		hits, total, err := src.Search(ctx, q)
		if err != nil {
			return err.Error()
		}
		got = cached{hits, total}
		if memo != nil {
			memo[u] = got
		}
	}
	res.Total += got.total
	for _, h := range got.hits {
		if seen[h.Source+h.ID] {
			continue
		}
		seen[h.Source+h.ID] = true
		if sc, why := Score(p, h, fam, src.Context()); sc >= min {
			res.Hits = append(res.Hits, Candidate{Hit: h, Score: sc, Why: why})
		}
	}
	return ""
}

// Sweep runs every person in turn, handing each result to each as it is made.
func Sweep(ctx context.Context, g *model.Graph, ids []string, srcs []Source, opts SweepOptions, each func(SweepResult)) {
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
		each(SweepPerson(ctx, g, p, place.PlacesOf(g, p, kids[rid]), kids[rid], srcs, opts, memo))
	}
}
