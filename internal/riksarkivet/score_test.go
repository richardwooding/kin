package riksarkivet

import (
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func swedes() *model.Graph {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "seed:me", Name: "Alex Smith", Living: true, Father: "seed:k"})
	g.Add(&model.Person{ID: "seed:k", Name: "Karl Nilsson", Gender: "male", Birth: "1885-01-16",
		BirthPlace: "Mjällby, Blekinge, Sverige", Father: "seed:n", Mother: "seed:s", Spouses: []string{"seed:b"}})
	g.Add(&model.Person{ID: "seed:n", Name: "Nils Johansson", Gender: "male", Birth: "1851", BirthPlace: "Mjällby, Sverige"})
	g.Add(&model.Person{ID: "seed:s", Name: "Sofia Andersdotter", Gender: "female"})
	g.Add(&model.Person{ID: "seed:b", Name: "Bengta Persdotter", Gender: "female"})
	g.Add(&model.Person{ID: "seed:x", Name: "Ole Hansen", Birth: "1850", BirthPlace: "Bergen, Norge"})
	return g
}

func has(why []string, s string) bool {
	for _, w := range why {
		if strings.Contains(w, s) {
			return true
		}
	}
	return false
}

func TestScoreBirthWithParents(t *testing.T) {
	g := swedes()
	p := g.Persons["seed:k"]
	r := Record{Type: Birth, Date: "1885-01-16", Child: "Carl Oscar", Father: "Johansson, Nils", Mother: "Andersdotter, Sophia", Parish: "Mjällby"}
	sc, why := Score(p, r, FamilyOf(g, p, p.BirthPlace))
	if sc < Keep+4 {
		t.Errorf("name, date, both parents and parish agree; score %d %v", sc, why)
	}
	for _, w := range []string{"given name Karl", "birth date", "patronymic from father Nils", "father Nils", "mother Sofia", "parish Mjällby"} {
		if !has(why, w) {
			t.Errorf("why lacks %q: %v", w, why)
		}
	}
}

func TestScoreBirthNamesAloneStayBelowKeep(t *testing.T) {
	p := &model.Person{Name: "Karl Nilsson", Birth: "1885"}
	r := Record{Type: Birth, Date: "1885-06-01", Child: "Karl", Father: "Olsson, Nils", Parish: "Ystad"}
	sc, why := Score(p, r, Family{})
	if sc >= Keep || !has(why, "names and dates only") {
		t.Errorf("a Karl Nilsson born to some Nils in 1885 proves nothing: %d %v", sc, why)
	}
}

func TestScoreBirthRejectsOtherChildAndYear(t *testing.T) {
	p := &model.Person{Name: "Karl Nilsson", Birth: "1885"}
	if sc, _ := Score(p, Record{Type: Birth, Date: "1885-01-01", Child: "Albert"}, Family{}); sc != 0 {
		t.Error("a sibling's baptism is not the person's")
	}
	if sc, _ := Score(p, Record{Type: Birth, Date: "1889-01-01", Child: "Karl"}, Family{}); sc != 0 {
		t.Error("four years out is another child")
	}
}

func TestScoreMarriage(t *testing.T) {
	g := swedes()
	p := g.Persons["seed:k"]
	f := FamilyOf(g, p, p.BirthPlace)
	sc, why := Score(p, Record{Type: Marriage, Date: "1910-05-01", Groom: "Nilsson, Karl Oscar", Bride: "Persdotter, Bengta", Parish: "Ystad"}, f)
	if sc < Keep || !has(why, "spouse Bengta") || !has(why, "spouse's surname") {
		t.Errorf("groom and bride agree: %d %v", sc, why)
	}
	if sc, _ := Score(p, Record{Type: Marriage, Date: "1910-05-01", Groom: "Persson, Karl", Bride: "Nilsson, Anna"}, f); sc != 0 {
		t.Error("a man is looked for only as the groom")
	}
	if sc, _ := Score(p, Record{Type: Marriage, Date: "1890-05-01", Groom: "Nilsson, Karl", Bride: "Persdotter, Bengta"}, f); sc != 0 {
		t.Error("a five-year-old does not marry")
	}
}

func TestPeopleSelectsSwedes(t *testing.T) {
	g := swedes()
	g.Persons["seed:k"].Father = "seed:n"
	g.Persons["seed:n"].Father = "seed:x"
	got := strings.Join(People(g, SweepOptions{Root: "seed:me"}), ",")
	if got != "seed:k,seed:n,seed:s" {
		t.Errorf("People = %s; the Swedish ancestors nearest first, the placeless mother by her son's parish", got)
	}
	if strings.Contains(got, "seed:x") {
		t.Error("the Norwegian is not in Swedish registers")
	}
}

func TestPlan(t *testing.T) {
	g := swedes()
	qs := Plan(g, g.Persons["seed:k"])
	if len(qs) != 3 {
		t.Fatalf("baptism by own name, by father's, and marriage: %+v", qs)
	}
	if qs[0].Name != "Karl Nilsson" || qs[0].YearMin != 1883 || qs[0].YearMax != 1887 || qs[0].Type != Birth {
		t.Errorf("own baptism = %+v", qs[0])
	}
	if qs[1].Name != "Nils Johansson" || qs[1].Type != Birth {
		t.Errorf("father's name finds the baptism a patronymic hides: %+v", qs[1])
	}
	if qs[2].Type != Marriage || qs[2].YearMin != 1900 || qs[2].YearMax != 1945 {
		t.Errorf("marriage = %+v", qs[2])
	}
	if qs := Plan(g, &model.Person{Name: "Anna Svensdotter", Death: "1900"}); len(qs) != 1 || qs[0].YearMin != 1840 {
		t.Errorf("without a birth, only the marriage before death: %+v", qs)
	}
}

func TestScoreBirthOtherMotherStaysBelowKeep(t *testing.T) {
	g := swedes()
	p := g.Persons["seed:k"]
	r := Record{Type: Birth, Date: "1885-09-26", Child: "Carl", Father: "Johansson, Nils Pet.", Mother: "Månsdotter, Botilla", Parish: "Grevie"}
	sc, why := Score(p, r, FamilyOf(g, p, p.BirthPlace))
	if sc >= Keep || !has(why, "mother Botilla, not Sofia") {
		t.Errorf("a Carl whose mother is not Sofia is another Nils's son: %d %v", sc, why)
	}
}
