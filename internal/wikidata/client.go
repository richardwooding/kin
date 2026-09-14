// Package wikidata queries the Wikidata SPARQL endpoint and search API.
package wikidata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/model"
)

const (
	SPARQL = "https://query.wikidata.org/sparql"
	API    = "https://www.wikidata.org/w/api.php"
)

type Client struct {
	HTTP *http.Client
}

func New() *Client { return &Client{HTTP: &http.Client{Timeout: 120 * time.Second}} }

type binding map[string]struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (b binding) get(k string) string { return b[k].Value }

// Query runs a SPARQL query and returns the raw bindings.
func (c *Client) Query(ctx context.Context, sparql string) ([]binding, error) {
	q := url.Values{"query": {sparql}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, SPARQL, strings.NewReader(q.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", httpx.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("wikidata sparql: %s: %s", resp.Status, truncate(string(body), 400))
	}
	var out struct {
		Results struct {
			Bindings []binding `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("wikidata sparql decode: %w", err)
	}
	return out.Results.Bindings, nil
}

// SearchHumans uses CirrusSearch to find humans whose label contains term.
func (c *Client) SearchHumans(ctx context.Context, term string, max int) ([]string, error) {
	var ids []string
	offset := 0
	for len(ids) < max {
		q := url.Values{
			"action":   {"query"},
			"list":     {"search"},
			"srsearch": {fmt.Sprintf("inlabel:%q haswbstatement:P31=Q5", term)},
			"srlimit":  {"50"},
			"sroffset": {fmt.Sprint(offset)},
			"format":   {"json"},
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, API+"?"+q.Encode(), nil)
		req.Header.Set("User-Agent", httpx.UserAgent)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return ids, err
		}
		var out struct {
			Continue *struct {
				Sroffset int `json:"sroffset"`
			} `json:"continue"`
			Query struct {
				Search []struct {
					Title string `json:"title"`
				} `json:"search"`
			} `json:"query"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil {
			return ids, err
		}
		for _, s := range out.Query.Search {
			ids = append(ids, s.Title)
		}
		if out.Continue == nil || len(out.Query.Search) == 0 {
			break
		}
		offset = out.Continue.Sroffset
	}
	return model.Uniq(ids), nil
}

// BySurname returns QIDs of humans whose family name (P734) has the given label.
func (c *Client) BySurname(ctx context.Context, surname string) ([]string, error) {
	sparql := fmt.Sprintf(`SELECT DISTINCT ?p WHERE {
  ?fn wdt:P31 wd:Q101352 ; rdfs:label %q@en .
  ?p wdt:P734 ?fn ; wdt:P31 wd:Q5 .
}`, surname)
	rows, err := c.Query(ctx, sparql)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, qid(r.get("p")))
	}
	return ids, nil
}

// ByBirthplace returns QIDs of humans born in a place whose English label starts
// with placePrefix, inside country (a QID such as Q258 for South Africa).
func (c *Client) ByBirthplace(ctx context.Context, placePrefix, country string) ([]string, map[string]string, error) {
	sparql := fmt.Sprintf(`SELECT DISTINCT ?p ?place ?placeLabel WHERE {
  ?place wdt:P17 wd:%s ; rdfs:label ?placeLabel .
  FILTER(LANG(?placeLabel) = "en" && STRSTARTS(?placeLabel, %q))
  ?p wdt:P19 ?place ; wdt:P31 wd:Q5 .
}`, country, placePrefix)
	rows, err := c.Query(ctx, sparql)
	if err != nil {
		return nil, nil, err
	}
	var ids []string
	places := map[string]string{}
	for _, r := range rows {
		ids = append(ids, qid(r.get("p")))
		places[qid(r.get("place"))] = r.get("placeLabel")
	}
	return model.Uniq(ids), places, nil
}

