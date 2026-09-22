package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/place"
)

const nbBody = `{"_embedded":{"items":[
 {"id":"2c7e991982a7b432f1a0540ce7ba219f","accessInfo":{"accessAllowedFrom":"EVERYWHERE","isPublicDomain":true},
  "metadata":{"title":"Norsk Kunngjørelsestidende ","series":["Norsk Kunngjørelsestidende"],"geographic":{"county":"Oslo","city":"Oslo"},"originInfo":{"issued":"18850225"}},
  "contentFragments":[{"pageNumber":"1","text":"... <em>Ole Olsen</em> ..."}]},
 {"id":"closed","accessInfo":{"accessAllowedFrom":"NB","isPublicDomain":false},"metadata":{"title":"Aftenposten","originInfo":{"issued":"19940101"}}}]},
 "page":{"totalElements":5842}}`

const kbBody = `[{"recordID":"doms_newspaperCollection:uuid:d0389132-segment_1","familyId":"frederiksborgamtstidende","pageUUID":"doms_aviser_page:uuid:d0389132-841a","lplace":"Hillerød","newspaper_page":"3","timestamp":"1850-01-05T00:00:00.000+00:00",
 "fulltext_org":"Gjoidc- moder A. M. Klem i tfsbenderup , Uuderlagsmand Moricn Larsen, Hmd. Christen Johansen og Niels Mat ien i Beibye, Hmd HanS Jensen i Unucrnp, Hmd. Ole Hansen Jacob Johansen"}]`

const euBody = `{"success":true,"totalResults":47,"items":[
 {"id":"/9200300/BibliographicResource_3000116302376","title":["Altonaer Nachrichten - 1859-02-01"],"edmIsShownAt":["http://anno.onb.ac.at/x"],"dataProvider":["Hamburg State and University Library"]},
 {"id":"/9200303/BibliographicResource_1","title":["Baltijas Vēstnesis - 1881-08-01"],"dataProvider":["National Library of Latvia"]}],
 "hits":[{"scope":"/9200300/BibliographicResource_3000116302376","selectors":[{"prefix":"Heute starb ","exact":"Hans Jensen","suffix":" in Altona."}]}]}`

func serve(t *testing.T, body string, n *int) *httpx.Polite {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*n++
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	p := httpx.NewPolite("test")
	p.Delay, p.Backoff = 0, []time.Duration{0}
	p.HTTP = srv.Client()
	p.HTTP.Transport = rewrite{srv.URL}
	return p
}

type rewrite struct{ base string }

func (rw rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	r, _ := http.NewRequest(req.Method, rw.base+req.URL.RequestURI(), nil)
	r.Header = req.Header
	return http.DefaultTransport.RoundTrip(r)
}

func TestNBDropsClosedItems(t *testing.T) {
	n := 0
	src := &NB{serve(t, nbBody, &n)}
	hits, total, err := src.Search(context.Background(), Query{Phrase: "Ole Olsen", From: 1880, To: 1885})
	if err != nil || total != 5842 || len(hits) != 1 {
		t.Fatalf("%d hits of %d, %v", len(hits), total, err)
	}
	h := hits[0]
	if h.Paper != "Norsk Kunngjørelsestidende" || h.Date != "1885-02-25" || h.Year != 1885 || h.Place != "Oslo" || h.Page != "1" ||
		h.URL != "https://www.nb.no/items/2c7e991982a7b432f1a0540ce7ba219f" || h.Text != "Ole Olsen" {
		t.Errorf("hit = %+v", h)
	}
	u := src.URL(Query{Phrase: "Ole Olsen", From: 1880, To: 1885})
	for _, want := range []string{"q=%22Ole+Olsen%22", "filter=mediatype%3Aaviser", "date%3A%5B18800101+TO+18851231%5D", "FULL_TEXT_SEARCH"} {
		if !strings.Contains(u, want) {
			t.Errorf("%s lacks %s", u, want)
		}
	}
	if _, to, _ := src.Window(1850, 2020); to != time.Now().Year()-90 {
		t.Errorf("only papers ninety years old are open: %d", to)
	}
}

func TestKBKeepsTheWordsAroundTheName(t *testing.T) {
	n := 0
	src := &KB{serve(t, kbBody, &n)}
	hits, _, err := src.Search(context.Background(), Query{Phrase: "Hans Jensen", From: 1850, To: 1860})
	if err != nil || len(hits) != 1 {
		t.Fatalf("%v %v", hits, err)
	}
	h := hits[0]
	if h.Date != "1850-01-05" || h.Place != "Hillerød" || h.Page != "3" || !strings.Contains(h.URL, "mediestream/avis/record/doms_aviser_page:uuid:d0389132-841a") {
		t.Errorf("hit = %+v", h)
	}
	if !strings.Contains(h.Text, "HanS Jensen i Unucrnp") {
		t.Errorf("the OCR spelling HanS is found: %q", h.Text)
	}
	if _, _, ok := src.Window(1890, 1900); ok {
		t.Error("the labs API serves nothing after 1880")
	}
	if !strings.Contains(src.URL(Query{Phrase: "Hans Jensen", From: 1850, To: 1860}), "py%3A%5B1850+TO+1860%5D") {
		t.Error("the year range goes in the query")
	}
}

