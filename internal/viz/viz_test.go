package viz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSiteDefaults(t *testing.T) {
	s, err := LoadSite("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Kin" || len(s.OrderingRows) != 1 {
		t.Errorf("unexpected defaults: %+v", s)
	}
}

func TestLoadSiteOverridesOnlyGivenKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "site.json")
	os.WriteFile(p, []byte(`{"title":"X","flags":[{"label":"echo","namePattern":"\\b(ann|anne)\\b"}]}`), 0o644)
	s, err := LoadSite(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "X" || s.Eyebrow != DefaultSite().Eyebrow || len(s.Flags) != 1 {
		t.Errorf("got %+v", s)
	}
	os.WriteFile(p, []byte(`{"flags":[{"label":"bad","namePattern":"("}]}`), 0o644)
	if _, err := LoadSite(p); err == nil {
		t.Error("invalid pattern should fail")
	}
}
