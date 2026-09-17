package tna

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/place"
)

// Candidate is one catalogue entry scored against a person.
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

// SweepOptions select the people to sweep and what to ask about them.
type SweepOptions struct {
	Root        string
	ProbableIDs []string
	MaxGen      int  // 0 means 20
	MaxPages    int  // pages per query, 1 by default
	Min         int  // keep hits scoring at least this; Keep by default
	Frontier    bool // also ask the archives that hold records elsewhere
	Log         func(string, ...any)
}

const maxHits = 15

// People lists the British and Irish ancestors: the ones whose records are in
// these catalogues, dead or born long enough ago to be catalogued at all.
func People(g *model.Graph, opts SweepOptions) []string {
	maxGen := opts.MaxGen
	if maxGen <= 0 {
		maxGen = 20
	}
	root := g.Resolve(opts.Root)
	anc := graph.Ancestors(g, root)
	kids := place.Kids(g)
	gen := map[string]int{}
	for id, d := range anc {
		if id != root && d <= maxGen {
			gen[id] = d
		}
	}
	for _, id := range opts.ProbableIDs {
		id = g.Resolve(id)
		if d, ok := anc[id]; ok && id != root {
			gen[id] = d
		}
	}
	var out []string
	for id := range gen {
		p := g.Persons[id]
		if p == nil || p.Living {
			continue
		}
		if !place.Of(place.PlacesOf(g, p, kids[id])).UK() {
			continue
		}
		if year(p.Death) == 0 && year(p.Birth) >= 1920 {
			continue
		}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		if gen[out[i]] != gen[out[j]] {
			return gen[out[i]] < gen[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// Plan lists the queries for one person: their name across the civil series
// over their lifetime and the sixty years an estate may take to settle, and,
// on the frontier, their surname and parish among the records held elsewhere.
func Plan(p *model.Person, places string, opts SweepOptions) []Query {
	surname, givens := names(p)
	if surname == "" || len(givens) == 0 {
		return nil
	}
	by, dy := year(p.Birth), year(p.Death)
	q := Query{Text: surname + " " + givens[0], Series: Keys(Civil), MaxPages: opts.MaxPages}
	if by > 0 {
		q.DateFrom = fmt.Sprintf("%d-01-01", by-1)
	}
	switch {
	case dy > 0:
		q.DateTo = fmt.Sprintf("%d-12-31", dy+60)
	case by > 0:
		q.DateTo = fmt.Sprintf("%d-12-31", by+110)
	}
	qs := []Query{q}
	if opts.Frontier && (p.Father == "" || p.Mother == "") {
		if parish := place.Parish(firstPlace(p, places)); parish != "" {
			qs = append(qs, Query{Text: surname + " " + parish, HeldBy: HeldElsewhere, MaxPages: opts.MaxPages})
		}
	}
	return qs
}

// firstPlace is the person's own birthplace, else whatever place is known.
func firstPlace(p *model.Person, places string) string {
	if p.BirthPlace != "" {
		return p.BirthPlace
	}
	if p.DeathPlace != "" {
		return p.DeathPlace
	}
	return strings.TrimSpace(strings.Split(places, "|")[0])
}

// SweepPerson runs one person's queries and scores the entries.
func (c *Client) SweepPerson(ctx context.Context, p *model.Person, places string, opts SweepOptions, memo map[string]cached) SweepResult {
	res := SweepResult{ID: p.ID, Name: p.Name, Birth: p.Birth, Death: p.Death, Places: places, Hits: []Candidate{}}
	min := opts.Min
	if min == 0 {
		min = Keep
	}
	best := map[string]Candidate{}
	for i, q := range Plan(p, places, opts) {
		u := q.URL(1)
		res.Query = append(res.Query, u)
		got, ok := memo[u]
		if !ok {
			recs, total, err := c.SearchQuery(ctx, q)
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
		o := ScoreOpts{Places: places, Common: got.total > 40, Frontier: i > 0}
		for _, r := range got.recs {
			// a record found by two queries is scored under each and kept once,
			// under whichever search judged it the stronger lead
			if sc, why := ScorePerson(p, r, o); sc >= min && sc > best[r.ID].Score {
				best[r.ID] = Candidate{Record: r, Score: sc, Why: why}
			}
		}
	}
	for _, c := range best {
		res.Hits = append(res.Hits, c)
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
	return res
}

// cached is one query's answer, kept so a family who share a surname and a
// parish cost the catalogue one request between them.
type cached struct {
	recs  []Record
	total int
}

// Sweep runs every person in turn, handing each result to each as it is made.
func (c *Client) Sweep(ctx context.Context, g *model.Graph, ids []string, opts SweepOptions, each func(SweepResult)) {
	kids := place.Kids(g)
	memo := map[string]cached{}
	for _, id := range ids {
		rid := g.Resolve(id)
		p := g.Persons[rid]
		if p == nil {
			continue
		}
		res := c.SweepPerson(ctx, p, place.PlacesOf(g, p, kids[rid]), opts, memo)
		if opts.Log != nil {
			if res.Err != "" {
				opts.Log("%s: error %s", p.Name, res.Err)
			} else {
				opts.Log("%s: %d records, %d candidates", p.Name, res.Total, len(res.Hits))
			}
		}
		each(res)
	}
}
