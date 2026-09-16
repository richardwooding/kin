// Package tna queries the UK National Archives' Discovery catalogue, whose
// name-indexed military series (Boer War attestations and rolls, First World
// War medal cards and officers' files, navy and air force registers) cover
// men from the Cape and Natal who served under imperial command.
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
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
)

// API is the Discovery search endpoint (JSON).
const API = "https://discovery.nationalarchives.gov.uk/API/search/records"

// Series lists the name-indexed series searched by default, with a label.
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

// Record is one catalogue hit.
type Record struct {
	ID          string `json:"id"`
	Reference   string `json:"reference"`
	Series      string `json:"series"`
	Description string `json:"description"`
	Dates       string `json:"dates"`
	URL         string `json:"url"`
}

type Client struct {
	HTTP  *http.Client
	Delay time.Duration
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 90 * time.Second}, Delay: 600 * time.Millisecond}
}

var tags = regexp.MustCompile(`<[^>]+>`)

// Search runs one catalogue query restricted to the given series, following
// pages up to maxPages, and returns the hits with descriptions cleaned of markup.
func (c *Client) Search(ctx context.Context, query string, series []string, maxPages int) ([]Record, int, error) {
	var out []Record
	total := 0
	for page := 1; page <= maxPages; page++ {
		q := url.Values{"sps.searchQuery": {query}, "sps.resultsPageSize": {"100"}, "sps.page": {fmt.Sprint(page)}}
		for _, s := range series {
			q.Add("sps.recordSeries", s)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, API+"?"+q.Encode(), nil)
		if err != nil {
			return out, total, err
		}
		req.Header.Set("User-Agent", httpx.UserAgent())
		req.Header.Set("Accept", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return out, total, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return out, total, fmt.Errorf("discovery: %s", resp.Status)
		}
		var r struct {
			Count   int `json:"count"`
			Records []struct {
				ID            string `json:"id"`
				Reference     string `json:"reference"`
				Description   string `json:"description"`
				CoveringDates string `json:"coveringDates"`
			} `json:"records"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return out, total, fmt.Errorf("discovery decode: %w", err)
		}
		total = r.Count
		for _, x := range r.Records {
			ser := x.Reference
			if i := strings.Index(ser, "/"); i > 0 {
				ser = ser[:i]
			}
			out = append(out, Record{ID: x.ID, Reference: x.Reference, Series: ser,
				Description: strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(x.Description, " "))),
				Dates:       x.CoveringDates, URL: "https://discovery.nationalarchives.gov.uk/details/r/" + x.ID})
		}
		if len(r.Records) < 100 || len(out) >= total {
			break
		}
		time.Sleep(c.Delay)
	}
	return out, total, nil
}
