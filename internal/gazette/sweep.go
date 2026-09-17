package gazette

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/place"
)

// Candidate is one notice scored against a person.
type Candidate struct {
	Notice
	Score int      `json:"score"`
	Why   []string `json:"why"`
}

// SweepResult holds the scored notices for one person.
type SweepResult struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Birth  string      `json:"birth,omitempty"`
	Death  string      `json:"death,omitempty"`
	Places string      `json:"places,omitempty"`
	Query  []string    `json:"query"` // the feed addresses asked, in order
	Total  int         `json:"total"` // notices the feed reported
	Capped bool        `json:"capped,omitempty"`
	Hits   []Candidate `json:"hits"`
	Raw    []Notice    `json:"raw,omitempty"` // fetched notices, so scoring can be redone offline
	Err    string      `json:"err,omitempty"`
}

// Options select the people to sweep and how politely to do it.
type Options struct {
	Root        string
	ProbableIDs []string
	MaxGen      int           // 0 means 20
	MaxPages    int           // pages per query, 2 by default
	ProbateFrom int           // no date-of-death query for deaths before this year; 1990 by default
	Min         int           // keep hits scoring at least this; Keep by default
	Delay       time.Duration // unused here; the client holds the pause
	Log         func(string, ...any)
}

const (
	maxHits = 15
	rawText = 300 // runes of a scanned page kept per notice
)

// People lists the ancestors worth asking about: the British and Irish ones
// who are dead or born long enough ago that their notices are published.
func People(g *model.Graph, opts Options) []string {
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
		by, dy := year(p.Birth), year(p.Death)
		if dy == 0 && by >= 1920 {
			continue // possibly still living, and modern notices name the living
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

// Plan lists the queries for one person: their name as a phrase, the same
// name in the notices' own order, and for a modern death the structured
// deceased estates notices by date of death.
func Plan(p *model.Person, places string, opts Options) []Query {
	surname, givens := names(p)
	if surname == "" || len(givens) == 0 {
		return nil
	}
	first := givens[0]
	by, dy := year(p.Birth), year(p.Death)
	from, to := by+16, dy+3
	if by == 0 {
		from = 0
	}
	switch {
	case dy == 0 && by > 0:
		to = by + 100
	case dy == 0:
		to = 0
	}
	edition := place.Of(places).Edition()
	qs := []Query{
		{Service: All, Text: fmt.Sprintf("%q", first+" "+surname), StartPublish: date(from, false), EndPublish: date(to, true), Edition: edition},
		{Service: All, Text: fmt.Sprintf("%q", surname+", "+first), StartPublish: date(from, false), EndPublish: date(to, true), Edition: edition},
	}
	probateFrom := opts.ProbateFrom
	if probateFrom == 0 {
		probateFrom = 1990
	}
	if dy >= probateFrom {
		qs = append(qs, Query{Service: Probate, Text: fmt.Sprintf("%q", surname),
			StartDeath: date(dy-1, false), EndDeath: date(dy+2, true)})
	}
	return qs
}

// SweepPerson runs one person's queries and scores what comes back.
func (c *Client) SweepPerson(ctx context.Context, p *model.Person, places string, opts Options) SweepResult {
	res := SweepResult{ID: p.ID, Name: p.Name, Birth: p.Birth, Death: p.Death, Places: places, Hits: []Candidate{}}
	min := opts.Min
	if min == 0 {
		min = Keep
	}
	pages := opts.MaxPages
	if pages <= 0 {
		pages = 2
	}
	seen := map[string]bool{}
	var notices []Notice
	for _, q := range Plan(p, places, opts) {
		res.Query = append(res.Query, q.URL())
		found, total, err := c.Search(ctx, q, pages)
		if err != nil {
			res.Err = err.Error()
			return res
		}
		res.Total += total
		if total > len(found) {
			res.Capped = true
		}
		common := total > 40
		for _, n := range found {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			if sc, why := Score(p, places, n, common); sc >= min {
				res.Hits = append(res.Hits, Candidate{Notice: truncate(n), Score: sc, Why: why})
			}
			notices = append(notices, truncate(n))
		}
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
	res.Raw = notices
	return res
}

// truncate shortens a scanned page's text; a whole page of print would make
// the result file unreadable and enormous.
func truncate(n Notice) Notice {
	r := []rune(n.Text)
	if len(r) > rawText {
		n.Text = string(r[:rawText]) + "…"
	}
	return n
}

// Sweep runs every person in turn, handing each result to each as it is made
// so the caller can save after every person.
func (c *Client) Sweep(ctx context.Context, g *model.Graph, ids []string, opts Options, each func(SweepResult)) {
	kids := place.Kids(g)
	for _, id := range ids {
		p := g.Persons[g.Resolve(id)]
		if p == nil {
			continue
		}
		res := c.SweepPerson(ctx, p, place.PlacesOf(g, p, kids[g.Resolve(id)]), opts)
		if opts.Log != nil {
			if res.Err != "" {
				opts.Log("%s: error %s", p.Name, res.Err)
			} else {
				opts.Log("%s: %d notices, %d candidates", p.Name, res.Total, len(res.Hits))
			}
		}
		each(res)
	}
}
