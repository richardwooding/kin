package geo

import (
	"strings"
	"testing"
)

func TestNormalise(t *testing.T) {
	cases := map[string]string{
		"Drakenstein, de Caep de Goede Hoop":                                                              "Paarl, Western Cape, South Africa",
		"'t Land van Waveren (Tulbagh from 1804), de Caep de Goede Hoop":                                  "Tulbagh, Western Cape, South Africa",
		"Riet Rivier, Jansenville district (then Uitenhage and Graaff-Reinet), Cape Colony, South Africa": "Riet Rivier, Jansenville, South Africa",
		"Nordhorn, Grafschaft Bentheim, Heiliges Römisches Reich":                                         "Nordhorn, Bentheim, Germany",
		"Delft, Zuid-Holland, Nederland":                                                                  "Delft, South Holland, Netherlands",
		"Stithians, Cornwall, England":                                                                    "Stithians, Cornwall, England",
	}
	for in, want := range cases {
		if got := strings.Join(Normalise(in), ", "); got != want {
			t.Errorf("Normalise(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAccept(t *testing.T) {
	yes := [][2]string{{"place", "town"}, {"place", "village"}, {"boundary", "administrative"}, {"waterway", "river"}, {"natural", "bay"}}
	no := [][2]string{{"amenity", "school"}, {"leisure", "pitch"}, {"office", "diplomatic"}, {"route", "bicycle"}, {"boundary", "maritime"}, {"historic", "boundary_stone"}, {"place", "house"}}
	for _, y := range yes {
		if !Accept(y[0], y[1]) {
			t.Errorf("Accept(%s, %s) = false", y[0], y[1])
		}
	}
	for _, n := range no {
		if Accept(n[0], n[1]) {
			t.Errorf("Accept(%s, %s) = true", n[0], n[1])
		}
	}
}

func TestNormaliseBrackets(t *testing.T) {
	got := Normalise("Cape of Good Hope, [South Africa]")
	want := []string{"Western Cape", "South Africa"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Normalise brackets = %v, want %v", got, want)
	}
	if got := Normalise("Island of Solor in the Indonesian Archipelago"); len(got) != 2 || got[0] != "Solor" {
		t.Errorf("Normalise solor = %v", got)
	}
}

func TestQueries(t *testing.T) {
	qs, precs := queries([]string{"Altena", "Netherlands", "Germany"})
	want := []string{"Altena, Netherlands, Germany", "Altena, Netherlands", "Altena, Germany", "Netherlands, Germany", "Germany"}
	if strings.Join(qs, "|") != strings.Join(want, "|") {
		t.Errorf("queries = %v", qs)
	}
	if strings.Join(precs, "|") != "town|town|town|region|country" {
		t.Errorf("precisions = %v", precs)
	}
	if qs, _ := queries([]string{"Springs", "South Africa"}); len(qs) != 2 {
		t.Errorf("two-part name should not try the bare town: %v", qs)
	}
}

func TestNormaliseDropsEmptyAlias(t *testing.T) {
	got := Normalise("Augsburg, Freie Reichsstadt, Heiliges Römisches Reich")
	if strings.Join(got, "|") != "Augsburg|Germany" {
		t.Errorf("Normalise = %v", got)
	}
}
