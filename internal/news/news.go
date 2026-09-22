// Package news searches the historical newspapers that let a program search
// their text: Nasjonalbiblioteket's for Norway, the Royal Danish Library's
// labs API for Denmark up to 1880, and Europeana Newspapers across Europe.
// The British, Irish, Swedish and Icelandic archives forbid or block programs,
// so kin leads links to them instead.
package news

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/namematch"
	"github.com/richardwooding/kin/internal/place"
)

// Hit is one newspaper page naming the phrase searched for.
type Hit struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Paper  string `json:"paper"`
	Date   string `json:"date,omitempty"` // yyyy-mm-dd
	Year   int    `json:"year,omitempty"`
	Page   string `json:"page,omitempty"`
	Place  string `json:"place,omitempty"`  // where the paper was published
	Holder string `json:"holder,omitempty"` // the library that digitised it, when not the source itself
	Text   string `json:"text,omitempty"`   // the OCR words around the name, or the name alone
	URL    string `json:"url"`
}

// Query is one phrase search between two years.
type Query struct {
	Phrase string
	From   int
	To     int
	Max    int // hits to read; 50 when zero
}

func (q Query) max() int {
	if q.Max <= 0 {
		return 50
	}
	return q.Max
}

// Source is one newspaper archive.
type Source interface {
	Name() string
	// Covers reports whether the archive holds papers for a region.
	Covers(place.Region) bool
	// Window clips a span of years to what the archive may show, and
	// reports false when nothing is left.
	Window(from, to int) (int, int, bool)
	// Context reports whether hits carry the words around the name; a source
	// that returns only the name is scored by the paper's town and date.
	Context() bool
	URL(Query) string
	Search(context.Context, Query) ([]Hit, int, error)
}

// Names lists the sources kin can search.
var Names = []string{"nb", "kb", "europeana"}

// New returns the named source, caching under dir/<name> and pausing delay
// after each request. Europeana needs a key.
func New(name, dir, europeanaKey string, delay time.Duration) (Source, error) {
	cache := func() *httpx.Cache {
		if dir == "" {
			return nil
		}
		return httpx.NewCache(dir + "/" + name)
	}
	p := httpx.NewPolite(name)
	p.Cache = cache()
	p.Delay = delay
	switch name {
	case "nb":
		return &NB{p}, nil
	case "kb":
		return &KB{p}, nil
	case "europeana":
		if europeanaKey == "" {
			return nil, fmt.Errorf("europeana needs an API key: -europeana-key or KIN_EUROPEANA_KEY (free from https://pro.europeana.eu/pages/get-api)")
		}
		return &Europeana{Polite: p, Key: europeanaKey}, nil
	}
	return nil, fmt.Errorf("unknown newspaper source %q; kin knows %s", name, strings.Join(Names, ", "))
}

// around returns up to span words either side of the first place in text
// where the phrase's last word follows its first, allowing one OCR error in
// each; without such a place it returns the opening words.
func around(text, phrase string, span int) string {
	words := strings.Fields(text)
	want := namematch.Tokens(phrase)
	if len(want) == 0 || len(words) == 0 {
		return ""
	}
	first, last := want[0], want[len(want)-1]
	at := -1
	for i := 0; i+1 < len(words) && at < 0; i++ {
		a, b := namematch.Tokens(words[i]), namematch.Tokens(words[i+1])
		if len(a) > 0 && len(b) > 0 && namematch.Lev1(a[len(a)-1], first) && namematch.OCR(b[:1], last) {
			at = i
		}
	}
	if at < 0 {
		at = 0
	}
	lo, hi := at-span, at+span+len(want)
	if lo < 0 {
		lo = 0
	}
	if hi > len(words) {
		hi = len(words)
	}
	return strings.Join(words[lo:hi], " ")
}
