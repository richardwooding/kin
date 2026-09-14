// Package wikitree wraps the public WikiTree API (https://github.com/wikitree/wikitree-api).
package wikitree

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/model"
)

const Endpoint = "https://api.wikitree.com/api.php"

// Fields is the default field list requested for every profile.
const Fields = "Id,Name,FirstName,MiddleName,LastNameAtBirth,LastNameCurrent,RealName,BirthDate,DeathDate,BirthLocation,DeathLocation,Gender,Father,Mother,IsLiving,Privacy,BirthDateDecade,DeathDateDecade"

type Client struct {
	HTTP  *http.Client
	Delay time.Duration
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 240 * time.Second}, Delay: 1500 * time.Millisecond}
}

// FlexInt tolerates numbers, numeric strings, "" and null.
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = FlexInt(n)
	return nil
}

// PersonMap tolerates PHP-style empty arrays where an object is expected.
type PersonMap map[string]*Profile

func (m *PersonMap) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "" || t == "null" || strings.HasPrefix(t, "[") {
		*m = PersonMap{}
		return nil
	}
	var raw map[string]*Profile
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = raw
	return nil
}

// ProfileList tolerates both a JSON array and a PHP-style object keyed by Id.
type ProfileList []*Profile

func (l *ProfileList) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "" || t == "null" {
		*l = nil
		return nil
	}
	if strings.HasPrefix(t, "[") {
		var arr []*Profile
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*l = arr
		return nil
	}
	var m map[string]*Profile
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	out := make([]*Profile, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	*l = out
	return nil
}

// Profile is a WikiTree person record.
type Profile struct {
	Id              FlexInt   `json:"Id"`
	Name            string    `json:"Name"`
	FirstName       string    `json:"FirstName"`
	MiddleName      string    `json:"MiddleName"`
	LastNameAtBirth string    `json:"LastNameAtBirth"`
	LastNameCurrent string    `json:"LastNameCurrent"`
	RealName        string    `json:"RealName"`
	BirthDate       string    `json:"BirthDate"`
	DeathDate       string    `json:"DeathDate"`
	BirthLocation   string    `json:"BirthLocation"`
	DeathLocation   string    `json:"DeathLocation"`
	Gender          string    `json:"Gender"`
	Father          FlexInt   `json:"Father"`
	Mother          FlexInt   `json:"Mother"`
	IsLiving        FlexInt   `json:"IsLiving"`
	Privacy         FlexInt   `json:"Privacy"`
	BirthDateDecade string    `json:"BirthDateDecade"`
	DeathDateDecade string    `json:"DeathDateDecade"`
	Parents         PersonMap `json:"Parents"`
	Children        PersonMap `json:"Children"`
	Siblings        PersonMap `json:"Siblings"`
	Spouses         PersonMap `json:"Spouses"`
	Bio             string    `json:"bio,omitempty"`
}

func (c *Client) call(ctx context.Context, params url.Values) (json.RawMessage, error) {
	params.Set("format", "json")
	params.Set("appId", httpx.AppID)
	backoff := []time.Duration{15 * time.Second, 45 * time.Second, 90 * time.Second, 180 * time.Second}
	var body []byte
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, strings.NewReader(params.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", httpx.UserAgent)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		var rerr error
		body, rerr = io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil && attempt < len(backoff) {
			fmt.Fprintf(os.Stderr, "wikitree: short read on %s (%v), retrying\n", params.Get("action"), rerr)
			time.Sleep(5 * time.Second)
			continue
		}
		limited := resp.StatusCode == 429 || strings.Contains(string(body), "Limit exceeded")
		if limited && attempt < len(backoff) {
			fmt.Fprintf(os.Stderr, "wikitree: rate limited on %s, waiting %s\n", params.Get("action"), backoff[attempt])
			time.Sleep(backoff[attempt])
			continue
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("wikitree %s: %s: %s", params.Get("action"), resp.Status, truncate(string(body), 300))
		}
		break
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("wikitree %s decode: %w: %s", params.Get("action"), err, truncate(string(body), 300))
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("wikitree %s: empty response", params.Get("action"))
	}
	time.Sleep(c.Delay)
	return arr[0], nil
}

