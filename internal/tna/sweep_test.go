package tna

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/place"
)

func family() *model.Graph {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Living: true, Father: "seed:f", Mother: "seed:m"})
	g.Add(&model.Person{ID: "seed:f", Name: "John Knuckey", Given: "John", Surname: "Knuckey",
		Birth: "1793", Death: "1842", BirthPlace: "Stithians, Cornwall, England"})
	g.Add(&model.Person{ID: "seed:m", Name: "Anna Botha", Given: "Anna", Surname: "Botha",
		Birth: "1800", Death: "1870", BirthPlace: "Graaff-Reinet, Cape Colony"})
	return g
}

func TestPeopleSelectsUKDead(t *testing.T) {
	got := People(family(), SweepOptions{Root: "seed:me"})
	if strings.Join(got, ",") != "seed:f" {
		t.Errorf("People = %v; only the British ancestor belongs in this catalogue", got)
	}
}

func TestPlanCivilAndFrontier(t *testing.T) {
	g := family()
	p := g.Persons["seed:f"]
	places := place.PlacesOf(g, p, nil)

	qs := Plan(p, places, SweepOptions{})
	if len(qs) != 1 {
		t.Fatalf("without the frontier flag, one query; got %d", len(qs))
	}
	if qs[0].Text != "Knuckey John" || strings.Join(qs[0].Series, ",") != strings.Join(Keys(Civil), ",") {
		t.Errorf("query = %+v", qs[0])
	}
	if qs[0].DateFrom != "1792-01-01" || qs[0].DateTo != "1902-12-31" {
		t.Errorf("a will may be proved long after the death: %q to %q", qs[0].DateFrom, qs[0].DateTo)
	}

	qs = Plan(p, places, SweepOptions{Frontier: true})
	if len(qs) != 2 || qs[1].HeldBy != HeldElsewhere {
		t.Fatalf("the frontier adds a search of the other archives: %+v", qs)
	}
	if qs[1].Text != "Knuckey Stithians" || len(qs[1].Series) != 0 {
		t.Errorf("the parish search is unrestricted by series: %+v", qs[1])
	}
	p.Father, p.Mother = "seed:gf", "seed:gm"
	if qs := Plan(p, places, SweepOptions{Frontier: true}); len(qs) != 1 {
		t.Error("a person with both parents is not on the frontier")
	}
}

func TestSweepMemoisesTheParishQuery(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = w.Write([]byte(`{"count":1,"records":[{"id":"X674","reference":"X674","description":"Knuckey family of Stithians.","coveringDates":"1903-1968"}]}`))
	}))
	defer srv.Close()
	c := New()
	c.Delay = 0
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}

	// two Knuckeys of the same parish, both missing a parent
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Knuckey", Living: true, Father: "a", Mother: "b"})
	for _, id := range []string{"a", "b"} {
		g.Add(&model.Person{ID: id, Name: "John Knuckey", Given: "John", Surname: "Knuckey",
			Birth: "1793", Death: "1842", BirthPlace: "Stithians, Cornwall, England"})
	}
	var results []SweepResult
	c.Sweep(context.Background(), g, People(g, SweepOptions{Root: "seed:me"}),
		SweepOptions{Root: "seed:me", Frontier: true}, func(r SweepResult) { results = append(results, r) })

	if len(results) != 2 {
		t.Fatalf("swept %d people", len(results))
	}
	if n != 2 {
		t.Errorf("the catalogue was asked %d times; the shared parish query should be asked once", n)
	}
	if len(results[0].Hits) == 0 {
		t.Error("the family collection should be a candidate")
	}
}
