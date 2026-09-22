package namematch

import (
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestExactRejectsStems(t *testing.T) {
	words := Tokens("Will of John Wood, Cooper of Portsmouth, Hampshire")
	if Exact(words, "Wooding") {
		t.Error("Wooding must not match the stemmed Wood the catalogue returns")
	}
	if !Exact(words, "Wood") {
		t.Error("Wood should match Wood")
	}
	if !Exact(words, "john") {
		t.Error("matching is case-insensitive")
	}
	if !Exact(Tokens("the Knuckeys of Stithians"), "Knuckey") {
		t.Error("a plural or possessive s should still match")
	}
}

func TestOCRAllowsOneLetter(t *testing.T) {
	words := Tokens("KNUCKEV, John, of Crellow in the parish of Stithians")
	if Exact(words, "Knuckey") {
		t.Error("the scanned name is not an exact match")
	}
	if !OCR(words, "Knuckey") {
		t.Error("one scanning error should still match a long name")
	}
	if OCR(Tokens("Ward, Thomas"), "Wood") {
		t.Error("short names must not be matched loosely")
	}
}

func TestLev1(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"KNUCKEY", "KNUCKEV", true},
		{"SPARGO", "SPARGOE", true},
		{"SPARGOE", "SPARGO", true},
		{"TRENGOVE", "TRENGOVE", true},
		{"WOODING", "WOOD", false},
		{"CLEGG", "CRAIG", false},
	}
	for _, c := range cases {
		if got := Lev1(c.a, c.b); got != c.want {
			t.Errorf("Lev1(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestNear(t *testing.T) {
	words := Tokens("NUNS, Lewis Anthony, late of Bermondsey, deceased")
	if !Near(words, "Nuns", "Lewis", 2) {
		t.Error("the given name follows the surname")
	}
	if Near(words, "Nuns", "Bermondsey", 2) {
		t.Error("Bermondsey is five words away")
	}
	if !Near(words, "Nuns", "Bermondsey", 6) {
		t.Error("a wider span should reach Bermondsey")
	}
	if Near(words, "Nuns", "Portsmouth", 9) {
		t.Error("a word that is absent is never near")
	}
}

func TestPlaceTokens(t *testing.T) {
	got := PlaceTokens("Crellow, Stithians, Cornwall, England | Redruth, Cornwall, England")
	want := "CRELLOW STITHIANS REDRUTH"
	if strings.Join(got, " ") != want {
		t.Errorf("PlaceTokens = %v, want %s", got, want)
	}
	if len(PlaceTokens("Cornwall, England")) != 0 {
		t.Error("a county and a country alone distinguish nothing")
	}
}

func TestTokensFoldNordicLetters(t *testing.T) {
	words := Tokens("Bjørn Ågård, Tromsø, Sjælland")
	if strings.Join(words, " ") != "BJORN AGARD TROMSO SJAELLAND" {
		t.Errorf("accents should fold, not split words: %q", words)
	}
	if !Exact(words, "Bjørn") || !Exact(words, "Bjorn") {
		t.Error("a term matches whichever way it is spelt")
	}
}

func TestPatronym(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Andersen", "Andersson", true},
		{"Hanssen", "Hansen", true},
		{"Christensdatter", "Kristensdotter", true},
		{"Aagaard", "Ågård", true},
		{"Olsen", "Olsson", true},
		{"Jensdóttir", "Jensdatter", true},
		{"Hansen", "Jensen", false},
		{"Nielsen", "Nilsson", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := Patronym(c.a, c.b); got != c.want {
			t.Errorf("Patronym(%q, %q) = %v (%q, %q)", c.a, c.b, got, Nordic(c.a), Nordic(c.b))
		}
	}
}

func TestNamesFallsBackToTheFullName(t *testing.T) {
	p := &model.Person{Name: "Mary Spargo (Knuckey)"}
	surname, givens := Names(p)
	if surname != "Spargo" || strings.Join(givens, " ") != "Mary" {
		t.Errorf("names = %q, %v", surname, givens)
	}
}

func TestNamesDropsInitialsNotNordicNames(t *testing.T) {
	_, givens := Names(&model.Person{Given: "J Øle", Surname: "Hansen"})
	if strings.Join(givens, " ") != "Øle" {
		t.Errorf("givens = %v", givens)
	}
}