// SearchOptions mirror the searchPerson parameters.
type SearchOptions struct {
	FirstName, LastName, BirthLocation, DeathLocation string
	BirthDate, DeathDate                              string
	FatherFirstName, FatherLastName                   string
	MotherFirstName, MotherLastName                   string
	DateSpread                                        int    // years of tolerance around BirthDate/DeathDate
	LastNameMatch                                     string // "", "current", "birth", "strict"
	SkipVariants                                      bool
	Limit, Start                                      int
}

type SearchResult struct {
	Matches []*Profile
	Total   int
}

// Search runs searchPerson once (max 100 rows per call).
func (c *Client) Search(ctx context.Context, o SearchOptions) (*SearchResult, error) {
	if o.Limit <= 0 || o.Limit > 100 {
		o.Limit = 100
	}
	p := url.Values{"action": {"searchPerson"}, "fields": {Fields}, "limit": {fmt.Sprint(o.Limit)}, "start": {fmt.Sprint(o.Start)}}
	add := func(k, v string) {
		if v != "" {
			p.Set(k, v)
		}
	}
	add("FirstName", o.FirstName)
	add("LastName", o.LastName)
	add("BirthLocation", o.BirthLocation)
	add("DeathLocation", o.DeathLocation)
	add("BirthDate", o.BirthDate)
	add("DeathDate", o.DeathDate)
	add("fatherFirstName", o.FatherFirstName)
	add("fatherLastName", o.FatherLastName)
	add("motherFirstName", o.MotherFirstName)
	add("motherLastName", o.MotherLastName)
	if o.DateSpread > 0 {
		p.Set("dateSpread", fmt.Sprint(o.DateSpread))
	}
	add("lastNameMatch", o.LastNameMatch)
	if o.SkipVariants {
		p.Set("skipVariants", "1")
	}
	raw, err := c.call(ctx, p)
	if err != nil {
		return nil, err
	}
	var out struct {
		Status  json.RawMessage `json:"status"`
		Matches ProfileList     `json:"matches"`
		Total   FlexInt         `json:"total"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &SearchResult{Matches: out.Matches, Total: int(out.Total)}, nil
}

// SearchAll pages through searchPerson until total or max is reached.
func (c *Client) SearchAll(ctx context.Context, o SearchOptions, max int) ([]*Profile, int, error) {
	var all []*Profile
	total := 0
	for start := 0; ; start += 100 {
		o.Start = start
		o.Limit = 100
		r, err := c.Search(ctx, o)
		if err != nil {
			return all, total, err
		}
		total = r.Total
		all = append(all, r.Matches...)
		if len(r.Matches) < 100 || len(all) >= total || len(all) >= max {
			break
		}
	}
	return all, total, nil
}

// Ancestors returns the profile's ancestors up to depth generations (max 10).
func (c *Client) Ancestors(ctx context.Context, key string, depth int) ([]*Profile, error) {
	raw, err := c.call(ctx, url.Values{"action": {"getAncestors"}, "key": {key}, "depth": {fmt.Sprint(depth)}, "fields": {Fields}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Ancestors ProfileList `json:"ancestors"`
	}
	return out.Ancestors, json.Unmarshal(raw, &out)
}

// Descendants returns the profile's descendants up to depth generations.
func (c *Client) Descendants(ctx context.Context, key string, depth int) ([]*Profile, error) {
	raw, err := c.call(ctx, url.Values{"action": {"getDescendants"}, "key": {key}, "depth": {fmt.Sprint(depth)}, "fields": {Fields}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Descendants ProfileList `json:"descendants"`
	}
	return out.Descendants, json.Unmarshal(raw, &out)
}

// Relatives fetches parents/children/siblings/spouses for a batch of keys.
func (c *Client) Relatives(ctx context.Context, keys []string) ([]*Profile, error) {
	var all []*Profile
	for i := 0; i < len(keys); i += 5 {
		j := i + 5
		if j > len(keys) {
			j = len(keys)
		}
		raw, err := c.call(ctx, url.Values{"action": {"getRelatives"}, "keys": {strings.Join(keys[i:j], ",")},
			"getParents": {"1"}, "getChildren": {"1"}, "getSiblings": {"1"}, "getSpouses": {"1"}, "fields": {Fields}})
		if err != nil {
			return all, err
		}
		var out struct {
			Items []struct {
				Key    string   `json:"key"`
				Person *Profile `json:"person"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			fmt.Fprintf(os.Stderr, "wikitree: relatives batch %d: %v (skipping)\n", i/5, err)
			continue
		}
		for _, it := range out.Items {
			if it.Person != nil {
				all = append(all, it.Person)
			}
		}
	}
	return all, nil
}

