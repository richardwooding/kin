package riksarkivet

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/richardwooding/kin/internal/model"
)

func TestSweepScoresMemoisesAndReadsEntries(t *testing.T) {
	g := swedes()
	g.Add(&model.Person{ID: "seed:k2", Name: "Karl Nilsson", Gender: "male", Birth: "1885-01-16",
		BirthPlace: "Mjällby, Blekinge, Sverige", Father: "seed:n"})
	searches, entries := 0, 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/register/") {
			entries++
			_, _ = w.Write([]byte(entry))
			return
		}
		searches++
		if strings.Contains(r.URL.RawQuery, "BirthRecord") {
			_, _ = w.Write([]byte(`{"totalHits":1,"items":[{"type":"BirthRecord","id":"B1","metadata":{"date":"1885-01-16","childsName":"Karl Oscar","fathersName":"Johansson, Nils","mothersName":"Andersdotter, Sofia","parish":"Mjällby"},"_links":{"self":"https://data.riksarkivet.se/register/B1"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"totalHits":0,"items":[]}`))
	}, "")
	var got []SweepResult
	c.Sweep(context.Background(), g, []string{"seed:k", "seed:k2"}, SweepOptions{}, func(r SweepResult) { got = append(got, r) })
	if len(got) != 2 || got[0].Err != "" {
		t.Fatalf("results = %+v", got)
	}
	if len(got[0].Hits) != 1 || got[0].Hits[0].Volume != "C I:13 s. 1" {
		t.Errorf("the baptism is kept and its entry read: %+v", got[0].Hits)
	}
	if searches != 3 {
		t.Errorf("the second Karl shares the first's queries; %d searches, want 3", searches)
	}
	if entries != 2 {
		t.Errorf("each kept hit's entry is read once per person: %d", entries)
	}
}
