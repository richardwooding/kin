package place

import (
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestOf(t *testing.T) {
	cases := []struct {
		places string
		want   Region
	}{
		{"Stithians, Cornwall, England", Cornwall | England},
		{"Bermondsey, Surrey, England", England},
		{"Cape Town, Cape Colony", SouthAfrica},
		{"Cork, County Cork, Ireland", Ireland},
		{"Edinburgh, Scotland", Scotland},
		{"Aberystwyth, Cardiganshire, Wales", Wales | England},
		{"Llanelli, Carmarthenshire", Wales | England},
		{"Sydney, New South Wales", 0},
		{"Portsmouth, Hampshire, England | Port Elizabeth, Eastern Cape, South Africa", England | SouthAfrica},
		{"", 0},
	}
	for _, c := range cases {
		if got := Of(c.places); got != c.want {
			t.Errorf("Of(%q) = %b, want %b", c.places, got, c.want)
		}
	}
}

func TestUKAndEdition(t *testing.T) {
	if !Of("Redruth, Cornwall, England").UK() {
		t.Error("Cornwall is in the United Kingdom")
	}
	if Of("Graaff-Reinet, Cape Colony").UK() {
		t.Error("the Cape is not")
	}
	cases := map[string]string{
		"Stithians, Cornwall, England": "London",
		"Cardiff, Wales":               "London",
		"Glasgow, Scotland":            "Edinburgh",
		"Cork, Ireland":                "Belfast",
		"Cape Town, Cape Colony":       "",
	}
	for places, want := range cases {
		if got := Of(places).Edition(); got != want {
			t.Errorf("Edition(%q) = %q, want %q", places, got, want)
		}
	}
}

func TestPlacesOfFallsBackToFamily(t *testing.T) {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "p", Name: "Thomas Spargo", Spouses: []string{"w"}})
	g.Add(&model.Person{ID: "w", Name: "Ann Martin", BirthPlace: "Stithians, Cornwall, England"})
	kid := &model.Person{ID: "k", Name: "Mary Spargo", BirthPlace: "Stithians, Cornwall, England", Father: "p"}
	g.Add(kid)
	g.Add(&model.Person{ID: "q", Name: "John Knuckey", BirthPlace: "Gwennap, Cornwall, England"})

	if got := PlacesOf(g, g.Persons["q"], nil); got != "Gwennap, Cornwall, England | " {
		t.Errorf("a person's own place wins: %q", got)
	}
	got := PlacesOf(g, g.Persons["p"], Kids(g)["p"])
	if Of(got)&Cornwall == 0 {
		t.Errorf("a placeless person takes the family's places, got %q", got)
	}
}

func TestDepot(t *testing.T) {
	cases := map[string]string{
		"Johannesburg, Transvaal":  "TAB",
		"Durban, Natal":            "NAB",
		"Bloemfontein, Free State": "VAB",
		"Wynberg, Cape Colony":     "KAB",
		"Springs, Gauteng":         "TAB",
		"unknown":                  "RSA",
	}
	for places, want := range cases {
		if got := Depot(places); got != want {
			t.Errorf("Depot(%q) = %q, want %q", places, got, want)
		}
	}
}

func TestParish(t *testing.T) {
	if got := Parish("Crellow, Stithians, Cornwall, England"); got != "Crellow" {
		t.Errorf("Parish = %q", got)
	}
	if got := Parish("Bermondsey"); got != "Bermondsey" {
		t.Errorf("Parish = %q", got)
	}
}

func TestOfNordic(t *testing.T) {
	cases := []struct {
		places string
		want   Region
	}{
		{"Balestrand, Sogn og Fjordane, Norge", Norway},
		{"Tromsø, Troms", Norway},
		{"Norra Strö, Kristianstads län, Sverige", Sweden},
		{"Ystad, Skåne", Sweden},
		{"Norra Strö, Kristianstads län", Sweden},
		{"Kirke Hyllinge, Frederiksborg amt, Danmark", Denmark},
		{"Ribe", Denmark},
		{"Åbo, Finland", Finland},
		{"Vaasa", Finland},
		{"Reykjavík, Ísland", Iceland},
		{"Bergen, Norway | Cape Town, Cape Colony", Norway | SouthAfrica},
		{"Holland", 0},
		{"Denmark Hill, Camberwell, London, England", England},
		{"Long Island, New York", 0},
		{"Isle of Wight", 0},
		{"Robertsbridge", 0},
	}
	for _, c := range cases {
		if got := Of(c.places); got != c.want {
			t.Errorf("Of(%q) = %b, want %b", c.places, got, c.want)
		}
	}
	if !Of("Odense, Fyn").Nordic() || Of("Stithians, Cornwall").Nordic() {
		t.Error("Nordic covers Denmark and not Cornwall")
	}
}

func TestDistinctDropsCounties(t *testing.T) {
	got := strings.Join(Distinct("Kirke Hyllinge, Frederiksborg amt, Danmark | Norra Strö, Kristianstads län, Skåne | Viborg"), ",")
	if got != "HYLLINGE,STRO,KRISTIANSTADS,VIBORG" {
		t.Errorf("Distinct = %s; the parishes and towns stay, the counties go", got)
	}
}
