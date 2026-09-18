package leads

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func testGraph() *model.Graph {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Given: "Alex", Surname: "Smith", Father: "fs:f", Mother: "fs:m"})
	g.Add(&model.Person{ID: "fs:f", Name: "William Thomas", Given: "William", Surname: "Thomas", Birth: "1803-09", BirthPlace: "St Gluvias, Cornwall, England", Death: "1875"})
	g.Add(&model.Person{ID: "fs:m", Name: "Catherine Ann Switzer", Given: "Catherine Ann", Surname: "Switzer", Birth: "1853", BirthPlace: "Mossel Bay, Cape Colony, South Africa", Death: "1933-02-19", Father: "wt:x"})
	g.Add(&model.Person{ID: "wt:x", Name: "Henry Charles Clegg", Given: "Henry Charles", Surname: "Clegg", Birth: "1874", BirthPlace: "Bermondsey, Surrey, England", Father: "wt:y", Mother: "wt:z"})
	g.Add(&model.Person{ID: "wt:y", Name: "William Clegg", Given: "William", Surname: "Clegg"})
	g.Add(&model.Person{ID: "wt:z", Name: "Ann Clegg", Given: "Ann", Surname: "Clegg", Father: "wt:n", Mother: "wt:t"})
	g.Add(&model.Person{ID: "fs:sarah", Name: "Sarah Thomas", Given: "Sarah", Surname: "Thomas"})
	g.Persons["fs:f"].Mother = "fs:sarah"
	g.Add(&model.Person{ID: "wt:n", Name: "James Nuns", Given: "James", Surname: "Nuns", Birth: "1829", BirthPlace: "London, England", Death: "1898", DeathPlace: "Mossel Bay, Cape Colony, South Africa"})
	g.Add(&model.Person{ID: "wt:t", Name: "Anna Marais", Given: "Anna", Surname: "Marais", Birth: "1830", Death: "1900", DeathPlace: "Johannesburg, Transvaal, South Africa"})
	return g
}

func find(entries []Entry, id string) *Entry {
	for i := range entries {
		if entries[i].Person.ID == id {
			return &entries[i]
		}
	}
	return nil
}

func hasCollection(g *Group, id string) bool {
	if g == nil {
		return false
	}
	for _, l := range g.Leads {
		if strings.Contains(l.URL, "f.collectionId="+id) {
			return true
		}
	}
	return false
}

func group(e *Entry, service string) *Group {
	for i := range e.Groups {
		if e.Groups[i].Service == service {
			return &e.Groups[i]
		}
	}
	return nil
}

func TestBuildFrontier(t *testing.T) {
	entries := Build(testGraph(), Options{Root: "seed:me", ProbableIDs: []string{"wt:x"}})
	f := find(entries, "fs:f")
	if f == nil || strings.Join(f.Why, ";") != "no father" || f.Gen != 1 {
		t.Fatalf("fs:f entry wrong: %+v", f)
	}
	m := find(entries, "fs:m")
	if m == nil || strings.Join(m.Why, ";") != "no mother" {
		t.Fatalf("fs:m entry wrong: %+v", m)
	}
	x := find(entries, "wt:x")
	if x == nil || strings.Join(x.Why, ";") != "probable link to parents" || x.Gen != 2 {
		t.Fatalf("probable entry wrong: %+v", x)
	}
	if find(entries, "seed:me") != nil {
		t.Error("the root has both parents and must not appear")
	}
	if y := find(entries, "wt:y"); y == nil || y.Gen != 3 || strings.Join(y.Why, ";") != "no father;no mother" {
		t.Errorf("wt:y should appear as the top of the line: %+v", y)
	}
	if find(Build(testGraph(), Options{Root: "seed:me"}), "wt:x") != nil {
		t.Error("wt:x has both parents and appears only when marked probable")
	}
	if entries[0].Gen > entries[len(entries)-1].Gen {
		t.Error("entries not sorted by generation")
	}
}

