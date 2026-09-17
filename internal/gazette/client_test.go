package gazette

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pageFeed is the shape the feed really returns for a scanned page: counts as
// strings, links as objects with @href, the matched words wrapped in markup.
const pageFeed = `{
 "id": "https://www.thegazette.co.uk/all-notices/notice/data.json?text=x",
 "title": "Search Result",
 "f:page-number": "1", "f:page-size": "2", "f:page-start": "1", "f:page-stop": "1", "f:total": "1",
 "entry": [
  {
   "id": "https://www.thegazette.co.uk/London/issue/34159/page/3065",
   "title": "The London Gazette, Issue 34159, Page 3065",
   "link": [{"@href": "/London/issue/34159/page/3065/data.pdf", "@rel": "self"}],
   "author": {"name": "tso"},
   "updated": "2014-03-24T19:33:53Z",
   "published": "1935-05-10T00:00:00",
   "content": "<div><p>…War Department Clerks (Special), Charles Henry <em class=\"highlight\">Knuckey, John</em> Thomas Lewis&…</p></div>"
  }
 ]
}`

// noticeFeed is a modern deceased estates notice: a notice code, the dead
// person's name as the title, and no issue or page.
const noticeFeed = `{
 "f:total": 3, "f:page-number": 1, "f:page-size": 1,
 "entry": [
  {
   "id": "https://www.thegazette.co.uk/id/notice/L-57776-035",
   "f:status": "published",
   "f:notice-code": "2903",
   "title": "Ronald KNUCKEY",
   "link": [{"@href": "https://www.thegazette.co.uk/notice/L-57776-035"}],
   "published": "2005-10-04T00:00:00",
   "content": "KNUCKEY, Ronald, late of Truro, Cornwall, died 4 August 2005; claims to the solicitors."
  }
 ]
}`

// objectFeed is the single-result shape: entry is an object, not a list.
const objectFeed = `{"f:total": "1", "entry": {"id": "https://www.thegazette.co.uk/id/notice/L-1-1", "title": "One", "published": "1999-01-02T00:00:00", "content": "text"}}`

const emptyFeed = `{"f:total": "0", "entry": null}`

func TestQueryURL(t *testing.T) {
	q := Query{
		Service: All, Text: `"Wooding, Charles"`,
		StartPublish: "1880-01-01", EndPublish: "1905-12-31",
		NoticeTypes: []string{"2903", "2450"}, Edition: "London", PageSize: 10, Page: 2,
	}
	u := q.URL()
	if !strings.HasPrefix(u, Host+"/all-notices/notice/data.json?") {
		t.Fatalf("URL = %q", u)
	}
	for _, want := range []string{
		"text=%22Wooding%2C+Charles%22",
		"start-publish-date=1880-01-01",
		"end-publish-date=1905-12-31",
		"noticetype=2903+2450", // the feed's own separator, written by Encode as +
		"edition=London",
		"results-page-size=10",
		"results-page=2",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("URL %q is missing %q", u, want)
		}
	}
	if got := (Query{}).URL(); got != Host+"/all-notices/notice/data.json?results-page-size=50" {
		t.Errorf("the zero query defaults to all notices: %q", got)
	}
	if (Query{Text: "a", Page: 1}).URL() != (Query{Text: "a"}).URL() {
		t.Error("page one is the default and should not change the cache key")
	}
}

func TestDecodePageEntry(t *testing.T) {
	f, err := decode([]byte(pageFeed))
	if err != nil {
		t.Fatal(err)
	}
	if f.Total != 1 || f.Page != 1 || f.PageSize != 2 {
		t.Errorf("counts written as strings must decode: %+v", f)
	}
	if len(f.Entries) != 1 {
		t.Fatalf("entries = %d", len(f.Entries))
	}
	n := f.Entries[0].Notice()
	if n.Edition != "London" || n.Issue != "34159" || n.Page != "3065" {
		t.Errorf("edition, issue and page come from the address: %+v", n)
	}
	if n.Published != "1935-05-10" || n.Year != 1935 {
		t.Errorf("published = %q, year = %d", n.Published, n.Year)
	}
	if !n.Scanned() {
		t.Error("a page of an old issue is scanned print")
	}
	if strings.Contains(n.Text, "<") || !strings.Contains(n.Text, "Knuckey, John") {
		t.Errorf("markup is stripped and the words kept: %q", n.Text)
	}
	if !strings.Contains(n.Text, "Lewis&") {
		t.Errorf("entities are unescaped: %q", n.Text)
	}
	if n.PDF != Host+"/London/issue/34159/page/3065/data.pdf" {
		t.Errorf("PDF = %q", n.PDF)
	}
}

func TestDecodeNoticeEntry(t *testing.T) {
	f, err := decode([]byte(noticeFeed))
	if err != nil {
		t.Fatal(err)
	}
	if f.Total != 3 {
		t.Errorf("counts written as numbers must decode too: %d", f.Total)
	}
	n := f.Entries[0].Notice()
	if n.Code != "2903" || n.Scanned() {
		t.Errorf("a modern notice carries its code and is not scanned: %+v", n)
	}
	if n.Title != "Ronald KNUCKEY" || n.Year != 2005 {
		t.Errorf("notice = %+v", n)
	}
}

func TestDecodeEntryAsObject(t *testing.T) {
	f, err := decode([]byte(objectFeed))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Notice().Title != "One" {
		t.Errorf("a single result arrives as an object, not a list: %+v", f.Entries)
	}
	f, err = decode([]byte(emptyFeed))
	if err != nil || len(f.Entries) != 0 || f.Total != 0 {
		t.Errorf("an empty feed decodes to nothing: %+v, %v", f, err)
	}
}

func TestFetchUsesCache(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "kin/") {
			t.Errorf("user agent = %q", got)
		}
		_, _ = w.Write([]byte(pageFeed))
	}))
	defer srv.Close()

	c := New(t.TempDir())
	c.Delay = 0
	c.HTTP = srv.Client()
	q := Query{Text: "Knuckey"}
	// point the client at the test server by rewriting the request address
	c.HTTP.Transport = rewrite{srv.URL}

	for i := 0; i < 3; i++ {
		notices, total, err := c.Search(context.Background(), q, 1)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(notices) != 1 {
			t.Fatalf("search returned %d of %d", len(notices), total)
		}
	}
	if n != 1 {
		t.Errorf("the same query was fetched %d times; the cache should answer after the first", n)
	}
}

func TestFetchReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := New("")
	c.Delay = 0
	c.HTTP = srv.Client()
	c.HTTP.Transport = rewrite{srv.URL}
	if _, _, err := c.Search(context.Background(), Query{Text: "x"}, 1); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Errorf("a failing service should be reported: %v", err)
	}
}

// rewrite sends every request to the test server instead of the real host.
type rewrite struct{ base string }

func (rw rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	srv, _ := http.NewRequest(req.Method, rw.base+u.RequestURI(), nil)
	srv.Header = req.Header
	return http.DefaultTransport.RoundTrip(srv)
}
