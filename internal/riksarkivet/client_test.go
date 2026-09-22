package riksarkivet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const births = `{"totalHits":628,"hits":2,"offset":0,"items":[
 {"type":"BirthRecord","metadata":{"date":"1885-01-16","childsName":"Nils Oscar","fathersName":"Johansson, Nils","mothersName":"Andersdotter, Sofia","parish":"Mjällby"},
  "id":"HcyoChOvigzE3KnEJ8u","objectType":"Register","caption":"Nils Oscar",
  "_links":{"self":"https://data.riksarkivet.se/register/HcyoChOvigzE3KnEJ8u","image":["https://lbiiif.riksarkivet.se/arkis!00133784/manifest"]}},
 {"type":"MarriageRecord","metadata":{"groomsName":"Johansson, Nils Magnus","bridesName":"Persdotter, Bengta","parish":"Nymö","date":"1874-11-21"},
  "id":"LcbdSsLiNpCpCpatC0","objectType":"Register","_links":{"self":"https://data.riksarkivet.se/register/LcbdSsLiNpCpCpatC0"}}]}`

const entry = `{"childsName":"Nils Oscar","date":"1885-01-16","parish":"Mjällby","fathersName":"Johansson, Nils","fathersOccupation":"fiskare","fathersPlaceOfResidence":"N-o 6 Tossö","mothersName":"Andersdotter, Sofia","note":"Fadern 34 år.","volume":"C I:13 s. 1","archive":"Mjällby kyrkoarkiv (SE/LLA/13269)","type":"BirthRecord"}`

func testClient(t *testing.T, h http.HandlerFunc, cache string) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New()
	if cache != "" {
		c = NewCached(cache)
	}
	c.Delay = 0
	c.Backoff = []time.Duration{0, 0}
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}
	return c
}

func TestQueryURL(t *testing.T) {
	u := Query{Name: "Nils Johansson", Type: Birth, YearMin: 1880, YearMax: 1885}.URL()
	for _, want := range []string{"name=Nils+Johansson", "facet=ObjectType%3ARegister%3BType%3ABirthRecord", "year_min=1880", "year_max=1885", "limit=100"} {
		if !strings.Contains(u, want) {
			t.Errorf("%s lacks %s", u, want)
		}
	}
}

func TestSearchDecodes(t *testing.T) {
	var ua string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		ua = r.UserAgent()
		_, _ = w.Write([]byte(births))
	}, "")
	recs, total, err := c.Search(context.Background(), Query{Name: "Nils Johansson"})
	if err != nil || total != 628 || len(recs) != 2 {
		t.Fatalf("%d of %d, %v", len(recs), total, err)
	}
	if !strings.HasPrefix(ua, "kin/") {
		t.Errorf("requests identify kin, got %q", ua)
	}
	b := recs[0]
	if b.Child != "Nils Oscar" || b.Father != "Johansson, Nils" || b.Parish != "Mjällby" || b.Year() != 1885 || b.Image == "" {
		t.Errorf("birth = %+v", b)
	}
	if m := recs[1]; m.Groom != "Johansson, Nils Magnus" || m.Bride != "Persdotter, Bengta" || m.URL == "" {
		t.Errorf("marriage = %+v", m)
	}
}

func TestDetailFillsReference(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(entry)) }, "")
	r := Record{URL: "https://data.riksarkivet.se/register/HcyoChOvigzE3KnEJ8u"}
	if err := c.Detail(context.Background(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Volume != "C I:13 s. 1" || r.Archive != "Mjällby kyrkoarkiv (SE/LLA/13269)" || r.Occupation != "fiskare" || r.Residence != "N-o 6 Tossö" {
		t.Errorf("detail = %+v", r)
	}
}

func TestCacheAnswersSecondSearch(t *testing.T) {
	n := 0
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		_, _ = w.Write([]byte(births))
	}, t.TempDir())
	for i := 0; i < 3; i++ {
		if _, _, err := c.Search(context.Background(), Query{Name: "Nils Johansson"}); err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Errorf("the archive was asked %d times; the cache should answer after the first", n)
	}
}

func TestBacksOffWhenThrottled(t *testing.T) {
	n := 0
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(births))
	}, t.TempDir())
	if _, _, err := c.Search(context.Background(), Query{Name: "Nils Johansson"}); err != nil {
		t.Fatalf("two throttled answers are retried: %v", err)
	}
	if n != 3 {
		t.Errorf("asked %d times, want 3", n)
	}

	n = -10
	if _, _, err := c.Search(context.Background(), Query{Name: "Anders Persson"}); err == nil {
		t.Error("the client gives up once the back-off schedule is spent")
	}
}

func TestErrorIsNotCached(t *testing.T) {
	n := 0
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(births))
	}, t.TempDir())
	if _, _, err := c.Search(context.Background(), Query{Name: "Nils Johansson"}); err == nil {
		t.Fatal("a 404 is an error")
	}
	if _, _, err := c.Search(context.Background(), Query{Name: "Nils Johansson"}); err != nil {
		t.Errorf("the failure was not cached: %v", err)
	}
}

type rewrite struct{ base string }

func (rw rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	srv, _ := http.NewRequest(req.Method, rw.base+u.RequestURI(), nil)
	srv.Header = req.Header
	return http.DefaultTransport.RoundTrip(srv)
}