func TestCornwallLeads(t *testing.T) {
	e := find(Build(testGraph(), Options{Root: "seed:me"}), "fs:f")
	opc := group(e, "Cornwall OPC")
	if opc == nil || len(opc.Leads) != 4 {
		t.Fatalf("expected baptisms, marriages, children and burials: %+v", opc)
	}
	u := opc.Leads[0].URL
	for _, want := range []string{"search-database/baptisms/index.php?", "forename1=William", "surname1=Thomas", "year_from=1798", "year_to=1806", "t=baptisms", "parish=St+Gluvias", "nearby=1"} {
		if !strings.Contains(u, want) {
			t.Errorf("baptism url missing %q: %s", want, u)
		}
	}
	if c := opc.Leads[2]; !strings.Contains(c.Label, "children of the couple") || !strings.Contains(c.URL, "forename2=William") || strings.Contains(c.URL, "forename1=") {
		t.Errorf("children url should search by the father's forename only: %+v", c)
	}
	if !strings.Contains(opc.Leads[3].URL, "burials") || !strings.Contains(opc.Leads[3].URL, "year_from=1874") {
		t.Errorf("burial url wrong: %s", opc.Leads[3].URL)
	}
	ew := group(e, "England and Wales")
	if ew == nil || ew.Leads[0].URL != "https://www.freereg.org.uk/search_queries/new" || !strings.Contains(ew.Leads[0].Hint, "surname Thomas, first name William, 1798 to 1806") {
		t.Errorf("FreeREG lead must be the search page plus a hint: %+v", ew)
	}
	for _, l := range ew.Leads {
		if strings.Contains(l.URL, "freereg") && strings.Contains(l.URL, "Thomas") {
			t.Error("FreeREG url must not carry search parameters")
		}
	}
	if fs := group(e, "FamilySearch"); fs == nil || !strings.Contains(fs.Leads[0].URL, "q.anyPlace=St+Gluvias%2C+Cornwall%2C+England") {
		t.Errorf("FamilySearch url wrong: %+v", fs)
	}
}

func TestSouthAfricaAndEnglandLeads(t *testing.T) {
	entries := Build(testGraph(), Options{Root: "seed:me", ProbableIDs: []string{"wt:x"}})
	m := find(entries, "fs:m")
	sa := group(m, "South Africa")
	if sa == nil || !strings.HasPrefix(sa.Leads[0].Hint, "kin naairs -db KAB -q 'SWITZER CATHERINE' -from 1932 -to 1936") {
		t.Errorf("NAAIRS hint wrong: %+v", sa)
	}
	if fs := group(m, "FamilySearch"); len(fs.Leads) != 3 || !strings.Contains(fs.Leads[1].URL, "f.collectionId=1478678") || !strings.Contains(fs.Leads[2].URL, "f.collectionId=2517051") {
		t.Errorf("Dutch Reformed and Cape probate leads expected: %+v", fs)
	}
	if group(m, "Cornwall OPC") != nil {
		t.Error("no OPC leads outside Cornwall")
	}
	x := find(entries, "wt:x")
	if fs := group(x, "FamilySearch"); len(fs.Leads) != 2 || !strings.Contains(fs.Leads[1].URL, "f.collectionId=2562194") {
		t.Errorf("only the 1881 census applies to a birth of 1874: %+v", fs)
	}
	if wt := group(x, "WikiTree"); wt == nil || wt.Leads[0].Hint != "kin wikitree search -last Clegg -first Henry -birth 1874 -spread 3" {
		t.Errorf("WikiTree hint wrong: %+v", wt)
	}
}

