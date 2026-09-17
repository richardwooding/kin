package tna

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestSeriesStaysMilitary guards kin war: it searches every entry of Series by
// default, so a civil series added there would silently change what that
// command does.
func TestSeriesStaysMilitary(t *testing.T) {
	want := map[string]bool{
		"WO 126": true, "WO 127": true, "WO 128": true, "WO 97": true, "WO 372": true,
		"WO 339": true, "WO 374": true, "ADM 188": true, "ADM 159": true, "AIR 79": true, "AIR 76": true,
	}
	if len(Series) != len(want) {
		t.Fatalf("Series holds %d entries, want %d", len(Series), len(want))
	}
	for code := range Series {
		if !want[code] {
			t.Errorf("%s does not belong in the military series kin war searches", code)
		}
	}
	for code := range Civil {
		if Series[code] != "" {
			t.Errorf("%s is in both maps", code)
		}
	}
	if len(All()) != len(Series)+len(Civil) {
		t.Error("All is the union of the two")
	}
	if strings.Join(Keys(Civil), ",") != "ADM 139,HO 334,IR 26,MEPO 4,PROB 11" {
		t.Errorf("Keys must be sorted, got %v", Keys(Civil))
	}
}

// TestLegacySearchParams pins the request kin war has always sent.
func TestLegacySearchParams(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"count":0,"records":[]}`))
	}))
	defer srv.Close()
	c := New()
	c.Delay = 0
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}

	if _, _, err := c.Search(context.Background(), "Nuns Lewis", []string{"WO 372", "WO 97"}, 1); err != nil {
		t.Fatal(err)
	}
	if got.Get("sps.searchQuery") != "Nuns Lewis" || got.Get("sps.resultsPageSize") != "100" || got.Get("sps.page") != "1" {
		t.Errorf("parameters changed: %v", got)
	}
	if series := got["sps.recordSeries"]; len(series) != 2 || series[0] != "WO 372" {
		t.Errorf("series = %v", series)
	}
	for _, unwanted := range []string{"sps.heldByCode", "sps.dateFrom", "sps.dateTo"} {
		if got.Has(unwanted) {
			t.Errorf("the legacy search must not send %s", unwanted)
		}
	}
}

func TestSearchQueryOptions(t *testing.T) {
	q := Query{Text: "Knuckey Stithians", HeldBy: HeldElsewhere, DateFrom: "1780-01-01", DateTo: "1860-12-31", PageSize: 20}
	u := q.URL(3)
	for _, want := range []string{
		"sps.searchQuery=Knuckey+Stithians",
		"sps.heldByCode=OTH",
		"sps.dateFrom=1780-01-01",
		"sps.dateTo=1860-12-31",
		"sps.resultsPageSize=20",
		"sps.page=3",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("URL %q is missing %q", u, want)
		}
	}
}

func TestDecodeRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"count":2,"records":[
			{"id":"D123","reference":"PROB 11/1678/61","description":"Will of Octavius <span>Wooding</span> of Luton , Bedfordshire","coveringDates":"22 November 1823"},
			{"id":"D456","reference":"X674","description":"Knuckey family of Stithians.","coveringDates":"1903-1968"}]}`))
	}))
	defer srv.Close()
	c := New()
	c.Delay = 0
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}

	recs, total, err := c.SearchQuery(context.Background(), Query{Text: "x"})
	if err != nil || total != 2 || len(recs) != 2 {
		t.Fatalf("%d of %d, %v", len(recs), total, err)
	}
	if recs[0].Series != "PROB 11" {
		t.Errorf("the series comes from the reference: %q", recs[0].Series)
	}
	if strings.Contains(recs[0].Description, "<") {
		t.Errorf("markup is stripped: %q", recs[0].Description)
	}
	if recs[0].URL != "https://discovery.nationalarchives.gov.uk/details/r/D123" {
		t.Errorf("URL = %q", recs[0].URL)
	}
}

func TestCacheAnswersSecondSearch(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		_, _ = w.Write([]byte(`{"count":1,"records":[{"id":"D1","reference":"PROB 11/1/1","description":"Will of John Knuckey of Stithians","coveringDates":"1763"}]}`))
	}))
	defer srv.Close()
	c := NewCached(t.TempDir())
	c.Delay = 0
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}

	for i := 0; i < 3; i++ {
		if _, _, err := c.SearchQuery(context.Background(), Query{Text: "Knuckey John"}); err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Errorf("the catalogue was asked %d times; the cache should answer after the first", n)
	}
	if New().Cache != nil {
		t.Error("the plain client caches nothing, as kin war expects")
	}
}

type rewrite struct{ base string }

func (rw rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	srv, _ := http.NewRequest(req.Method, rw.base+u.RequestURI(), nil)
	srv.Header = req.Header
	return http.DefaultTransport.RoundTrip(srv)
}
