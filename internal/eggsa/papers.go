package eggsa

import (
	"context"
	"crypto/sha1"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// PapersHost is eGGSA's newspaper-extract site (Joomla).
const PapersHost = "https://newspapers.eggsa.org"

var (
	reCategory = regexp.MustCompile(`href="/index\.php/([a-z0-9-]+)"`)
	reArticle  = regexp.MustCompile(`href="(/index\.php/[a-z0-9-]+/[a-z0-9-]+)"`)
	reStart    = regexp.MustCompile(`[?&]start=(\d+)`)
	reTitle    = regexp.MustCompile(`(?is)<title>([^<]*)</title>`)
	reTags     = regexp.MustCompile(`<[^>]+>`)
	reBreaks   = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</li>|</tr>|</div>|</h[1-6]>`)
	reScripts  = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`)
	reSpace    = regexp.MustCompile(`\s+`)
)

// Notice is one transcribed newspaper line mentioning the surname.
type Notice struct {
	Paper string `json:"paper"`
	Page  string `json:"page"`
	URL   string `json:"url"`
	Text  string `json:"text"`
}

// Papers crawls every newspaper category, caches each extract page under
// cacheDir, and returns lines mentioning surname.
func Papers(ctx context.Context, surname, cacheDir string, workers int, log func(string, ...any)) ([]Notice, error) {
	os.MkdirAll(cacheDir, 0o755)
	c := &http.Client{Timeout: 90 * time.Second}
	home, err := fetchCached(ctx, c, PapersHost+"/", cacheDir, true)
	if err != nil {
		return nil, err
	}
	cats := map[string]bool{}
	for _, m := range reCategory.FindAllStringSubmatch(home, -1) {
		cats[m[1]] = true
	}
	log("papers: %d newspaper categories", len(cats))

	// collect article URLs per category (paginated 10 per page)
	var articles []string
	seen := map[string]bool{}
	catList := make([]string, 0, len(cats))
	for k := range cats {
		catList = append(catList, k)
	}
	sort.Strings(catList)
	for _, cat := range catList {
		start := 0
		for {
			u := fmt.Sprintf("%s/index.php/%s?start=%d", PapersHost, cat, start)
			if start == 0 {
				u = fmt.Sprintf("%s/index.php/%s", PapersHost, cat)
			}
			body, err := fetchCached(ctx, c, u, cacheDir, true)
			if err != nil {
				log("papers: %s: %v", u, err)
				break
			}
			added := 0
			for _, m := range reArticle.FindAllStringSubmatch(body, -1) {
				if strings.HasPrefix(m[1], "/index.php/"+cat+"/") && !seen[m[1]] {
					seen[m[1]] = true
					articles = append(articles, m[1])
					added++
				}
			}
			maxStart := -1
			for _, m := range reStart.FindAllStringSubmatch(body, -1) {
				var n int
				fmt.Sscanf(m[1], "%d", &n)
				if n > maxStart {
					maxStart = n
				}
			}
			if added == 0 || maxStart <= start {
				break
			}
			start += 10
		}
	}
	log("papers: %d extract pages to scan", len(articles))

	want := strings.ToLower(surname)
	var mu sync.Mutex
	var out []Notice
	sem := make(chan struct{}, max(1, workers))
	var wg sync.WaitGroup
	done := 0
	for _, a := range articles {
		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()
			u := PapersHost + path
			body, err := fetchCached(ctx, c, u, cacheDir, true)
			mu.Lock()
			done++
			if done%100 == 0 {
				log("papers: %d/%d pages, %d notices", done, len(articles), len(out))
			}
			mu.Unlock()
			if err != nil {
				log("papers: %s: %v", u, err)
				return
			}
			if !strings.Contains(strings.ToLower(body), want) {
				return
			}
			title := ""
			if m := reTitle.FindStringSubmatch(body); m != nil {
				title = strings.TrimSpace(html.UnescapeString(m[1]))
			}
			paper := strings.Split(strings.TrimPrefix(path, "/index.php/"), "/")[0]
			text := reScripts.ReplaceAllString(body, "")
			text = reBreaks.ReplaceAllString(text, "\n")
			text = html.UnescapeString(reTags.ReplaceAllString(text, " "))
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(strings.ToLower(line), want) {
					line = strings.TrimSpace(reSpace.ReplaceAllString(line, " "))
					if len(line) < 6 || len(line) > 600 {
						continue
					}
					mu.Lock()
					out = append(out, Notice{Paper: paper, Page: title, URL: u, Text: line})
					mu.Unlock()
				}
			}
		}(a)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Paper != out[j].Paper {
			return out[i].Paper < out[j].Paper
		}
		if out[i].URL != out[j].URL {
			return out[i].URL < out[j].URL
		}
		return out[i].Text < out[j].Text
	})
	return out, nil
}

func fetchCached(ctx context.Context, c *http.Client, u, cacheDir string, useCache bool) (string, error) {
	sum := sha1.Sum([]byte(u))
	path := filepath.Join(cacheDir, fmt.Sprintf("%x.html", sum[:8]))
	if useCache {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b), nil
		}
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", ua)
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
			continue
		}
		b, err := readAll(resp)
		if err != nil || resp.StatusCode != 200 {
			lastErr = fmt.Errorf("%s: status %d %v", u, resp.StatusCode, err)
			time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
			continue
		}
		time.Sleep(250 * time.Millisecond)
		os.WriteFile(path, b, 0o644)
		return string(b), nil
	}
	return "", lastErr
}

func readAll(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var buf strings.Builder
	chunk := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(chunk)
		buf.Write(chunk[:n])
		if err != nil {
			if err.Error() == "EOF" {
				return []byte(buf.String()), nil
			}
			return []byte(buf.String()), err
		}
	}
}
