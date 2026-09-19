package tree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/viz"
)

func person(id, name, gender, birth, father, mother string, sources ...string) *model.Person {
	return &model.Person{ID: id, Name: name, Gender: gender, Birth: birth, Father: father, Mother: mother, Sources: sources}
}

// three generations: me <- f + m; f <- ff + fm; m <- mf + mm
func threeGen() *model.Graph {
	g := model.NewGraph()
	g.Add(person("me", "Alex Smith", "male", "1980", "f", "m", "user"))
	g.Add(person("f", "John Smith", "male", "1950", "ff", "fm", "user"))
	g.Add(person("m", "Mary Jones", "female", "1952", "mf", "mm", "user"))
	g.Add(person("ff", "William Smith", "male", "1920", "", "", "wikitree"))
	g.Add(person("fm", "Anna Botha", "female", "1923", "", "", "eggsa"))
	g.Add(person("mf", "Peter Jones", "male", "1925", "", ""))
	g.Add(person("mm", "Elizabeth Brown", "female", "1928", "", "", "familysearch"))
	return g
}

func byID(p *Pedigree) map[string]Node {
	out := map[string]Node{}
	for _, n := range p.Nodes {
		out[n.ID] = n
	}
	return out
}

func TestBuildAhnentafelAndGenerations(t *testing.T) {
	p, err := Build(threeGen(), Options{Root: "me"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Nodes) != 7 || p.Generations != 2 {
		t.Fatalf("nodes %d generations %d", len(p.Nodes), p.Generations)
	}
	want := map[string]int{"me": 1, "f": 2, "m": 3, "ff": 4, "fm": 5, "mf": 6, "mm": 7}
	for i, n := range p.Nodes {
		if n.Numbers[0] != want[n.ID] {
			t.Errorf("%s numbered %v, want %d", n.ID, n.Numbers, want[n.ID])
		}
		if i > 0 && p.Nodes[i-1].Numbers[0] > n.Numbers[0] {
			t.Errorf("nodes not in ascending Ahnentafel order at %d", i)
		}
	}
	if n := byID(p)["mm"]; n.Gen != 2 || n.Relation != "grandmother" || n.Status != "record" {
		t.Errorf("mm = %+v", n)
	}
	if n := byID(p)["me"]; n.Relation != "self" || n.Gen != 0 {
		t.Errorf("root = %+v", n)
	}
	if n := byID(p)["fm"]; n.Status != "gravestone" {
		t.Errorf("fm status %q", n.Status)
	}
	if n := byID(p)["mf"]; n.Status != "tree" {
		t.Errorf("mf status %q", n.Status)
	}
}

func TestBuildGenerationCap(t *testing.T) {
	p, err := Build(threeGen(), Options{Root: "me", MaxGen: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Nodes) != 3 || p.Truncated != 4 || p.Generations != 1 {
		t.Errorf("nodes %d truncated %d generations %d", len(p.Nodes), p.Truncated, p.Generations)
	}
}

func TestBuildDanglingParentCountedNotEmitted(t *testing.T) {
	g := threeGen()
	g.Persons["ff"].Father = "wt:id:1"
	p, err := Build(g, Options{Root: "me"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Dangling != 1 || len(p.Nodes) != 7 {
		t.Errorf("dangling %d nodes %d", p.Dangling, len(p.Nodes))
	}
	if byID(p)["ff"].Father != "wt:id:1" {
		t.Errorf("raw father id should be kept, got %q", byID(p)["ff"].Father)
	}
}

func TestBuildDuplicateAncestorRecordedUnderBothNumbers(t *testing.T) {
	g := threeGen()
	g.Add(person("gg", "Old Smith", "male", "1890", "ggg", "", "wikitree"))
	g.Add(person("ggg", "Older Smith", "male", "1860", "", "", "wikitree"))
	g.Persons["ff"].Father = "gg"
	g.Persons["mf"].Father = "gg" // cousins married: gg reached at 8 and 12
	p, err := Build(g, Options{Root: "me"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := byID(p)
	if got := n["gg"].Numbers; len(got) != 2 || got[0] != 8 || got[1] != 12 {
		t.Errorf("gg numbers %v", got)
	}
	if got := n["ggg"].Numbers; len(got) != 1 || got[0] != 16 {
		t.Errorf("ggg numbers %v (must not be re-expanded under 24)", got)
	}
	count := 0
	for _, x := range p.Nodes {
		if x.ID == "gg" {
			count++
		}
	}
	if count != 1 || p.Generations != 4 {
		t.Errorf("gg emitted %d times, generations %d", count, p.Generations)
	}
}

func TestBuildSpousesAndChildren(t *testing.T) {
	g := threeGen()
	g.Add(person("other", "Jane Doe", "female", "1955", "", ""))
	g.Add(person("sib", "Sam Smith", "male", "1975", "f", "other"))
	g.Persons["f"].Spouses = []string{"m", "other"}
	p, err := Build(g, Options{Root: "me"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := byID(p)["f"]
	if strings.Join(f.Spouses, ",") != "m,other" {
		t.Errorf("spouses %v", f.Spouses)
	}
	if strings.Join(f.Children, ",") != "sib,me" {
		t.Errorf("children %v (birth order expected)", f.Children)
	}
	if _, ok := p.People["other"]; !ok {
		t.Error("other spouse missing from People")
	}
	if _, ok := p.People["sib"]; !ok {
		t.Error("sibling missing from People")
	}
	if _, ok := p.People["m"]; ok {
		t.Error("a line member must not be duplicated in People")
	}
}

func TestBuildProbableAndRecords(t *testing.T) {
	g := threeGen()
	g.Add(person("ggg", "Older Smith", "male", "1860", "", "", "wikitree"))
	g.Persons["ff"].Father = "ggg"
	recs := []json.RawMessage{json.RawMessage(`{"person":"mm","event":"Baptism"}`), json.RawMessage(`{"person":"nobody"}`)}
	p, err := Build(g, Options{Root: "me", ProbableIDs: []string{"ff"}}, recs)
	if err != nil {
		t.Fatal(err)
	}
	n := byID(p)
	if n["ff"].Status != "probable" || n["ggg"].Status != "probable" {
		t.Errorf("probable not propagated: ff %s ggg %s", n["ff"].Status, n["ggg"].Status)
	}
	if n["m"].Status != "record" {
		t.Errorf("m status %s", n["m"].Status)
	}
	if got := n["mm"].Records; len(got) != 1 || got[0] != 0 {
		t.Errorf("mm records %v", got)
	}
	if _, err := Build(g, Options{Root: "ghost"}, nil); err == nil {
		t.Error("unknown root should fail")
	}
}

func TestRenderSmoke(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tree.html")
	err := Render(threeGen(), Options{Root: "me", Site: viz.Site{Title: "Smith Kin", Eyebrow: "Test"}, DashboardURL: "https://example.test/dash"}, out)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	s := string(b)
	for _, want := range []string{"Smith Kin", `"generations":2`, "cdnjs.cloudflare.com/ajax/libs/d3/7.9.0/d3.min.js", `"dashboardUrl":"https://example.test/dash"`, `<meta name="viewport"`} {
		if !strings.Contains(s, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(s, "/*__DATA__*/") {
		t.Error("placeholder not replaced")
	}
	i := strings.Index(s, `<script id="data" type="application/json">`)
	j := strings.Index(s[i:], "</script>")
	var pl Payload
	if err := json.Unmarshal([]byte(strings.ReplaceAll(s[i+len(`<script id="data" type="application/json">`):i+j], `<\/`, "</")), &pl); err != nil {
		t.Fatal(err)
	}
	if len(pl.Nodes) != 7 || pl.ReaderName != "Alex Smith" {
		t.Errorf("payload nodes %d reader %q", len(pl.Nodes), pl.ReaderName)
	}
}
