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
	if opc == nil || len(opc.Leads) != 3 {
		t.Fatalf("expected baptisms, marriages and burials: %+v", opc)
	}
	u := opc.Leads[0].URL
	for _, want := range []string{"search-database/baptisms/index.php?", "forename1=William", "surname1=Thomas", "year_from=1798", "year_to=1806", "t=baptisms"} {
		if !strings.Contains(u, want) {
			t.Errorf("baptism url missing %q: %s", want, u)
		}
	}
	if !strings.Contains(opc.Leads[2].URL, "burials") || !strings.Contains(opc.Leads[2].URL, "year_from=1874") {
		t.Errorf("burial url wrong: %s", opc.Leads[2].URL)
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
	if fs := group(m, "FamilySearch"); len(fs.Leads) != 2 || !strings.Contains(fs.Leads[1].URL, "f.collectionId=2517051") {
		t.Errorf("Cape probate lead missing: %+v", fs)
	}
	if group(m, "Cornwall OPC") != nil {
		t.Error("no OPC leads outside Cornwall")
	}
	x := find(entries, "wt:x")
	if fs := group(x, "FamilySearch"); len(fs.Leads) != 2 || !strings.Contains(fs.Leads[1].URL, "f.collectionId=2562194") {
		t.Errorf("1881 census lead missing: %+v", fs)
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
	if fs := group(n, "FamilySearch"); len(fs.Leads) != 3 || !strings.Contains(fs.Leads[2].URL, "f.collectionId=2517051") {
		t.Errorf("1881 census and Cape probate both expected: %+v", fs)
	}
	m := find(entries, "wt:t")
	if fs := group(m, "FamilySearch"); len(fs.Leads) != 1 {
		t.Errorf("Cape probate must not be offered for a Transvaal death: %+v", fs)
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
