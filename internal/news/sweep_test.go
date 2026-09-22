package news

import (
	"context"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestSweepMemoisesSharedQueries(t *testing.T) {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "me", Name: "Alex Smith", Living: true, Father: "f"})
	g.Add(&model.Person{ID: "f", Name: "Hans Jensen", Birth: "1820", Death: "1875", BirthPlace: "Hillerød, Danmark", Father: "gf", Spouses: []string{"w"}})
	g.Add(&model.Person{ID: "gf", Name: "Hans Jensen", Birth: "1820", Death: "1875", BirthPlace: "Hillerød, Danmark"})
	g.Add(&model.Person{ID: "w", Name: "Karen Nielsdatter"})
	n := 0
	src := &KB{serve(t, kbBody, &n)}
	var got []SweepResult
	ids := People(g, []Source{src}, SweepOptions{Root: "me"})
	Sweep(context.Background(), g, ids, []Source{src}, SweepOptions{Min: 1}, func(r SweepResult) { got = append(got, r) })
	if len(got) != 2 || n != 2 {
		t.Fatalf("two people sharing a life query and a death query cost two requests: %d results, %d requests", len(got), n)
	}
	if len(got[0].Hits) != 1 || got[0].Hits[0].Source != "kb" || got[0].Query[0] == "" {
		t.Errorf("result = %+v", got[0])
	}
	if !has(got[0].Hits[0].Why, "paper of HILLEROD") {
		t.Errorf("the paper's town is his: %v", got[0].Hits[0].Why)
	}
}
