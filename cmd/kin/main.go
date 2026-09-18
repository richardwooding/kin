// kin — ancestry toolkit: pulls family records from public sources (WikiTree,
// eGGSA, NAAIRS, Wikidata), merges them into one kinship graph, labels the
// relationships and renders an ancestry page and printable reports.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/kin/internal/eggsa"
	"github.com/richardwooding/kin/internal/gazette"
	"github.com/richardwooding/kin/internal/geomap"
	"github.com/richardwooding/kin/internal/graph"
	"github.com/richardwooding/kin/internal/httpx"
	"github.com/richardwooding/kin/internal/leads"
	"github.com/richardwooding/kin/internal/model"
	"github.com/richardwooding/kin/internal/naairs"
	"github.com/richardwooding/kin/internal/place"
	"github.com/richardwooding/kin/internal/report"
	"github.com/richardwooding/kin/internal/tna"
	"github.com/richardwooding/kin/internal/tree"
	"github.com/richardwooding/kin/internal/viz"
	"github.com/richardwooding/kin/internal/war"
	"github.com/richardwooding/kin/internal/wikidata"
	"github.com/richardwooding/kin/internal/wikitree"
)

// Set at build time by GoReleaser through -ldflags "-X main.version=…".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func logf(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

func die(err error) {
	if err != nil {
		logf("error: %v", err)
		os.Exit(1)
	}
}

