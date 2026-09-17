// Package tna queries the UK National Archives' Discovery catalogue. Its
// name-indexed military series (Boer War attestations and rolls, First World
// War medal cards and officers' files, navy and air force registers) cover men
// from the Cape and Natal who served under imperial command; its civil series
// hold the wills proved before 1858, the death duty registers, naturalisations
// and police and navy service books; and with the holder filter it searches
// the catalogues of the county record offices that keep the parish registers.
//
// The catalogue is an index: the documents themselves are paid downloads or a
// visit to Kew or the archive that holds them.
package tna

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
)

// API is the Discovery search endpoint (JSON).
const API = "https://discovery.nationalarchives.gov.uk/API/search/records"

// Series lists the name-indexed military series kin war searches, with a
// label. Do not add to it: war searches every entry by default, so a new
// series would silently change what that command does and how long it takes.
var Series = map[string]string{
	"WO 126":  "Boer War: local armed forces attestation papers",
	"WO 127":  "Boer War: local armed forces nominal rolls",
	"WO 128":  "Boer War: Imperial Yeomanry attestation papers",
	"WO 97":   "Soldiers' documents to 1913",
	"WO 372":  "First World War medal index cards",
	"WO 339":  "First World War officers' files",
	"WO 374":  "First World War Territorial and temporary officers' files",
	"ADM 188": "Royal Navy ratings' registers",
	"ADM 159": "Royal Marines registers of service",
	"AIR 79":  "Royal Air Force airmen's records",
	"AIR 76":  "Royal Air Force officers' records",
}

// Civil lists the name-indexed civil series: the wills, death duty registers,
// naturalisations and service books that name ordinary people.
var Civil = map[string]string{
	"PROB 11": "Prerogative Court of Canterbury wills, 1384-1858",
	"IR 26":   "Death duty register abstracts, 1796-1903",
	"ADM 139": "Royal Navy continuous service engagement books, 1853-1872",
	"MEPO 4":  "Metropolitan Police registers of joiners and leavers",
	"HO 334":  "Naturalisation certificates from 1870",
}

// All is every series kin knows by code.
func All() map[string]string {
	out := make(map[string]string, len(Series)+len(Civil))
	for k, v := range Series {
		out[k] = v
	}
	for k, v := range Civil {
		out[k] = v
	}
	return out
}

// Keys lists a series map's codes in a fixed order, so a request built from
// one is always the same.
func Keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Held names the catalogue's holder filters.
const (
	HeldKew       = "TNA" // records at Kew
	HeldElsewhere = "OTH" // catalogues of other archives, such as county record offices
	HeldAll       = "ALL"
)

// Query is one catalogue search.
type Query struct {
	Text     string
	Series   []string // series codes; empty searches the whole catalogue
	HeldBy   string   // HeldKew, HeldElsewhere or HeldAll; empty leaves the default
	DateFrom string   // YYYY-MM-DD
	DateTo   string   // YYYY-MM-DD
	MaxPages int      // 1 by default
	PageSize int      // 100 by default
}

// Record is one catalogue hit.
type Record struct {
	ID          string `json:"id"`
	Reference   string `json:"reference"`
	Series      string `json:"series"`
	Title       string `json:"title,omitempty"` // the collection's own name, where it has one
	Description string `json:"description"`     // what the entry itself is
	Dates       string `json:"dates"`
	HeldBy      string `json:"heldBy,omitempty"` // the archive, when it is not Kew
	URL         string `json:"url"`
}

// Text is everything about a record that a name or place may appear in.
func (r Record) Text() string {
	if r.Title == "" || r.Title == r.Description {
		return r.Description
	}
	if r.Description == "" {
		return r.Title
	}
	return strings.TrimRight(r.Title, ". ") + ". " + r.Description
}

type Client struct {
	HTTP  *http.Client
	Delay time.Duration
	Cache *httpx.Cache // optional; nil fetches every time, as kin war does
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 90 * time.Second}, Delay: 600 * time.Millisecond}
}

// NewCached returns a client keeping every answer under dir.
func NewCached(dir string) *Client {
	c := New()
	c.Cache = httpx.NewCache(dir)
	return c
}

var tags = regexp.MustCompile(`<[^>]+>`)

// clean strips the markup the catalogue wraps matched words in.
func clean(s string) string {
	return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(s, " ")))
}

// Search runs one catalogue query restricted to the given series, following
// pages up to maxPages, and returns the hits with descriptions cleaned of markup.
func (c *Client) Search(ctx context.Context, query string, series []string, maxPages int) ([]Record, int, error) {
	return c.SearchQuery(ctx, Query{Text: query, Series: series, MaxPages: maxPages})
}

// URL is the address of one page of a query's results.
func (q Query) URL(page int) string {
	size := q.PageSize
	if size <= 0 {
		size = 100
	}
	v := url.Values{"sps.searchQuery": {q.Text}, "sps.resultsPageSize": {fmt.Sprint(size)}, "sps.page": {fmt.Sprint(page)}}
	for _, s := range q.Series {
		v.Add("sps.recordSeries", s)
	}
	if q.HeldBy != "" {
		v.Set("sps.heldByCode", q.HeldBy)
	}
	if q.DateFrom != "" {
		v.Set("sps.dateFrom", q.DateFrom)
	}
	if q.DateTo != "" {
		v.Set("sps.dateTo", q.DateTo)
	}
	return API + "?" + v.Encode()
}

// SearchQuery runs a catalogue search with every filter the catalogue offers.
func (c *Client) SearchQuery(ctx context.Context, q Query) ([]Record, int, error) {
	pages := q.MaxPages
	if pages <= 0 {
		pages = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 100
	}
	var out []Record
	total := 0
	for page := 1; page <= pages; page++ {
		u := q.URL(page)
		body, cached, err := c.fetch(ctx, u)
		if err != nil {
			return out, total, err
		}
		var r struct {
			Count   int `json:"count"`
			Records []struct {
				ID            string   `json:"id"`
				Reference     string   `json:"reference"`
				Title         string   `json:"title"`
				Description   string   `json:"description"`
				CoveringDates string   `json:"coveringDates"`
				HeldBy        []string `json:"heldBy"`
			} `json:"records"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return out, total, fmt.Errorf("discovery decode: %w", err)
		}
		c.Cache.Put(u, body)
		total = r.Count
		for _, x := range r.Records {
			ser := x.Reference
			if i := strings.Index(ser, "/"); i > 0 {
				ser = ser[:i]
			}
			rec := Record{ID: x.ID, Reference: x.Reference, Series: ser,
				Title:       clean(x.Title),
				Description: clean(x.Description),
				Dates:       x.CoveringDates, URL: "https://discovery.nationalarchives.gov.uk/details/r/" + x.ID}
			if len(x.HeldBy) > 0 && !strings.Contains(x.HeldBy[0], "The National Archives") {
				rec.HeldBy = x.HeldBy[0]
			}
			out = append(out, rec)
		}
		if len(r.Records) < size || len(out) >= total {
			break
		}
		if !cached {
			time.Sleep(c.Delay)
		}
	}
	return out, total, nil
}

// fetch reads one address, from the cache when it holds it. The pause belongs
// to the caller, and is skipped when nothing was asked of the catalogue.
func (c *Client) fetch(ctx context.Context, u string) (body []byte, cached bool, err error) {
	if b, ok := c.Cache.Get(u); ok {
		return b, true, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", httpx.UserAgent())
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("discovery: %s", resp.Status)
	}
	return b, false, nil
}
