package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func twoGen() *model.Graph {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "me", Name: "Alex Smith", Gender: "male", Birth: "1980", Father: "f", Mother: "m", Sources: []string{"user"}})
	g.Add(&model.Person{ID: "f", Name: "John Smith", Gender: "male", Birth: "1950", BirthPlace: "Portsmouth, England", Sources: []string{"user"}})
	g.Add(&model.Person{ID: "m", Name: "Mary Jones", Gender: "female", Birth: "1952", Death: "2010", Sources: []string{"wikitree"}})
	return g
}

func TestRenderSmoke(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.html")
	if err := Render(twoGen(), Options{Root: "me", Reader: "me", Title: "Alex Smith and his ancestors", DashboardURL: "index.html"}, out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	s := string(b)
	for _, want := range []string{
		`<meta name="viewport"`,
		"<title>Alex Smith and his ancestors</title>",
		"John Smith", "Mary Jones",
		`<table class="line">`, `data-label="Born"`, `data-label="Died"`, `data-label="Status"`,
		`<a href="index.html">&larr; Back to the dashboard</a>`,
		"@media screen and (max-width: 640px)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Without a dashboard the back link is left out entirely.
	if err := Render(twoGen(), Options{Root: "me", Reader: "me", Title: "t"}, out); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(out)
	if strings.Contains(string(b), "Back to the dashboard") {
		t.Error("back link rendered without a dashboard url")
	}
}
