// Package riksarkivet searches the Swedish National Archives' open Search API
// (data.riksarkivet.se), whose registers of births and marriages are indexed
// by name, parish and date. The metadata is CC0 and the API needs no key; the
// archive reserves the right to throttle a client, so requests are spaced,
// cached, and backed off when the service pushes back.
//
// The registers are mostly the birth and marriage indexes of Skåne, Blekinge
// and Halland made by Riksarkivet in Lund; the censuses of 1860 to 1930 are
// searchable only on the website, which bars programs, and are offered to the
// user as links by kin leads.
package riksarkivet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/richardwooding/kin/internal/httpx"
)

const API = "https://data.riksarkivet.se/api/records"

// Register types the API indexes by person.
const (
	Birth    = "BirthRecord"
	Marriage = "MarriageRecord"
)

// Query is one search of a register. Limit defaults to 100, the API's own.
type Query struct {
	Name    string
	Type    string
	Place   string
	YearMin int
	YearMax int
	Limit   int
	Offset  int
}

// URL is the address of the query's page of results, and its cache key.
func (q Query) URL() string {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	v := url.Values{"name": {q.Name}, "limit": {strconv.Itoa(limit)}}
	if q.Type != "" {
		v.Set("facet", "ObjectType:Register;Type:"+q.Type)
	}
	if q.Place != "" {
		v.Set("place", q.Place)
	}
	if q.YearMin > 0 {
		v.Set("year_min", strconv.Itoa(q.YearMin))
	}
	if q.YearMax > 0 {
		v.Set("year_max", strconv.Itoa(q.YearMax))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	return API + "?" + v.Encode()
}

// Record is one register entry. Names are as the register writes them,
// "Surname, Given" for adults and the given names alone for a baptised child,
// whose surname the register leaves to the father's.
type Record struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Date   string `json:"date,omitempty"` // yyyy-mm-dd, with 00 for an unknown month or day
	Parish string `json:"parish,omitempty"`
	Child  string `json:"child,omitempty"`
	Father string `json:"father,omitempty"`
	Mother string `json:"mother,omitempty"`
	Groom  string `json:"groom,omitempty"`
	Bride  string `json:"bride,omitempty"`
	URL    string `json:"url"`
	Image  string `json:"image,omitempty"`

	// From the full entry, fetched only for the candidates kept.
	Occupation string `json:"occupation,omitempty"`
	Residence  string `json:"residence,omitempty"`
	Volume     string `json:"volume,omitempty"`
	Archive    string `json:"archive,omitempty"`
	Note       string `json:"note,omitempty"`
}

// Year is the year of the entry, or 0.
func (r Record) Year() int {
	if len(r.Date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(r.Date[:4])
	return y
}

// Text is a one-line summary of the entry.
func (r Record) Text() string {
	var parts []string
	switch r.Type {
	case Birth:
		child := r.Child
		if child == "" {
			child = "an unnamed child"
		}
		parts = append(parts, "birth of "+child)
		if r.Father != "" {
			parts = append(parts, "father "+r.Father)
		}
		if r.Mother != "" {
			parts = append(parts, "mother "+r.Mother)
		}
	case Marriage:
		parts = append(parts, "marriage of "+r.Groom+" and "+r.Bride)
	default:
		parts = append(parts, r.Type)
	}
	if r.Parish != "" {
		parts = append(parts, r.Parish)
	}
	return strings.Join(parts, ", ")
}

type Client struct{ *httpx.Polite }

func New() *Client { return &Client{httpx.NewPolite("riksarkivet")} }

// NewCached returns a client keeping every answer under dir.
func NewCached(dir string) *Client {
	c := New()
	c.Cache = httpx.NewCache(dir)
	return c
}

// Search runs one query and returns its page of entries and the total hits.
func (c *Client) Search(ctx context.Context, q Query) ([]Record, int, error) {
	u := q.URL()
	body, err := c.fetch(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	var r struct {
		TotalHits int `json:"totalHits"`
		Items     []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Metadata struct {
				Date        string `json:"date"`
				Parish      string `json:"parish"`
				ChildsName  string `json:"childsName"`
				FathersName string `json:"fathersName"`
				MothersName string `json:"mothersName"`
				GroomsName  string `json:"groomsName"`
				BridesName  string `json:"bridesName"`
			} `json:"metadata"`
			Links struct {
				Self  string   `json:"self"`
				Image []string `json:"image"`
			} `json:"_links"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, 0, fmt.Errorf("riksarkivet decode: %w", err)
	}
	c.Cache.Put(u, body)
	out := make([]Record, 0, len(r.Items))
	for _, x := range r.Items {
		m := x.Metadata
		rec := Record{ID: x.ID, Type: x.Type, Date: m.Date, Parish: m.Parish,
			Child: m.ChildsName, Father: m.FathersName, Mother: m.MothersName,
			Groom: m.GroomsName, Bride: m.BridesName, URL: x.Links.Self}
		if rec.URL == "" {
			rec.URL = "https://data.riksarkivet.se/register/" + x.ID
		}
		if len(x.Links.Image) > 0 {
			rec.Image = x.Links.Image[0]
		}
		out = append(out, rec)
	}
	return out, r.TotalHits, nil
}

// Detail fills in the occupation, residence and archive reference from the
// full entry.
func (c *Client) Detail(ctx context.Context, r *Record) error {
	body, err := c.fetch(ctx, r.URL)
	if err != nil {
		return err
	}
	var d map[string]any
	if err := json.Unmarshal(body, &d); err != nil {
		return fmt.Errorf("riksarkivet decode: %w", err)
	}
	c.Cache.Put(r.URL, body)
	text := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := d[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	r.Occupation = text("fathersOccupation", "groomsOccupation", "occupation")
	r.Residence = text("fathersPlaceOfResidence", "groomsPlaceOfResidence", "placeOfResidence")
	r.Volume = text("volume")
	r.Archive = text("archive")
	r.Note = text("note")
	if r.Image == "" {
		r.Image = text("image")
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, u string) ([]byte, error) {
	return c.Get(ctx, u, "application/json")
}
