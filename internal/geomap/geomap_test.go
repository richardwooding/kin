package geomap

import "testing"

func TestHistoricName(t *testing.T) {
	names := []string{"Graaff-Reinet, Cape Colony", "Graaff Reinet, Cape Colony, South Africa", "Graaff-Reinet, Cape Colony, South Africa", "48 Eighth Avenue, Graaff-Reinet"}
	if got := historicName(names); got != "Graaff-Reinet" {
		t.Errorf("historicName = %q", got)
	}
	if got := historicName([]string{"Riet Rivier, Jansenville district (then Uitenhage), Cape Colony"}); got != "Riet Rivier" {
		t.Errorf("historicName = %q", got)
	}
	if got := historicName(nil); got != "" {
		t.Errorf("historicName(nil) = %q", got)
	}
}

func TestFirstPart(t *testing.T) {
	if got := firstPart("Robert Sobukwe Town, Dr Beyers Naudé Local Municipality, South Africa"); got != "Robert Sobukwe Town" {
		t.Errorf("firstPart = %q", got)
	}
}