// Details fetches biographical fields for a set of QIDs and returns persons.
func (c *Client) Details(ctx context.Context, qids []string) (map[string]*model.Person, error) {
	out := map[string]*model.Person{}
	for i := 0; i < len(qids); i += 60 {
		j := i + 60
		if j > len(qids) {
			j = len(qids)
		}
		batch := qids[i:j]
		vals := make([]string, len(batch))
		for k, q := range batch {
			vals[k] = "wd:" + q
		}
		sparql := fmt.Sprintf(`SELECT ?p ?pLabel ?pDesc ?dob ?dod ?pobLabel ?podLabel ?genderLabel ?occLabel ?wikitree ?father ?mother ?spouse ?citLabel ?gnLabel ?fnLabel WHERE {
  VALUES ?p { %s }
  OPTIONAL { ?p rdfs:label ?pLabel FILTER(LANG(?pLabel)="en") }
  OPTIONAL { ?p schema:description ?pDesc FILTER(LANG(?pDesc)="en") }
  OPTIONAL { ?p wdt:P569 ?dob }
  OPTIONAL { ?p wdt:P570 ?dod }
  OPTIONAL { ?p wdt:P19 ?pob . ?pob rdfs:label ?pobLabel FILTER(LANG(?pobLabel)="en") }
  OPTIONAL { ?p wdt:P20 ?pod . ?pod rdfs:label ?podLabel FILTER(LANG(?podLabel)="en") }
  OPTIONAL { ?p wdt:P21 ?g . ?g rdfs:label ?genderLabel FILTER(LANG(?genderLabel)="en") }
  OPTIONAL { ?p wdt:P106 ?occ . ?occ rdfs:label ?occLabel FILTER(LANG(?occLabel)="en") }
  OPTIONAL { ?p wdt:P7910 ?wikitree }
  OPTIONAL { ?p wdt:P22 ?father }
  OPTIONAL { ?p wdt:P25 ?mother }
  OPTIONAL { ?p wdt:P26 ?spouse }
  OPTIONAL { ?p wdt:P27 ?cit . ?cit rdfs:label ?citLabel FILTER(LANG(?citLabel)="en") }
  OPTIONAL { ?p wdt:P735 ?gn . ?gn rdfs:label ?gnLabel FILTER(LANG(?gnLabel)="en") }
  OPTIONAL { ?p wdt:P734 ?fn . ?fn rdfs:label ?fnLabel FILTER(LANG(?fnLabel)="en") }
}`, strings.Join(vals, " "))
		rows, err := c.Query(ctx, sparql)
		if err != nil {
			return out, err
		}
		for _, r := range rows {
			q := qid(r.get("p"))
			p := out["wd:"+q]
			if p == nil {
				p = &model.Person{ID: "wd:" + q, Wikidata: q, URL: "https://www.wikidata.org/wiki/" + q, Sources: []string{"wikidata"}}
				out[p.ID] = p
			}
			set := func(dst *string, v string) {
				if *dst == "" && v != "" {
					*dst = v
				}
			}
			set(&p.Name, r.get("pLabel"))
			set(&p.Description, r.get("pDesc"))
			set(&p.Birth, date(r.get("dob")))
			set(&p.Death, date(r.get("dod")))
			set(&p.BirthPlace, r.get("pobLabel"))
			set(&p.DeathPlace, r.get("podLabel"))
			set(&p.Gender, r.get("genderLabel"))
			set(&p.WikiTree, r.get("wikitree"))
			set(&p.Given, r.get("gnLabel"))
			set(&p.Surname, r.get("fnLabel"))
			if f := qid(r.get("father")); f != "" {
				set(&p.Father, "wd:"+f)
			}
			if m := qid(r.get("mother")); m != "" {
				set(&p.Mother, "wd:"+m)
			}
			if s := qid(r.get("spouse")); s != "" {
				p.Spouses = model.Uniq(append(p.Spouses, "wd:"+s))
			}
			if o := r.get("occLabel"); o != "" {
				p.Occupations = model.Uniq(append(p.Occupations, o))
			}
			if cz := r.get("citLabel"); cz != "" {
				p.Citizenship = model.Uniq(append(p.Citizenship, cz))
			}
		}
	}
	for _, p := range out {
		sort.Strings(p.Occupations)
		if p.Surname == "" {
			if i := strings.LastIndex(p.Name, " "); i > 0 {
				p.Surname = p.Name[i+1:]
			}
		}
	}
	return out, nil
}

func qid(uri string) string {
	if i := strings.LastIndex(uri, "/"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

func date(s string) string {
	if len(s) >= 10 {
		return strings.TrimPrefix(s[:10], "+")
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