func TestBothCountries(t *testing.T) {
	entries := Build(testGraph(), Options{Root: "seed:me"})
	n := find(entries, "wt:n")
	if group(n, "England and Wales") == nil || group(n, "South Africa") == nil {
		t.Fatalf("born in England, died at the Cape: both groups expected, got %+v", n.Groups)
	}
	if fs := group(n, "FamilySearch"); !hasCollection(fs, "2563939") || !hasCollection(fs, "2562194") || !hasCollection(fs, "2517051") {
		t.Errorf("1851 and 1881 censuses and Cape probate all expected: %+v", fs)
	}
	m := find(entries, "wt:t")
	if fs := group(m, "FamilySearch"); hasCollection(fs, "2517051") || !hasCollection(fs, "2520237") || !hasCollection(fs, "2155416") {
		t.Errorf("a Transvaal death gets the Transvaal probate and Hervormde registers, not the Cape probate: %+v", fs)
	}
	if sa := group(m, "South Africa"); sa == nil || !strings.Contains(sa.Leads[0].Hint, "-db TAB") {
		t.Errorf("Transvaal depot expected: %+v", sa)
	}
}

func TestPlacesFallBackToChildren(t *testing.T) {
	e := find(Build(testGraph(), Options{Root: "seed:me"}), "fs:sarah")
	if e == nil || group(e, "Cornwall OPC") == nil || group(e, "England and Wales") == nil {
		t.Fatalf("a mother with no places should take her Cornish child's region: %+v", e)
	}
	if fs := group(e, "FamilySearch"); strings.Contains(fs.Leads[0].URL, "q.anyPlace") {
		t.Error("inferred places must not be put into the search itself")
	}
}

func TestOnlyAndOutputs(t *testing.T) {
	entries := Build(testGraph(), Options{Root: "seed:me", Only: "wt:y"})
	if len(entries) != 1 || entries[0].Person.ID != "wt:y" || entries[0].Why[0] != "requested" {
		t.Fatalf("Only: %+v", entries)
	}
	var buf bytes.Buffer
	WriteText(&buf, entries)
	if !strings.Contains(buf.String(), "William Clegg (wt:y)") || !strings.Contains(buf.String(), "kin wikitree search") {
		t.Errorf("text output: %s", buf.String())
	}
	path := filepath.Join(t.TempDir(), "leads.html")
	if err := Render(entries, "Leads", path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "<title>Leads</title>") || !strings.Contains(string(b), "William Clegg") || !strings.Contains(string(b), `<a href="https://www.familysearch.org/search/record/results?`) {
		t.Errorf("html output: %s", b)
	}
}

func TestGazetteLead(t *testing.T) {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Father: "seed:f"})
	g.Add(&model.Person{ID: "seed:f", Name: "William Thomas", Given: "William", Surname: "Thomas",
		Birth: "1803-09", Death: "1870", BirthPlace: "St Gluvias, Cornwall, England"})
	g.Add(&model.Person{ID: "seed:m", Name: "Anna Botha", Given: "Anna", Surname: "Botha",
		Birth: "1840", Death: "1900", BirthPlace: "Graaff-Reinet, Cape Colony"})
	g.Persons["seed:me"].Mother = "seed:m"

	var uk, sa string
	for _, e := range Build(g, Options{Root: "seed:me"}) {
		for _, grp := range e.Groups {
			if grp.Service != "The Gazette" {
				continue
			}
			if e.Person.ID == "seed:f" {
				uk = grp.Leads[0].URL
			} else {
				sa = grp.Leads[0].URL
			}
		}
	}
	if sa != "" {
		t.Errorf("a Cape ancestor gets no Gazette lead, got %q", sa)
	}
	if !strings.Contains(uk, "thegazette.co.uk/all-notices/notice?") {
		t.Errorf("the lead opens the Gazette search page, got %q", uk)
	}
	for _, want := range []string{"%22Thomas%2C+William%22", "start-publish-date=1819-01-01", "end-publish-date=1873-12-31", "edition=London"} {
		if !strings.Contains(uk, want) {
			t.Errorf("lead %q is missing %q", uk, want)
		}
	}
}

