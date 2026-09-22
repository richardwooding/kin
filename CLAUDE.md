# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`kin` is a command-line ancestry toolkit in Go 1.27, **standard library only** (no `go.sum`, no third-party
modules; do not add dependencies). It pulls people from public genealogy sources, merges them into one kinship
graph, labels relationships relative to a chosen person, and renders self-contained HTML pages (dashboard,
pan-and-zoom pedigree, world map) and a printable report. The README documents every subcommand, flag, the
graph JSON schema, the id namespaces and the terms of use of each upstream source; read it before changing
user-facing behaviour, and update it when you do.

## Commands

```sh
go build -o bin/kin ./cmd/kin        # build
go test ./...                        # all tests
go test ./internal/tree -run TestBuildProbableAndRecords   # one test
go vet ./... && test -z "$(gofmt -l .)"                    # CI runs both; gofmt -l must print nothing
go run ./cmd/kin <command> -h        # every subcommand prints its flags
```

The CI smoke test (`.github/workflows/ci.yml`) runs the offline pipeline against `examples/` and greps the
output; run the same sequence locally after touching a renderer:

```sh
go run ./cmd/kin graph build -seed examples/seed.json -out /tmp/kin/graph.json
go run ./cmd/kin graph redact -graph /tmp/kin/graph.json -out /tmp/kin/graph.public.json
go run ./cmd/kin viz  -graph /tmp/kin/graph.json -seed seed:me -site examples/site.json -records examples/records.json -notices "" -out /tmp/kin/index.html
go run ./cmd/kin tree -graph /tmp/kin/graph.json -root seed:me -site examples/site.json -records examples/records.json -out /tmp/kin/tree.html
go run ./cmd/kin map  -graph /tmp/kin/graph.json -root seed:me -site examples/site.json -offline -cache "" -places examples/places.json -out /tmp/kin/map.html
go run ./cmd/kin report -graph /tmp/kin/graph.json -root seed:me -records examples/records.json -out /tmp/kin/report.html
go run ./cmd/kin leads -graph /tmp/kin/graph.json -root seed:me -out /tmp/kin/leads.html
go run ./cmd/kin gazette sweep -graph /tmp/kin/graph.json -root seed:me -dry-run   # plans the queries, fetches nothing
go run ./cmd/kin tna sweep -graph /tmp/kin/graph.json -root seed:me -frontier -dry-run
go run ./cmd/kin riksarkivet sweep -graph /tmp/kin/graph.json -root seed:me -dry-run
go run ./cmd/kin linklives sweep -graph /tmp/kin/graph.json -root seed:me -dry-run   # lists files under -dir, reads no rows
go run ./cmd/kin news sweep -graph /tmp/kin/graph.json -root seed:me -dry-run
```

Releases are cut by pushing a `v*` tag; GoReleaser (`.goreleaser.yaml`) builds archives, a ghcr.io image via ko
and a Homebrew cask. CI runs `goreleaser check`, so keep that file valid. `data/`, `dist/`, `seed/` and `bin/`
are gitignored working directories for real family data and output.

## Architecture

**Pipeline shape.** Every command reads and writes JSON files on disk; nothing is held between commands.
Source commands (`wikitree`, `eggsa`, `naairs`, `wikidata`, `war`, `gazette`, `tna`, `riksarkivet`,
`linklives`, `news`) each write a graph or result file.
`graph build` merges seed plus source graphs into `data/graph.json`; `graph redact` optionally rewrites it with
living people reduced to names and links. Renderers (`viz`, `tree`, `map`, `report`) read a graph plus optional
side files (records, notices, site, places) and write one HTML file. `leads` reads the graph and writes only
search URLs and command hints for the ancestors still missing a parent; it makes no requests.

**`internal/model`** is the hub every other package imports. `Person` and `Graph` are the only shared types.
Parent links live on the child (`Father`, `Mother` ids); spouses are a list. `Graph.Add` merges by id via
`Person.Merge`, which fills empty fields and unions lists, so re-adding a person is always safe. Ids are
namespaced by source prefix (`seed:`, `fs:`, `wt:`, `wd:`, `eggsa:`); `wt:id:<n>` is an unresolved WikiTree
placeholder that a real id may replace. `Graph.Aliases` records ids merged away; always call `g.Resolve(id)`
before looking up a user-supplied id.

**`cmd/kin/main.go`** holds all flag parsing and the per-command glue in `cmd*` functions, one `flag.FlagSet`
per subcommand. Cross-source deduplication (`crossLink`) lives here, not in `model`: people sharing a WikiTree
or Wikidata id are merged, preferring the seed id, then `wt:`, then `wd:`, and the loser is recorded in
`Aliases`. `version`, `commit` and `date` are injected by ldflags and copied into `httpx.Version`.

