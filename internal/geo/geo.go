// Package geo turns the place names in a graph into coordinates. It normalises
// the historic spellings genealogists use (de Caep de Goede Hoop, Drakenstein,
// 't Land van Waveren, Heiliges Römisches Reich), asks Nominatim once per
// distinct place at its permitted rate, and keeps every answer in a JSON cache
// so a second run needs no network.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
)

// Point is a resolved place.
type Point struct {
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Label     string  `json:"label"`             // what the geocoder matched
	Query     string  `json:"query"`             // the normalised string that resolved
	Precision string  `json:"precision"`         // town | region | country
	Class     string  `json:"class,omitempty"`   // OSM class of the match: place, boundary, waterway, natural
	Type      string  `json:"type,omitempty"`    // OSM type of the match: city, town, village, administrative, river...
	Version   int     `json:"v,omitempty"`       // geocoder algorithm that produced the entry; older entries are re-queried
	Manual    bool    `json:"manual,omitempty"`  // came from an overrides file
	Failed    bool    `json:"failed,omitempty"`  // nothing resolved
	Fetched   string  `json:"fetched,omitempty"` // date of the geocoder answer
}

// Nominatim is the OpenStreetMap geocoder endpoint.
const Nominatim = "https://nominatim.openstreetmap.org/search"

// version is bumped whenever the aliases, the query order or the acceptance
// rules change, so cached answers from an older algorithm are asked again.
const version = 2

// aliases rewrite historic or Dutch names to what a modern gazetteer knows.
// Longer keys are applied first so "Drakenstein, de Caep de Goede Hoop" is
// handled before "de Caep de Goede Hoop".
var aliases = [][2]string{
	{"'t land van waveren (tulbagh from 1804)", "Tulbagh"},
	{"'t land van waveren", "Tulbagh"},
	{"land van waveren", "Tulbagh"},
	{"de caep de goede hoop, dutch cape colony", "Western Cape, South Africa"},
	{"de caep de goede hoop", "Western Cape, South Africa"},
	{"de kaap de goede hoop", "Western Cape, South Africa"},
	{"cabo de goede hoop", "Western Cape, South Africa"},
	{"caep de goede hoop", "Western Cape, South Africa"},
	{"cape of good hope colony", "South Africa"},
	{"cape of good hope", "Western Cape, South Africa"},
	{"cape colony", "South Africa"},
	{"cape province", "South Africa"},
	{"kaapkolonie", "South Africa"},
	{"orange river sovereignty", "Free State, South Africa"},
	{"orange free state", "Free State, South Africa"},
	{"oranje vrijstaat", "Free State, South Africa"},
	{"orange river colony", "Free State, South Africa"},
	{"zuid-afrikaansche republiek", "South Africa"},
	{"transvaal colony", "South Africa"},
	{"transvaal", "South Africa"},
	{"natal", "KwaZulu-Natal, South Africa"},
	{"union of south africa", "South Africa"},
	{"drakenstein", "Paarl"},
	{"stellenbosch district", "Stellenbosch"},
	{"heiliges römisches reich", "Germany"},
	{"holy roman empire", "Germany"},
	{"grafschaft bentheim", "Bentheim"},
	{"kurfürstentum köln", "Köln"},
	{"hessen-kassel", "Kassel"},
	{"republiek der zeven verenigde nederlanden", "Netherlands"},
	{"heilige roomse rijk", "Germany"},
	{"graafschap holland", "Netherlands"},
	{"hertogdom brabant", "Brabant"},
	{"hertogdom gelre", "Gelderland"},
	{"pays-bas espagnols", "Belgium"},
	{"spaanse nederlanden", "Belgium"},
	{"comté de flandre", "Flanders"},
	{"graafschap vlaanderen", "Flanders"},
	{"royaume de france", "France"},
	{"koninkrijk frankrijk", "France"},
	{"prinsbisdom luik", "Liège"},
	{"kurpfalz", "Palatinate, Germany"},
	{"herzogtum württemberg", "Württemberg, Germany"},
	{"prussia", "Germany"},
	{"preussen", "Germany"},
	{"kingdom of great britain", "United Kingdom"},
	{"great britain", "United Kingdom"},
	{"kingdom of england", "England"},
	{"nederland", "Netherlands"},
	{"zuid-holland", "South Holland"},
	{"noord-holland", "North Holland"},
	{"holland", "Netherlands"},
	{"normandie", "Normandy"},
	{"île-de-france", "Ile-de-France"},
	{"greuville en caux", "Greuville, Normandy, France"},
	{"spanish netherlands", "Belgium"},
	{"french flanders", "Nord, France"},
	{"courtai", "Kortrijk"},
	{"courtrai", "Kortrijk"},
	{"cortrijk", "Kortrijk"},
	{"aeth", "Ath, Belgium"},
	{"henegouw", "Hainaut"},
	{"west-africa", ""},
	{"west africa", ""},
	{"freie reichsstadt", ""},
	{"freie stadt", ""},
	{"blois loir et cher", "Blois"},
	{"blois loir-et-cher", "Blois"},
	{"basses-alpes", "Alpes-de-Haute-Provence"},
	{"aunis", ""},
	{"dauphine", "Dauphiné"},
	{"hurepoix", "Essonne, France"},
	{"island of solor", "Solor, Indonesia"},
	{"kaffirskraal potchefstroom", "Potchefstroom, South Africa"},
	{"marolles-et-hure", "Marolles-en-Hurepoix, France"},
	{"menars-la-ville", "Menars, France"},
	{"vogelrivier", "Voëlrivier, Eastern Cape, South Africa"},
	{"batavia", "Jakarta, Indonesia"},
	{"frankryk", "France"},
	{"the cape", "Western Cape, South Africa"},
	{"caep", "Western Cape, South Africa"},
	{"kaap", "Western Cape, South Africa"},
}

