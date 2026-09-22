// Package linklives searches a local copy of Link-Lives release 2, the Danish
// censuses of 1787 to 1901 and the Copenhagen burial register linked into
// life courses by the Danish National Archives, Copenhagen City Archives and
// the University of Copenhagen (DOI 10.5279/dk-ra-14001).
//
// The release is downloaded by the user from Rigsarkivet's DigiData, under
// its conditions: personal, educational or research use, nothing commercial,
// and credit to Link-Lives with the release number. kin reads the harmonised
// files (*_std.csv) from disk and never contacts link-lives.dk, whose terms
// forbid harvesting its search; the links it writes are for the user to open.
package linklives

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Release is the life-course release the links point at.
const Release = "2.1"

// Row is one person appearance in a harmonised file.
type Row struct {
	Source     string `json:"source"` // e.g. "Census 1845"
	SourceID   string `json:"sourceId"`
	PaID       string `json:"paId"`
	Name       string `json:"name"`
	FirstNames string `json:"firstNames,omitempty"`
	Surnames   string `json:"surnames,omitempty"` // family names, patronyms and maiden names
	Sex        string `json:"sex,omitempty"`
	Age        string `json:"age,omitempty"`
	BirthYear  int    `json:"birthYear,omitempty"`
	BirthPlace string `json:"birthPlace,omitempty"`
	EventYear  int    `json:"eventYear,omitempty"`
	EventType  string `json:"eventType,omitempty"`
	EventPlace string `json:"eventPlace,omitempty"`
	Household  string `json:"household,omitempty"`
	Position   string `json:"position,omitempty"`
}

// URL is the person appearance on link-lives.dk.
func (r Row) URL() string { return "https://link-lives.dk/soeg/pa/" + r.SourceID + "-" + r.PaID }

func (r Row) key() string { return r.SourceID + "-" + r.PaID }

func (r Row) householdKey() string {
	if r.Household == "" {
		return ""
	}
	return r.SourceID + "/" + r.Household
}

// Files lists the harmonised source files under dir, in name order.
func Files(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := strings.ToLower(d.Name())
		if !d.IsDir() && strings.HasSuffix(name, ".csv") && strings.Contains(name, "std") && !strings.HasPrefix(name, "ala") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// LifeCourses finds the release 2 life-course file under dir, or "".
func LifeCourses(dir string) string {
	var found string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && lifeCourseFile.MatchString(strings.ToLower(d.Name())) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

var (
	lifeCourseFile = regexp.MustCompile(`^life[ _-]?courses[ _-]*v?2.*\.csv$`)
	versionSuffix  = regexp.MustCompile(`[ _-]+v\d+([._]\d+)?[ _-]+std$`)
)

// Label names a source file for people: "census_1845_v1_std.csv" is
// "Census 1845", and the Copenhagen burial register says so.
func Label(path string) string {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	base = versionSuffix.ReplaceAllString(base, "")
	base = strings.NewReplacer("_", " ", "-", " ").Replace(base)
	if strings.HasPrefix(base, "cbp") {
		return "Copenhagen burials" + strings.TrimPrefix(base, "cbp")
	}
	if base == "" {
		return base
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

// columns maps the harmonised header, whose names the guide prints with
// spaces and the files may write with underscores, to positions.
type columns map[string]int

func header(rec []string) columns {
	c := columns{}
	for i, h := range rec {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))
		h = strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(h)
		c[h] = i
	}
	return c
}

func (c columns) get(rec []string, names ...string) string {
	for _, n := range names {
		if i, ok := c[n]; ok && i < len(rec) {
			if v := strings.TrimSpace(rec[i]); v != "" {
				return v
			}
		}
	}
	return ""
}

// scan reads one harmonised file, handing each row to each.
func scan(path string, each func(Row)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	head, err := r.Read()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	c := header(head)
	if _, ok := c["pa_id"]; !ok {
		return fmt.Errorf("%s: no pa_id column; is this a Link-Lives harmonised file?", path)
	}
	label := Label(path)
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				continue // a stray quote in one transcription should not stop the file
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		row := Row{
			Source:     label,
			SourceID:   c.get(rec, "source_id"),
			PaID:       c.get(rec, "pa_id"),
			Name:       c.get(rec, "name", "name_cl"),
			FirstNames: c.get(rec, "first_names"),
			Surnames: strings.Join(nonEmpty(c.get(rec, "family_names"), c.get(rec, "patronyms"),
				c.get(rec, "maiden_names"), c.get(rec, "all_family_names"), c.get(rec, "all_patronyms")), " "),
			Sex:        c.get(rec, "sex"),
			Age:        c.get(rec, "age"),
			BirthYear:  atoi(c.get(rec, "birth_year")),
			BirthPlace: c.get(rec, "birth_place", "birth_parish", "birth_place_cl"),
			EventYear:  atoi(c.get(rec, "event_year")),
			EventType:  c.get(rec, "event_type"),
			EventPlace: strings.Join(nonEmpty(c.get(rec, "event_parish", "event_town", "event_location"), c.get(rec, "event_county")), ", "),
			Household:  c.get(rec, "household_id"),
			Position:   c.get(rec, "household_position", "role"),
		}
		each(row)
	}
}

// lifeCourses maps person appearances ("source-pa") to their life course.
func lifeCourses(path string, want map[string]bool) (map[string]string, error) {
	out := map[string]string{}
	if path == "" || len(want) == 0 {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	head, err := r.Read()
	if err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	c := header(head)
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				continue
			}
			return out, fmt.Errorf("%s: %w", path, err)
		}
		pas := strings.Split(c.get(rec, "pa_ids"), ",")
		srcs := strings.Split(c.get(rec, "source_ids"), ",")
		id := c.get(rec, "life_course_id")
		for i := range pas {
			if i < len(srcs) {
				if k := strings.TrimSpace(srcs[i]) + "-" + strings.TrimSpace(pas[i]); want[k] {
					out[k] = id
				}
			}
		}
	}
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
