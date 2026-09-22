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

func TestRenderClientMode(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.html")
	notes := []string{"John Smith was born in Portsmouth.", "He married Mary Jones."}
	render := func(o Options) string {
		o.Root, o.Reader, o.Title, o.Notes = "me", "me", "t", notes
		if err := Render(twoGen(), o, out); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(out)
		return string(b)
	}

	s := render(Options{DashboardURL: "index.html"})
	for _, want := range []string{"Where to order copies", "<h2>Notes and open questions</h2>", "<li>John Smith was born in Portsmouth.</li>", "Back to the dashboard"} {
		if !strings.Contains(s, want) {
			t.Errorf("the researcher's report lacks %q", want)
		}
	}

	s = render(Options{Client: true, DashboardURL: "index.html"})
	for _, gone := range []string{"Where to order copies", "Notes and open questions", "Back to the dashboard", "<li>John Smith"} {
		if strings.Contains(s, gone) {
			t.Errorf("the client's report still has %q", gone)
		}
	}
	for _, want := range []string{"<h2>The story of this line</h2>", "<p>John Smith was born in Portsmouth.</p>", "<p>He married Mary Jones.</p>", "John Smith"} {
		if !strings.Contains(s, want) {
			t.Errorf("the client's report lacks %q", want)
		}
	}

	if s := render(Options{Client: true, NotesTitle: "The Smiths of Portsmouth"}); !strings.Contains(s, "<h2>The Smiths of Portsmouth</h2>") {
		t.Error("-notes-title overrides the client heading")
	}
	if s := render(Options{NotesTitle: "To do"}); !strings.Contains(s, "<h2>To do</h2>") || !strings.Contains(s, "<li>He married") {
		t.Error("-notes-title overrides the research heading and keeps the list")
	}
}
