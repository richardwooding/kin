package gazette

import (
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func nuns() *model.Person {
	return &model.Person{
		ID: "wt:Nuns-4", Name: "Lewis Anthony Nuns", Given: "Lewis Anthony", Surname: "Nuns",
		Birth: "1887-03-02", Death: "1949-11-20",
		BirthPlace: "Bermondsey, Surrey, England", DeathPlace: "Cape Town, South Africa",
		Occupations: []string{"sapper, East African Transport Corps"},
	}
}

const nunsPlaces = "Bermondsey, Surrey, England | Cape Town, South Africa"

func score(t *testing.T, n Notice, common bool) (int, []string) {
	t.Helper()
	return Score(nuns(), nunsPlaces, n, common)
}

func TestScoreFullName(t *testing.T) {
	n := Notice{ID: "1", Title: "Lewis Anthony NUNS", Code: "2903", Published: "1950-02-01", Year: 1950,
		Text: "NUNS, Lewis Anthony, late of Bermondsey, deceased. Died 20 November 1949."}
	sc, why := score(t, n, false)
	if sc < Keep {
		t.Fatalf("the person's own estate notice scored %d (%v)", sc, why)
	}
	for _, want := range []string{"full name", "further given names", "place Bermondsey", "year of death", "deceased estates notice"} {
		if !has(why, want) {
			t.Errorf("why %v is missing %q", why, want)
		}
	}
}

func TestScoreStemmedSurnameIsZero(t *testing.T) {
	// the feed stems: a search for Nuns returns Nun, and Discovery returns Wood
	// for Wooding. Neither is the person.
	n := Notice{ID: "2", Title: "The London Gazette, Issue 1, Page 2", Issue: "1", Year: 1930,
		Text: "NUN, Lewis, of Bermondsey, bankrupt."}
	if sc, why := score(t, n, false); sc != 0 {
		t.Errorf("a stemmed surname scored %d (%v)", sc, why)
	}
	wooding := &model.Person{Name: "Charles Wooding", Given: "Charles", Surname: "Wooding", Birth: "1850", Death: "1910"}
	will := Notice{ID: "3", Issue: "9", Year: 1900, Text: "Will of Charles Wood, Cooper of Portsmouth, Hampshire"}
	if sc, _ := Score(wooding, "Portsmouth, Hampshire, England", will, false); sc != 0 {
		t.Errorf("Wooding must not match Wood, scored %d", sc)
	}
}

func TestScoreOutsideLifetimeDropped(t *testing.T) {
	early := Notice{ID: "4", Issue: "1", Year: 1880, Text: "NUNS, Lewis Anthony, of Bermondsey"}
	if sc, why := score(t, early, false); sc != 0 {
		t.Errorf("a notice from before he was sixteen scored %d (%v)", sc, why)
	}
	late := Notice{ID: "5", Issue: "1", Year: 2020, Text: "NUNS, Lewis Anthony, of Bermondsey"}
	if sc, _ := score(t, late, false); sc != 0 {
		t.Error("a notice long after the estate was wound up should be dropped")
	}
	within := Notice{ID: "6", Issue: "1", Year: 1930, Text: "NUNS, Lewis Anthony, of Bermondsey, sapper"}
	if sc, why := score(t, within, false); sc < Keep {
		t.Errorf("a notice in his lifetime scored %d (%v)", sc, why)
	} else if !has(why, "in their lifetime") || !has(why, "occupation") {
		t.Errorf("why = %v", why)
	}
}

func TestScoreInitialsAndScannedSpelling(t *testing.T) {
	initials := Notice{ID: "7", Issue: "2", Year: 1919, Text: "Sapper NUNS, L. A., East African Transport Corps"}
	sc, why := score(t, initials, false)
	if sc < 2 || !has(why, "surname and initials") {
		t.Errorf("initials scored %d (%v)", sc, why)
	}
	// a scanned page may have lost a letter from a long name; a filed notice
	// may not, and a short name is never matched loosely
	knuckey := &model.Person{Name: "John Knuckey", Given: "John", Surname: "Knuckey", Birth: "1793", Death: "1842"}
	scanned := Notice{ID: "8", Issue: "3", Year: 1830, Text: "KNUCKEV, John, of Stithians, copper miner"}
	if sc, _ := Score(knuckey, "Stithians, Cornwall, England", scanned, false); sc == 0 {
		t.Error("one scanning error should still match a long name on a printed page")
	}
	filed := Notice{ID: "9", Code: "2903", Year: 1830, Text: "KNUCKEV, John, of Stithians"}
	if sc, _ := Score(knuckey, "Stithians, Cornwall, England", filed, false); sc != 0 {
		t.Error("a notice filed as text is spelt as filed; no loose matching")
	}
	short := Notice{ID: "10", Issue: "3", Year: 1930, Text: "NUNN, Lewis Anthony, of Bermondsey"}
	if sc, _ := score(t, short, false); sc != 0 {
		t.Error("a four-letter surname is too short to match loosely")
	}
}

func TestScoreCommonSurnameDamper(t *testing.T) {
	n := Notice{ID: "11", Issue: "4", Year: 1930, Text: "NUNS, Lewis, dissolution of partnership"}
	plain, _ := score(t, n, false)
	damped, _ := score(t, n, true)
	if damped >= plain || damped > 2 {
		t.Errorf("a name alone in a flood of results scored %d, plain %d", damped, plain)
	}
	// a place or a death year is evidence, so the damper must not apply
	strong := Notice{ID: "12", Code: "2903", Year: 1950, Text: "NUNS, Lewis Anthony, late of Bermondsey, deceased"}
	if sc, _ := score(t, strong, true); sc < Keep {
		t.Errorf("a place and a death year survive the damper, scored %d", sc)
	}
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestScoredInTheWordsAroundTheName(t *testing.T) {
	// a page of an old issue holds dozens of unrelated notices: the word
	// "deceased" and a place elsewhere on the page say nothing about this name
	page := Notice{ID: "20", Issue: "24013", Page: "4077", Edition: "London", Year: 1930,
		Text: "Re JOHN SMITH, Deceased, late of Bermondsey. " + strings.Repeat("other notices about other people ", 12) +
			" NUNS, Lewis Anthony, appointed inspector of weights"}
	sc, why := score(t, page, false)
	if has(why, "deceased estates notice") || has(why, "place Bermondsey") {
		t.Errorf("a neighbouring notice was read as this one: %v", why)
	}
	near := Notice{ID: "21", Issue: "24013", Edition: "London", Year: 1930,
		Text: "Re NUNS, Lewis Anthony, Deceased, late of Bermondsey, cooper"}
	sc2, why2 := score(t, near, false)
	if !has(why2, "deceased estates notice") || !has(why2, "place Bermondsey") {
		t.Errorf("their own notice should score both: %v", why2)
	}
	if sc2 <= sc {
		t.Errorf("their own notice scored %d, a page mentioning them %d", sc2, sc)
	}
	// the edition's own city is on every page and proves nothing
	london := &model.Person{Name: "Henry Clegg", Given: "Henry", Surname: "Clegg", Birth: "1874", Death: "1931",
		BirthPlace: "London, England"}
	n := Notice{ID: "22", Issue: "1", Edition: "London", Year: 1931, Text: "Joseph Henry Clegg, of Rochdale Road, Milnrow, Cotton Mill Manager"}
	if _, why := Score(london, "London, England | ", n, false); has(why, "place London") {
		t.Errorf("London is not evidence in the London Gazette: %v", why)
	}
}
