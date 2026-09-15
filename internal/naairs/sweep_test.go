package naairs

import (
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestTerms(t *testing.T) {
	if got := Terms(&model.Person{Given: "Philippus Rudolf", Surname: "Nell"}); len(got) != 2 || got[0] != "NELL" || got[1] != "PHILIPPUS" {
		t.Errorf("got %v", got)
	}
	if got := Terms(&model.Person{Given: "Susanna", Surname: "van der Vyver"}); len(got) != 4 || got[3] != "SUSANNA" {
		t.Errorf("got %v", got)
	}
	if got := Terms(&model.Person{Name: "Henry Charles Clegg"}); len(got) != 2 || got[0] != "CLEGG" || got[1] != "HENRY" {
		t.Errorf("name fallback got %v", got)
	}
	if Terms(&model.Person{Name: "Unknown"}) != nil {
		t.Error("single word should yield nil")
	}
}

func TestScore(t *testing.T) {
	p := &model.Person{ID: "x", Given: "Philippus Rudolf", Surname: "Nell", Birth: "1868", Death: "1900"}
	estate := Record{Depot: "TAB", Source: "MHG", Description: "NELL, PHILLIPUS RUDOLF.", Starting: "19000000", Remarks: "NAGELATE EGGENOTE SOFIA ELIZABETH (GEBORE REYNECKE)."}
	sc, why := Score(p, estate, []string{"REYNEKE", "REYNECKE"})
	if sc < 6 {
		t.Errorf("estate with matching year and spouse scored %d (%v)", sc, why)
	}
	if sc, _ := Score(p, Record{Source: "MHG", Description: "BOTHA, PHILIPPUS JACOBUS.", Starting: "19450000"}, nil); sc != 0 {
		t.Errorf("wrong surname should score 0, got %d", sc)
	}
	wife := &model.Person{ID: "w", Given: "Susanna", Surname: "van der Vyver", Birth: "1898", Death: "1950"}
	if sc, why := Score(wife, Record{Depot: "KAB", Source: "MOOC", Description: "WOODING, SUSANNA. ESTATE PAPERS.", Starting: "19500000"}, []string{"WOODING"}); sc < 4 {
		t.Errorf("married-name estate scored %d (%v)", sc, why)
	}
	if sc, _ := Score(wife, Record{Source: "MOOC", Description: "WOODING, SUSANNA VAN DER VYVER.", Starting: "19500000"}, []string{"WOODING"}); sc < 6 {
		t.Errorf("maiden and married name should score high, got %d", sc)
	}
	early := Record{Source: "CO", Description: "LETTER FROM PHILIPPUS NELL", Starting: "18500000"}
	if sc, _ := Score(p, early, nil); sc >= 2 {
		t.Errorf("document before birth should be noise, got %d", sc)
	}
}

func TestLoose(t *testing.T) {
	pairs := [][2]string{{"PHILLIPUS", "Philippus"}, {"REYNECKE", "Reyneke"}, {"Van der Vijver", "VAN DER VYVER"}, {"Marthinus", "Martinus"}}
	for _, pr := range pairs {
		if loose(pr[0]) != loose(pr[1]) {
			t.Errorf("%q and %q should share a key: %q vs %q", pr[0], pr[1], loose(pr[0]), loose(pr[1]))
		}
	}
	if !matchWord(tokens("NEL, PHILIPPUS RUDOLPH."), "RUDOLF") {
		t.Error("RUDOLPH should match RUDOLF within one edit")
	}
	if matchWord(tokens("NEL, PETRUS"), "NELL") == false {
		t.Error("NEL should match NELL (doubled letter)")
	}
	if matchWord(tokens("BOTHA, ANNA"), "SMITH") {
		t.Error("unrelated word matched")
	}
}