func TestCoupleLeads(t *testing.T) {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Father: "fs:h", Mother: "fs:w"})
	g.Add(&model.Person{ID: "fs:h", Name: "Thomas Spargo", Given: "Thomas", Surname: "Spargo", Gender: "male", Spouses: []string{"fs:w"}})
	g.Add(&model.Person{ID: "fs:w", Name: "Ann Martin (Spargo)", Given: "Ann", Surname: "Martin", Gender: "female", Spouses: []string{"fs:h"}})
	g.Add(&model.Person{ID: "fs:k1", Name: "Mary Spargo", Given: "Mary", Surname: "Spargo", Birth: "1762-10-09", BirthPlace: "Stithians, Cornwall, England", Father: "fs:h", Mother: "fs:w"})
	g.Add(&model.Person{ID: "fs:k2", Name: "William Spargo", Given: "William", Surname: "Spargo", Birth: "1756", BirthPlace: "Stithians, Cornwall, England", Father: "fs:h", Mother: "fs:w"})
	entries := Build(g, Options{Root: "seed:me"})
	h := group(find(entries, "fs:h"), "Cornwall OPC")
	if h == nil {
		t.Fatal("a father known only from his children's Stithians baptisms takes their parish")
	}
	var children, marriage string
	for _, l := range h.Leads {
		switch {
		case strings.HasPrefix(l.Label, "children of the couple"):
			children = l.URL
		case strings.HasPrefix(l.Label, "marriage to Ann Martin"):
			marriage = l.URL
		}
	}
	for _, want := range []string{"parish=Stithians", "nearby=1", "forename2=Thomas", "forename3=Ann", "surname1=Spargo", "year_from=1753", "year_to=1765"} {
		if !strings.Contains(children, want) {
			t.Errorf("children url missing %q: %s", want, children)
		}
	}
	for _, want := range []string{"forename1=Thomas", "surname1=Spargo", "forename2=Ann", "surname2=Martin", "year_from=1741", "year_to=1762"} {
		if !strings.Contains(marriage, want) {
			t.Errorf("marriage url missing %q: %s", want, marriage)
		}
	}
	w := group(find(entries, "fs:w"), "Cornwall OPC")
	var wc string
	for _, l := range w.Leads {
		if strings.HasPrefix(l.Label, "children of the couple") {
			wc = l.URL
		}
	}
	if !strings.Contains(wc, "surname1=Spargo") || !strings.Contains(wc, "forename3=Ann") || !strings.Contains(wc, "forename2=Thomas") {
		t.Errorf("a mother's children carry her husband's surname: %s", wc)
	}
}

func TestUpstreamAndSearched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "searched.json")
	os.WriteFile(path, []byte(`[{"person":"fs:f","service":"Cornwall OPC","when":"2026-09-18","note":"baptisms 1798 to 1806 St Gluvias: none"}]`), 0o644)
	done, err := LoadSearched(path)
	if err != nil || len(done["fs:f"]) != 1 {
		t.Fatalf("LoadSearched: %v %+v", err, done)
	}
	entries := Build(testGraph(), Options{Root: "seed:me", ProbableIDs: []string{"wt:x"}, Upstream: []string{"wt:"}, Searched: done})
	if find(entries, "wt:y") != nil || find(entries, "wt:n") != nil {
		t.Error("WikiTree ends must be left off the frontier when wt: is upstream")
	}
	if find(entries, "wt:x") == nil {
		t.Error("a probable WikiTree link is still ours to prove")
	}
	f := find(entries, "fs:f")
	if len(f.Searched) != 1 || f.Searched[0].Note == "" {
		t.Fatalf("searched entries not attached: %+v", f)
	}
	var buf bytes.Buffer
	WriteText(&buf, entries)
	if !strings.Contains(buf.String(), "already searched: Cornwall OPC (2026-09-18): baptisms 1798 to 1806 St Gluvias: none") {
		t.Errorf("text output lacks the searched line: %s", buf.String())
	}
	html := filepath.Join(t.TempDir(), "leads.html")
	if err := Render(entries, "Leads", html); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(html)
	if !strings.Contains(string(b), "Already searched") || !strings.Contains(string(b), "St Gluvias: none") {
		t.Errorf("html output lacks the searched list")
	}
	if _, err := LoadSearched(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("a missing file is an error")
	}
}