// Person fetches one profile by key (Name like Smith-12 or numeric Id).
func (c *Client) Person(ctx context.Context, key string) (*Profile, error) {
	raw, err := c.call(ctx, url.Values{"action": {"getPerson"}, "key": {key}, "fields": {Fields}, "resolveRedirect": {"1"}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Person *Profile `json:"person"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Person, nil
}

// Bio fetches the biography wikitext for a profile.
func (c *Client) Bio(ctx context.Context, key string) (string, error) {
	raw, err := c.call(ctx, url.Values{"action": {"getBio"}, "key": {key}})
	if err != nil {
		return "", err
	}
	var out struct {
		Bio string `json:"bio"`
	}
	return out.Bio, json.Unmarshal(raw, &out)
}

// ToPerson converts a WikiTree profile to the shared model. Parent links are
// filled by name when idToName knows the parent's Id, else left for a later resolve pass.
func ToPerson(p *Profile, idToName map[int64]string) *model.Person {
	if p == nil || p.Name == "" {
		return nil
	}
	name := strings.TrimSpace(strings.Join(model.Uniq([]string{p.FirstName, p.MiddleName, p.LastNameAtBirth}), " "))
	if p.LastNameCurrent != "" && !strings.EqualFold(p.LastNameCurrent, p.LastNameAtBirth) {
		name += " (" + p.LastNameCurrent + ")"
	}
	if strings.TrimSpace(name) == "" {
		name = p.Name
	}
	m := &model.Person{
		ID:         "wt:" + p.Name,
		Name:       name,
		Given:      p.FirstName,
		Surname:    p.LastNameAtBirth,
		Gender:     strings.ToLower(p.Gender),
		Birth:      cleanDate(p.BirthDate, p.BirthDateDecade),
		Death:      cleanDate(p.DeathDate, p.DeathDateDecade),
		BirthPlace: p.BirthLocation,
		DeathPlace: p.DeathLocation,
		WikiTree:   p.Name,
		URL:        "https://www.wikitree.com/wiki/" + p.Name,
		Living:     p.IsLiving == 1,
		Sources:    []string{"wikitree"},
	}
	if p.Father > 0 {
		if n, ok := idToName[int64(p.Father)]; ok {
			m.Father = "wt:" + n
		} else {
			m.Father = fmt.Sprintf("wt:id:%d", p.Father)
		}
	}
	if p.Mother > 0 {
		if n, ok := idToName[int64(p.Mother)]; ok {
			m.Mother = "wt:" + n
		} else {
			m.Mother = fmt.Sprintf("wt:id:%d", p.Mother)
		}
	}
	for _, s := range p.Spouses {
		if s != nil && s.Name != "" {
			m.Spouses = append(m.Spouses, "wt:"+s.Name)
		}
	}
	return m
}

func cleanDate(d, decade string) string {
	d = strings.TrimSpace(d)
	if d == "" || d == "0000-00-00" {
		return strings.TrimSpace(decade)
	}
	d = strings.TrimSuffix(d, "-00-00")
	d = strings.TrimSuffix(d, "-00")
	return d
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
