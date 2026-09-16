package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func full(id string, living bool) *Person {
	return &Person{
		ID: id, Name: "Ann Smith", Given: "Ann", Surname: "Smith", Gender: "female",
		Birth: "1980-05-12", Death: "2070", BirthPlace: "Cape Town", DeathPlace: "Durban",
		Occupations: []string{"teacher"}, Citizenship: []string{"South Africa"},
		Description: "teacher (born 1980)", Wikidata: "Q1", WikiTree: "Smith-1",
		URL: "https://example.org/ann", Father: "seed:ff", Mother: "seed:mm",
		Spouses: []string{"seed:sp"}, Sources: []string{"user", "wikitree"}, Living: living, Note: "born at home",
	}
}

func TestRedactedStripsLivingOnly(t *testing.T) {
	g := NewGraph()
	g.Add(full("seed:me", true))
	g.Add(full("seed:ff", false))
	g.Aliases["wt:id:1"] = "seed:ff"

	r, n := g.Redacted()
	if n != 1 {
		t.Fatalf("redacted %d, want 1", n)
	}
	want := &Person{
		ID: "seed:me", Name: "Ann Smith", Given: "Ann", Surname: "Smith", Gender: "female",
		Wikidata: "Q1", WikiTree: "Smith-1", Father: "seed:ff", Mother: "seed:mm",
		Spouses: []string{"seed:sp"}, Sources: []string{"user", "wikitree"}, Living: true,
	}
	if got := r.Persons["seed:me"]; !reflect.DeepEqual(got, want) {
		t.Errorf("living person:\n got %+v\nwant %+v", got, want)
	}
	if got := r.Persons["seed:ff"]; !reflect.DeepEqual(got, full("seed:ff", false)) {
		t.Errorf("dead person changed: %+v", got)
	}
	if !reflect.DeepEqual(r.Aliases, g.Aliases) || r.Resolve("wt:id:1") != "seed:ff" {
		t.Errorf("aliases not preserved: %v", r.Aliases)
	}
	if g.Persons["seed:me"].Birth != "1980-05-12" {
		t.Error("input graph was modified")
	}
	r.Persons["seed:ff"].Spouses[0] = "changed"
	if g.Persons["seed:ff"].Spouses[0] != "seed:sp" {
		t.Error("copy shares a slice with the input")
	}
}

func TestRedactedRoundTripsJSON(t *testing.T) {
	g := NewGraph()
	g.Add(full("seed:me", true))
	r, _ := g.Redacted()
	path := filepath.Join(t.TempDir(), "graph.public.json")
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var raw struct {
		Persons map[string]map[string]any `json:"persons"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	me := raw.Persons["seed:me"]
	for _, k := range []string{"birth", "death", "birthPlace", "deathPlace", "occupations", "citizenship", "description", "url", "note"} {
		if _, ok := me[k]; ok {
			t.Errorf("%s still present in saved JSON", k)
		}
	}
	if me["name"] != "Ann Smith" || me["living"] != true {
		t.Errorf("identity lost: %v", me)
	}
	l, err := Load(path)
	if err != nil || l.Persons["seed:me"].Father != "seed:ff" {
		t.Errorf("reload: %v %+v", err, l.Persons["seed:me"])
	}
}

func TestRedactedEmptyGraph(t *testing.T) {
	r, n := NewGraph().Redacted()
	if n != 0 || r.Persons == nil || r.Aliases == nil || len(r.Persons) != 0 {
		t.Errorf("got %d %+v", n, r)
	}
}
