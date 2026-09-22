package news

import (
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/place"
)

func has(why []string, s string) bool {
	for _, w := range why {
		if strings.Contains(w, s) {
			return true
		}
	}
	return false
}

var hans = &model.Person{Name: "Hans Jensen", Birth: "1820", Death: "1875", BirthPlace: "Unnerup, Frederiksborg, Danmark"}

func TestScoreDeathNoticeNamingTheWidow(t *testing.T) {
	f := Family{Places: hans.BirthPlace, Region: place.Denmark, Relatives: []Relative{{Role: "spouse", Given: "Karen"}}}
	h := Hit{Year: 1875, Text: "Idag døde min kjære Mand Gaardmand Hans Jensen af Unnerup, dybt savnet af hans Enke Karen Nielsdatter"}
	sc, why := Score(hans, h, f, true)
	for _, w := range []string{"death notice", "spouse Karen", "place UNNERUP", "at the death"} {
		if !has(why, w) {
			t.Errorf("why lacks %q: %v", w, why)
		}
	}
	if sc < Keep+4 {
		t.Errorf("a death notice naming the widow and the parish: %d", sc)
	}
}

func TestScoreNameAndDateAloneStayBelowKeep(t *testing.T) {
	h := Hit{Year: 1850, Text: "Gaardmand Hans Jensen af Viborg solgte sin Hest"}
	sc, why := Score(hans, h, Family{Places: hans.BirthPlace, Region: place.Denmark}, true)
	if sc >= Keep {
		t.Errorf("a Hans Jensen selling a horse in Viborg is not a lead: %d %v", sc, why)
	}
}

func TestScoreRejectsWrongDatesAndNames(t *testing.T) {
	f := Family{Places: hans.BirthPlace, Region: place.Denmark}
	if sc, _ := Score(hans, Hit{Year: 1830, Text: "Hans Jensen af Unnerup døde"}, f, true); sc != 0 {
		t.Error("a paper from when he was ten is not about him")
	}
	if sc, _ := Score(hans, Hit{Year: 1950, Text: "Hans Jensen af Unnerup"}, f, true); sc != 0 {
		t.Error("seventy-five years after his death is too late")
	}
	if sc, _ := Score(hans, Hit{Year: 1860, Text: "Hans Petersen og Jens Jensen af Unnerup"}, f, true); sc != 0 {
		t.Error("the given name must stand beside the surname")
	}
}

func TestScoreWithoutContextUsesThePapersTown(t *testing.T) {
	ole := &model.Person{Name: "Ole Olsen", Birth: "1840", Death: "1900", BirthPlace: "Oslo, Norge"}
	f := Family{Places: ole.BirthPlace, Region: place.Norway}
	sc, why := Score(ole, Hit{Year: 1885, Paper: "Norsk Kunngjørelsestidende", Place: "Oslo", Text: "Ole Olsen"}, f, false)
	if sc < Keep || !has(why, "paper of OSLO") {
		t.Errorf("a paper from his town: %d %v", sc, why)
	}
	sc, _ = Score(ole, Hit{Year: 1885, Paper: "Bergens Tidende", Place: "Bergen", Text: "Ole Olsen"}, f, false)
	if sc >= Keep {
		t.Errorf("an Ole Olsen in a Bergen paper, with nothing more, is not a lead: %d", sc)
	}
}

func TestScoreNordicSpellingVariant(t *testing.T) {
	karen := &model.Person{Name: "Karen Olsen", Birth: "1830", Death: "1890", BirthPlace: "Odense, Danmark"}
	h := Hit{Year: 1890, Text: "Enken Karen Olsson af Odense er død"}
	if sc, why := Score(karen, h, Family{Places: karen.BirthPlace, Region: place.Denmark}, true); sc < Keep || !has(why, "death notice") {
		t.Errorf("Olsson is Olsen in a Nordic paper: %d %v", sc, why)
	}
	if sc, _ := Score(karen, h, Family{Places: "Oxford, England", Region: place.England}, true); sc != 0 {
		t.Error("outside the Nordic countries Olsson is not Olsen")
	}
}

func TestScoreParentNeedsSurnameAndDeathNeedsItsYear(t *testing.T) {
	f := Family{Places: "Viborg", Region: place.Denmark, Relatives: []Relative{{Role: "father", Given: "Jens", Surname: "Nielsen"}}}
	sc, why := Score(hans, Hit{Year: 1850, Text: "Hans Jensen og Jens Petersen af Aarhus"}, f, true)
	if has(why, "father") || sc >= Keep {
		t.Errorf("a Jens without the father's surname is anyone: %d %v", sc, why)
	}
	if _, why := Score(hans, Hit{Year: 1850, Text: "Hans Jensen og Jens Nielsen af Aarhus"}, f, true); !has(why, "father Jens") {
		t.Errorf("Jens Nielsen is the father: %v", why)
	}
	if _, why := Score(hans, Hit{Year: 1850, Text: "Hans Jensen, hvis Hustru døde"}, Family{Region: place.Denmark}, true); has(why, "death notice") {
		t.Errorf("a death twenty-five years before his is not his notice: %v", why)
	}
}