// need exits with a usage error when a required flag was not given.
func need(name, value string) {
	if value == "" {
		die(fmt.Errorf("-%s is required", name))
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kin <command> [flags]

  wikidata surname   -name Smith [-name Smyth] -out data/wd_surname.json
  wikidata place     -name Stellenbosch -country Q258 -out data/wd_place.json
  wikitree search    -last Smith [-first John] [-birthloc "South Africa"] [-birth 1890 -spread 3] [-father "John Smith"] -out data/wt_search.json
  wikitree expand    -keys Smith-1,Smith-2 [-anc 10] [-desc 10] [-relatives] -in data/wt_search.json -out data/wt_tree.json
  wikitree relatives -in data/wt_tree.json      (fetch spouses/children/siblings for every profile in a graph)
  eggsa graves       -surname Smith -out data/eggsa.json   (South African gravestone indexes, all provinces)
  eggsa papers       -surname Smith -out data/papers.json  (crawl eGGSA newspaper extracts; pages cached for reuse)
  graph build        -seed seed.json -in data/a.json,data/b.json -out data/graph.json
  graph kin          -graph data/graph.json -from seed:me -to wt:Smith-1
  graph report       -graph data/graph.json -from seed:me
  graph redact       -graph data/graph.json -out data/graph.public.json   (strip dates, places, notes and URLs from living people before publishing)
  viz                -graph data/graph.json -seed seed:me [-site site.json] [-notices data/papers.json] [-records records.json] [-probable wt:X] [-tree-url URL] -out dist/index.html
  map                -graph data/graph.json -root seed:me [-reader seed:me] [-site site.json] [-cache data/cache/geo.json] [-places places.json] [-offline] [-dashboard-url URL] [-tree-url URL] -out dist/map.html   (birth and death places on a pan-and-zoom map)
  tree               -graph data/graph.json -root seed:me [-reader seed:me] [-gen 20] [-site site.json] [-records records.json] [-probable wt:X] [-dashboard-url URL] -out dist/tree.html   (pan-and-zoom pedigree)
  naairs             -db TAB -q "SMITH JOHN HENRY" [-from 1930 -to 1932] [-out data/naairs_smith.json]   (National Archives of South Africa index)
  naairs sweep       -graph data/graph.json -from seed:me [-gen 20] [-db RSA] [-delay 3s] [-resume] -out data/naairs_sweep.json   (score index hits for every ancestor)
  gazette            -q '"Wooding, Charles"' [-service all-notices|wills-and-probate|insolvency] [-deceased] [-from 1880 -to 1905] [-edition London] [-out data/gazette_wooding.json]   (The Gazette, the official public record since 1665)
  gazette sweep      -graph data/graph.json -root seed:me [-probable wt:X] [-gen 20] [-max 2] [-delay 1.1s] [-resume] [-dry-run] -out data/gazette.json   (score notices for every British and Irish ancestor)
  tna                -q "Wooding Portsmouth" [-series "PROB 11"] [-held kew|elsewhere|all] [-from 1780 -to 1860] [-list] [-out data/tna_wooding.json]   (UK National Archives Discovery catalogue)
  tna sweep          -graph data/graph.json -root seed:me [-probable wt:X] [-gen 20] [-frontier] [-resume] [-dry-run] -out data/tna.json   (wills, death duties and record offices for every British ancestor)
  war                -graph data/graph.json -from seed:me [-min-birth 1855] [-max-birth 1927] [-boer] -out data/war.json   (military records for the men of the tree: UK National Archives series and the SA archives for 1899-1903)
  report             -root seed:me [-reader seed:me] -title "…" [-probable wt:X] [-note "…"] -out dist/report.html   (printable ancestry report)
  leads              -graph data/graph.json -root seed:me [-probable wt:X] [-upstream wt:] [-searched seed/searched.json] [-id fs:X] [-out dist/leads.html]   (search links for every ancestor still missing a parent; nothing is fetched)
  version            print the version, commit and build date
`)
	os.Exit(2)
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func main() {
	httpx.Version = version
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()
	switch os.Args[1] {
	case "version", "-version", "--version":
		fmt.Printf("kin %s (%s, built %s)\n", version, commit, date)
	case "wikidata":
		cmdWikidata(ctx, os.Args[2:])
	case "wikitree":
		cmdWikitree(ctx, os.Args[2:])
	case "eggsa":
		cmdEggsa(ctx, os.Args[2:])
	case "graph":
		cmdGraph(ctx, os.Args[2:])
	case "viz":
		cmdViz(os.Args[2:])
	case "tree":
		cmdTree(os.Args[2:])
	case "war":
		cmdWar(ctx, os.Args[2:])
	case "map":
		cmdMap(ctx, os.Args[2:])
	case "report":
		cmdReport(os.Args[2:])
	case "leads":
		cmdLeads(os.Args[2:])
	case "naairs":
		cmdNaairs(ctx, os.Args[2:])
	case "gazette":
		cmdGazette(ctx, os.Args[2:])
	case "tna":
		cmdTNA(ctx, os.Args[2:])
	default:
		usage()
	}
}

// ---------------------------------------------------------------- wikidata

func cmdWikidata(ctx context.Context, args []string) {
	if len(args) < 1 {
		usage()
	}
	c := wikidata.New()
	switch args[0] {
	case "surname":
		fs := flag.NewFlagSet("wikidata surname", flag.ExitOnError)
		var names multi
		fs.Var(&names, "name", "surname (repeatable)")
		out := fs.String("out", "data/wd_surname.json", "output graph json")
		fs.Parse(args[1:])
		if len(names) == 0 {
			die(fmt.Errorf("-name is required"))
		}
		var qids []string
		for _, n := range names {
			ids, err := c.BySurname(ctx, n)
			die(err)
			logf("wikidata: %d people with family name %q (P734)", len(ids), n)
			qids = append(qids, ids...)
			sids, err := c.SearchHumans(ctx, n, 500)
			die(err)
			logf("wikidata: %d humans with %q in label (search)", len(sids), n)
			qids = append(qids, sids...)
		}
		qids = model.Uniq(qids)
		people, err := c.Details(ctx, qids)
		die(err)
		g := model.NewGraph()
		want := map[string]bool{}
		for _, n := range names {
			want[strings.ToLower(n)] = true
		}
		for _, p := range people {
			// keep only people who actually carry the surname (search is fuzzy)
			if !hasSurname(p, want) {
				continue
			}
			g.Add(p)
		}
		logf("wikidata: kept %d people carrying the surname", len(g.Persons))
		die(g.Save(*out))
		printPeople(g)
	case "place":
		fs := flag.NewFlagSet("wikidata place", flag.ExitOnError)
		name := fs.String("name", "", "birthplace label prefix (required)")
		country := fs.String("country", "Q258", "country QID (Q258 = South Africa)")
		out := fs.String("out", "data/wd_place.json", "output graph json")
		fs.Parse(args[1:])
		need("name", *name)
		qids, places, err := c.ByBirthplace(ctx, *name, *country)
		die(err)
		for q, l := range places {
			logf("wikidata: place %s = %s", q, l)
		}
		logf("wikidata: %d people born in %s*", len(qids), *name)
		people, err := c.Details(ctx, qids)
		die(err)
		g := model.NewGraph()
		for _, p := range people {
			g.Add(p)
		}
		die(g.Save(*out))
		printPeople(g)
	default:
		usage()
	}
}

func hasSurname(p *model.Person, want map[string]bool) bool {
	if want[strings.ToLower(p.Surname)] {
		return true
	}
	for _, tok := range strings.Fields(strings.ToLower(strings.NewReplacer("-", " ", ",", " ", "(", " ", ")", " ").Replace(p.Name))) {
		if want[tok] {
			return true
		}
	}
	return false
}

func printPeople(g *model.Graph) {
	for _, p := range g.Sorted() {
		fmt.Printf("%-28s %-10s %-24s %s\n", trunc(p.Name, 28), model.Year(p.Birth), trunc(p.BirthPlace, 24), trunc(strings.Join(p.Occupations, ", "), 60))
	}
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// ---------------------------------------------------------------- wikitree

func cmdWikitree(ctx context.Context, args []string) {
	if len(args) < 1 {
		usage()
	}
	c := wikitree.New()
	switch args[0] {
	case "search":
		fs := flag.NewFlagSet("wikitree search", flag.ExitOnError)
		last := fs.String("last", "", "last name (required)")
		first := fs.String("first", "", "first name")
		birthloc := fs.String("birthloc", "", "birth location substring")
		birth := fs.String("birth", "", "birth date (YYYY or YYYY-MM-DD)")
		spread := fs.Int("spread", 0, "years of tolerance around -birth")
		father := fs.String("father", "", "father's name, e.g. 'John Smith'")
		mother := fs.String("mother", "", "mother's name")
		match := fs.String("match", "birth", "lastNameMatch: current|birth|strict|''")
		variants := fs.Bool("variants", false, "include surname spelling variants")
		max := fs.Int("max", 1000, "max profiles")
		out := fs.String("out", "data/wt_search.json", "output graph json")
		fs.Parse(args[1:])
		need("last", *last)
		opts := wikitree.SearchOptions{
			LastName: *last, FirstName: *first, BirthLocation: *birthloc, BirthDate: *birth, DateSpread: *spread,
			LastNameMatch: *match, SkipVariants: !*variants,
		}
		if *father != "" {
			f := strings.Fields(*father)
			opts.FatherFirstName = strings.Join(f[:len(f)-1], " ")
			opts.FatherLastName = f[len(f)-1]
		}
		if *mother != "" {
			f := strings.Fields(*mother)
			opts.MotherFirstName = strings.Join(f[:len(f)-1], " ")
			opts.MotherLastName = f[len(f)-1]
		}
		profiles, total, err := c.SearchAll(ctx, opts, *max)
		die(err)
		logf("wikitree: %d matches (total reported %d)", len(profiles), total)
		g := profilesToGraph(profiles)
		die(g.Save(*out))
		for _, p := range g.Sorted() {
			fmt.Printf("%-16s %-32s %-10s %-10s %s\n", p.WikiTree, trunc(p.Name, 32), model.Year(p.Birth), model.Year(p.Death), trunc(p.BirthPlace, 50))
		}
	case "expand":
		fs := flag.NewFlagSet("wikitree expand", flag.ExitOnError)
		keys := fs.String("keys", "", "comma-separated WikiTree IDs to expand")
		in := fs.String("in", "", "graph json whose wt: profiles all get expanded (alternative to -keys)")
		anc := fs.Int("anc", 10, "ancestor generations")
		desc := fs.Int("desc", 10, "descendant generations")
		relatives := fs.Bool("relatives", true, "also fetch spouses/siblings/children for every profile")
		out := fs.String("out", "data/wt_tree.json", "output graph json")
		fs.Parse(args[1:])
		var ks []string
		if *keys != "" {
			ks = strings.Split(*keys, ",")
		}
		if *in != "" {
			g, err := model.Load(*in)
			die(err)
			for _, p := range g.Persons {
				if p.WikiTree != "" {
					ks = append(ks, p.WikiTree)
				}
			}
		}
		ks = model.Uniq(ks)
		if len(ks) == 0 {
			die(fmt.Errorf("no keys"))
		}
		all := map[string]*wikitree.Profile{}
		add := func(ps []*wikitree.Profile) {
			for _, p := range ps {
				if p != nil && p.Name != "" {
					if e, ok := all[p.Name]; !ok || (e.Father == 0 && p.Father != 0) {
						all[p.Name] = p
					}
				}
			}
		}
		for i, k := range ks {
			a, err := c.Ancestors(ctx, k, *anc)
			if err != nil {
				logf("wikitree: ancestors %s: %v", k, err)
			}
			d, err := c.Descendants(ctx, k, *desc)
			if err != nil {
				logf("wikitree: descendants %s: %v", k, err)
			}
			add(a)
			add(d)
			logf("wikitree: [%d/%d] %s: %d ancestors, %d descendants (total %d)", i+1, len(ks), k, len(a), len(d), len(all))
		}
		// second wave: descendants of the top-most ancestors pull in the whole clan
		if *desc > 0 {
			var roots []string
			for _, p := range all {
				if p.Father == 0 && p.Mother == 0 && strings.EqualFold(p.LastNameAtBirth, surnameOf(ks)) {
					roots = append(roots, p.Name)
				}
			}
			sort.Strings(roots)
			logf("wikitree: %d root %s ancestors; fetching their descendants", len(roots), surnameOf(ks))
			for _, r := range roots {
				d, err := c.Descendants(ctx, r, *desc)
				if err != nil {
					logf("wikitree: descendants %s: %v", r, err)
					continue
				}
				add(d)
			}
		}
		if *relatives {
			names := make([]string, 0, len(all))
			for n := range all {
				names = append(names, n)
			}
			sort.Strings(names)
			logf("wikitree: fetching relatives for %d profiles", len(names))
			rel, err := c.Relatives(ctx, names)
			if err != nil {
				logf("wikitree: relatives: %v", err)
			}
			for _, p := range rel {
				if e, ok := all[p.Name]; ok {
					e.Spouses = p.Spouses
					e.Children = p.Children
					e.Siblings = p.Siblings
					e.Parents = p.Parents
					for _, m := range []wikitree.PersonMap{p.Spouses, p.Children, p.Siblings, p.Parents} {
						for _, q := range m {
							if q != nil && q.Name != "" {
								if _, ok := all[q.Name]; !ok {
									all[q.Name] = q
								}
							}
						}
					}
				}
			}
		}
		profiles := make([]*wikitree.Profile, 0, len(all))
		for _, p := range all {
			profiles = append(profiles, p)
		}
		g := profilesToGraph(profiles)
		die(g.Save(*out))
		logf("wikitree: saved %d profiles to %s", len(g.Persons), *out)
	case "relatives":
		fs := flag.NewFlagSet("wikitree relatives", flag.ExitOnError)
		in := fs.String("in", "data/wt_tree.json", "graph json to enrich")
		out := fs.String("out", "", "output graph json (default: overwrite -in)")
		fs.Parse(args[1:])
		if *out == "" {
			*out = *in
		}
		g, err := model.Load(*in)
		die(err)
		var keys []string
		for _, p := range g.Persons {
			if p.WikiTree != "" {
				keys = append(keys, p.WikiTree)
			}
		}
		sort.Strings(keys)
		logf("wikitree: fetching relatives for %d profiles", len(keys))
		rel, err := c.Relatives(ctx, keys)
		if err != nil {
			logf("wikitree: relatives: %v", err)
		}
		idToName := map[int64]string{}
		var profiles []*wikitree.Profile
		for _, p := range rel {
			profiles = append(profiles, p)
			for _, m := range []wikitree.PersonMap{p.Spouses, p.Children, p.Siblings, p.Parents} {
				for _, q := range m {
					if q != nil && q.Name != "" {
						profiles = append(profiles, q)
					}
				}
			}
		}
		for _, p := range profiles {
			if p.Id > 0 {
				idToName[int64(p.Id)] = p.Name
			}
		}
		added := 0
		for _, p := range profiles {
			if m := wikitree.ToPerson(p, idToName); m != nil {
				if _, ok := g.Persons[m.ID]; !ok {
					added++
				}
				g.Add(m)
			}
		}
		// resolve placeholder parent ids now that more ids are known
		for _, p := range g.Persons {
			for _, ref := range []*string{&p.Father, &p.Mother} {
				if strings.HasPrefix(*ref, "wt:id:") {
					var id int64
					fmt.Sscanf(strings.TrimPrefix(*ref, "wt:id:"), "%d", &id)
					if n, ok := idToName[id]; ok {
						*ref = "wt:" + n
					}
				}
			}
		}
		logf("wikitree: relatives merged; %d new profiles, %d total", added, len(g.Persons))
		die(g.Save(*out))
	default:
		usage()
	}
}

func surnameOf(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	k := keys[0]
	if i := strings.LastIndex(k, "-"); i > 0 {
		return k[:i]
	}
	return k
}

func profilesToGraph(profiles []*wikitree.Profile) *model.Graph {
	idToName := map[int64]string{}
	for _, p := range profiles {
		if p.Id > 0 {
			idToName[int64(p.Id)] = p.Name
		}
	}
	g := model.NewGraph()
	for _, p := range profiles {
		if m := wikitree.ToPerson(p, idToName); m != nil {
			g.Add(m)
		}
	}
	return g
}

// ---------------------------------------------------------------- eggsa

func cmdEggsa(ctx context.Context, args []string) {
	if len(args) < 1 {
		usage()
	}
	if args[0] == "papers" {
		fs := flag.NewFlagSet("eggsa papers", flag.ExitOnError)
		surname := fs.String("surname", "", "surname to look for in newspaper extracts (required)")
		cache := fs.String("cache", "data/cache/papers", "page cache dir")
		workers := fs.Int("workers", 2, "concurrent fetches")
		out := fs.String("out", "data/papers.json", "output json (list of notices)")
		fs.Parse(args[1:])
		need("surname", *surname)
		notices, err := eggsa.Papers(ctx, *surname, *cache, *workers, logf)
		die(err)
		b, _ := json.MarshalIndent(notices, "", "  ")
		die(os.WriteFile(*out, b, 0o644))
		for _, n := range notices {
			fmt.Printf("[%s] %s\n    %s\n", n.Paper, n.Page, n.Text)
		}
		logf("papers: %d notices mentioning %s saved to %s", len(notices), *surname, *out)
		return
	}
	if args[0] != "graves" {
		usage()
	}
	fs := flag.NewFlagSet("eggsa graves", flag.ExitOnError)
	surname := fs.String("surname", "", "surname (required)")
	out := fs.String("out", "data/eggsa.json", "output graph json")
	fs.Parse(args[1:])
	need("surname", *surname)
	graves, err := eggsa.Search(ctx, *surname, logf)
	die(err)
	g := model.NewGraph()
	for _, gr := range graves {
		g.Add(gr.ToPerson())
		fmt.Printf("%-14s %-40s %-5s %-5s %s\n", gr.Province, trunc(gr.Text, 40), gr.Birth, gr.Death, gr.Cemetery)
	}
	die(g.Save(*out))
	logf("eggsa: %d gravestones saved to %s", len(g.Persons), *out)
}

// ---------------------------------------------------------------- graph

func cmdGraph(ctx context.Context, args []string) {
	if len(args) < 1 {
		usage()
	}
	switch args[0] {
	case "build":
		fs := flag.NewFlagSet("graph build", flag.ExitOnError)
		seed := fs.String("seed", "seed.json", "seed graph json (hand-entered persons)")
		in := fs.String("in", "", "comma-separated source graph json files")
		out := fs.String("out", "data/graph.json", "merged graph json")
		fs.Parse(args[1:])
		g := model.NewGraph()
		files := []string{*seed}
		if *in != "" {
			files = append(files, strings.Split(*in, ",")...)
		}
		for _, f := range files {
			src, err := model.Load(f)
			if err != nil {
				logf("graph: skip %s: %v", f, err)
				continue
			}
			for _, p := range src.Persons {
				g.Add(p)
			}
			logf("graph: +%s (%d persons)", filepath.Base(f), len(src.Persons))
		}
		merged := crossLink(g)
		logf("graph: %d persons after merging %d cross-source duplicates", len(g.Persons), merged)
		die(g.Save(*out))
	case "redact":
		fs := flag.NewFlagSet("graph redact", flag.ExitOnError)
		gp := fs.String("graph", "data/graph.json", "graph json")
		out := fs.String("out", "data/graph.public.json", "redacted graph json")
		fs.Parse(args[1:])
		g, err := model.Load(*gp)
		die(err)
		r, n := g.Redacted()
		logf("graph: redacted %d living of %d persons", n, len(r.Persons))
		die(r.Save(*out))
	case "kin":
		fs := flag.NewFlagSet("graph kin", flag.ExitOnError)
		gp := fs.String("graph", "data/graph.json", "graph json")
		from := fs.String("from", "", "person id (required)")
		to := fs.String("to", "", "person id (required)")
		fs.Parse(args[1:])
		need("from", *from)
		need("to", *to)
		g, err := model.Load(*gp)
		die(err)
		rel, ok := graph.Relationship(g, *from, *to)
		if !ok {
			fmt.Printf("%s and %s share no known ancestor in the graph\n", *from, *to)
			return
		}
		fmt.Printf("%s is the %s of %s\n", nameOf(g, *to), rel.Label, nameOf(g, *from))
		for _, c := range rel.Common {
			fmt.Printf("  via %s (%s)\n", nameOf(g, c), c)
		}
	case "report":
		fs := flag.NewFlagSet("graph report", flag.ExitOnError)
		gp := fs.String("graph", "data/graph.json", "graph json")
		from := fs.String("from", "", "seed person id (required)")
		fs.Parse(args[1:])
		need("from", *from)
		g, err := model.Load(*gp)
		die(err)
		comps := graph.Components(g)
		seedComp := comps[*from]
		network := 0
		for _, c := range comps {
			if c == seedComp {
				network++
			}
		}
		anc := graph.Ancestors(g, *from)
		delete(anc, *from)
		byGen := map[int]int{}
		maxGen, earliestYear, earliest := 0, "9999", ""
		for id, gen := range anc {
			byGen[gen]++
			if gen > maxGen {
				maxGen = gen
			}
			if p := g.Persons[id]; p != nil {
				if y := model.Year(p.Birth); y != "" && y < earliestYear {
					earliestYear, earliest = y, p.Name
				}
			}
		}
		fmt.Printf("persons: %d   family network: %d   documented ancestors: %d   generations: %d\n", len(g.Persons), network, len(anc), maxGen)
		if earliest != "" {
			fmt.Printf("earliest dated ancestor: %s (%s)\n", earliest, earliestYear)
		}
		fmt.Println("ancestors by generation:")
		for gen := 1; gen <= maxGen; gen++ {
			fmt.Printf("  %2d  %4d of %d\n", gen, byGen[gen], 1<<gen)
		}
	default:
		usage()
	}
}

func nameOf(g *model.Graph, id string) string {
	if p := g.Persons[id]; p != nil && p.Name != "" {
		return p.Name
	}
	return id
}

// crossLink merges persons that different sources describe (same Wikidata or
// WikiTree identifier) and rewrites references to the surviving ID.
func crossLink(g *model.Graph) int {
	alias := map[string]string{}
	byKey := map[string]string{} // "wd:Q1"/"wt:X" -> canonical id
	rank := func(id string) int {
		switch {
		case strings.HasPrefix(id, "seed:"):
			return 0
		case strings.HasPrefix(id, "wt:"):
			return 1
		case strings.HasPrefix(id, "wd:"):
			return 2
		}
		return 3
	}
	ids := make([]string, 0, len(g.Persons))
	for id := range g.Persons {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if rank(ids[i]) != rank(ids[j]) {
			return rank(ids[i]) < rank(ids[j])
		}
		return ids[i] < ids[j]
	})
	merged := 0
	for _, id := range ids {
		p := g.Persons[id]
		if p == nil {
			continue
		}
		var keys []string
		if p.Wikidata != "" {
			keys = append(keys, "wd:"+p.Wikidata)
		}
		if p.WikiTree != "" {
			keys = append(keys, "wt:"+p.WikiTree)
		}
		target := ""
		for _, k := range keys {
			if t, ok := byKey[k]; ok && t != id {
				target = t
				break
			}
		}
		if target != "" {
			g.Persons[target].Merge(p)
			delete(g.Persons, id)
			alias[id] = target
			merged++
			for _, k := range keys {
				byKey[k] = target
			}
			continue
		}
		for _, k := range keys {
			byKey[k] = id
		}
	}
	resolve := func(id string) string {
		for i := 0; i < 5; i++ {
			t, ok := alias[id]
			if !ok {
				return id
			}
			id = t
		}
		return id
	}
	for _, p := range g.Persons {
		p.Father = resolve(p.Father)
		p.Mother = resolve(p.Mother)
		for i, s := range p.Spouses {
			p.Spouses[i] = resolve(s)
		}
		p.Spouses = model.Uniq(p.Spouses)
	}
	if g.Aliases == nil {
		g.Aliases = map[string]string{}
	}
	for from := range alias {
		g.Aliases[from] = resolve(from)
	}
	return merged
}

// ---------------------------------------------------------------- viz

func cmdViz(args []string) {
	fs := flag.NewFlagSet("viz", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	seed := fs.String("seed", "", "person id the page is about (required)")
	out := fs.String("out", "dist/index.html", "output html")
	site := fs.String("site", "", "site json: title, eyebrow, flags, extra ordering rows (optional)")
	treeURL := fs.String("tree-url", "", "absolute URL of the published family tree page, linked from the header (optional)")
	mapURL := fs.String("map-url", "", "absolute URL of the published map page, linked from the header (optional)")
	notices := fs.String("notices", "data/papers.json", "newspaper notices json from `kin eggsa papers` (optional)")
	records := fs.String("records", "records.json", "hand-collected record citations json (optional)")
	var probable multi
	fs.Var(&probable, "probable", "person id from which upward the line is only probable (repeatable)")
	fs.Parse(args)
	need("seed", *seed)
	g, err := model.Load(*gp)
	die(err)
	st, err := viz.LoadSite(*site)
	die(err)
	die(os.MkdirAll(filepath.Dir(*out), 0o755))
	die(viz.Render(g, viz.Options{Seed: *seed, NoticesPath: *notices, RecordsPath: *records, ProbableIDs: probable, Site: st, TreeURL: *treeURL, MapURL: *mapURL}, *out))
	logf("viz: wrote %s", *out)
}

// ---------------------------------------------------------------- tree

func cmdTree(args []string) {
	fs := flag.NewFlagSet("tree", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	root := fs.String("root", "", "person whose ancestors are drawn (required)")
	reader := fs.String("reader", "", "person the relationship labels are relative to (default: -root)")
	gen := fs.Int("gen", 20, "generations above root to draw")
	site := fs.String("site", "", "site json: title and eyebrow are used (optional)")
	records := fs.String("records", "records.json", "hand-collected record citations json (optional)")
	dash := fs.String("dashboard-url", "", "absolute URL of the published dashboard page, linked from the panel (optional)")
	out := fs.String("out", "dist/tree.html", "output html")
	var probable multi
	fs.Var(&probable, "probable", "person id from which upward the line is only probable (repeatable)")
	fs.Parse(args)
	need("root", *root)
	g, err := model.Load(*gp)
	die(err)
	st, err := viz.LoadSite(*site)
	die(err)
	die(os.MkdirAll(filepath.Dir(*out), 0o755))
	die(tree.Render(g, tree.Options{Root: *root, Reader: *reader, MaxGen: *gen, ProbableIDs: probable, RecordsPath: *records, DashboardURL: *dash, Site: st}, *out))
	logf("tree: wrote %s", *out)
}

// ---------------------------------------------------------------- report

func cmdLeads(args []string) {
	fs := flag.NewFlagSet("leads", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	root := fs.String("root", "", "person whose ancestors are examined (required unless -id)")
	only := fs.String("id", "", "compose leads for this one person instead of the frontier")
	gen := fs.Int("gen", 20, "generations above root to examine")
	title := fs.String("title", "Research leads", "title of the html page")
	out := fs.String("out", "", "also write a self-contained html page here (optional)")
	searched := fs.String("searched", "", "json list of searches already made ({person, service, when, note}), shown under each person (optional)")
	var probable, upstream multi
	fs.Var(&probable, "probable", "person id whose link to their parents is unproven (repeatable)")
	fs.Var(&upstream, "upstream", "id prefix whose parentless people are another site's ends and are left off the frontier, e.g. wt: (repeatable)")
	fs.Parse(args)
	if *only == "" {
		need("root", *root)
	}
	g, err := model.Load(*gp)
	die(err)
	var done map[string][]leads.Searched
	if *searched != "" {
		done, err = leads.LoadSearched(*searched)
		die(err)
	}
	entries := leads.Build(g, leads.Options{Root: *root, ProbableIDs: probable, Only: *only, MaxGen: *gen, Upstream: upstream, Searched: done})
	leads.WriteText(os.Stdout, entries)
	if *out != "" {
		die(os.MkdirAll(filepath.Dir(*out), 0o755))
		die(leads.Render(entries, *title, *out))
		logf("leads: %d people, wrote %s", len(entries), *out)
	} else {
		logf("leads: %d people", len(entries))
	}
}

func cmdReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	root := fs.String("root", "", "person whose ancestry is reported (required)")
	reader := fs.String("reader", "", "person the relationship labels are relative to (default: -root)")
	title := fs.String("title", "", "report title")
	subtitle := fs.String("subtitle", "", "one-line subtitle")
	maxGen := fs.Int("gen", 8, "generations above root")
	records := fs.String("records", "records.json", "hand-collected record citations json (optional)")
	out := fs.String("out", "dist/report.html", "output html")
	var probable, notes multi
	fs.Var(&probable, "probable", "person id from which upward the line is only probable (repeatable)")
	fs.Var(&notes, "note", "note to print under open questions (repeatable)")
	fs.Parse(args)
	need("root", *root)
	if *reader == "" {
		*reader = *root
	}
	g, err := model.Load(*gp)
	die(err)
	if *title == "" {
		*title = "Ancestry of " + nameOf(g, *root)
	}
	die(os.MkdirAll(filepath.Dir(*out), 0o755))
	die(report.Render(g, report.Options{Root: *root, Reader: *reader, Title: *title, Subtitle: *subtitle, MaxGen: *maxGen,
		ProbableIDs: probable, Notes: notes, RecordsPath: *records}, *out))
	logf("report: wrote %s", *out)
}

// ---------------------------------------------------------------- naairs

func cmdNaairs(ctx context.Context, args []string) {
	if len(args) > 0 && args[0] == "sweep" {
		cmdNaairsSweep(ctx, args[1:])
		return
	}
	fs := flag.NewFlagSet("naairs", flag.ExitOnError)
	db := fs.String("db", "RSA", "repository code: RSA (all), TAB, KAB, NAB, VAB, TBD, TBE, TBK, SAB, GEN")
	q := fs.String("q", "", "search words, ANDed (e.g. \"SMITH JOHN HENRY\")")
	from := fs.String("from", "", "starting year (CCYY)")
	to := fs.String("to", "", "ending year (CCYY)")
	out := fs.String("out", "", "write hits as json")
	fs.Parse(args)
	if *q == "" {
		die(fmt.Errorf("-q required"))
	}
	recs, n, err := naairs.New().Query(ctx, *db, strings.Fields(*q), *from, *to)
	die(err)
	logf("naairs: %s (%s): %d documents, %d parsed", *db, naairs.Repositories[*db], n, len(recs))
	for _, r := range recs {
		fmt.Printf("%-4s %-6s %-4s vol %-6s ref %-14s %s  [%s-%s]\n", r.Depot, r.Source, r.Type, r.Volume, r.Reference, r.Description, r.Starting, r.Ending)
	}
	if *out != "" {
		b, _ := json.MarshalIndent(recs, "", "  ")
		die(os.WriteFile(*out, b, 0o644))
	}
}

// cmdNaairsSweep queries the archives index once per documented ancestor and
// writes scored candidates, saving after every person so a run can resume.
func cmdNaairsSweep(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("naairs sweep", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	from := fs.String("from", "", "person whose ancestors are swept (required)")
	gen := fs.Int("gen", 20, "generations above -from")
	db := fs.String("db", "RSA", "repository code (RSA = all)")
	delay := fs.Duration("delay", 3*time.Second, "pause between queries")
	resume := fs.Bool("resume", false, "skip persons already in -out")
	redoCapped := fs.Bool("redo-capped", false, "with -resume: query again the persons whose earlier result hit the detail cap without a death-year pass")
	out := fs.String("out", "data/naairs_sweep.json", "results json")
	fs.Parse(args)
	need("from", *from)
	g, err := model.Load(*gp)
	die(err)
	root := g.Resolve(*from)
	anc := graph.Ancestors(g, root)
	delete(anc, root)
	var ids []string
	for id, d := range anc {
		if p := g.Persons[id]; p != nil && d <= *gen && !p.Living {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if anc[ids[i]] != anc[ids[j]] {
			return anc[ids[i]] < anc[ids[j]]
		}
		return ids[i] < ids[j]
	})
	results := map[string]naairs.SweepResult{}
	if *resume {
		if b, err := os.ReadFile(*out); err == nil {
			var prev []naairs.SweepResult
			if json.Unmarshal(b, &prev) == nil {
				for _, r := range prev {
					if r.Err != "" {
						continue
					}
					if *redoCapped && r.Total > 130 && !r.Capped && model.Year(r.Death) != "" {
						continue
					}
					results[r.ID] = r
				}
			}
		}
	}
	var todo []string
	for _, id := range ids {
		if _, done := results[id]; !done {
			todo = append(todo, id)
		}
	}
	logf("naairs sweep: %d ancestors, %d to query (%s)", len(ids), len(todo), *db)
	save := func() {
		list := make([]naairs.SweepResult, 0, len(results))
		for _, id := range ids {
			if r, ok := results[id]; ok {
				list = append(list, r)
			}
		}
		b, _ := json.MarshalIndent(list, "", "  ")
		die(os.MkdirAll(filepath.Dir(*out), 0o755))
		die(os.WriteFile(*out, b, 0o644))
	}
	done := 0
	naairs.New().Sweep(ctx, *db, g, todo, *delay, func(r naairs.SweepResult) {
		done++
		results[r.ID] = r
		save()
		if r.Err != "" {
			logf("[%d/%d] %s: error %s", done, len(todo), r.Name, r.Err)
			return
		}
		logf("[%d/%d] %s (%s-%s): %d documents, %d candidates", done, len(todo), r.Name, model.Year(r.Birth), model.Year(r.Death), r.Total, len(r.Hits))
		for i, h := range r.Hits {
			if i >= 3 {
				break
			}
			fmt.Printf("  %d  %s %s %s  %s [%s]  %s\n", h.Score, h.Depot, h.Source, h.Reference, h.Description, model.Year(h.Starting), strings.Join(h.Why, ", "))
		}
	})
	logf("naairs sweep: wrote %s", *out)
}

// ---------------------------------------------------------------- war

func cmdWar(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("war", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	from := fs.String("from", "", "person whose ancestors and their sons are checked (required)")
	minB := fs.Int("min-birth", 1855, "earliest birth year")
	maxB := fs.Int("max-birth", 1927, "latest birth year")
	boer := fs.Bool("boer", true, "also query the South African archives index for the war years 1899-1903")
	delay := fs.Duration("delay", 800*time.Millisecond, "pause between remote calls")
	out := fs.String("out", "data/war.json", "results json")
	fs.Parse(args)
	need("from", *from)
	g, err := model.Load(*gp)
	die(err)
	men := war.Men(g, *from, *minB, *maxB)
	logf("war: %d men of military age", len(men))
	results := war.Run(ctx, g, men, war.Options{Delay: *delay, Boer: *boer, Log: logf})
	for _, r := range results {
		fmt.Printf("\n%s (%s-%s) %s: %s\n", r.Name, r.Birth, r.Death, r.Role, strings.Join(r.Wars, "; "))
		for i, h := range r.Hits {
			if i >= 5 {
				break
			}
			fmt.Printf("  %d  %-6s %-22s %s [%s]  %s\n", h.Score, h.Source, h.Reference, trunc(h.Description, 110), h.Dates, strings.Join(h.Why, ", "))
		}
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	die(os.MkdirAll(filepath.Dir(*out), 0o755))
	die(os.WriteFile(*out, b, 0o644))
	logf("war: wrote %s", *out)
}

// ---------------------------------------------------------------- map

func cmdMap(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("map", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	root := fs.String("root", "", "person whose ancestors are mapped (required)")
	reader := fs.String("reader", "", "person the relationship labels are relative to (default: -root)")
	gen := fs.Int("gen", 20, "generations above root")
	site := fs.String("site", "", "site json: title and eyebrow are used (optional)")
	cache := fs.String("cache", "data/cache/geo.json", "geocoding cache json, read and written")
	placesPath := fs.String("places", "", "manual coordinates json: {\"place string\": {\"lat\":..,\"lon\":..,\"label\":..}} (optional)")
	offline := fs.Bool("offline", false, "use the cache and overrides only; never call the geocoder")
	dash := fs.String("dashboard-url", "", "absolute URL of the published dashboard (optional)")
	treeURL := fs.String("tree-url", "", "absolute URL of the published family tree (optional)")
	out := fs.String("out", "dist/map.html", "output html")
	fs.Parse(args)
	need("root", *root)
	g, err := model.Load(*gp)
	die(err)
	st, err := viz.LoadSite(*site)
	die(err)
	die(os.MkdirAll(filepath.Dir(*out), 0o755))
	if *cache != "" {
		die(os.MkdirAll(filepath.Dir(*cache), 0o755))
	}
	die(geomap.Render(ctx, g, geomap.Options{Root: *root, Reader: *reader, MaxGen: *gen, CachePath: *cache, PlacesPath: *placesPath, Offline: *offline,
		DashboardURL: *dash, TreeURL: *treeURL, Site: st, Log: logf}, *out))
	logf("map: wrote %s", *out)
}

// ---------------------------------------------------------------- gazette

func cmdGazette(ctx context.Context, args []string) {
	if len(args) > 0 && args[0] == "sweep" {
		cmdGazetteSweep(ctx, args[1:])
		return
	}
	fs := flag.NewFlagSet("gazette", flag.ExitOnError)
	q := fs.String("q", "", "words to find; quote a phrase (e.g. '\"Wooding, Charles\"')")
	service := fs.String("service", gazette.All, "all-notices, wills-and-probate or insolvency")
	deceased := fs.Bool("deceased", false, "shorthand for -service wills-and-probate")
	from := fs.Int("from", 0, "earliest year of publication (CCYY)")
	to := fs.Int("to", 0, "latest year of publication (CCYY)")
	edition := fs.String("edition", "", "London, Edinburgh or Belfast")
	notice := fs.String("type", "", "notice code, e.g. 2903 for deceased estates")
	max := fs.Int("max", 2, "pages of results to read")
	cache := fs.String("cache", "data/cache/gazette", "directory of cached answers; empty disables it")
	out := fs.String("out", "", "write the notices as json")
	fs.Parse(args)
	need("q", *q)
	if *deceased {
		*service = gazette.Probate
	}
	query := gazette.Query{Service: *service, Text: *q, Edition: *edition, Sort: "oldest-date"}
	if *from > 0 {
		query.StartPublish = fmt.Sprintf("%d-01-01", *from)
	}
	if *to > 0 {
		query.EndPublish = fmt.Sprintf("%d-12-31", *to)
	}
	if *notice != "" {
		query.NoticeTypes = []string{*notice}
	}
	notices, total, err := gazette.New(*cache).Search(ctx, query, *max)
	die(err)
	logf("gazette: %d notices, %d read", total, len(notices))
	for _, n := range notices {
		where := n.Edition
		if n.Issue != "" {
			where = fmt.Sprintf("%s %s/%s", n.Edition, n.Issue, n.Page)
		}
		fmt.Printf("%-10s %-18s %-60s %s\n", n.Published, where, trim(n.Title, 60), n.URL)
		if n.Text != "" {
			fmt.Printf("           %s\n", trim(n.Text, 150))
		}
	}
	if *out != "" {
		b, _ := json.MarshalIndent(notices, "", "  ")
		die(os.MkdirAll(filepath.Dir(*out), 0o755))
		die(os.WriteFile(*out, b, 0o644))
		logf("gazette: wrote %s", *out)
	}
}

// cmdGazetteSweep searches the official notices for every British and Irish
// ancestor, saving after each so a run can resume.
func cmdGazetteSweep(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("gazette sweep", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	root := fs.String("root", "", "person whose ancestors are swept (required)")
	var probable multi
	fs.Var(&probable, "probable", "person whose link to their parents is unproven (repeatable)")
	gen := fs.Int("gen", 20, "generations above -root")
	max := fs.Int("max", 2, "pages of results per query")
	min := fs.Int("min", gazette.Keep, "keep candidates scoring at least this")
	probateFrom := fs.Int("probate-from", 1990, "no date-of-death search for deaths before this year")
	delay := fs.Duration("delay", 1100*time.Millisecond, "pause between requests")
	cache := fs.String("cache", "data/cache/gazette", "directory of cached answers; empty disables it")
	resume := fs.Bool("resume", false, "skip persons already in -out")
	dry := fs.Bool("dry-run", false, "print the queries and stop, fetching nothing")
	out := fs.String("out", "data/gazette.json", "results json")
	fs.Parse(args)
	need("root", *root)
	g, err := model.Load(*gp)
	die(err)
	opts := gazette.Options{Root: *root, ProbableIDs: probable, MaxGen: *gen, MaxPages: *max,
		Min: *min, ProbateFrom: *probateFrom, Log: logf}
	ids := gazette.People(g, opts)

	if *dry {
		kids := place.Kids(g)
		n := 0
		for _, id := range ids {
			p := g.Persons[g.Resolve(id)]
			qs := gazette.Plan(p, place.PlacesOf(g, p, kids[g.Resolve(id)]), opts)
			fmt.Printf("%s (%s-%s)\n", p.Name, model.Year(p.Birth), model.Year(p.Death))
			for _, q := range qs {
				fmt.Printf("  %s\n", q.URL())
				n++
			}
		}
		logf("gazette sweep: %d people, %d queries, up to %d requests", len(ids), n, n**max)
		return
	}

	results := map[string]gazette.SweepResult{}
	if *resume {
		if b, err := os.ReadFile(*out); err == nil {
			var prev []gazette.SweepResult
			if json.Unmarshal(b, &prev) == nil {
				for _, r := range prev {
					if r.Err == "" {
						results[r.ID] = r
					}
				}
			}
		}
	}
	var todo []string
	for _, id := range ids {
		if _, done := results[id]; !done {
			todo = append(todo, id)
		}
	}
	logf("gazette sweep: %d British and Irish ancestors, %d to query", len(ids), len(todo))
	save := func() {
		list := make([]gazette.SweepResult, 0, len(results))
		for _, id := range ids {
			if r, ok := results[id]; ok {
				list = append(list, r)
			}
		}
		b, _ := json.MarshalIndent(list, "", "  ")
		die(os.MkdirAll(filepath.Dir(*out), 0o755))
		die(os.WriteFile(*out, b, 0o644))
	}
	c := gazette.New(*cache)
	c.Delay = *delay
	done := 0
	opts.Log = nil
	c.Sweep(ctx, g, todo, opts, func(r gazette.SweepResult) {
		done++
		results[r.ID] = r
		save()
		if r.Err != "" {
			logf("[%d/%d] %s: error %s", done, len(todo), r.Name, r.Err)
			return
		}
		logf("[%d/%d] %s (%s-%s): %d notices, %d candidates", done, len(todo), r.Name, model.Year(r.Birth), model.Year(r.Death), r.Total, len(r.Hits))
		for i, h := range r.Hits {
			if i >= 3 {
				break
			}
			fmt.Printf("  %d  %s  %s  %s\n", h.Score, h.Published, trim(h.Title, 50), strings.Join(h.Why, ", "))
		}
	})
	logf("gazette sweep: wrote %s", *out)
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---------------------------------------------------------------- tna

func cmdTNA(ctx context.Context, args []string) {
	if len(args) > 0 && args[0] == "sweep" {
		cmdTNASweep(ctx, args[1:])
		return
	}
	fs := flag.NewFlagSet("tna", flag.ExitOnError)
	q := fs.String("q", "", "words to find (e.g. \"Wooding Portsmouth\")")
	var series multi
	fs.Var(&series, "series", "series code such as \"PROB 11\" (repeatable); empty searches the whole catalogue")
	held := fs.String("held", "", "kew, elsewhere or all: where the records are kept")
	from := fs.Int("from", 0, "earliest year of the records (CCYY)")
	to := fs.Int("to", 0, "latest year of the records (CCYY)")
	max := fs.Int("max", 2, "pages of results to read")
	cache := fs.String("cache", "data/cache/tna", "directory of cached answers; empty disables it")
	list := fs.Bool("list", false, "print the series kin knows and stop")
	out := fs.String("out", "", "write the records as json")
	fs.Parse(args)
	if *list {
		all := tna.All()
		for _, code := range tna.Keys(all) {
			fmt.Printf("%-8s %s\n", code, all[code])
		}
		return
	}
	need("q", *q)
	query := tna.Query{Text: *q, Series: series, MaxPages: *max}
	switch strings.ToLower(*held) {
	case "kew":
		query.HeldBy = tna.HeldKew
	case "elsewhere", "other":
		query.HeldBy = tna.HeldElsewhere
	case "all":
		query.HeldBy = tna.HeldAll
	case "":
	default:
		die(fmt.Errorf("-held must be kew, elsewhere or all"))
	}
	if *from > 0 {
		query.DateFrom = fmt.Sprintf("%d-01-01", *from)
	}
	if *to > 0 {
		query.DateTo = fmt.Sprintf("%d-12-31", *to)
	}
	recs, total, err := tna.NewCached(*cache).SearchQuery(ctx, query)
	die(err)
	logf("tna: %d records, %d read", total, len(recs))
	for _, r := range recs {
		fmt.Printf("%-22s %-22s %s\n", r.Reference, trim(r.Dates, 22), trim(r.Text(), 90))
		if r.HeldBy != "" {
			fmt.Printf("%-45s %s\n", "", r.HeldBy)
		}
	}
	if *out != "" {
		b, _ := json.MarshalIndent(recs, "", "  ")
		die(os.MkdirAll(filepath.Dir(*out), 0o755))
		die(os.WriteFile(*out, b, 0o644))
		logf("tna: wrote %s", *out)
	}
}

// cmdTNASweep searches the civil series, and optionally the archives that hold
// records elsewhere, for every British and Irish ancestor.
func cmdTNASweep(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("tna sweep", flag.ExitOnError)
	gp := fs.String("graph", "data/graph.json", "graph json")
	root := fs.String("root", "", "person whose ancestors are swept (required)")
	var probable multi
	fs.Var(&probable, "probable", "person whose link to their parents is unproven (repeatable)")
	gen := fs.Int("gen", 20, "generations above -root")
	max := fs.Int("max", 1, "pages of results per query")
	min := fs.Int("min", tna.Keep, "keep candidates scoring at least this")
	frontier := fs.Bool("frontier", false, "also search the archives that hold records elsewhere, by surname and parish, for ancestors missing a parent")
	delay := fs.Duration("delay", 700*time.Millisecond, "pause between requests")
	cache := fs.String("cache", "data/cache/tna", "directory of cached answers; empty disables it")
	resume := fs.Bool("resume", false, "skip persons already in -out")
	dry := fs.Bool("dry-run", false, "print the queries and stop, fetching nothing")
	out := fs.String("out", "data/tna.json", "results json")
	fs.Parse(args)
	need("root", *root)
	g, err := model.Load(*gp)
	die(err)
	opts := tna.SweepOptions{Root: *root, ProbableIDs: probable, MaxGen: *gen, MaxPages: *max,
		Min: *min, Frontier: *frontier}
	ids := tna.People(g, opts)

	if *dry {
		kids := place.Kids(g)
		seen := map[string]bool{}
		n := 0
		for _, id := range ids {
			p := g.Persons[g.Resolve(id)]
			fmt.Printf("%s (%s-%s)\n", p.Name, model.Year(p.Birth), model.Year(p.Death))
			for _, q := range tna.Plan(p, place.PlacesOf(g, p, kids[g.Resolve(id)]), opts) {
				u := q.URL(1)
				fmt.Printf("  %s\n", u)
				if !seen[u] {
					seen[u] = true
					n++
				}
			}
		}
		logf("tna sweep: %d people, %d distinct queries", len(ids), n)
		return
	}

	results := map[string]tna.SweepResult{}
	if *resume {
		if b, err := os.ReadFile(*out); err == nil {
			var prev []tna.SweepResult
			if json.Unmarshal(b, &prev) == nil {
				for _, r := range prev {
					if r.Err == "" {
						results[r.ID] = r
					}
				}
			}
		}
	}
	var todo []string
	for _, id := range ids {
		if _, done := results[id]; !done {
			todo = append(todo, id)
		}
	}
	logf("tna sweep: %d British and Irish ancestors, %d to query", len(ids), len(todo))
	save := func() {
		list := make([]tna.SweepResult, 0, len(results))
		for _, id := range ids {
			if r, ok := results[id]; ok {
				list = append(list, r)
			}
		}
		b, _ := json.MarshalIndent(list, "", "  ")
		die(os.MkdirAll(filepath.Dir(*out), 0o755))
		die(os.WriteFile(*out, b, 0o644))
	}
	c := tna.NewCached(*cache)
	c.Delay = *delay
	done := 0
	c.Sweep(ctx, g, todo, opts, func(r tna.SweepResult) {
		done++
		results[r.ID] = r
		save()
		if r.Err != "" {
			logf("[%d/%d] %s: error %s", done, len(todo), r.Name, r.Err)
			return
		}
		logf("[%d/%d] %s (%s-%s): %d records, %d candidates", done, len(todo), r.Name, model.Year(r.Birth), model.Year(r.Death), r.Total, len(r.Hits))
		for i, h := range r.Hits {
			if i >= 3 {
				break
			}
			fmt.Printf("  %d  %-20s %s [%s]  %s\n", h.Score, h.Reference, trim(h.Text(), 70), h.Dates, strings.Join(h.Why, ", "))
		}
	})
	logf("tna sweep: wrote %s", *out)
}
