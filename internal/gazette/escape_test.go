package gazette

import (
	"encoding/json"
	"strings"
	"testing"
)

// strayFeed is the answer the live feed gives to a search for "Mary McConnel",
// cut to the one entry that breaks it: the optical character reader of the
// London Gazette of 2 March 1943 read a stray backslash, and the feed writes
// it into the JSON unescaped, so `\a` appears where JSON allows no escape.
const strayFeed = `{
 "f:page-number": "1", "f:page-size": "50", "f:total": "35",
 "entry": [
  {
   "id": "https://www.thegazette.co.uk/London/issue/35925/page/1039",
   "title": "The London Gazette, Issue 35925, Page 1039",
   "link": [{"@href": "/London/issue/35925/page/1039/data.pdf", "@rel": "self"}],
   "published": "1943-03-02T00:00:00",
   "content": "<div><p>…dwin.&apos; \ar&apos; . ,23rd December, 1942.Miss Phillis Mary Mansel Pacey.2&apos;gth December, 1942. &quot;JKfiss Joan <em class=\"highlight\">Mary McConnel</em>.…</p></div>"
  }
 ]
}`

func TestDecodeStrayBackslashFromScannedPage(t *testing.T) {
	if err := json.Unmarshal([]byte(strayFeed), new(map[string]any)); err == nil {
		t.Fatal("the fixture no longer holds the invalid escape the feed really sends")
	}
	f, err := decode([]byte(strayFeed))
	if err != nil {
		t.Fatalf("a stray backslash must not fail the search: %v", err)
	}
	if f.Total != 35 || len(f.Entries) != 1 {
		t.Fatalf("feed = %+v", f)
	}
	n := f.Entries[0].Notice()
	if !strings.Contains(n.Text, "Mary McConnel") {
		t.Errorf("the matched words are lost: %q", n.Text)
	}
	if !strings.Contains(n.Text, `\ar`) {
		t.Errorf("the backslash the page holds should survive as itself: %q", n.Text)
	}
	if n.Issue != "35925" || n.Page != "1039" || n.Year != 1943 {
		t.Errorf("notice = %+v", n)
	}
}

func TestEscapeStraysLeavesValidJSONAlone(t *testing.T) {
	for _, body := range []string{pageFeed, noticeFeed, objectFeed, emptyFeed} {
		out, changed := escapeStrays([]byte(body))
		if changed {
			t.Errorf("valid JSON was reported as repaired: %.60s", body)
		}
		if string(out) != body {
			t.Errorf("valid JSON was rewritten: %.60s", body)
		}
	}
}

func TestEscapeStrays(t *testing.T) {
	cases := []struct {
		name    string
		in      string // the bytes the feed sends
		changed bool
		want    string // the value the string field must decode to
	}{
		{"stray escape", `{"a":"x\ay"}`, true, `x\ay`},
		{"escaped backslash before a stray", `{"a":"x\\\ay"}`, true, `x\\ay`},
		{"escaped backslash alone", `{"a":"x\\y"}`, false, `x\y`},
		{"unicode escape survives", `{"a":"A\ab"}`, true, "A" + `\ab`},
		{"short unicode escape is a stray", `{"a":"\u12g"}`, true, `\u12g`},
		{"escaped quote before a stray", `{"a":"he said \"hi\" \and left"}`, true, `he said "hi" \and left`},
		{"every valid escape", `{"a":"\"\\\/\b\f\n\r\t"}`, false, "\"\\/\b\f\n\r\t"},
		{"backslash outside a string is untouched", `{"a":"\ab","b":2}`, true, `\ab`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed := escapeStrays([]byte(c.in))
			if changed != c.changed {
				t.Errorf("changed = %v, want %v", changed, c.changed)
			}
			if !changed && string(out) != c.in {
				t.Errorf("unchanged input was rewritten: %q", out)
			}
			var got struct{ A string }
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("repaired body does not parse: %v (%s)", err, out)
			}
			if got.A != c.want {
				t.Errorf("decoded %q, want %q", got.A, c.want)
			}
		})
	}
}

func TestDecodeKeepsOtherErrors(t *testing.T) {
	if _, err := decode([]byte(`{"f:total": "1", "entry": [`)); err == nil {
		t.Error("a truncated body is still an error")
	}
	if _, err := decode([]byte(`{"f:total": "1" "entry": null}`)); err == nil ||
		!strings.Contains(err.Error(), "gazette decode") {
		t.Errorf("a body broken for another reason keeps its error: %v", err)
	}
}
