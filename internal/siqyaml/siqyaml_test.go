package siqyaml_test

import (
	"bytes"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"

	"siq_converter/internal/model"
	"siq_converter/internal/siqxml"
	"siq_converter/internal/siqyaml"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestFullRoundTrip checks XML -> YAML -> XML on the fixture covering every construct.
func TestFullRoundTrip(t *testing.T) {
	src, err := os.ReadFile("../../testdata/full.xml")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := siqxml.Decode(src)
	if err != nil {
		t.Fatal(err)
	}
	y1, err := siqyaml.Encode(p1)
	if err != nil {
		t.Fatal(err)
	}

	golden := "../../testdata/full.yaml"
	if *update {
		if err := os.WriteFile(golden, y1, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test ./... -update to create it)", err)
	}
	if !bytes.Equal(y1, want) {
		t.Errorf("YAML differs from %s (run with -update and review the diff)\n%s", golden, y1)
	}

	p2, err := siqyaml.Decode(y1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Fatalf("model changed after YAML round trip:\n%#v\n%#v", p1, p2)
	}
	y2, err := siqyaml.Encode(p2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(y1, y2) {
		t.Errorf("YAML writer is not idempotent")
	}

	out := siqxml.Encode(p2, nil)
	c1, err := siqxml.Canonical(src)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := siqxml.Canonical(out)
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Errorf("canonical XML differs\noriginal:\n%s\nconverted:\n%s", c1, c2)
	}
}

func TestWhitespaceAndSpecialStrings(t *testing.T) {
	values := []string{
		"", " ", "  lead", "trail  ", "a\nb", "a\n", "\nlead newline", "a \nb", "tab\there",
		"yes", "no", "null", "~", "1", "1.0", "0x1F", "true", "- dash", "# hash", "key: value",
		"'quote", `"dq`, `back\slash`, `\U0001F412 literal escape`, "🐒 emoji", "a\r\nb", " nbsp", "replacement \uFFFD char",
	}
	for _, v := range values {
		p := &model.Package{Version: model.Version, Name: v, Info: model.Info{Comments: v}, Rounds: []model.Round{{
			Name: v, Themes: []model.Theme{{Name: v, Questions: []model.Question{{
				Params: []model.Param{{Name: "question", Type: model.ParamContent, Items: []model.Item{{Value: v}}}},
				Right:  []string{v},
			}}}},
		}}}
		if v == "" {
			p.Info.Comments = ""
			p.Rounds[0].Themes[0].Questions[0].Right = nil // a single empty answer means none
		}
		y, err := siqyaml.Encode(p)
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		back, err := siqyaml.Decode(y)
		if err != nil {
			t.Fatalf("%q: %v\n%s", v, err, y)
		}
		if !reflect.DeepEqual(p, back) {
			t.Errorf("%q did not survive YAML:\n%s", v, y)
		}
	}
}

func TestLenientForms(t *testing.T) {
	src := `
name: x
rounds:
  - name: r
    themes:
      - name: t
        questions:
          - price: 100
            params:
              question:
                - text: plain
                - image: http://ext/a.png
                  ref: false
                - audio: a.mp3
                  placement: background
                  wait: true
            right: [only]
            wrong: []
`
	p, err := siqyaml.Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	q := p.Rounds[0].Themes[0].Questions[0]
	want := []model.Item{
		{Value: "plain"},
		{Type: "image", Value: "http://ext/a.png"},
		{Type: "audio", Value: "a.mp3", IsRef: true},
	}
	if !reflect.DeepEqual(q.Params[0].Items, want) {
		t.Errorf("items = %#v", q.Params[0].Items)
	}
	if !reflect.DeepEqual(q.Right, []string{"only"}) || q.Wrong != nil {
		t.Errorf("answers = %#v / %#v", q.Right, q.Wrong)
	}
}

func TestStrictErrors(t *testing.T) {
	cases := map[string]string{
		"unknown key":              "name: x\nrounds: []\ntitle: oops\n",
		"missing name":             "rounds: []\n",
		"missing price":            "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - right: a\n",
		"two item types":           "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              question:\n                - image: a\n                  audio: b\n            right: a\n",
		"duplicate key":            "name: x\nname: y\n",
		"bad version":              "siq: 4\nname: x\n",
		"future version":           "siq: 6\nname: x\n",
		"item type typo":           "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              question:\n                - imge: a.jpg\n            right: a\n",
		"form feed":                "name: \"page\\fbreak\"\nrounds: []\n",
		"vertical tab key":         "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              \"a\\vb\": c\n            right: a\n",
		"raw content":              "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              question: {raw: {type: content, value: Question text}}\n            right: a\n",
		"raw group":                "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              g: {raw: {type: group, value: x}}\n            right: a\n",
		"raw numberSet with value": "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              price: {raw: {type: numberSet, value: \"100\"}}\n            right: a\n",
		"second document":          "name: x\nrounds: []\n---\nname: y\n",
		"bad param mapping":        "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              p: {foo: 1}\n            right: a\n",
	}
	for name, src := range cases {
		if _, err := siqyaml.Decode([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		} else if !strings.Contains(err.Error(), "line") && name != "duplicate key" {
			t.Logf("%s: %v", name, err)
		}
	}
}

func TestRawEmptyNumberSetIsAccepted(t *testing.T) {
	// The encoder writes a numberSet parameter without a <numberSet> element this way.
	src := "name: x\nrounds:\n  - name: r\n    themes:\n      - name: t\n        questions:\n          - price: 1\n            params:\n              price: {raw: {type: numberSet, value: \"\"}}\n            right: a\n"
	if _, err := siqyaml.Decode([]byte(src)); err != nil {
		t.Error(err)
	}
}