var parens = regexp.MustCompile(`\([^)]*\)`)

// Normalise rewrites one place string into geocoder-friendly comma-separated parts.
func Normalise(place string) []string {
	s := parens.ReplaceAllString(place, "")
	s = strings.NewReplacer("[", "", "]", "", " district", "").Replace(s)
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		low := strings.ToLower(p)
		for _, a := range aliases {
			if low == a[0] || strings.Contains(low, a[0]) {
				p = a[1]
				break
			}
		}
		out = append(out, strings.Split(p, ",")...)
	}
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	// drop repeats, keep order
	seen := map[string]bool{}
	var uniq []string
	for _, p := range out {
		k := strings.ToLower(p)
		if p != "" && !seen[k] {
			seen[k] = true
			uniq = append(uniq, p)
		}
	}
	return uniq
}

// Cache is the place → point store.
type Cache struct {
	Path   string
	Points map[string]Point
}

func LoadCache(path string) *Cache {
	c := &Cache{Path: path, Points: map[string]Point{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c.Points)
	}
	return c
}

func (c *Cache) Save() error {
	if c.Path == "" {
		return nil
	}
	b, _ := json.MarshalIndent(c.Points, "", "  ")
	return os.WriteFile(c.Path, b, 0o644)
}

// LoadOverrides reads a manual place → {lat, lon, label} file and stores the
// entries in the cache as manual points.
func (c *Cache) LoadOverrides(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]struct {
		Lat, Lon  float64
		Label     string
		Precision string
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for k, v := range m {
		p := Point{Lat: v.Lat, Lon: v.Lon, Label: v.Label, Query: k, Precision: v.Precision, Manual: true}
		if p.Precision == "" {
			p.Precision = "town"
		}
		if p.Label == "" {
			p.Label = k
		}
		c.Points[k] = p
	}
	return nil
}

type Client struct {
	HTTP  *http.Client
	Delay time.Duration
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}, Delay: 1100 * time.Millisecond}
}

// match is one accepted geocoder answer.
type match struct {
	Lat, Lon    float64
	Label       string
	Class, Type string
}

// Accept reports whether an OSM class/type pair denotes a place rather than a
// shop, school, monument, road or maritime zone that happens to carry the name.
func Accept(class, typ string) bool {
	switch class {
	case "place":
		return typ != "house" && typ != "houses" && typ != "postcode"
	case "boundary":
		return typ == "administrative"
	case "waterway", "natural":
		return true
	}
	return false
}

// regionTypes are OSM place types coarser than a settlement.
var regionTypes = map[string]bool{"state": true, "region": true, "province": true, "county": true, "district": true, "state_district": true}

