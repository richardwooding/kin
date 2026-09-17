package namematch

import (
	"strings"
	"testing"
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
