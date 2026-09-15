package naairs

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/model"
)

// Candidate is one index hit scored against a person.
type Candidate struct {
	Record
	Score int      `json:"score"`
	Why   []string `json:"why"`
}

// SweepResult holds the scored hits for one person.
type SweepResult struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Birth string      `json:"birth,omitempty"`
	Death string      `json:"death,omitempty"`
	Query []string    `json:"query"`
	Total int         `json:"total"` // documents the index reported
	Hits  []Candidate `json:"hits"`
	Raw   []Record    `json:"raw,omitempty"` // every fetched record, so scoring can be redone offline
	Err   string      `json:"err,omitempty"`
}

// Terms builds the AND-ed keywords for a person: every surname token plus the
// first given name. It returns nil when either half is missing.
func Terms(p *model.Person) []string {
	surname := strings.ToUpper(strings.TrimSpace(p.Surname))
	given := strings.ToUpper(strings.TrimSpace(p.Given))
	if surname == "" || given == "" {
		name := strings.ToUpper(strings.TrimSpace(strings.Split(p.Name, "(")[0]))
		f := strings.Fields(name)
		if len(f) < 2 {
			return nil
		}
		if surname == "" {
			surname = f[len(f)-1]
		}
		if given == "" {
			given = f[0]
		}
	}
	terms := strings.Fields(surname)
	return append(terms, strings.Fields(given)[0])
}

func yearOf(s string) int {
	y := model.Year(s)
	if y == "" {
		return 0
	}
	n, _ := strconv.Atoi(y)
	return n
}

// Score rates how well r fits p. spouseSurnames are upper-case surnames of
// p's spouses. A score below 2 is noise.
func Score(p *model.Person, r Record, spouseSurnames []string) (int, []string) {
	words := tokens(r.Description + " " + r.Remarks)
	has := func(w string) bool { return matchWord(words, w) }
	terms := Terms(p)
	if terms == nil {
		return 0, nil
	}
	given := terms[len(terms)-1]
	maiden := true
	for _, t := range terms[:len(terms)-1] {
		if !has(t) {
			maiden = false
		}
	}
	married := ""
	for _, s := range spouseSurnames {
		if has(s) {
			married = s
			break
		}
	}
	if !has(given) || (!maiden && married == "") {
		return 0, nil
	}
	// "BORN X" / "NEE X" / "GEBORE X" naming another maiden surname means another woman
	if born := bornSurname(r.Description + " " + r.Remarks); born != "" && !matchWord([]string{born}, terms[len(terms)-2]) && !maiden {
		return 0, nil
	}
	score, why := 1, []string{"name"}
	if maiden && married != "" {
		score += 2
		why = append(why, "maiden and married name "+strings.ToLower(married))
	} else if married != "" {
		why = append(why, "married name "+strings.ToLower(married))
	}
	givens := strings.Fields(strings.ToUpper(p.Given))
	extra := 0
	for _, gn := range givens[min(1, len(givens)):] {
		if has(gn) && extra < 2 {
			extra++
		}
	}
	if extra > 0 {
		score += extra
		why = append(why, "full given names")
	}
	st := yearOf(r.Starting)
	birth, death := yearOf(p.Birth), yearOf(p.Death)
	yearHit, ownName := false, false
	switch {
	case st == 0:
	case death != 0 && abs(st-death) <= 2:
		score += 2
		yearHit = true
		why = append(why, "year of death")
	case death != 0 && abs(st-death) <= 10:
		score++
		why = append(why, "within ten years of death")
	case birth != 0 && st < birth-1:
		score -= 3
		why = append(why, "dated before birth")
	}
	if r.Source == "MHG" || r.Source == "MOOC" {
		score++
		why = append(why, "estate file")
		head := tokens(strings.SplitN(r.Description, ".", 2)[0])
		if len(head) > 0 && matchWord(head, given) && (matchWord(head, terms[len(terms)-2]) || (married != "" && matchWord(head, married))) {
			score++
			ownName = true
			why = append(why, "estate in this name")
		}
	}
	// a married-name match alone is weak: another person's file that mentions
	// the wife, or a namesake, unless the file is in her name or dated at her death
	if !maiden && !ownName && !yearHit {
		score = min(score, 1)
	}
	return score, why
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SweepPerson queries db for one person and returns the scored hits, best first.
func (c *Client) SweepPerson(ctx context.Context, db string, g *model.Graph, p *model.Person) SweepResult {
	res := SweepResult{ID: p.ID, Name: p.Name, Birth: p.Birth, Death: p.Death, Query: Terms(p)}
	if res.Query == nil {
		res.Err = "no usable name"
		return res
	}
	from := ""
	if y := yearOf(p.Birth); y > 0 {
		from = strconv.Itoa(y)
	}
	var spouses []string
	for _, s := range p.Spouses {
		if q := g.Persons[g.Resolve(s)]; q != nil && q.Surname != "" {
			spouses = append(spouses, strings.ToUpper(q.Surname))
		}
	}
	// query under the own surname, then under each different spouse surname
	// (women appear in the index under their married names)
	queries := [][]string{res.Query}
	given := res.Query[len(res.Query)-1]
	for _, s := range model.Uniq(spouses) {
		if loose(s) != loose(strings.Join(res.Query[:len(res.Query)-1], " ")) {
			queries = append(queries, append(strings.Fields(s), given))
		}
	}
	seen := map[string]bool{}
	var recs []Record
	for i, q := range queries {
		if i > 0 {
			time.Sleep(time.Second)
		}
		rs, n, err := c.Query(ctx, db, q, from, "")
		res.Total += n
		if err != nil {
			res.Err = err.Error()
			return res
		}
		for _, r := range rs {
			k := r.Depot + "|" + r.Source + "|" + r.Volume + "|" + r.Reference
			if !seen[k] {
				seen[k] = true
				recs = append(recs, r)
			}
		}
	}
	res.Raw = recs
	for _, r := range recs {
		if sc, why := Score(p, r, spouses); sc >= 2 {
			res.Hits = append(res.Hits, Candidate{Record: r, Score: sc, Why: why})
		}
	}
	sort.SliceStable(res.Hits, func(i, j int) bool { return res.Hits[i].Score > res.Hits[j].Score })
	if len(res.Hits) > 15 {
		res.Hits = res.Hits[:15]
	}
	return res
}

// Sweep runs SweepPerson for each id, pausing delay between queries and
// calling each after every person (for progress and incremental saving).
func (c *Client) Sweep(ctx context.Context, db string, g *model.Graph, ids []string, delay time.Duration, each func(SweepResult)) {
	for i, id := range ids {
		p := g.Persons[id]
		if p == nil {
			continue
		}
		if i > 0 && delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		each(c.SweepPerson(ctx, db, g, p))
	}
}

// tokens splits text into upper-case alphabetic words.
func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool { return r < 'A' || r > 'Z' })
}

