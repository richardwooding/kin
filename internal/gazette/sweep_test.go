package gazette

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

// family is a root with British, Cape, living and placeless ancestors.
func family() *model.Graph {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Living: true, Father: "seed:f", Mother: "seed:m"})
	g.Add(&model.Person{ID: "seed:f", Name: "William Smith", Given: "William", Surname: "Smith",
		Birth: "1920", Death: "1990", BirthPlace: "Portsmouth, Hampshire, England", Father: "seed:ff"})
	g.Add(&model.Person{ID: "seed:m", Name: "Anna Botha", Given: "Anna", Surname: "Botha",
		Birth: "1923", Death: "1999", BirthPlace: "Graaff-Reinet, Cape Colony"})
	g.Add(&model.Person{ID: "seed:ff", Name: "John Smith", Given: "John", Surname: "Smith",
		Birth: "1890", Death: "1960", BirthPlace: "Portsmouth, Hampshire, England", Mother: "seed:ffm"})
	// no place of her own: she takes her son's
	g.Add(&model.Person{ID: "seed:ffm", Name: "Jane Webb", Given: "Jane", Surname: "Webb", Death: "1930"})
	// alive as far as the graph knows
	g.Add(&model.Person{ID: "seed:living", Name: "Young Smith", Surname: "Smith", Birth: "1960",
		BirthPlace: "London, England", Living: true})
	g.Persons["seed:ff"].Father = "seed:living"
	return g
}

func TestPeopleSelectsUKDead(t *testing.T) {
	g := family()
	got := People(g, Options{Root: "seed:me"})
	want := []string{"seed:f", "seed:ff", "seed:ffm"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("People = %v, want %v (Cape and living ancestors excluded, order by generation)", got, want)
	}
	if People(g, Options{Root: "seed:me", MaxGen: 1})[0] != "seed:f" {
		t.Error("the generation cap should keep the nearest")
	}
	if len(People(g, Options{Root: "seed:me", MaxGen: 1})) != 1 {
		t.Error("the generation cap should exclude the rest")
	}
}

func TestPlanQueries(t *testing.T) {
	p := &model.Person{Name: "William Smith", Given: "William", Surname: "Smith", Birth: "1920", Death: "1975"}
	qs := Plan(p, "Portsmouth, Hampshire, England", Options{})
	if len(qs) != 2 {
		t.Fatalf("a death before the structured notices begin gives two queries, got %d", len(qs))
	}
	if qs[0].Text != `"William Smith"` || qs[1].Text != `"Smith, William"` {
		t.Errorf("queries = %q, %q", qs[0].Text, qs[1].Text)
	}
	for _, q := range qs {
		if q.StartPublish != "1936-01-01" || q.EndPublish != "1978-12-31" || q.Edition != "London" {
			t.Errorf("window = %q to %q, edition %q", q.StartPublish, q.EndPublish, q.Edition)
		}
	}

	modern := &model.Person{Name: "Ronald Knuckey", Given: "Ronald", Surname: "Knuckey", Birth: "1930", Death: "2005"}
	qs = Plan(modern, "Truro, Cornwall, England", Options{})
	if len(qs) != 3 || qs[2].Service != Probate {
		t.Fatalf("a modern death adds the deceased estates query, got %d", len(qs))
	}
	if qs[2].StartDeath != "2004-01-01" || qs[2].EndDeath != "2007-12-31" {
		t.Errorf("date of death window = %q to %q", qs[2].StartDeath, qs[2].EndDeath)
	}

	unknown := Plan(&model.Person{Name: "Ann Martin", Given: "Ann", Surname: "Martin"}, "Stithians, Cornwall, England", Options{})
	if len(unknown) != 2 || unknown[0].StartPublish != "" || unknown[0].EndPublish != "" {
		t.Errorf("unknown dates leave the window open: %+v", unknown)
	}
	if Plan(&model.Person{Name: "Mononym"}, "England", Options{}) != nil {
		t.Error("a person without both names cannot be searched for")
	}
}

func TestSweepPersonOffline(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// the same notice comes back from both name orders
		_, _ = w.Write([]byte(`{"f:total":"1","entry":[{"id":"https://www.thegazette.co.uk/London/issue/1/page/2",
			"title":"The London Gazette, Issue 1, Page 2","published":"1961-03-01T00:00:00",
			"content":"<p>SMITH, John, late of Portsmouth, deceased. ` + strings.Repeat("filler ", 120) + `</p>"}]}`))
	}))
	defer srv.Close()
	c := New(t.TempDir())
	c.Delay = 0
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}

	p := &model.Person{ID: "seed:ff", Name: "John Smith", Given: "John", Surname: "Smith", Birth: "1890", Death: "1960"}
	res := c.SweepPerson(context.Background(), p, "Portsmouth, Hampshire, England", Options{})
	if res.Err != "" {
		t.Fatal(res.Err)
	}
	if len(res.Query) != 2 || calls != 2 {
		t.Errorf("two queries, %d planned, %d fetched", len(res.Query), calls)
	}
	if len(res.Hits) != 1 || len(res.Raw) != 1 {
		t.Fatalf("the same notice from both queries is kept once: %d hits, %d raw", len(res.Hits), len(res.Raw))
	}
	if res.Hits[0].Score < Keep {
		t.Errorf("hit = %+v", res.Hits[0])
	}
	if r := []rune(res.Raw[0].Text); len(r) > rawText+1 {
		t.Errorf("a scanned page is truncated, got %d runes", len(r))
	}
}