**`internal/graph`** is the relationship engine: `Ancestors` (id to generation distance), `Relationship`
(nearest common ancestors, up/down counts), `Label` (cousin degree and removal wording), `Gendered`, and
`Components` for connected-component colouring. It is source-neutral and fully unit tested; renderers call it
rather than walking parent links themselves.

**Renderers embed their HTML** with `//go:embed`. `viz`, `tree` and `geomap` build a `Payload` struct,
marshal it to JSON, escape `</` as `<\/`, and splice it into the `<script id="data">` block at the
`/*__DATA__*/` marker; all page logic is JavaScript inside the template. `report` is the exception and uses
`html/template`. `tree` and `geomap` reuse `viz.Site` for title and eyebrow, so the site file is shared.
Ahnentafel numbering (root 1, father 2n, mother 2n+1) is computed in `tree.Build` and `report.Render`; an
ancestor reached twice keeps every number but is drawn once under the lowest.

**Probable links** (`-probable id`, repeatable) mark the given person and every ancestor above them as resting
on a name-and-date match. Each renderer expands this the same way through `graph.Ancestors` and shows a
distinct style; keep the three in step.

**Source clients** (`wikitree`, `eggsa`, `naairs`, `tna`, `gazette`, `riksarkivet`, `news`, `wikidata`, `geo`) are deliberately polite to
volunteer-run and rate-limited services: fixed delays between calls, back-off on 429, a small worker count,
and on-disk caches under `data/cache/`. All outbound requests send `httpx.UserAgent()` (and `httpx.AppID` to
WikiTree). New clients should use `httpx.Polite`, which holds the delay, the 429/503 back-off, the user agent
and the cache in one place (`riksarkivet` and `news` do). Do not remove delays or caching, and do not add
parallelism against these hosts. Each client
converts its own records into `model.Person` (`ToPerson`) so the rest of the tool never sees source formats.

**`internal/geo`** geocodes place strings through Nominatim with historic-spelling normalisation and a
versioned JSON cache. The `version` constant must be bumped whenever aliases, query order or `Accept` rules
change, otherwise stale cached answers survive. Entries marked `Manual` (from a `-places` file) are never
re-queried.

**Scoring packages** (`naairs/sweep.go`, `war`) score archive hits against a person by loose name matching
(`loose`, `lev1`), dates and spouse surnames. Tests pin the scoring rules; add a case when changing them.
The British services stem their search terms instead (a search for Wooding returns Wood), so `gazette/score.go`
and `tna/score.go` use `internal/namematch` and require the surname exactly, relaxing to one edit only for a
Gazette page scanned from print. The Nordic scorers (`riksarkivet`, `linklives`, `news`) compare names through
`namematch.Nordic`, which folds accents, spelling variants and the -sen/-son/-datter/-dotter endings, and hold a
match on names and dates alone below `Keep` unless a parent, spouse, household member or parish backs it.
`internal/place` holds the region rules `leads` and the sweeps share, including `place.Ancestors`, with which the
gazette, tna and Nordic sweeps pick their people, and `tna.Series` must stay the military series alone because `war` searches every
entry of it by default.

**`internal/linklives`** makes no requests: it streams the harmonised CSVs of the Link-Lives release the user
downloaded, matching columns by header name, and writes link-lives.dk URLs for the user to open.

## Conventions

- Commit messages follow `feat:` / `fix:` / `ci:` / `docs:` prefixes; GoReleaser groups the changelog by them.
- Tests are plain `testing` with hand-built graphs (see `tree_test.go`'s `threeGen`), no fixtures or golden files.
- Log to stderr through `logf`; results go only to the `-out` file so stdout stays clean.
- `internal/leads` only composes URLs. FreeREG, FreeCEN and FreeBMD forbid programs that submit searches and
  Cornwall OPC allows personal research only, so never add a client for them; a pre-filled link the user opens
  is the most kin may do, and for the FreeUKGen sites only the search page plus the values to type.
  The same holds for Digitalarkivet's search, histreg.no, the Link-Lives site and API, DDD, HisKi, the Swedish
  census search and Íslendingabók: links only. Link-Lives data is used solely through the release the user downloads.
  Newspapers likewise: the British Newspaper Archive / Findmypast, Welsh Newspapers Online, Irish Newspaper
  Archives, Svenska dagstidningar (tidningar.kb.se), timarit.is and Digi get links from `leads`, never a client.
- Living people are never hidden by the renderers; the `living` flag is carried through, and `kin graph redact`
  (`model.Graph.Redacted`, an allow-list in `Person.Redact`) is the one place that strips dates, places and free
  text from them. A new `Person` field must be classified there as well as added to `Merge`.
