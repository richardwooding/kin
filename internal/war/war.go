// Package war looks for military service records of the men in a tree:
// imperial records at the UK National Archives and, for the Boer side, the
// South African archives index for the war years.
package war

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/naairs"
	"github.com/richardwooding/kin/internal/tna"
)

// Man is a person of military age with the years that make him eligible.
type Man struct {
	*model.Person
	Role  string // "ancestor" or "child of ancestor"
	Birth int
	Death int
	Wars  []string
}

// Hit is a scored record for one man.
type Hit struct {
	Source      string   `json:"source"` // "tna" or "naairs"
	Reference   string   `json:"reference"`
	Description string   `json:"description"`
	Dates       string   `json:"dates,omitempty"`
	URL         string   `json:"url,omitempty"`
	Score       int      `json:"score"`
	Why         []string `json:"why"`
}

// Result is the outcome for one man.
type Result struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Role  string   `json:"role"`
	Birth string   `json:"birth,omitempty"`
	Death string   `json:"death,omitempty"`
	Wars  []string `json:"wars"`
	Hits  []Hit    `json:"hits"`
	Err   string   `json:"err,omitempty"`
}

func yearOf(s string) int {
	y := model.Year(s)
	if y == "" {
		return 0
	}
	n, _ := strconv.Atoi(y)
	return n
}

// Men returns the men of the root's ancestry (ancestors and their sons) who
// could have served: born between minBirth and maxBirth, or of unknown birth
// but dead between 1899 and 1950. Living people are skipped.
func Men(g *model.Graph, root string, minBirth, maxBirth int) []Man {
	anc := graph.Ancestors(g, g.Resolve(root))
	ids := map[string]string{}
	for id := range anc {
		ids[id] = "ancestor"
	}
	for id, p := range g.Persons {
		for _, par := range []string{p.Father, p.Mother} {
			if par != "" {
				if _, ok := anc[g.Resolve(par)]; ok {
					if _, seen := ids[id]; !seen {
						ids[id] = "child of ancestor"
					}
				}
			}
		}
	}
	var out []Man
	for id, role := range ids {
		p := g.Persons[id]
		if p == nil || p.Gender != "male" || p.Living {
			continue
		}
		b, d := yearOf(p.Birth), yearOf(p.Death)
		if !((b >= minBirth && b <= maxBirth) || (b == 0 && d >= 1899 && d <= 1950)) {
			continue
		}
		m := Man{Person: p, Role: role, Birth: b, Death: d}
		alive := func(from, to int) bool { return (b == 0 || b <= to-17) && (d == 0 || d >= from) }
		if alive(1899, 1902) && (b == 0 || b >= 1845) {
			m.Wars = append(m.Wars, "Anglo-Boer War 1899-1902")
		}
		if alive(1914, 1918) && (b == 0 || b >= 1865) {
			m.Wars = append(m.Wars, "First World War 1914-1918")
		}
		if alive(1939, 1945) && (b == 0 || b >= 1890) {
			m.Wars = append(m.Wars, "Second World War 1939-1945")
		}
		if len(m.Wars) > 0 {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Birth != out[j].Birth {
			return out[i].Birth < out[j].Birth
		}
		return out[i].ID < out[j].ID
	})
	return out
}

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

var wordRe = regexp.MustCompile(`[A-Za-z]+`)
var yearRe = regexp.MustCompile(`\b(18[4-9]\d|19[0-2]\d)\b`)

// ScoreTNA rates a Discovery record against a man. The surname must appear;
// then the given names, initials, a birth year and a birthplace add weight.
func ScoreTNA(m Man, r tna.Record, common bool) (int, []string) {
	words := wordRe.FindAllString(r.Description, -1)
	up := make([]string, len(words))
	for i, w := range words {
		up[i] = loose(w)
	}
	has := func(w string) bool {
		lw := loose(w)
		for _, u := range up {
			if u == lw {
				return true
			}
		}
		return false
	}
	surname := loose(m.Surname)
	if surname == "" {
		f := strings.Fields(m.Name)
		surname = loose(f[len(f)-1])
	}
	if !has(surname) {
		return 0, nil
	}
	givens := strings.Fields(m.Given)
	if len(givens) == 0 {
		f := strings.Fields(strings.Split(m.Name, "(")[0])
		if len(f) > 1 {
			givens = f[:len(f)-1]
		}
	}
	if len(givens) == 0 {
		return 0, nil
	}
	score, why := 0, []string{}
	if has(givens[0]) {
		score += 2
		why = append(why, "first name")
		extra := 0
		for _, gn := range givens[1:] {
			if has(gn) {
				extra++
			}
		}
		if extra > 0 {
			score++
			why = append(why, "further given names")
		}
	} else {
		// initials, as on medal cards: "Nuns, Lewis A." or "Smith, J. H."
		ini := 0
		for _, gn := range givens {
			for _, u := range words {
				if len(u) == 1 && strings.EqualFold(u, gn[:1]) {
					ini++
					break
				}
			}
		}
		if ini == len(givens) {
			score++
			why = append(why, "initials only")
		} else {
			return 0, nil
		}
	}
	for _, y := range yearRe.FindAllString(r.Description, -1) {
		if n, _ := strconv.Atoi(y); m.Birth != 0 && n >= m.Birth-1 && n <= m.Birth+1 {
			score += 3
			why = append(why, "birth year")
			break
		}
	}
	if m.BirthPlace != "" {
		for _, tok := range strings.FieldsFunc(m.BirthPlace, func(r rune) bool { return r == ',' }) {
			tok = strings.TrimSpace(tok)
			if len(tok) > 3 && has(tok) {
				score += 2
				why = append(why, "birthplace "+tok)
				break
			}
		}
	}
	// a bare first-name match on a common surname (Clegg, Stephens) is noise;
	// keep it only for rare surnames, where any hit is worth a look
	if common && len(why) == 1 && why[0] == "first name" {
		score = 1
	}
	return score, why
}

