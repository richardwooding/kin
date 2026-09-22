package linklives

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/namematch"
	"github.com/richardwooding/kin/internal/place"
)

// Keep is the least score worth showing.
const Keep = 4

// Member is someone who shared a household with a candidate.
type Member struct {
	Name     string `json:"name"`
	Age      string `json:"age,omitempty"`
	Position string `json:"position,omitempty"`
}

// Candidate is one person appearance scored against a person.
type Candidate struct {
	Row
	URL           string   `json:"url"`
	LifeCourse    string   `json:"lifeCourse,omitempty"`
	LifeCourseURL string   `json:"lifeCourseUrl,omitempty"`
	Members       []Member `json:"members,omitempty"` // the rest of the household
	Score         int      `json:"score"`
	Why           []string `json:"why"`
}

// SweepResult holds the scored appearances for one person.
type SweepResult struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Birth  string      `json:"birth,omitempty"`
	Death  string      `json:"death,omitempty"`
	Places string      `json:"places,omitempty"`
	Total  int         `json:"total"` // appearances matching the name and dates, before scoring
	Hits   []Candidate `json:"hits"`
}

// SweepOptions select the people to sweep.
type SweepOptions struct {
	Root        string
	ProbableIDs []string
	MaxGen      int // 0 means 20
	Min         int // keep hits scoring at least this; Keep by default
	Log         func(string, ...any)
}

const maxHits = 15

// People lists the Danish ancestors.
func People(g *model.Graph, opts SweepOptions) []string {
	return place.Ancestors(g, opts.Root, opts.ProbableIDs, opts.MaxGen, func(r place.Region) bool { return r&place.Denmark != 0 })
}

// target is a person being looked for, with what scoring needs of the graph.
type target struct {
	p         *model.Person
	places    string
	surname   string // namematch.Nordic key
	spelt     string
	givens    []string
	by, dy    int
	relatives []relative
}

type relative struct{ role, given string }

// Surnames lists the surnames the sweep looks for, one spelling of each.
func Surnames(g *model.Graph, ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range targets(g, ids) {
		if !seen[t.surname] {
			seen[t.surname] = true
			out = append(out, t.spelt)
		}
	}
	sort.Strings(out)
	return out
}

func targets(g *model.Graph, ids []string) []*target {
	kids := place.Kids(g)
	var out []*target
	for _, id := range ids {
		rid := g.Resolve(id)
		p := g.Persons[rid]
		if p == nil {
			continue
		}
		surname, givens := namematch.Names(p)
		if surname == "" || len(givens) == 0 {
			continue
		}
		t := &target{p: p, places: place.PlacesOf(g, p, kids[rid]), surname: namematch.Nordic(surname),
			spelt: surname, givens: givens, by: year(p.Birth), dy: year(p.Death)}
		add := func(role, id string) {
			if q := g.Persons[g.Resolve(id)]; id != "" && q != nil {
				if _, gs := namematch.Names(q); len(gs) > 0 {
					t.relatives = append(t.relatives, relative{role: role, given: gs[0]})
				}
			}
		}
		add("father", p.Father)
		add("mother", p.Mother)
		for _, s := range p.Spouses {
			add("spouse", s)
		}
		for _, k := range kids[rid] {
			add("child", k.ID)
		}
		out = append(out, t)
	}
	return out
}