func (c *Client) query(ctx context.Context, q string) (settlement, water *match, err error) {
	u := Nominatim + "?" + url.Values{"q": {q}, "format": {"jsonv2"}, "limit": {"6"}, "accept-language": {"en"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", httpx.UserAgent())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("nominatim: %s", resp.Status)
	}
	var res []struct {
		Lat, Lon    string
		DisplayName string `json:"display_name"`
		Class       string `json:"class"`    // format=json
		Category    string `json:"category"` // format=jsonv2 calls the class a category
		Type        string `json:"type"`
		AddressType string `json:"addresstype"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, nil, err
	}
	for _, r := range res {
		class := r.Category
		if class == "" {
			class = r.Class
		}
		if !Accept(class, r.Type) {
			continue
		}
		m := &match{Label: r.DisplayName, Class: class, Type: r.Type}
		if class == "boundary" && r.AddressType != "" {
			m.Type = r.AddressType // administrative boundaries carry the level here: country, state, county, municipality
		}
		fmt.Sscanf(r.Lat, "%g", &m.Lat)
		fmt.Sscanf(r.Lon, "%g", &m.Lon)
		if class == "place" || class == "boundary" {
			return m, water, nil
		}
		if water == nil {
			water = m // a river or bay counts only when no settlement carries the name
		}
	}
	return nil, water, nil
}

// queries lists the strings to try for one normalised name, most specific
// first: the whole name, the name without its historic polity, the first part
// with the country, then successively coarser suffixes. Each comes with the
// precision a match at that step deserves.
func queries(parts []string) (qs []string, precs []string) {
	n := len(parts)
	add := func(ps []string, prec string) {
		q := strings.Join(ps, ", ")
		for _, have := range qs {
			if have == q {
				return
			}
		}
		qs = append(qs, q)
		precs = append(precs, prec)
	}
	add(parts, "town")
	if n >= 3 {
		add(parts[:n-1], "town")
		add([]string{parts[0], parts[n-1]}, "town")
	}
	for i := 1; i < n; i++ {
		prec := "region"
		if n-i == 1 {
			prec = "country"
		}
		add(parts[i:], prec)
	}
	return qs, precs
}

// Resolve returns the point for a place string, from the cache or the
// geocoder, trying the full normalised name first and then successively
// coarser suffixes. Precision records how much of the name resolved.
func (c *Client) Resolve(ctx context.Context, cache *Cache, place string, offline bool) (Point, error) {
	if p, ok := cache.Points[place]; ok && (p.Manual || offline || p.Version == version) {
		return p, nil
	}
	parts := Normalise(place)
	if len(parts) == 0 {
		return Point{Failed: true}, nil
	}
	if offline {
		return Point{Failed: true}, nil
	}
	qs, precs := queries(parts)
	var water *match
	waterAt := -1
	point := func(m *match, i int) Point {
		prec := precs[i]
		switch {
		case m.Type == "country" || m.Type == "continent" || m.Type == "ocean":
			prec = "country"
		case prec == "town" && regionTypes[m.Type]:
			prec = "region"
		}
		return Point{Lat: m.Lat, Lon: m.Lon, Label: m.Label, Query: qs[i], Precision: prec, Class: m.Class, Type: m.Type, Version: version, Fetched: time.Now().Format("2006-01-02")}
	}
	for i, q := range qs {
		m, w, err := c.query(ctx, q)
		time.Sleep(c.Delay)
		if err != nil {
			return Point{}, err
		}
		if m != nil {
			p := point(m, i)
			cache.Points[place] = p
			return p, nil
		}
		if w != nil && water == nil {
			water, waterAt = w, i
		}
	}
	if water != nil {
		// no settlement at any step: fall back to the river or bay of the most specific step
		p := point(water, waterAt)
		cache.Points[place] = p
		return p, nil
	}
	p := Point{Failed: true, Query: strings.Join(parts, ", "), Version: version, Fetched: time.Now().Format("2006-01-02")}
	cache.Points[place] = p
	return p, nil
}

// ResolveAll geocodes every distinct place, saving the cache after each new
// answer, and reports progress through log.
func (c *Client) ResolveAll(ctx context.Context, cache *Cache, places []string, offline bool, log func(string, ...any)) map[string]Point {
	out := map[string]Point{}
	sort.Strings(places)
	n := 0
	for _, pl := range places {
		if pl == "" {
			continue
		}
		prev, cached := cache.Points[pl]
		cached = cached && (prev.Manual || offline || prev.Version == version)
		p, err := c.Resolve(ctx, cache, pl, offline)
		if err != nil {
			log("geo: %s: %v", pl, err)
			continue
		}
		out[pl] = p
		if !cached {
			n++
			if err := cache.Save(); err != nil {
				log("geo: cache: %v", err)
			}
			if p.Failed {
				log("geo: no match for %q", pl)
			} else if n%25 == 0 {
				log("geo: %d places resolved", n)
			}
		}
	}
	return out
}