func TestEuropeanaSplitsTitleAndSnippet(t *testing.T) {
	n := 0
	src := &Europeana{Polite: serve(t, euBody, &n), Key: "k"}
	hits, total, err := src.Search(context.Background(), Query{Phrase: "Hans Jensen", From: 1850, To: 1870})
	if err != nil || total != 47 || len(hits) != 2 {
		t.Fatalf("%d of %d, %v", len(hits), total, err)
	}
	h := hits[0]
	if h.Paper != "Altonaer Nachrichten" || h.Date != "1859-02-01" || h.Year != 1859 || h.URL != "http://anno.onb.ac.at/x" ||
		h.Text != "Heute starb Hans Jensen in Altona." || h.Holder != "Hamburg State and University Library" || h.Place != "" {
		t.Errorf("hit = %+v", h)
	}
	if hits[1].URL != "https://www.europeana.eu/item/9200303/BibliographicResource_1" {
		t.Errorf("without edmIsShownAt the Europeana page is linked: %q", hits[1].URL)
	}
	if u := src.URL(Query{Phrase: "Hans Jensen", From: 1850, To: 1870}); !strings.Contains(u, "proxy_dcterms_issued%3A%5B1850-01-01+TO+1870-12-31%5D") {
		t.Errorf("dates filter through proxy_dcterms_issued: %s", u)
	}
	if _, err := New("europeana", "", "", 0); err == nil {
		t.Error("Europeana without a key is refused")
	}
}

func TestSearchIsCached(t *testing.T) {
	n := 0
	p := serve(t, kbBody, &n)
	p.Cache = httpx.NewCache(t.TempDir())
	src := &KB{p}
	for i := 0; i < 3; i++ {
		if _, _, err := src.Search(context.Background(), Query{Phrase: "Hans Jensen"}); err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Errorf("asked %d times", n)
	}
}

func TestAround(t *testing.T) {
	text := strings.Repeat("x ", 100) + "Hmd HanS Jenfen i Unucrnp" + strings.Repeat(" y", 100)
	got := around(text, "Hans Jensen", 3)
	if got != "x x Hmd HanS Jenfen i Unucrnp y" {
		t.Errorf("around = %q", got)
	}
}

func TestPlanWindows(t *testing.T) {
	p := &model.Person{Name: "Hans Jensen", Birth: "1820", Death: "1875"}
	qs := Plan(p, "Hillerød, Frederiksborg, Danmark", &KB{}, 0)
	if len(qs) != 2 || qs[0].Phrase != "Hans Jensen" || qs[0].From != 1836 || qs[0].To != 1877 || qs[1].From != 1875 || qs[1].To != 1877 {
		t.Errorf("the life and the death, each a query: %+v", qs)
	}
	if qs := Plan(p, "Bergen, Norge", &KB{}, 0); len(qs) != 0 {
		t.Error("a Norwegian gets no Danish query")
	}
	late := &model.Person{Name: "Hans Jensen", Birth: "1870", Death: "1950"}
	if qs := Plan(late, "Danmark", &KB{}, 0); len(qs) != 0 {
		t.Errorf("a man sixteen in 1886 is past the labs API's 1880: %+v", qs)
	}
	if qs := Plan(&model.Person{Name: "Ole Olsen", Death: "1900"}, "Norge", &NB{}, 0); len(qs) != 2 || qs[0].From != 1840 || qs[0].To != 1902 {
		t.Errorf("without a birth, sixty years before death: %+v", qs)
	}
	if qs := Plan(&model.Person{Name: "Ole Olsen"}, "Norge", &NB{}, 0); len(qs) != 0 {
		t.Error("no dates, no query")
	}
}

func TestPeopleByRegion(t *testing.T) {
	g := model.NewGraph()
	g.Add(&model.Person{ID: "me", Name: "Alex Smith", Living: true, Father: "f", Mother: "m"})
	g.Add(&model.Person{ID: "f", Name: "Ole Olsen", Birth: "1850", BirthPlace: "Bergen, Norge"})
	g.Add(&model.Person{ID: "m", Name: "Karen Hansen", Birth: "1852", BirthPlace: "Odense, Danmark"})
	if got := strings.Join(People(g, []Source{&NB{}}, SweepOptions{Root: "me"}), ","); got != "f" {
		t.Errorf("NB covers the Norwegian only: %s", got)
	}
	if got := People(g, []Source{&NB{}, &KB{}}, SweepOptions{Root: "me"}); len(got) != 2 {
		t.Errorf("both sources, both parents: %v", got)
	}
	_ = place.Norway
}