// Sweep reads every file under dir for the people in ids and returns their
// results in the same order. Each file is read twice: once for the
// appearances whose surname, first given name and birth year could be the
// person's, and once for the households those appearances lived in.
func Sweep(dir string, g *model.Graph, ids []string, opts SweepOptions) ([]SweepResult, error) {
	min := opts.Min
	if min == 0 {
		min = Keep
	}
	ts := targets(g, ids)
	bySurname := map[string][]*target{}
	for _, t := range ts {
		bySurname[t.surname] = append(bySurname[t.surname], t)
	}
	files, err := Files(dir)
	if err != nil {
		return nil, err
	}
	found := map[*target][]Row{}
	households := map[string][]Row{}
	for _, path := range files {
		if opts.Log != nil {
			opts.Log("linklives: reading %s", filepath.Base(path))
		}
		want := map[string]bool{}
		err := scan(path, func(r Row) {
			seen := map[*target]bool{}
			for _, w := range namematch.Tokens(r.Surnames) {
				for _, t := range bySurname[namematch.Nordic(w)] {
					if !seen[t] && possible(t, r) {
						seen[t] = true
						found[t] = append(found[t], r)
						if k := r.householdKey(); k != "" {
							want[k] = true
						}
					}
				}
			}
		})
		if err != nil {
			return nil, err
		}
		if len(want) == 0 {
			continue
		}
		if err := scan(path, func(r Row) {
			if k := r.householdKey(); want[k] {
				households[k] = append(households[k], r)
			}
		}); err != nil {
			return nil, err
		}
	}

	var out []SweepResult
	wantLC := map[string]bool{}
	for _, t := range ts {
		res := SweepResult{ID: t.p.ID, Name: t.p.Name, Birth: t.p.Birth, Death: t.p.Death, Places: t.places,
			Total: len(found[t]), Hits: []Candidate{}}
		for _, r := range found[t] {
			hh := households[r.householdKey()]
			if sc, why := score(t, r, hh); sc >= min {
				c := Candidate{Row: r, URL: r.URL(), Score: sc, Why: why}
				for _, m := range hh {
					if m.key() != r.key() {
						c.Members = append(c.Members, Member{Name: m.Name, Age: m.Age, Position: m.Position})
					}
				}
				res.Hits = append(res.Hits, c)
			}
		}
		sort.Slice(res.Hits, func(i, j int) bool {
			if res.Hits[i].Score != res.Hits[j].Score {
				return res.Hits[i].Score > res.Hits[j].Score
			}
			return res.Hits[i].EventYear < res.Hits[j].EventYear
		})
		if len(res.Hits) > maxHits {
			res.Hits = res.Hits[:maxHits]
		}
		for _, h := range res.Hits {
			wantLC[h.key()] = true
		}
		out = append(out, res)
	}

	lc, err := lifeCourses(LifeCourses(dir), wantLC)
	if err != nil {
		return out, err
	}
	for i := range out {
		for j := range out[i].Hits {
			h := &out[i].Hits[j]
			if id := lc[h.key()]; id != "" {
				h.LifeCourse = id
				h.LifeCourseURL = "https://link-lives.dk/soeg/life-course/" + Release + "-" + id
			}
		}
	}
	return out, nil
}

// possible is the cheap test made of every row: the first given name agrees,
// and the birth year, where both are known, is within five years.
func possible(t *target, r Row) bool {
	if !namematch.NordicIn(t.givens[0], r.FirstNames+" "+r.Name) {
		return false
	}
	if t.by > 0 && r.BirthYear > 0 && abs(t.by-r.BirthYear) > 5 {
		return false
	}
	if t.by > 0 && r.EventYear > 0 && r.EventYear < t.by {
		return false
	}
	return t.dy == 0 || r.EventYear == 0 || r.EventYear <= t.dy+1
}

// score rates an appearance. Census ages drift, so a birth year within two
// scores. A match on names and dates alone, with neither the parish nor a
// relative in the household to back it, is held below Keep: Jens Jensen was
// born in every parish in Denmark.
func score(t *target, r Row, household []Row) (int, []string) {
	sc, why := 3, []string{"named " + r.Name}
	if len(t.givens) > 1 {
		all := true
		for _, g := range t.givens {
			all = all && namematch.NordicIn(g, r.FirstNames+" "+r.Name)
		}
		if all {
			sc++
			why = append(why, "all given names")
		}
	}
	if t.by > 0 && r.BirthYear > 0 {
		switch d := abs(t.by - r.BirthYear); {
		case d == 0:
			sc += 2
			why = append(why, "born "+strconv.Itoa(r.BirthYear))
		case d <= 2:
			sc++
			why = append(why, "born "+strconv.Itoa(r.BirthYear)+", within two years")
		case d > 3:
			return 0, nil
		}
	}
	if strings.Contains(strings.ToLower(r.EventType), "burial") && t.dy > 0 && abs(t.dy-r.EventYear) <= 1 {
		sc += 2
		why = append(why, "buried "+strconv.Itoa(r.EventYear))
	}

	backed := false
	mine := namematch.PlaceTokens(t.places)
	for _, pl := range []struct{ what, text string }{{"born in", r.BirthPlace}, {"living in", r.EventPlace}} {
		if w := shared(mine, pl.text); w != "" {
			sc += 2
			backed = true
			why = append(why, pl.what+" "+w)
			break
		}
	}
	matched := 0
	for _, rel := range t.relatives {
		if matched == 2 {
			break
		}
		for _, m := range household {
			if m.key() != r.key() && namematch.NordicIn(rel.given, m.FirstNames+" "+m.Name) {
				sc += 2
				matched++
				backed = true
				why = append(why, "household has "+rel.role+" "+rel.given)
				break
			}
		}
	}
	if !backed && sc >= Keep {
		sc = Keep - 1
		why = append(why, "names and dates only")
	}
	return sc, why
}

// shared returns the first distinctive word of text that is one of mine.
func shared(mine []string, text string) string {
	for _, w := range namematch.Tokens(text) {
		for _, m := range mine {
			if w == m {
				return w
			}
		}
	}
	return ""
}

func year(s string) int {
	y, _ := strconv.Atoi(model.Year(s))
	return y
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
