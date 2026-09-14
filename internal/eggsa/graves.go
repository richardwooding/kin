// Package eggsa queries the Genealogical Society of South Africa's gravestone
// photograph indexes (one site per province) for a surname.
package eggsa

import (
	"context"
	"crypto/sha1"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/model"
)

// Sites maps a province label to its search form page.
var Sites = map[string]string{
	"Gauteng":       "https://gravesgauteng.eggsa.org/Search/ggsearchGraves.htm",
	"Western Cape":  "https://graveswcape.eggsa.org/Search/wcsearchGraves.htm",
	"KwaZulu-Natal": "https://natalgraves.eggsa.org/Search/kwasearchGraves.htm",
	"Eastern Cape":  "https://graveseastcape.eggsa.org/Search/ecsearchGraves.htm",
	"Free State":    "https://gravesfreestate.eggsa.org/Search/fssearchGraves.htm",
	"Mpumalanga":    "https://gravesmpumalanga.eggsa.org/Search/new_mpsearchGraves.htm",
	"North West":    "https://gravesnorthwest.eggsa.org/Search/new_nwsearchGraves.htm",
	"Northern Cape": "https://gravesnortherncape.eggsa.org/Search/new_ncsearchGraves.htm",
	"Limpopo":       "https://graveslimpopo.eggsa.org/Search/limsearchGraves.htm",
	"World":         "https://gravesworld.eggsa.org/Search/wosearchGraves.htm",
}

// ua is the user agent sent to the eGGSA sites.
func ua() string { return httpx.UserAgent() }

