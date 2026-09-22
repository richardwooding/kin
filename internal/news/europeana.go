package news

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/place"
)

// EuropeanaAPI is Europeana's newspaper full-text search.
const EuropeanaAPI = "https://newspapers.eanadev.org/api/v2/search.json"

// Europeana searches the full text Europe's national libraries have given
// Europeana Newspapers: Austrian, German, Latvian, Serbian and other papers,
// but almost nothing British or Irish. It needs a free key and runs only
// when asked for.
type Europeana struct {
	*httpx.Polite
	Key string
}

func (*Europeana) Name() string                         { return "europeana" }
func (*Europeana) Covers(place.Region) bool             { return true }
func (*Europeana) Context() bool                        { return true }
func (*Europeana) Window(from, to int) (int, int, bool) { return from, to, from <= to }

func (e *Europeana) URL(q Query) string {
	query := `"` + q.Phrase + `"`
	if q.From > 0 || q.To > 0 {
		query += fmt.Sprintf(" AND proxy_dcterms_issued:[%04d-01-01 TO %04d-12-31]", q.From, q.To)
	}
	v := url.Values{"wskey": {e.Key}, "query": {query}, "rows": {strconv.Itoa(q.max())}, "profile": {"minimal,hits"}}
	return EuropeanaAPI + "?" + v.Encode()
}

func (e *Europeana) Search(ctx context.Context, q Query) ([]Hit, int, error) {
	u := e.URL(q)
	body, err := e.Get(ctx, u, "application/json")
	if err != nil {
		return nil, 0, err
	}
	var r struct {
		Success      bool   `json:"success"`
		Error        string `json:"error"`
		TotalResults int    `json:"totalResults"`
		Items        []struct {
			ID           string   `json:"id"`
			Title        []string `json:"title"`
			ShownAt      []string `json:"edmIsShownAt"`
			DataProvider []string `json:"dataProvider"`
		} `json:"items"`
		Hits []struct {
			Scope     string `json:"scope"`
			Selectors []struct {
				Prefix string `json:"prefix"`
				Exact  string `json:"exact"`
				Suffix string `json:"suffix"`
			} `json:"selectors"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, 0, fmt.Errorf("europeana decode: %w", err)
	}
	if !r.Success {
		return nil, 0, fmt.Errorf("europeana: %s", r.Error)
	}
	e.Cache.Put(u, body)
	snippets := map[string][]string{}
	for _, h := range r.Hits {
		for _, s := range h.Selectors {
			snippets[h.Scope] = append(snippets[h.Scope], strings.TrimSpace(s.Prefix+s.Exact+s.Suffix))
		}
	}
	out := make([]Hit, 0, len(r.Items))
	for _, it := range r.Items {
		h := Hit{Source: "europeana", ID: it.ID, Text: strings.Join(snippets[it.ID], " … "),
			URL: "https://www.europeana.eu/item" + it.ID}
		if len(it.ShownAt) > 0 && it.ShownAt[0] != "" {
			h.URL = it.ShownAt[0]
		}
		if len(it.Title) > 0 {
			// titles are "Paper - yyyy-mm-dd"
			title := it.Title[0]
			if i := strings.LastIndex(title, " - "); i > 0 {
				h.Paper, h.Date = title[:i], strings.TrimSpace(title[i+3:])
			} else {
				h.Paper = title
			}
			if len(h.Date) >= 4 {
				h.Year, _ = strconv.Atoi(h.Date[:4])
			}
		}
		if len(it.DataProvider) > 0 {
			h.Holder = it.DataProvider[0]
		}
		out = append(out, h)
	}
	return out, r.TotalResults, nil
}
