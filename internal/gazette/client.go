// Package gazette queries The Gazette (thegazette.co.uk), the official public
// record of the United Kingdom since 1665. Its notices name the dead and their
// executors, bankrupts, dissolved partnerships, naturalised subjects and
// officers commissioned or decorated, which makes it one of the few free
// British sources with an open interface a program may ask directly.
//
// The feed needs no key. Its content is Crown copyright published under the
// Open Government Licence v3.0, and its fair use policy asks callers to be
// reasonable, so this client sends one request at a time with a pause between
// them and keeps every answer in an on-disk cache.
package gazette

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
)

// Host is the service's base address.
const Host = "https://www.thegazette.co.uk"

// The three notice services. All covers every notice and, through the scanned
// pages, every issue back to 1665; Probate is the modern structured deceased
// estates notice (type 2903), which begins in the late 1990s.
const (
	All        = "all-notices"
	Probate    = "wills-and-probate"
	Insolvency = "insolvency"
)

// Query is one search of the notice feed.
type Query struct {
	Service      string   // All, Probate or Insolvency; empty means All
	Text         string   // words to find; quote a phrase to hold it together
	StartPublish string   // YYYY-MM-DD
	EndPublish   string   // YYYY-MM-DD
	StartDeath   string   // YYYY-MM-DD, honoured by deceased estates notices only
	EndDeath     string   // YYYY-MM-DD
	NoticeTypes  []string // four-digit notice codes
	Category     string   // two-digit category code
	Edition      string   // London, Edinburgh or Belfast
	Page         int      // 1 based
	PageSize     int      // 50 by default
	Sort         string   // latest-date or oldest-date
}

// URL is the address of the JSON feed for this query. Values are encoded with
// sorted keys, so the same query always yields the same cache key.
func (q Query) URL() string {
	service := q.Service
	if service == "" {
		service = All
	}
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	set("text", q.Text)
	set("start-publish-date", q.StartPublish)
	set("end-publish-date", q.EndPublish)
	set("start-date-of-death", q.StartDeath)
	set("end-date-of-death", q.EndDeath)
	set("categorycode", q.Category)
	set("edition", q.Edition)
	set("sort-by", q.Sort)
	if len(q.NoticeTypes) > 0 {
		// the feed separates codes with +, which is how Encode writes a space
		v.Set("noticetype", strings.Join(q.NoticeTypes, " "))
	}
	size := q.PageSize
	if size <= 0 {
		size = 50
	}
	v.Set("results-page-size", strconv.Itoa(size))
	if q.Page > 1 {
		v.Set("results-page", strconv.Itoa(q.Page))
	}
	return Host + "/" + service + "/notice/data.json?" + v.Encode()
}

// Client fetches the notice feed.
type Client struct {
	HTTP  *http.Client
	Delay time.Duration
	Cache *httpx.Cache
}

// New returns a client caching under dir; an empty dir disables the cache.
func New(dir string) *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: 90 * time.Second},
		Delay: 1100 * time.Millisecond,
		Cache: httpx.NewCache(dir),
	}
}

// Fetch runs one query. A cached answer is returned without a request, and
// without the pause, so a resumed sweep costs the service nothing.
func (c *Client) Fetch(ctx context.Context, q Query) (*Feed, error) {
	u := q.URL()
	if b, ok := c.Cache.Get(u); ok {
		return decode(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", httpx.UserAgent())
	// no Accept header: the data.json path already chooses the format, and the
	// service answers 500 when asked to negotiate it as well
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	time.Sleep(c.Delay)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gazette: %s", resp.Status)
	}
	if err != nil {
		return nil, err
	}
	feed, err := decode(b)
	if err != nil {
		return nil, err
	}
	c.Cache.Put(u, b)
	return feed, nil
}

// Search reads up to maxPages of results and reports the total the service
// says it holds, so a caller can see when it stopped short.
func (c *Client) Search(ctx context.Context, q Query, maxPages int) ([]Notice, int, error) {
	if maxPages <= 0 {
		maxPages = 1
	}
	var out []Notice
	total := 0
	for page := 1; page <= maxPages; page++ {
		q.Page = page
		feed, err := c.Fetch(ctx, q)
		if err != nil {
			return out, total, err
		}
		total = feed.Total
		for _, e := range feed.Entries {
			out = append(out, e.Notice())
		}
		if len(feed.Entries) == 0 || len(out) >= total {
			break
		}
	}
	return out, total, nil
}

// Feed is one page of results.
type Feed struct {
	Total    int
	Page     int
	PageSize int
	Entries  []Entry
}

// Entry is one result: a scanned page of an old issue, or a modern notice.
type Entry struct {
	ID        string
	Title     string
	Published string
	Content   string
	Code      string
	Links     []Link
}

// Link is one of an entry's alternative representations.
type Link struct {
	Href  string `json:"@href"`
	Rel   string `json:"@rel"`
	Type  string `json:"@type"`
	Title string `json:"@title"`
}

// Notice is an entry reduced to what a researcher needs to judge it.
type Notice struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Published string `json:"published"`         // YYYY-MM-DD
	Year      int    `json:"year,omitempty"`    // 0 when the date is unreadable
	Edition   string `json:"edition,omitempty"` // London, Edinburgh or Belfast
	Issue     string `json:"issue,omitempty"`
	Page      string `json:"page,omitempty"`
	Code      string `json:"code,omitempty"` // notice type, modern notices only
	Text      string `json:"text"`           // the matched text, markup removed
	URL       string `json:"url"`
	PDF       string `json:"pdf,omitempty"`
}

