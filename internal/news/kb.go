package news

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/place"
)

// KBAPI is the Royal Danish Library's experimental labs API, served over
// plain HTTP only.
const KBAPI = "http://labs.statsbiblioteket.dk/labsapi/api/aviser/export/fields"

// KB searches the Danish newspapers in Mediestream through the labs API,
// which serves only papers more than 140 years old, so 1880 and before, as
// Public Domain Mark. Later years are in Mediestream, which kin leads links.
type KB struct{ *httpx.Polite }

const kbLast = 1880

func (*KB) Name() string               { return "kb" }
func (*KB) Covers(r place.Region) bool { return r&place.Denmark != 0 }
func (*KB) Context() bool              { return true }
func (*KB) Window(from, to int) (int, int, bool) {
	if to > kbLast {
		to = kbLast
	}
	return from, to, from <= to
}

func (*KB) URL(q Query) string {
	query := `"` + q.Phrase + `"`
	if q.From > 0 || q.To > 0 {
		query += fmt.Sprintf(" AND py:[%d TO %d]", q.From, q.To)
	}
	v := url.Values{"query": {query}, "max": {strconv.Itoa(q.max())}, "format": {"JSON"}}
	for _, f := range []string{"recordID", "timestamp", "familyId", "newspaper_page", "lplace", "pageUUID", "fulltext_org"} {
		v.Add("fields", f)
	}
	return KBAPI + "?" + v.Encode()
}

func (k *KB) Search(ctx context.Context, q Query) ([]Hit, int, error) {
	u := k.URL(q)
	body, err := k.Get(ctx, u, "application/json")
	if err != nil {
		return nil, 0, err
	}
	var rows []struct {
		RecordID  string `json:"recordID"`
		Timestamp string `json:"timestamp"`
		FamilyID  string `json:"familyId"`
		Page      string `json:"newspaper_page"`
		Place     string `json:"lplace"`
		PageUUID  string `json:"pageUUID"`
		Text      string `json:"fulltext_org"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, 0, fmt.Errorf("kb decode: %w", err)
	}
	k.Cache.Put(u, body)
	out := make([]Hit, 0, len(rows))
	for _, r := range rows {
		h := Hit{Source: "kb", ID: r.RecordID, Paper: r.FamilyID, Page: r.Page, Place: r.Place,
			Text: around(r.Text, q.Phrase, 40)}
		if len(r.Timestamp) >= 10 {
			h.Date = r.Timestamp[:10]
			h.Year, _ = strconv.Atoi(r.Timestamp[:4])
		}
		if r.PageUUID != "" {
			h.URL = "https://www2.statsbiblioteket.dk/mediestream/avis/record/" + url.PathEscape(r.PageUUID)
		}
		out = append(out, h)
	}
	return out, len(out), nil
}
