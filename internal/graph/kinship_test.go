package graph

import (
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestLabel(t *testing.T) {
	cases := []struct {
		up, down int
		want     string
	}{
		{0, 1, "child"},
		{1, 0, "parent"},
		{2, 0, "grandparent"},
		{3, 0, "great-grandparent"},
		{1, 1, "sibling (or half-sibling)"},
		{2, 1, "aunt/uncle"},
		{1, 2, "niece/nephew"},
		{2, 2, "1st cousin"},
		{3, 3, "2nd cousin"},
		{3, 2, "1st cousin, 1× removed"},
		{4, 2, "1st cousin, 2× removed"},
		{5, 5, "4th cousin"},
	}
	for _, c := range cases {
		if got := Label(c.up, c.down); got != c.want {
			t.Errorf("Label(%d,%d) = %q, want %q", c.up, c.down, got, c.want)
		}
	}
}

func TestRelationship(t *testing.T) {
	g := model.NewGraph()
	add := func(id, father string) {
		g.Add(&model.Person{ID: id, Name: id, Father: father})
	}
	add("gg", "")
	add("g1", "gg")
	add("g2", "gg")
	add("p1", "g1")
	add("p2", "g2")
	add("me", "p1")
	add("cousin", "p2")
	rel, ok := Relationship(g, "me", "cousin")
	if !ok {
		t.Fatal("expected a relationship")
	}
	if rel.Label != "2nd cousin" || rel.Common[0] != "gg" {
		t.Errorf("got %+v", rel)
	}
	if _, ok := Relationship(g, "me", "stranger"); ok {
		t.Error("stranger should not relate")
	}
}
