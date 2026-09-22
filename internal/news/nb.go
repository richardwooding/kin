package news

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/place"
)

// NBAPI is Nasjonalbiblioteket's catalogue search.
const NBAPI = "https://api.nb.no/catalog/v1/items"

// NB searches the Norwegian newspapers of Nasjonalbiblioteket. Papers ninety
// years old or more are open to everyone; the search also returns items
// readable only in Norway, which are dropped. Its snippets give the matched
// name and nothing around it.
type NB struct{ *httpx.Polite }

func (*NB) Name() string               { return "nb" }
func (*NB) Covers(r place.Region) bool { return r&place.Norway != 0 }
func (*NB) Context() bool              { return false }
func (*NB) Window(from, to int) (int, int, bool) {
	if open := time.Now().Year() - 90; to > open {
		to = open
	}
	return from, to, from <= to
}

func (*NB) URL(q Query) string {
	v := url.Values{"q": {`"` + q.Phrase + `"`}, "searchType": {"FULL_TEXT_SEARCH"}, "size": {strconv.Itoa(q.max())}}
	v.Add("filter", "mediatype:aviser")
	if q.From > 0 || q.To > 0 {
		v.Add("filter", fmt.Sprintf("date:[%04d0101 TO %04d1231]", q.From, q.To))
	}
	return NBAPI + "?" + v.Encode()
}

func (n *NB) Search(ctx context.Context, q Query) ([]Hit, int, error) {
	u := n.URL(q)
	body, err := n.Get(ctx, u, "application/json")
	if err != nil {
		return nil, 0, err
	}
	var r struct {
		Embedded struct {
			Items []struct {
				ID         string `json:"id"`
				AccessInfo struct {
					AccessAllowedFrom string `json:"accessAllowedFrom"`
					IsPublicDomain    bool   `json:"isPublicDomain"`
				} `json:"accessInfo"`
				Metadata struct {
					Title      string   `json:"title"`
					Series     []string `json:"series"`
					Geographic struct {
						County string `json:"county"`
						City   string `json:"city"`
					} `json:"geographic"`
					OriginInfo struct {
						Issued string `json:"issued"`
					} `json:"originInfo"`
				} `json:"metadata"`
				ContentFragments []struct {
					PageNumber string `json:"pageNumber"`
				} `json:"contentFragments"`
			} `json:"items"`
		} `json:"_embedded"`
		Page struct {
			TotalElements int `json:"totalElements"`
		} `json:"page"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, 0, fmt.Errorf("nb decode: %w", err)
	}
	n.Cache.Put(u, body)
	var out []Hit
	for _, it := range r.Embedded.Items {
		if !it.AccessInfo.IsPublicDomain && it.AccessInfo.AccessAllowedFrom != "EVERYWHERE" {
			continue
		}
		m := it.Metadata
		h := Hit{Source: "nb", ID: it.ID, Paper: strings.TrimSpace(m.Title), Text: q.Phrase,
			URL: "https://www.nb.no/items/" + it.ID}
		if len(m.Series) > 0 {
			h.Paper = strings.TrimSpace(m.Series[0])
		}
		if d := m.OriginInfo.Issued; len(d) == 8 {
			h.Date = d[:4] + "-" + d[4:6] + "-" + d[6:]
			h.Year, _ = strconv.Atoi(d[:4])
		}
		h.Place = strings.Join(nonEmpty(m.Geographic.City, m.Geographic.County), ", ")
		if len(it.ContentFragments) > 0 {
			h.Page = it.ContentFragments[0].PageNumber
		}
		out = append(out, h)
	}
	return out, r.Page.TotalElements, nil
}

func nonEmpty(ss ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