// Scanned reports whether the entry is optical character recognition of a
// printed page rather than a notice filed as text.
func (n Notice) Scanned() bool { return n.Code == "" && n.Issue != "" }

var pagePath = regexp.MustCompile(`/(London|Edinburgh|Belfast)/issue/([0-9]+)(?:/page/([0-9]+))?`)

// Notice reduces an entry, reading the edition, issue and page from its address.
func (e Entry) Notice() Notice {
	n := Notice{ID: e.ID, Title: strip(e.Title), Code: e.Code, Text: strip(e.Content), URL: e.ID}
	if len(e.Published) >= 10 {
		n.Published = e.Published[:10]
		if y, err := strconv.Atoi(n.Published[:4]); err == nil {
			n.Year = y
		}
	}
	if m := pagePath.FindStringSubmatch(e.ID); m != nil {
		n.Edition, n.Issue, n.Page = m[1], m[2], m[3]
	}
	for _, l := range e.Links {
		if strings.HasSuffix(l.Href, ".pdf") {
			n.PDF = abs(l.Href)
			break
		}
	}
	return n
}

func abs(href string) string {
	if strings.HasPrefix(href, "/") {
		return Host + href
	}
	return href
}

var tags = regexp.MustCompile("<[^>]+>")
var spaces = regexp.MustCompile(`\s+`)

// strip removes the markup the feed wraps matched words in.
func strip(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(tags.ReplaceAllString(s, " ")), " "))
}

// The feed writes its counts as strings, its single result as an object
// rather than a list, and some of its text fields as objects, so every
// changeable shape is decoded through a tolerant type.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil // a count we cannot read is not worth failing a search for
	}
	*f = flexInt(n)
	return nil
}

type flexText string

func (f *flexText) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = flexText(s)
		return nil
	}
	var o struct {
		Text string `json:"#text"`
	}
	if json.Unmarshal(b, &o) == nil {
		*f = flexText(o.Text)
	}
	return nil
}

type flexLinks []Link

func (f *flexLinks) UnmarshalJSON(b []byte) error {
	var many []Link
	if json.Unmarshal(b, &many) == nil {
		*f = many
		return nil
	}
	var one Link
	if json.Unmarshal(b, &one) == nil {
		*f = []Link{one}
	}
	return nil
}

type rawEntry struct {
	ID        string    `json:"id"`
	Title     flexText  `json:"title"`
	Published string    `json:"published"`
	Content   flexText  `json:"content"`
	Code      string    `json:"f:notice-code"`
	Links     flexLinks `json:"link"`
}

func decode(b []byte) (*Feed, error) {
	var raw struct {
		Total    flexInt         `json:"f:total"`
		Page     flexInt         `json:"f:page-number"`
		PageSize flexInt         `json:"f:page-size"`
		Entries  json.RawMessage `json:"entry"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("gazette decode: %w", err)
	}
	f := &Feed{Total: int(raw.Total), Page: int(raw.Page), PageSize: int(raw.PageSize)}
	if len(raw.Entries) == 0 || string(raw.Entries) == "null" {
		return f, nil
	}
	var many []rawEntry
	if err := json.Unmarshal(raw.Entries, &many); err != nil {
		var one rawEntry
		if err2 := json.Unmarshal(raw.Entries, &one); err2 != nil {
			return f, fmt.Errorf("gazette decode entries: %w", err)
		}
		many = []rawEntry{one}
	}
	for _, e := range many {
		f.Entries = append(f.Entries, Entry{
			ID: e.ID, Title: string(e.Title), Published: e.Published,
			Content: string(e.Content), Code: e.Code, Links: e.Links,
		})
	}
	return f, nil
}