var (
	reForm   = regexp.MustCompile(`(?is)<form[^>]*action="([^"]+)"[^>]*>(.*?)</form>`)
	reInput  = regexp.MustCompile(`(?is)<input[^>]*>`)
	reAttr   = regexp.MustCompile(`(?i)(name|value|type)="([^"]*)"`)
	reSelect = regexp.MustCompile(`(?is)<select[^>]*name="([^"]+)"[^>]*>(.*?)</select>`)
	reOption = regexp.MustCompile(`(?is)<option[^>]*value="([^"]*)"[^>]*>([^<]*)`)
	reResult = regexp.MustCompile(`(?is)<a\s+[^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)
	reDates  = regexp.MustCompile(`(\d{4}|\d{3}\?|\?{4})\s*-\s*(\d{4}|\d{3}\?|\?{4})?`)
)

// Grave is one indexed gravestone photograph.
type Grave struct {
	Province  string
	Text      string
	URL       string
	Cemetery  string
	Surname   string
	Given     string
	Birth     string
	Death     string
	Companion string
}

// Search posts the surname to every provincial index and returns matching graves.
func Search(ctx context.Context, surname string, log func(string, ...any)) ([]Grave, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	var out []Grave
	var firstErr error
	want := strings.ToUpper(surname)
	for prov, page := range Sites {
		graves, err := searchSite(ctx, c, prov, page, surname)
		if err != nil {
			log("eggsa: %s: %v", prov, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		kept := 0
		for _, g := range graves {
			// multi-word surnames (VAN DER MERWE) are parsed as first token only; match on the text prefix instead
			if strings.ToUpper(g.Surname) == want || (strings.Contains(want, " ") && strings.HasPrefix(strings.ToUpper(g.Text), want+" ")) {
				if strings.Contains(want, " ") {
					g.Surname = surnameTitle(want)
					g.Given = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(g.Text), want+" "))
					if i := strings.IndexAny(g.Given, "0123456789&-"); i > 0 {
						g.Given = strings.TrimSpace(g.Given[:i])
					}
				}
				out = append(out, g)
				kept++
			}
		}
		log("eggsa: %-14s %d results, %d with surname %s", prov, len(graves), kept, surname)
		time.Sleep(700 * time.Millisecond)
	}
	if len(out) == 0 && firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

func searchSite(ctx context.Context, c *http.Client, prov, page, surname string) ([]Grave, error) {
	body, err := get(ctx, c, page)
	if err != nil {
		return nil, err
	}
	fm := reForm.FindStringSubmatch(body)
	if fm == nil {
		return nil, fmt.Errorf("no search form on %s", page)
	}
	base, _ := url.Parse(page)
	action, err := base.Parse(html.UnescapeString(fm[1]))
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	for _, in := range reInput.FindAllString(fm[2], -1) {
		attrs := map[string]string{}
		for _, a := range reAttr.FindAllStringSubmatch(in, -1) {
			attrs[strings.ToLower(a[1])] = a[2]
		}
		name := attrs["name"]
		if name == "" {
			continue
		}
		switch {
		case strings.Contains(strings.ToLower(name), "surname"):
			form.Set(name, surname)
		case attrs["type"] == "hidden" || attrs["type"] == "submit":
			form.Set(name, attrs["value"])
		default:
			form.Set(name, "")
		}
	}
	for _, sel := range reSelect.FindAllStringSubmatch(fm[2], -1) {
		name := sel[1]
		opts := reOption.FindAllStringSubmatch(sel[2], -1)
		val := ""
		for _, o := range opts {
			if strings.Contains(strings.ToLower(o[1]+o[2]), "exact") {
				val = o[1]
				break
			}
		}
		if val == "" && len(opts) > 0 {
			val = opts[0][1]
		}
		form.Set(name, val)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, action.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", ua())
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", action, resp.Status)
	}
	res := string(rb)
	if i := strings.Index(strings.ToLower(res), "search criteria"); i > 0 {
		res = res[i:]
	}
	var graves []Grave
	for _, m := range reResult.FindAllStringSubmatch(res, -1) {
		text := strings.TrimSpace(html.UnescapeString(m[2]))
		href := html.UnescapeString(m[1])
		if text == "" || !strings.Contains(strings.ToLower(href), ".jpg") && !strings.Contains(strings.ToLower(href), ".png") {
			continue
		}
		graves = append(graves, parse(prov, text, href))
	}
	return graves, nil
}

func parse(prov, text, href string) Grave {
	g := Grave{Province: prov, Text: text, URL: href}
	if u, err := url.Parse(href); err == nil {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		var cem []string
		for _, s := range segs {
			if strings.HasSuffix(strings.ToLower(s), ".jpg") || strings.Contains(s, "Surnames") || len(s) <= 2 {
				continue
			}
			cem = append(cem, strings.ReplaceAll(strings.ReplaceAll(s, "_", " "), "-", " "))
		}
		g.Cemetery = strings.Join(cem, ", ")
	}
	main := text
	if i := strings.Index(text, "&"); i > 0 {
		main = strings.TrimSpace(text[:i])
		g.Companion = strings.TrimSpace(text[i+1:])
	}
	fields := strings.Fields(main)
	if len(fields) > 0 {
		g.Surname = strings.Trim(fields[0], ",")
	}
	var given []string
	for _, f := range fields[1:] {
		if reDates.MatchString(f) || strings.HasPrefix(f, "nee") || strings.HasPrefix(f, "née") {
			break
		}
		given = append(given, f)
	}
	g.Given = strings.Join(given, " ")
	if d := reDates.FindStringSubmatch(main); d != nil {
		g.Birth = strings.ReplaceAll(d[1], "?", "")
		g.Death = strings.ReplaceAll(d[2], "?", "")
	}
	return g
}

// ToPerson converts a grave into the shared model.
func (g Grave) ToPerson() *model.Person {
	sum := sha1.Sum([]byte(g.URL))
	name := strings.TrimSpace(g.Given + " " + titleCase(g.Surname))
	return &model.Person{
		ID:         fmt.Sprintf("eggsa:%x", sum[:6]),
		Name:       name,
		Given:      g.Given,
		Surname:    titleCase(g.Surname),
		Birth:      g.Birth,
		Death:      g.Death,
		DeathPlace: g.Cemetery + ", " + g.Province + ", South Africa",
		URL:        g.URL,
		Sources:    []string{"eggsa"},
		Note:       "gravestone: " + g.Text,
	}
}

func surnameTitle(s string) string {
	parts := strings.Fields(strings.ToLower(s))
	for i, w := range parts {
		if i == len(parts)-1 {
			parts[i] = titleCase(w)
		}
	}
	return strings.Join(parts, " ")
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func get(ctx context.Context, c *http.Client, u string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", ua())
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", u, resp.Status)
	}
	return string(b), err
}