// loose reduces a name to a spelling-insensitive key: PH/F, C/K, Z/S, IJ/Y and
// TH/T are merged and doubled letters collapsed, so PHILLIPUS matches
// PHILIPPUS and REYNECKE matches REYNEKE.
func loose(w string) string {
	w = strings.ToUpper(w)
	for _, r := range [][2]string{{"PH", "F"}, {"IJ", "Y"}, {"TH", "T"}, {"CK", "K"}, {"C", "K"}, {"Z", "S"}, {"DT", "T"}} {
		w = strings.ReplaceAll(w, r[0], r[1])
	}
	var b strings.Builder
	var prev rune
	for _, r := range w {
		if r < 'A' || r > 'Z' || r == prev {
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// lev1 reports whether a and b are within one edit of each other.
func lev1(a, b string) bool {
	if a == b {
		return true
	}
	if abs(len(a)-len(b)) > 1 {
		return false
	}
	i, j, edits := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		if edits++; edits > 1 {
			return false
		}
		switch {
		case len(a) > len(b):
			i++
		case len(b) > len(a):
			j++
		default:
			i++
			j++
		}
	}
	return edits+(len(a)-i)+(len(b)-j) <= 1
}

// matchWord reports whether any token matches term, allowing the loose
// spelling key and one edit for longer names.
func matchWord(words []string, term string) bool {
	lt := loose(term)
	if lt == "" {
		return false
	}
	for _, w := range words {
		lw := loose(w)
		if lw == lt || (len(lt) >= 6 && lev1(lw, lt)) {
			return true
		}
	}
	return false
}

// bornSurname returns the maiden surname a description gives after BORN, NEE,
// GEBORE or NOOIENSVAN, or "" when there is none.
func bornSurname(text string) string {
	w := tokens(text)
	for i, t := range w {
		switch t {
		case "BORN", "NEE", "GEBORE", "NOOIENSVAN", "GEB":
			if i+1 < len(w) {
				return w[i+1]
			}
		}
	}
	return ""
}
