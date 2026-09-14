// Package viz renders the kinship graph into a self-contained HTML page.
package viz

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/model"
)

//go:embed template.html
var template string

// Site holds the page text that belongs to one family rather than to the
// toolkit: the title, an optional flags column for the regional-records table
// and extra rows for the "Where to order copies" table. Load it from a JSON
// file with -site; every key is optional and falls back to DefaultSite.
type Site struct {
	Title        string        `json:"title"`                  // <title> and <h1>
	Eyebrow      string        `json:"eyebrow"`                // small line above the heading
	FlagsNote    string        `json:"flagsNote,omitempty"`    // sentence explaining the flags
	Flags        []Flag        `json:"flags,omitempty"`        // pills shown in the regional-records table
	OrderingRows []OrderingRow `json:"orderingRows,omitempty"` // rows added to "Where to order copies"
}

// Flag marks people in the regional-records table: everyone descended from
// DescendantsOf, or everyone whose given name matches NamePattern.
type Flag struct {
	Label         string `json:"label"`
	DescendantsOf string `json:"descendantsOf,omitempty"` // person id
	NamePattern   string `json:"namePattern,omitempty"`   // case-insensitive regular expression
	Style         string `json:"style,omitempty"`         // "ok", "prob" or empty
}

// OrderingRow is one row of the "Where to order copies" table.
type OrderingRow struct {
	Prefix  string `json:"prefix"`
	Holding string `json:"holding"`
	How     string `json:"how"`
}

// DefaultSite is what the page shows when no site file is given.
func DefaultSite() Site {
	return Site{
		Title:   "Kin",
		Eyebrow: "Family history · public records",
		OrderingRows: []OrderingRow{{
			Prefix:  "Estates after the index",
			Holding: "Master of the High Court for the district where the person died. Estate numbers appear in the newspaper estate notices and on the Master's records.",
			How:     "A child or grandchild may request a copy of the death notice and liquidation account from the Master's office, quoting name and date of death; identity documents are asked for.",
		}},
	}
}

// LoadSite reads a site JSON file over the defaults. An empty path returns
// the defaults; keys present in the file replace the corresponding default.
func LoadSite(path string) (Site, error) {
	s := DefaultSite()
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	for _, f := range s.Flags {
		if f.NamePattern != "" {
			if _, err := regexp.Compile("(?i)" + f.NamePattern); err != nil {
				return s, fmt.Errorf("%s: flag %q: %w", path, f.Label, err)
			}
		}
	}
	return s, nil
}

// Options controls what the page shows.
type Options struct {
	Seed        string   // person id the page is about; relationships are computed from here
	NoticesPath string   // JSON list of newspaper notices from `kin eggsa papers` (optional)
	RecordsPath string   // JSON list of hand-collected record citations (optional)
	ProbableIDs []string // ids from which upward the line is only probable
	Site        Site
}

// Payload is the JSON handed to the page.
type Payload struct {
	Seed       string                     `json:"seed"`
	Generated  string                     `json:"generated"`
	Persons    []*model.Person            `json:"persons"`
	Components map[string]int             `json:"components"`
	Relations  map[string]*graph.Relation `json:"relations"`
	Notices    []json.RawMessage          `json:"notices"`
	Records    []json.RawMessage          `json:"records"`
	Probable   []string                   `json:"probable"`
	Site       Site                       `json:"site"`
}

// Render writes the page for g to path.
func Render(g *model.Graph, opts Options, path string) error {
	comps := graph.Components(g)
	rels := map[string]*graph.Relation{}
	for id := range g.Persons {
		if id == opts.Seed {
			continue
		}
		if r, ok := graph.Relationship(g, opts.Seed, id); ok {
			rels[id] = r
		}
	}
	pl := Payload{Seed: opts.Seed, Persons: g.Sorted(), Components: comps, Relations: rels, Site: opts.Site}
	for _, id := range opts.ProbableIDs {
		id = g.Resolve(id)
		for anc := range graph.Ancestors(g, id) {
			pl.Probable = append(pl.Probable, anc)
		}
	}
	if opts.NoticesPath != "" {
		if b, err := os.ReadFile(opts.NoticesPath); err == nil {
			_ = json.Unmarshal(b, &pl.Notices)
		}
	}
	if opts.RecordsPath != "" {
		if b, err := os.ReadFile(opts.RecordsPath); err == nil {
			_ = json.Unmarshal(b, &pl.Records)
		}
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return err
	}
	// keep the JSON safe inside a <script> block
	s := strings.ReplaceAll(string(b), "</", "<\\/")
	html := strings.Replace(template, "/*__DATA__*/", s, 1)
	return os.WriteFile(path, []byte(html), 0o644)
}
