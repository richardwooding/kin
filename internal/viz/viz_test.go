package viz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestLoadSiteDefaults(t *testing.T) {
	s, err := LoadSite("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Kin" || len(s.OrderingRows) != 1 {
		t.Errorf("unexpected defaults: %+v", s)
	}
}

func TestLoadSiteOverridesOnlyGivenKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "site.json")
	os.WriteFile(p, []byte(`{"title":"X","flags":[{"label":"echo","namePattern":"\\b(ann|anne)\\b"}]}`), 0o644)
	s, err := LoadSite(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "X" || s.Eyebrow != DefaultSite().Eyebrow || len(s.Flags) != 1 {
		t.Errorf("got %+v", s)
	}
	os.WriteFile(p, []byte(`{"flags":[{"label":"bad","namePattern":"("}]}`), 0o644)
	if _, err := LoadSite(p); err == nil {
		t.Error("invalid pattern should fail")
	}
}

func TestLoadSiteLinks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "site.json")
	os.WriteFile(p, []byte(`{"links":[{"label":"A","href":"a.html"},{"label":"B","href":"b.html"}]}`), 0o644)
	s, err := LoadSite(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Links) != 2 || s.Links[1].Href != "b.html" {
		t.Errorf("got %+v", s.Links)
	}
	os.WriteFile(p, []byte(`{"links":[{"label":"no href"}]}`), 0o644)
	if _, err := LoadSite(p); err == nil {
		t.Error("link without href should fail")
	}
}

func person(id, name, surname, birth, father, mother string, sources ...string) *model.Person {
	return &model.Person{ID: id, Name: name, Surname: surname, Birth: birth, Father: father, Mother: mother, Sources: sources}
}

// The page is handed only the people it draws, with only the fields it reads,
// while the counts describe the whole graph.
func TestRenderTrimsPayload(t *testing.T) {
	g := model.NewGraph()
	g.Add(person("seed:me", "Alex Smith", "Smith", "1980", "wt:Smith-1", "wt:Jones-1", "user"))
	f := person("wt:Smith-1", "John Smith", "Smith", "1950", "", "", "wikitree")
	f.WikiTree = "Smith-1"
	f.URL = "https://www.wikitree.com/wiki/Smith-1"
	f.Gender = "male"
	f.Note = "private research note"
	g.Add(f)
	g.Add(person("wt:Jones-1", "Mary Jones", "Jones", "1952", "", "", "wikitree"))
	g.Add(person("wt:Smith-2", "Sam Smith", "Smith", "1975", "wt:Smith-1", "wt:Jones-1", "wikitree")) // sibling: one step from the parents
	far := person("wt:Smith-9", "Far Smith", "Smith", "1900", "", "", "wikitree")                     // no link to the family: not drawn
	far.DeathPlace = "Cape Town, South Africa"                                                        // but a regional row, so shipped after all
	g.Add(far)
	g.Add(person("wt:Brown-1", "Unrelated Brown", "Brown", "1900", "", "", "wikitree")) // neither: counted only
	out := filepath.Join(t.TempDir(), "index.html")
	if err := Render(g, Options{Seed: "seed:me", Site: DefaultSite()}, out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	s := string(b)
	i := strings.Index(s, `<script id="data" type="application/json">`)
	j := strings.Index(s[i:], "</script>")
	var pl Payload
	if err := json.Unmarshal([]byte(strings.ReplaceAll(s[i+len(`<script id="data" type="application/json">`):i+j], `<\/`, "</")), &pl); err != nil {
		t.Fatal(err)
	}
	ids := map[string]Person{}
	for _, p := range pl.Persons {
		ids[p.ID] = p
	}
	for _, want := range []string{"seed:me", "wt:Smith-1", "wt:Jones-1", "wt:Smith-2", "wt:Smith-9"} {
		if _, ok := ids[want]; !ok {
			t.Errorf("payload lacks %s", want)
		}
	}
	if _, ok := ids["wt:Brown-1"]; ok {
		t.Error("payload carries a person the page never draws")
	}
	if pl.Counts.Persons != 6 || pl.Counts.Network != 4 || pl.Counts.Sources["wikitree"] != 5 {
		t.Errorf("counts %+v", pl.Counts)
	}
	if ids["wt:Smith-1"].URL != "" {
		t.Error("derivable WikiTree url shipped")
	}
	if strings.Contains(s, "private research note") || strings.Contains(s, `"gender"`) {
		t.Error("field the page never reads shipped")
	}
	if pl.Relations["wt:Smith-1"] != "parent" || len(pl.Regional) != 1 || pl.Regional[0] != "wt:Smith-9" || strings.Join(pl.Family, ",") != "Smith,Jones" {
		t.Errorf("relations %v regional %v family %v", pl.Relations, pl.Regional, pl.Family)
	}
}
