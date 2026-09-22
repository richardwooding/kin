package linklives

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

const census = "\uFEFFpa_id,name,first_names,patronyms,family_names,sex,age,birth_year,birth_place,event_year,event_parish,event_county,household_id,household_position,source_id\n" +
	"1,Jens Christian Hansen,jens christian,hansen,,m,5,1840,Kirke Hyllinge,1845,Kirke Hyllinge,Frederiksborg,10,barn,4\n" +
	"2,Hans Jensen,hans,jensen,,m,40,1805,Kirke Hyllinge,1845,Kirke Hyllinge,Frederiksborg,10,husfader,4\n" +
	"3,Karen Nielsdatter,karen,nielsdatter,,k,38,1807,,1845,Kirke Hyllinge,Frederiksborg,10,hustru,4\n" +
	"4,Jens Hansen,jens,hansen,,m,5,1840,Ribe,1845,Ribe,Ribe,11,barn,4\n" +
	"5,Jens Hansen,jens,hansen,,m,30,1815,Ribe,1845,Ribe,Ribe,12,husfader,4\n" +
	"6,Jens Hanssen,jens,hanssen,,m,50,1795,Viborg,1845,Viborg,Viborg,13,husfader,4\n"

const courses = "life_course_id,pa_ids,source_ids,link_ids,n_sources\n" +
	"77,\"1,900\",\"4,5\",\"1\",2\n"

func release(t *testing.T) string {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"main datasets/census_1845_v1_std.csv":        census,
		"main datasets/census_1845_v1_cl.csv":         "not read",
		"main datasets/ALA/ALA census 1845.csv":       "not read",
		"links and lifecourses/life_courses_v2.1.csv": courses,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func danes() *model.Graph {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Living: true, Father: "seed:j"})
	g.Add(&model.Person{ID: "seed:j", Name: "Jens Christian Hansen", Birth: "1840", Death: "1901",
		BirthPlace: "Kirke Hyllinge, Frederiksborg, Danmark", Father: "seed:h", Mother: "seed:k"})
	g.Add(&model.Person{ID: "seed:h", Name: "Hans Jensen"})
	g.Add(&model.Person{ID: "seed:k", Name: "Karen Nielsdatter"})
	return g
}

func TestFilesAndLabels(t *testing.T) {
	files, err := Files(release(t))
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0], "census_1845_v1_std.csv") {
		t.Fatalf("only the harmonised files are read: %v %v", files, err)
	}
	for path, want := range map[string]string{
		"census_1845_v1_std.csv":   "Census 1845",
		"census 1901 v1 std.csv":   "Census 1901",
		"cbp_1860-1911_v1_std.csv": "Copenhagen burials 1860 1911",
	} {
		if got := Label(path); got != want {
			t.Errorf("Label(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestSweepFindsTheHouseholdAndLifeCourse(t *testing.T) {
	g := danes()
	dir := release(t)
	ids := People(g, SweepOptions{Root: "seed:me"})
	res, err := Sweep(dir, g, ids, SweepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var j SweepResult
	for _, r := range res {
		if r.ID == "seed:j" {
			j = r
		}
	}
	if len(j.Hits) != 1 {
		t.Fatalf("one Jens is backed by his parish and parents: %+v", j.Hits)
	}
	h := j.Hits[0]
	if h.PaID != "1" || h.Source != "Census 1845" || h.URL != "https://link-lives.dk/soeg/pa/4-1" {
		t.Errorf("hit = %+v", h)
	}
	for _, w := range []string{"all given names", "born 1840", "born in HYLLINGE", "household has father Hans", "household has mother Karen"} {
		found := false
		for _, why := range h.Why {
			found = found || strings.Contains(why, w)
		}
		if !found {
			t.Errorf("why lacks %q: %v", w, h.Why)
		}
	}
	if len(h.Members) != 2 {
		t.Errorf("the rest of the household is listed: %+v", h.Members)
	}
	if h.LifeCourse != "77" || h.LifeCourseURL != "https://link-lives.dk/soeg/life-course/2.1-77" {
		t.Errorf("life course = %q %q", h.LifeCourse, h.LifeCourseURL)
	}
	if j.Total != 2 {
		t.Errorf("the Ribe Jens of the same age is considered and dropped, the older ones never: total %d", j.Total)
	}
}

func TestNamesAloneStayBelowKeep(t *testing.T) {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Living: true, Father: "seed:j"})
	g.Add(&model.Person{ID: "seed:j", Name: "Jens Hansen", Birth: "1840", BirthPlace: "Danmark"})
	res, err := Sweep(release(t), g, []string{"seed:j"}, SweepOptions{Min: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res[0].Hits {
		if h.Score >= Keep {
			t.Errorf("a Jens Hansen of 1840 with nothing else in common is not a lead: %+v", h)
		}
	}
}

func TestSurnamesForTheDryRun(t *testing.T) {
	g := danes()
	got := Surnames(g, People(g, SweepOptions{Root: "seed:me"}))
	if strings.Join(got, ",") != "Hansen,Jensen,Nielsdatter" {
		t.Errorf("Surnames = %v", got)
	}
}

func TestRejectsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes_std.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sweep(dir, danes(), []string{"seed:j"}, SweepOptions{}); err == nil {
		t.Error("a file without pa_id is reported, not silently skipped")
	}
}