// Options for Run.
type Options struct {
	Series   []string      // Discovery series to search; nil means tna.Series
	Delay    time.Duration // pause between remote calls
	Boer     bool          // also query the South African archives for 1899-1903
	NaairsDB string        // repository code, default RSA
	Log      func(string, ...any)
}

// Run checks every man and returns one Result each, in the order of Men.
func Run(ctx context.Context, g *model.Graph, men []Man, opts Options) []Result {
	series := opts.Series
	if series == nil {
		for s := range tna.Series {
			series = append(series, s)
		}
		sort.Strings(series)
	}
	logf := opts.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	tc := tna.New()
	// one Discovery query per surname spelling, shared by the men who carry it
	bySurname := map[string][]tna.Record{}
	spellings := func(s string) []string {
		out := []string{s}
		l := strings.ToLower(s)
		switch {
		case strings.HasSuffix(l, "ll"):
			out = append(out, s[:len(s)-1])
		case strings.HasSuffix(l, "l") && len(s) > 2:
			out = append(out, s+"l")
		}
		if strings.Contains(l, "ph") {
			out = append(out, strings.ReplaceAll(strings.ReplaceAll(s, "ph", "v"), "Ph", "V"))
		}
		if strings.Contains(l, "v") && !strings.Contains(l, "van") {
			out = append(out, strings.ReplaceAll(strings.ReplaceAll(s, "v", "ph"), "V", "Ph"))
		}
		return model.Uniq(out)
	}
	results := make([]Result, 0, len(men))
	for _, m := range men {
		res := Result{ID: m.ID, Name: m.Name, Role: m.Role, Birth: m.Birth_(), Death: m.Death_(), Wars: m.Wars}
		surname := m.Surname
		if surname == "" {
			f := strings.Fields(m.Name)
			surname = f[len(f)-1]
		}
		for _, sp := range spellings(surname) {
			recs, ok := bySurname[sp]
			if !ok {
				var err error
				recs, _, err = tc.Search(ctx, sp, series, 5)
				if err != nil {
					logf("tna: %s: %v", sp, err)
				}
				bySurname[sp] = recs
				time.Sleep(opts.Delay)
			}
			common := len(recs) > 40
			for _, r := range recs {
				if sc, why := ScoreTNA(m, r, common); sc >= 2 {
					res.Hits = append(res.Hits, Hit{Source: "tna", Reference: r.Reference, Description: r.Description, Dates: r.Dates, URL: r.URL, Score: sc, Why: why})
				}
			}
		}
		if opts.Boer && containsWar(m.Wars, "Anglo-Boer") {
			db := opts.NaairsDB
			if db == "" {
				db = "RSA"
			}
			terms := naairs.Terms(m.Person)
			if terms != nil {
				recs, _, err := naairs.New().Query(ctx, db, terms, "1899", "1903")
				if err != nil {
					logf("naairs: %s: %v", m.Name, err)
				}
				for _, r := range recs {
					if sc, why := naairs.Score(m.Person, r, nil); sc >= 1 {
						res.Hits = append(res.Hits, Hit{Source: "naairs", Reference: r.Depot + " " + r.Source + " " + r.Reference, Description: r.Description + " " + r.Remarks, Dates: model.Year(r.Starting), Score: sc, Why: append(why, "war years 1899-1903")})
					}
				}
				time.Sleep(opts.Delay)
			}
		}
		sort.SliceStable(res.Hits, func(i, j int) bool { return res.Hits[i].Score > res.Hits[j].Score })
		results = append(results, res)
		logf("%s (%s): %d records", m.Name, strings.Join(m.Wars, "; "), len(res.Hits))
	}
	return results
}

func containsWar(ws []string, prefix string) bool {
	for _, w := range ws {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

// Birth_ and Death_ return the years as strings for the result JSON.
func (m Man) Birth_() string {
	if m.Birth == 0 {
		return ""
	}
	return strconv.Itoa(m.Birth)
}

func (m Man) Death_() string {
	if m.Death == 0 {
		return ""
	}
	return strconv.Itoa(m.Death)
}
