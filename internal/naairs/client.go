// Package naairs queries the National Archives of South Africa's NAAIRS
// index (http://www.national.archives.gov.za/naairs.htm), a session-driven
// legacy web front end: choose a repository, post keywords, then request
// "Result Details".
package naairs

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
)

const Base = "http://www.national.archsrch.gov.za/sm300cv/smws/"

// Repositories maps the three-letter database codes to their names.
var Repositories = map[string]string{
	"RSA": "All archives repositories", "TAB": "Pretoria (former Transvaal)", "KAB": "Cape Town", "NAB": "Pietermaritzburg",
	"VAB": "Free State", "TBD": "Durban", "TBE": "Port Elizabeth", "TBK": "Cape Town Records Centre", "SAB": "Central government since 1910",
	"GEN": "Genealogical Society gravestones",
}

// Record is one NAAIRS hit. Fields follow the index's own headings.
type Record struct {
	Depot       string `json:"depot"`
	Source      string `json:"source"`
	Type        string `json:"type"`
	Volume      string `json:"volume"`
	System      string `json:"system"`
	Reference   string `json:"reference"`
	Part        string `json:"part"`
	Description string `json:"description"`
	Starting    string `json:"starting"`
	Ending      string `json:"ending"`
	Remarks     string `json:"remarks,omitempty"`
}

type Client struct{ HTTP *http.Client }

func New() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{HTTP: &http.Client{Jar: jar, Timeout: 120 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, u string, form url.Values) (string, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", httpx.UserAgent())
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// the search server answers 503 "No server is available" when it is down;
		// say so instead of failing later on a page that lists no repositories
		return "", fmt.Errorf("NAAIRS: %s", resp.Status)
	}
	return string(b), err
}

var (
	reDBLink = regexp.MustCompile(`href="(SM200gi\?([^"&]*)&DB=%sE)"`)
	reInact  = regexp.MustCompile(`(?i)Search Manager Server is inactive`)
	reCount  = regexp.MustCompile(`located in\s+(\d+)\s+documents`)
	reSpace  = regexp.MustCompile(`\s+`)
	reAction = regexp.MustCompile(`action="(sm300cd\?[^"]+)"`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
)

// Query runs keywords (ANDed) against repository db, optionally within
// [from,to] years, and returns the detailed records.
func (c *Client) Query(ctx context.Context, db string, keywords []string, from, to string) ([]Record, int, error) {
	sel, err := c.do(ctx, http.MethodGet, Base+"sm300dl", nil)
	if err != nil {
		return nil, 0, err
	}
	if reInact.MatchString(sel) {
		return nil, 0, fmt.Errorf("NAAIRS search server is inactive")
	}
	re := regexp.MustCompile(fmt.Sprintf(reDBLink.String(), regexp.QuoteMeta(strings.ToUpper(db))))
	m := re.FindStringSubmatch(sel)
	if m == nil {
		return nil, 0, fmt.Errorf("repository %s not offered", db)
	}
	token := m[2]
	if _, err := c.do(ctx, http.MethodGet, Base+html.UnescapeString(m[1]), nil); err != nil {
		return nil, 0, err
	}
	form := url.Values{"lstBool1": {"AND"}, "lstBool2": {"AND"}, "lstBool3": {"AND"}, "lstBool4": {"AND"},
		"B/D": {from}, "CB/D": {"GE"}, "fldSort": {"A"}, "E/D": {to}, "CE/D": {"LE"}, "btnSearch": {"Search"}}
	for i := 1; i <= 5; i++ {
		v := ""
		if i-1 < len(keywords) {
			v = keywords[i-1]
		}
		form.Set(fmt.Sprintf("fldKeyword%d", i), v)
	}
	res, err := c.do(ctx, http.MethodPost, Base+"sm200dr?"+token, form)
	if err != nil {
		return nil, 0, err
	}
	plain := reSpace.ReplaceAllString(html.UnescapeString(reTag.ReplaceAllString(res, " ")), " ")
	cm := reCount.FindStringSubmatch(plain)
	if cm == nil {
		if len(plain) > 200 {
			plain = plain[:200]
		}
		return nil, 0, fmt.Errorf("no result count in reply: %s", strings.TrimSpace(plain))
	}
	n := 0
	fmt.Sscanf(cm[1], "%d", &n)
	if n == 0 {
		return nil, 0, nil
	}
	am := reAction.FindStringSubmatch(res)
	if am == nil {
		return nil, n, fmt.Errorf("no details action found")
	}
	// first details page gives the session token for direct document access
	det, err := c.do(ctx, http.MethodPost, Base+am[1], url.Values{"btnSub": {"Result Details"}})
	if err != nil {
		return nil, n, err
	}
	recs := parseDetails(det)
	tm := regexp.MustCompile(`sm20ddf0\?([0-9A-F]+)`).FindStringSubmatch(det)
	if tm != nil {
		limit := n
		if limit > maxDocs {
			limit = maxDocs
		}
		for k := 2; k <= limit; k++ {
			page, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%ssm20ddf0?%s&DN=%08d&F=P", Base, tm[1], k), nil)
			if err != nil {
				break
			}
			recs = append(recs, parseDetails(page)...)
			time.Sleep(300 * time.Millisecond)
		}
	}
	return recs, n, nil
}

// maxDocs caps how many documents are fetched in detail per query.
const maxDocs = 130

// parseDetails reads a NAAIRS "Result Details" page: inside <pre>, each
// field name (DEPOT, SOURCE, …) stands on its own line and its value follows.
func parseDetails(page string) []Record {
	pre := regexp.MustCompile(`(?is)<pre>(.*?)(</pre>|</td>|</form>)`).FindStringSubmatch(page)
	if pre == nil {
		return nil
	}
	t := regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(pre[1], "\n")
	t = html.UnescapeString(reTag.ReplaceAllString(t, ""))
	fields := map[string]bool{"DEPOT": true, "SOURCE": true, "TYPE": true, "VOLUME_NO": true, "SYSTEM": true, "REFERENCE": true, "PART": true, "DESCRIPTION": true, "STARTING": true, "ENDING": true, "REMARKS": true}
	rec := Record{}
	cur := ""
	set := func(k, v string) {
		v = strings.TrimSpace(reSpace.ReplaceAllString(v, " "))
		if v == "" {
			return
		}
		switch k {
		case "DEPOT":
			rec.Depot = v
		case "SOURCE":
			rec.Source = v
		case "TYPE":
			rec.Type = v
		case "VOLUME_NO":
			rec.Volume = v
		case "SYSTEM":
			rec.System = v
		case "REFERENCE":
			rec.Reference = v
		case "PART":
			rec.Part = v
		case "DESCRIPTION":
			rec.Description = strings.TrimSpace(rec.Description + " " + v)
		case "STARTING":
			rec.Starting = v
		case "ENDING":
			rec.Ending = v
		case "REMARKS":
			rec.Remarks = strings.TrimSpace(rec.Remarks + " " + v)
		}
	}
	for _, line := range strings.Split(t, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if fields[l] {
			cur = l
			continue
		}
		if cur != "" {
			set(cur, l)
		}
	}
	if rec.Reference == "" && rec.Description == "" {
		return nil
	}
	return []Record{rec}
}
