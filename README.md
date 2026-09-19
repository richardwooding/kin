# kin

A command-line ancestry toolkit. `kin` pulls family records from public sources
(WikiTree, the eGGSA gravestone and newspaper indexes, the South African National
Archives index NAAIRS, Wikidata), merges them with your own hand-entered people
into one kinship graph, labels every relationship relative to you, and renders
a single-file ancestry web page, a pan-and-zoom family tree and printable A4
reports.

It grew out of one family's research in South Africa, Cornwall and London, so the
South African sources are first-class; the graph, relationship engine, page and
reports are source-neutral.

Go 1.27, standard library only.

## Install

Homebrew (macOS):

```sh
brew install --cask richardwooding/tap/kin
```

Prebuilt archives for Linux, macOS and Windows (amd64 and arm64) are on the
[Releases page](https://github.com/richardwooding/kin/releases); each one
bundles the `examples/` directory. Or build from source:

```sh
go install github.com/richardwooding/kin/cmd/kin@latest
```

```sh
git clone https://github.com/richardwooding/kin && cd kin
go build -o bin/kin ./cmd/kin && go test ./...
```

A container image is published to `ghcr.io/richardwooding/kin` (linux/amd64
and linux/arm64, a static distroless base holding only the binary). Mount a
working directory and run as yourself so output files are writable:

```sh
docker run --rm --user "$(id -u)" -v "$PWD:/work" -w /work \
  ghcr.io/richardwooding/kin:latest graph build -seed examples/seed.json -out graph.json
```

`kin version` prints the version, commit and build date.

## Quick start

The `examples/` directory holds a small fictional family. This runs offline:

```sh
kin graph build -seed examples/seed.json -out data/graph.json
kin graph report -graph data/graph.json -from seed:me
kin viz -graph data/graph.json -seed seed:me -site examples/site.json -records examples/records.json -out dist/index.html
kin tree -graph data/graph.json -root seed:me -site examples/site.json -records examples/records.json -out dist/tree.html
kin map -graph data/graph.json -root seed:me -site examples/site.json -offline -places examples/places.json -out dist/map.html
kin report -graph data/graph.json -root seed:me -records examples/records.json -out dist/report.html
```

Open `dist/index.html`, `dist/tree.html` and `dist/map.html` in a browser. Then replace the seed with your own family
and start pulling records:

```sh
kin wikitree search -last Smith -birthloc "South Africa" -out data/wt_search.json
kin wikitree expand -in data/wt_search.json -anc 10 -desc 10 -out data/wt_tree.json
kin eggsa graves -surname Smith -out data/eggsa.json
kin eggsa papers -surname Smith -out data/papers.json
kin graph build -seed seed.json -in data/wt_tree.json,data/eggsa.json -out data/graph.json
kin viz -graph data/graph.json -seed seed:me -notices data/papers.json -records records.json -out dist/index.html
```

## Commands

| Command | Source | What it does |
|---|---|---|
| `kin wikitree search -last Smith [-first …] [-birthloc …] [-birth 1890 -spread 3] [-father "John Smith"] [-mother …]` | WikiTree API `searchPerson` | Profiles matching a surname, place, birth year or parent |
| `kin wikitree expand -keys Smith-1,Smith-2` or `-in data/wt_search.json` | WikiTree `getAncestors`, `getDescendants`, `getRelatives` | Ancestors and descendants of each profile, then descendants of the top ancestors, then spouses, siblings and children |
| `kin wikitree relatives -in data/wt_tree.json` | WikiTree `getRelatives` | Adds spouses, children and siblings for every profile already in a graph |
| `kin eggsa graves -surname Smith` | eGGSA gravestone photograph indexes, all provinces | Gravestone entries as persons with birth and death years and cemetery |
| `kin eggsa papers -surname Smith` | eGGSA newspaper extracts | Newspaper notices mentioning the surname; pages are cached under `data/cache/papers` |
| `kin naairs -db TAB -q "SMITH JOHN HENRY" [-from 1930 -to 1932]` | National Archives of South Africa index | Estate, court and government file references |
| `kin naairs sweep -graph data/graph.json -from seed:me [-gen 20] [-db RSA] [-delay 3s] [-resume]` | National Archives of South Africa index | Queries the index once per ancestor (and under married names), scores every hit against names, dates and spouses, and saves the candidates |
| `kin gazette -q '"Wooding, Charles"' [-from 1880 -to 1905] [-edition London] [-deceased]` | The Gazette | One search of the official notices: deceased estates naming the dead and their executors, bankruptcies, dissolved partnerships, naturalisations, commissions and awards, back to 1665 |
| `kin gazette sweep -graph data/graph.json -root seed:me [-gen 20] [-max 2] [-delay 1.1s] [-resume] [-dry-run]` | The Gazette | Two or three searches per British or Irish ancestor, scored against their names, places and life window; every answer cached on disk |
| `kin tna -q "Wooding Portsmouth" [-series "PROB 11"] [-held elsewhere] [-from 1780 -to 1860] [-list]` | UK National Archives Discovery catalogue | One catalogue search: wills, death duty registers, naturalisations, police and navy registers, or the holdings of the archives that keep records elsewhere |
| `kin tna sweep -graph data/graph.json -root seed:me [-frontier] [-resume] [-dry-run]` | UK National Archives Discovery catalogue | The name-indexed civil series for every British and Irish ancestor and, on the frontier, the county record offices' own catalogues by surname and parish |
| `kin war -graph data/graph.json -from seed:me [-min-birth 1855] [-max-birth 1927] [-boer]` | UK National Archives Discovery catalogue; NAAIRS | Lists the men of military age among the ancestors and their sons and checks them against the name-indexed imperial military series (Boer War attestations and rolls, First World War medal cards and officers' files, navy and air force registers) and, for the Boer side, the South African archives for 1899 to 1903 |
| `kin wikidata surname -name Smith`, `kin wikidata place -name Stellenbosch -country Q258` | Wikidata SPARQL and search | People with a family name or born in a place (optional; not needed for the pipeline) |
| `kin graph build -seed seed.json -in a.json,b.json` | — | Merges graphs, deduplicating people that carry the same WikiTree or Wikidata id and recording the merge aliases |
| `kin graph kin -graph data/graph.json -from seed:me -to wt:Smith-1` | — | Labels the relationship and lists the common ancestors |
| `kin graph report -graph data/graph.json -from seed:me` | — | Counts: network size, documented ancestors by generation, earliest dated ancestor |
| `kin graph redact -graph data/graph.json -out data/graph.public.json` | — | Copy of the graph in which every living person keeps only names, gender, parent and spouse links, source tags and WikiTree/Wikidata ids; dates, places, occupations, notes and URLs are removed |
| `kin viz -graph data/graph.json -seed seed:me [-site site.json] [-notices …] [-records …] [-probable id] [-tree-url URL]` | — | Writes the ancestry page |
| `kin tree -graph data/graph.json -root seed:me [-reader id] [-gen 20] [-site site.json] [-records …] [-probable id] [-dashboard-url URL]` | — | Writes the pan-and-zoom family tree page |
| `kin map -graph data/graph.json -root seed:me [-reader id] [-site site.json] [-cache data/cache/geo.json] [-places places.json] [-offline] [-dashboard-url URL] [-tree-url URL]` | Nominatim (OpenStreetMap) for coordinates | Writes a pan-and-zoom map of the ancestors' birth and death places |
| `kin leads -graph data/graph.json -root seed:me [-probable id] [-id fs:X] [-out dist/leads.html]` | — | Prints search links for every ancestor still missing a parent, and for the probable ids, on FamilySearch, Cornwall OPC, WikiTree, the National Archives and the South African archives; fetches nothing |
| `kin report -root seed:me [-reader seed:me] [-title …] [-probable id] [-note …] [-dashboard-url URL]` | — | Writes a printable A4 ancestry report that also reads as a web page |

Every subcommand prints its flags with `-h`.

## The graph

`kin graph build` writes one JSON file: `{"persons": {id: person}, "aliases": {mergedId: survivingId}}`.
A person has `id`, `name`, `given`, `surname`, `gender`, `birth`, `death` (ISO dates or
years), `birthPlace`, `deathPlace`, `occupations`, `father`, `mother`, `spouses`,
`sources`, `url`, `living` and a free-text `note`. Parent links live on the child.
Unknown keys are ignored, so a seed file may carry extra notes.

Ids are namespaced by source:

| Prefix | Meaning |
|---|---|
| `seed:` | hand-entered from family knowledge |
| `fs:` | hand-entered from a FamilySearch index entry or register image |
| `wt:Smith-12` | WikiTree profile |
| `wd:Q42` | Wikidata item |
| `eggsa:<hash>` | eGGSA gravestone entry |

The renderers show living people exactly as the seed and the sources record
them; the `"living": true` flag is carried through the graph but hides nothing
on its own. To publish, run `kin graph redact` first and point `viz`, `tree`,
`map` and `report` at the file it writes: every living person then keeps only
`id`, `name`, `given`, `surname`, `gender`, `father`, `mother`, `spouses`,
`sources`, `wikitree`, `wikidata` and `living`, so they appear by name in the
tree and tables with no dates, places, notes or links. WikiTree's own privacy
settings already hide most living profiles.

### Records

`-records records.json` attaches hand-collected citations to people. Each entry has
`person` (an id), `names`, `event`, `date`, `place`, `detail`, `collection`, `url` and
`image` (a digital folder and image number). See `examples/records.json`.

### Site file

`kin viz -site site.json` supplies the text that belongs to one family rather than
to the tool. All keys are optional:

```json
{
  "title": "Smith Kin",
  "eyebrow": "Family history · public records",
  "flagsNote": "Sentence explaining the flags column.",
  "flags": [
    { "label": "Portsmouth line", "descendantsOf": "seed:ff", "style": "ok" },
    { "label": "name echo", "namePattern": "\\b(william|peter)\\b" }
  ],
  "orderingRows": [ { "prefix": "…", "holding": "…", "how": "…" } ],
  "links": [ { "label": "Mary Jones and her ancestors", "href": "jones-line.html" } ]
}
```

`flags` add a column to the regional-records table: a flag applies to everyone
descended from `descendantsOf`, or to everyone whose given name matches
`namePattern` (case-insensitive). `orderingRows` are added to the "Where to order
copies" table. `links` are shown in the dashboard header beside the tree and map
links; use them to point at the printable reports. An `href` is used as written,
so a relative path suits a site where every page is published together, unlike
`-tree-url`, which is meant to be absolute.

### Probable links

`-probable id` (repeatable, on `viz`, `tree` and `report`) marks the given person and all
their ancestors as resting on a name-and-date match rather than on a record that
names the parent. The page and report show these in a distinct style.

## Family tree

`kin tree` draws a pedigree on an infinite canvas: the root person at the left,
parents to the right, grandparents further right, each couple a pair of boxes
joined by a bracket. Siblings are not drawn (they are listed in the detail
panel). An ancestor reached by two lines is drawn once, at the lowest
Ahnentafel number, and later occurrences appear as a dashed "see N" stub.

Controls: drag to pan, scroll or pinch to zoom, the toolbar for zoom in and
out, fit all and centre on the root, and a search box that flies to a name.
Keys: `+` `-` zoom, `0` fit, `/` search, `Escape` closes the panel. Click a box
for the detail panel (dates, places, relationship, Ahnentafel numbers, sources,
other spouses, children of the couple, attached records). The toggle on a box's
right edge collapses that branch; the choice is remembered in the browser. The
accent colour is the status: blue for a person known from a record, amber and
dashed for a probable link, green for a gravestone, grey for a tree entry. On a
phone the toolbar sits at the bottom of the screen and the detail panel opens
as a sheet over the lower part of it.

The dashboard and the tree can link to each other. Because published pages
often live at two different addresses, the links are given as absolute URLs:
`kin viz -tree-url URL` puts "Open the family tree" in the dashboard header and
`kin tree -dashboard-url URL` puts a dashboard link in the tree's panel.
Relative paths do not survive publishing to separate addresses.

## Map

`kin map` puts every ancestor's birthplace and place of death on a world map
drawn from an embedded Natural Earth outline, so the page loads nothing from a
tile server and can be published anywhere. Pins are sized by the number of
people at a place and coloured by grandparent line; hollow pins mark a place
that resolved only to a region or country; thin arcs join each person's
birthplace to their place of death. Click a pin for the people, a person for
their journey, and use the toolbar to fit all places, southern Africa or Europe.
On a phone the toolbar sits at the bottom, the line chips scroll sideways and
the panel opens as a sheet over the lower part of the screen.

Coordinates come from OpenStreetMap's Nominatim geocoder, one query per distinct
place string at its permitted rate of one a second, and are cached in the file
named by `-cache`, so a second run is instant and `-offline` never calls out.
Historic spellings are normalised before lookup (de Caep de Goede Hoop,
Drakenstein, 't Land van Waveren, Cabo de Goede Hoop, Heiliges Römisches Reich,
Spaanse Nederlanden, Courtrai), the whole name is tried first and then the name
without its historic polity, the first part with the country, and coarser
suffixes, and only settlements, administrative areas and waters are accepted,
never a school, shop or monument that happens to carry the name. The cache
records which version of these rules produced each answer, so a new release
re-asks only what its rules would answer differently. Names the geocoder cannot
place are listed in the header; give them coordinates in a `-places` JSON file
(`{"place string": {"lat": .., "lon": .., "label": ..}}`), as
`examples/places.json` does for the example family.

`kin viz -map-url URL` and `kin map -dashboard-url URL -tree-url URL` link the
pages together, as for the tree.

## Printable reports

```sh
kin report -graph data/graph.json -root seed:mother -reader seed:me \
  -title "Mary Jones and her ancestors" -probable wt:Smith-1 -note "…" -out dist/jones-line.html
chromium --headless=new --disable-gpu --no-pdf-header-footer \
  --print-to-pdf=$PWD/dist/jones-line.pdf file://$PWD/dist/jones-line.html
```

The report lists the direct line with Ahnentafel numbers, the children of every
couple on it, the records consulted, where to order copies, and your notes. To
link the reports from the dashboard, list them under `links` in the site file;
`-dashboard-url URL` puts a link back to the dashboard at the top of the page.
The print layout is A4; on screen the same file reads as a web page, and on a
phone each table row stacks into a card, while the PDF is unchanged.

## Research leads

```sh
kin leads -graph data/graph.json -root seed:me -probable wt:Smith-1 -upstream wt: -searched seed/searched.json -out dist/leads.html
```

`kin leads` walks the ancestry of `-root`, picks out the research frontier, every
ancestor missing a father or mother plus every `-probable` id, and composes search
links for each from their names, dates, places, spouses and children (a person with
no place of their own takes the region of their spouses and children, so a parent
known only from a baptism still gets that parish's country). It prints them as text
and, with `-out`, writes a private page of clickable links. `-id` composes leads for
one person instead. Nothing is fetched: the links open searches on FamilySearch (by
name and date; the 1851, 1861 and 1881 censuses of England and Wales while the person
was alive; the Dutch Reformed and, for the Transvaal, the Hervormde church registers;
the Cape, Transvaal or Free State probate records for the year of death), the Cornwall
Online Parish Clerks database (baptisms, marriage and burials in the person's own
parish and its neighbours, the marriage to a known spouse, and every child of the
couple by both parents' forenames, when a place is in Cornwall) and the National
Archives' Discovery catalogue, and give the `kin wikitree search`, `kin naairs` and
`kin eggsa graves` commands to run.

`-upstream wt:` (repeatable) leaves off the frontier the parentless people whose id
carries that prefix: the ends of WikiTree's own pedigrees are WikiTree's to extend,
not the family's, and without the flag they swamp the page. A `-probable` id is
always kept. `-searched` names a json list of searches already made,
`[{"person": "fs:x", "service": "Cornwall OPC", "when": "2026-09-18", "note": "baptisms 1745 to 1765 Stithians: one hit"}]`,
which the page shows under each person as "already searched", so a negative result
is kept and the same search is not offered as new.

FreeREG, FreeCEN and FreeBMD forbid front-end programs that enter search parameters,
so for them `kin leads` links only to the search page and prints the values to type.
The Cornwall OPC allows personal research only, so its links are for the researcher
to open by hand. Do not add clients for those sites.

## Sources: terms and manners

- **WikiTree**: the public API is free for non-commercial use under WikiTree's
  terms. `kin` sends `appId=kin`, waits 1.5 s between calls and backs off on
  `429 Limit exceeded`. Keep it that way. WikiTree data is volunteer-contributed
  and should be checked against original records.
- **eGGSA** (Genealogical Society of South Africa): the gravestone and newspaper
  sites are volunteer-run and have no API, so `kin` reads their HTML search forms
  and pages. It fetches with a small number of workers, caches every page under
  `data/cache/`, and re-reads the cache on later runs. Run one surname at a time,
  do not redistribute the scraped content, and credit eGGSA when you publish
  findings that rest on it.
- **NAAIRS**: the National Archives index is a legacy, session-driven web front
  end that is often down ("Search Manager Server is inactive"). `kin naairs` caps
  a query at 130 documents. `kin naairs sweep` runs one or two queries per
  ancestor with a pause between them (3 s by default), saves after every person
  and resumes with `-resume`; a sweep of a few hundred ancestors takes an hour or
  more, so run it once and keep the result. Scores: 1 for a name match, more for
  the full given names, an estate file (MHG, MOOC) in the person's own name, a
  date near the death year, and both maiden and married names together; hits
  dated before birth are dropped. Spelling is matched loosely (Philippus and
  Phillipus, Reyneke and Reynecke, Nel and Nell). Repository codes: `RSA` (all), `TAB` Pretoria (former
  Transvaal), `KAB` Cape Town, `NAB` Pietermaritzburg, `VAB` Bloemfontein (Free
  State), `TBD` Durban, `TBE` Port Elizabeth, `TBK` Kimberley, `SAB` Pretoria
  central, `GEN` genealogical. Sources `MHG` (Transvaal and Free State estates) and
  `MOOC` (Cape estates) hold death notices that name parents and children.
- **Wikidata**: queries carry a descriptive user agent as its policy requires.
- **OpenStreetMap Nominatim**: `kin map` sends one request per place at one per
  second with the kin user agent, as the usage policy asks, and caches every
  answer. Do not run it in a loop against the same places; the cache is the point.
- **UK National Archives**: `kin war` and `kin tna` use the public Discovery
  catalogue API, one query per surname or parish, with a pause between calls and,
  for `kin tna`, an on-disk cache. Besides the military series `kin war` reads,
  `kin tna` searches the name-indexed civil series (PROB 11 wills to 1858, IR 26
  death duty abstracts, HO 334 naturalisations, MEPO 4, ADM 139) and, with
  `-held elsewhere`, the catalogues of the archives that keep records outside Kew,
  such as Kresen Kernow for Cornwall. Discovery stems its search terms, so a
  search for Wooding also returns Wood; every hit is scored locally and the
  surname must match exactly. It finds catalogue entries only; the record images
  (medal cards, attestations, wills) are paid downloads at Kew or via the
  commercial partners, and a record office's holdings are read at the office.
  South African units' own records are not at Kew but at the SANDF Documentation
  Centre in Pretoria, which answers written requests.
- **The Gazette**: the United Kingdom's official public record publishes an open
  JSON feed with no key, and its content is Crown copyright under the Open
  Government Licence v3.0. Its fair use policy asks callers to be reasonable, so
  `kin gazette` sends one request every 1.1 seconds, asks at most three per
  ancestor, caches every answer under `data/cache/gazette` and re-reads the cache
  on a resumed sweep. Search is stemmed, and issues before about 1998 are optical
  character recognition of the printed page, so hits are scored locally and should
  be read against the PDF before they are believed. When you publish a finding
  that rests on a notice, cite the edition, issue, page and date, and credit
  "Contains public sector information licensed under the Open Government Licence
  v3.0".
- **FamilySearch** is not queried; its index entries and register images are read
  by hand and recorded in the records file with their ark and image references.

## Where to order copies

Archive references are index entries. The files are held by the National Archives
repositories (Cape Town, Pretoria, Port Elizabeth, Bloemfontein, Pietermaritzburg)
and are copied on written request quoting the full reference. Estates too recent
for the index are with the Master of the High Court for the district of death.
Cape probate files 1822 to 1990 and Transvaal probate files 1869 to 1961 are also
photographed on FamilySearch and can be read there free. English civil entries
come from the General Register Office; Cornish parish registers from Kresen Kernow.

## Licence

MIT. See `LICENSE`.
