package tna

import (
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func wooding() *model.Person {
	return &model.Person{ID: "fs:w", Name: "Charles Wooding", Given: "Charles", Surname: "Wooding",
		Birth: "1795", Death: "1850", BirthPlace: "Portsmouth, Hampshire, England"}
}

const woodingPlaces = "Portsmouth, Hampshire, England | "

func TestScorePersonRequiresExactSurname(t *testing.T) {
	// the catalogue stems: a search for Wooding returns Wood and Woods
	stemmed := Record{ID: "1", Reference: "PROB 11/351/256", Series: "PROB 11",
		Description: "Will of John Wood, Cooper of Portsmouth, Hampshire", Dates: "21 June 1830"}
	if sc, why := ScorePerson(wooding(), stemmed, ScoreOpts{Places: woodingPlaces}); sc != 0 {
		t.Errorf("Wood is not Wooding, scored %d (%v)", sc, why)
	}
	own := Record{ID: "2", Reference: "PROB 11/2107/113", Series: "PROB 11",
		Description: "Will of Charles Wooding, Cooper of Portsmouth, Hampshire", Dates: "01 February 1850"}
	sc, why := ScorePerson(wooding(), own, ScoreOpts{Places: woodingPlaces})
	if sc < Keep {
		t.Fatalf("their own will scored %d (%v)", sc, why)
	}
	for _, want := range []string{"full name", "the record is in their name", "place Portsmouth", "dates fit their life"} {
		if !has(why, want) {
			t.Errorf("why %v is missing %q", why, want)
		}
	}
}

func TestScoreDatesOutsideLifeDropped(t *testing.T) {
	early := Record{ID: "3", Reference: "PROB 11/1/1", Description: "Will of Charles Wooding of Portsmouth", Dates: "1650"}
	if sc, _ := ScorePerson(wooding(), early, ScoreOpts{Places: woodingPlaces}); sc != 0 {
		t.Error("a will proved before they were born is someone else's")
	}
	late := Record{ID: "4", Reference: "PROB 11/9/9", Description: "Will of Charles Wooding of Portsmouth", Dates: "1960"}
	if sc, _ := ScorePerson(wooding(), late, ScoreOpts{Places: woodingPlaces}); sc != 0 {
		t.Error("a will proved a century after their death is someone else's")
	}
}

func TestScoreFrontierCollection(t *testing.T) {
	knuckey := &model.Person{Name: "John Knuckey", Given: "John", Surname: "Knuckey",
		Birth: "1793", Death: "1842", BirthPlace: "Stithians, Cornwall, England"}
	places := "Stithians, Cornwall, England | Crellow, Stithians, Cornwall, England"
	fam := Record{ID: "5", Reference: "X674", Description: "Knuckey family of Stithians.", Dates: "1903-1968"}
	sc, why := ScorePerson(knuckey, fam, ScoreOpts{Places: places, Frontier: true})
	if sc < Keep || !has(why, "a family's papers from that parish") {
		t.Errorf("a record office's family collection scored %d (%v)", sc, why)
	}
	// the same record outside a frontier search is a weaker lead
	plain, _ := ScorePerson(knuckey, fam, ScoreOpts{Places: places})
	if plain >= sc {
		t.Errorf("scored %d without the frontier flag, %d with it", plain, sc)
	}
}

func TestScoreInitialsAndDamper(t *testing.T) {
	nuns := &model.Person{Name: "Lewis Anthony Nuns", Given: "Lewis Anthony", Surname: "Nuns", Birth: "1887", Death: "1949"}
	card := Record{ID: "6", Reference: "WO 372/15/16834", Description: "Nuns, Lewis A Corps: East African Transport Corps", Dates: "1914-1920"}
	sc, why := ScorePerson(nuns, card, ScoreOpts{})
	if sc < 2 {
		t.Errorf("a medal card scored %d (%v)", sc, why)
	}
	// a bare name, with no place and no dates, among a flood of same-name records
	common := Record{ID: "7", Reference: "HO 334/9/9", Description: "Wooding, Charles"}
	plain, _ := ScorePerson(wooding(), common, ScoreOpts{})
	damped, _ := ScorePerson(wooding(), common, ScoreOpts{Common: true})
	if damped >= plain || damped > 2 {
		t.Errorf("a name alone among many scored %d, plain %d", damped, plain)
	}
}

func TestDates(t *testing.T) {
	cases := map[string][2]int{
		"09 October 1773": {1773, 1773},
		"[1853-1872]":     {1853, 1872},
		"1903-1968":       {1903, 1968},
		"18-19th cent":    {0, 0},
	}
	for s, want := range cases {
		from, to := dates(s)
		if from != want[0] || to != want[1] {
			t.Errorf("dates(%q) = %d, %d, want %v", s, from, to, want)
		}
	}
}

func has(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func TestWillOfAnotherParishIsANamesake(t *testing.T) {
	// the Bermondsey Cleggs are not the Cleggs of Hull or Haslingden
	clegg := &model.Person{Name: "William Clegg", Given: "William", Surname: "Clegg",
		Birth: "1820", Death: "1873", BirthPlace: "Bermondsey, Surrey, England"}
	places := "Bermondsey, Surrey, England | "
	for _, desc := range []string{
		"Will of William Clegg, Wesleyan Minister of Hull , Yorkshire",
		"Will of William Turner Clegg of Chapel House in Haslingden , Lancashire",
	} {
		r := Record{ID: "n", Reference: "PROB 11/1/1", Description: desc, Dates: "1848"}
		if sc, why := ScorePerson(clegg, r, ScoreOpts{Places: places}); sc != 0 {
			t.Errorf("%q scored %d (%v); the will names another county", desc, sc, why)
		}
	}
	own := Record{ID: "y", Reference: "PROB 11/2/2",
		Description: "Will of William Clegg, Tanner of Bermondsey , Surrey", Dates: "1873"}
	if sc, _ := ScorePerson(clegg, own, ScoreOpts{Places: places}); sc < Keep {
		t.Error("a will naming their own parish is the strongest lead there is")
	}
	// a record naming no residence at all is still worth reporting
	bare := Record{ID: "z", Reference: "IR 26/1/1", Description: "Abstract of Will of William Clegg", Dates: "1873"}
	if sc, _ := ScorePerson(clegg, bare, ScoreOpts{Places: places}); sc < 4 {
		t.Error("an entry that names no place cannot be ruled out")
	}
}

func TestSessionRollsAreNotFamilyPapers(t *testing.T) {
	p := &model.Person{Name: "Thomas Stephens", Given: "Thomas", Surname: "Stephens",
		Birth: "1869", BirthPlace: "Stithians, Cornwall, England"}
	places := "Stithians, Cornwall, England | "
	roll := Record{ID: "r", Reference: "QS/1/7/84-100", Dates: "14th April 1801",
		Title:       "Sessions held at Truro",
		Description: "Justices Davies Giddy, John Vivian; presentments from the parishes of Stithians, Gwennap and Redruth; Thomas Penrose of Stithians"}
	if sc, why := ScorePerson(p, roll, ScoreOpts{Places: places, Frontier: true}); sc != 0 {
		t.Errorf("a quarter sessions roll from 1801 scored %d for a man born in 1869 (%v)", sc, why)
	}
	fam := Record{ID: "f", Reference: "X674", Title: "Stephens family of Stithians.",
		Description: "Receipted accounts", Dates: "1903-1968"}
	if sc, _ := ScorePerson(p, fam, ScoreOpts{Places: places, Frontier: true}); sc < Keep {
		t.Error("a collection named for the family and the parish is a real lead")
	}
}
