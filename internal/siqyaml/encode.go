// Package siqyaml converts between the package model and human-editable YAML.
//
// The writer always produces one canonical form (shorthands, defaults omitted);
// the reader additionally accepts a few lenient forms, e.g. a list where a
// single answer would do.
package siqyaml

import (
	"bytes"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"siq_converter/internal/model"
)

// Keys of a content item mapping other than its type key.
const (
	keyRef       = "ref"
	keyPlacement = "placement"
	keyWait      = "wait"
	keyDuration  = "duration"
)

// Encode renders the package as YAML.
func Encode(p *model.Package) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(packageNode(p)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if readable := unescapeAstral(out); !bytes.Equal(readable, out) {
		// yaml.v3 escapes characters outside the BMP (emoji) as \UXXXXXXXX.
		// Keep the readable form only if it still means exactly the same.
		if back, err := Decode(readable); err == nil {
			if again, err := Decode(out); err == nil && reflect.DeepEqual(back, again) {
				out = readable
			}
		}
	}
	return out, nil
}

var astralEscape = regexp.MustCompile(`(\\*)\\U([0-9A-Fa-f]{8})`)

// unescapeAstral turns \UXXXXXXXX escapes (not themselves escaped) back into characters.
func unescapeAstral(b []byte) []byte {
	return astralEscape.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := astralEscape.FindSubmatch(m)
		if len(sub[1])%2 == 1 {
			return m // the backslash is escaped, so this is literal text
		}
		r, err := strconv.ParseUint(string(sub[2]), 16, 32)
		if err != nil || !utf8.ValidRune(rune(r)) {
			return m
		}
		return append(sub[1], string(rune(r))...)
	})
}

type mapping struct{ n *yaml.Node }

func newMapping() mapping { return mapping{&yaml.Node{Kind: yaml.MappingNode}} }

func (m mapping) add(key string, v *yaml.Node) {
	m.n.Content = append(m.n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

// addStr adds a string value unless it is empty.
func (m mapping) addStr(key, v string) {
	if v != "" {
		m.add(key, str(v))
	}
}

func str(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.Contains(s, "\n") {
		n.Style = yaml.LiteralStyle
		if !survives(n) {
			// e.g. a leading newline is lost in a yaml.v3 literal block
			n.Style = yaml.DoubleQuotedStyle
		}
	}
	return n
}

// survives reports whether a scalar node reads back as the same string.
func survives(n *yaml.Node) bool {
	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{n}})
	if err != nil {
		return false
	}
	var back []string
	return yaml.Unmarshal(out, &back) == nil && len(back) == 1 && back[0] == n.Value
}

func intNode(v int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
}

// numericOrStr emits canonical integers unquoted and everything else as a string.
func numericOrStr(s string) *yaml.Node {
	if v, err := strconv.Atoi(s); err == nil && strconv.Itoa(v) == s {
		return intNode(v)
	}
	return str(s)
}

func seq(values []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode}
	for _, v := range values {
		n.Content = append(n.Content, str(v))
	}
	return n
}

func packageNode(p *model.Package) *yaml.Node {
	m := newMapping()
	version := p.Version
	if version == "" {
		version = model.Version
	}
	m.add("siq", numericOrStr(version))
	m.add("name", str(p.Name))
	m.addStr("id", p.ID)
	m.addStr("restriction", p.Restriction)
	m.addStr("date", p.Date)
	m.addStr("publisher", p.Publisher)
	m.addStr("contactUri", p.ContactURI)
	if p.Difficulty > 0 {
		m.add("difficulty", intNode(p.Difficulty))
	}
	m.addStr("logo", p.Logo)
	m.addStr("language", p.Language)
	m.addStr("generator", p.Generator)
	if len(p.Tags) > 0 {
		m.add("tags", seq(p.Tags))
	}
	addInfo(m, p.Info)
	if !p.Global.IsEmpty() {
		m.add("global", globalNode(p.Global))
	}
	rounds := &yaml.Node{Kind: yaml.SequenceNode}
	for _, r := range p.Rounds {
		rounds.Content = append(rounds.Content, roundNode(r))
	}
	m.add("rounds", rounds)
	return m.n
}

func addInfo(m mapping, i model.Info) {
	if i.IsEmpty() {
		return
	}
	im := newMapping()
	if len(i.Authors) > 0 {
		im.add("authors", seq(i.Authors))
	}
	if len(i.Sources) > 0 {
		im.add("sources", seq(i.Sources))
	}
	im.addStr("comments", i.Comments)
	im.addStr("showmanComments", i.ShowmanComments)
	im.addStr("extension", i.Extension)
	m.add("info", im.n)
}

func globalNode(g model.Global) *yaml.Node {
	m := newMapping()
	if len(g.Authors) > 0 {
		s := &yaml.Node{Kind: yaml.SequenceNode}
		for _, a := range g.Authors {
			am := newMapping()
			am.add("id", str(a.ID))
			am.addStr("name", a.Name)
			am.addStr("secondName", a.SecondName)
			am.addStr("surname", a.Surname)
			am.addStr("country", a.Country)
			am.addStr("city", a.City)
			s.Content = append(s.Content, am.n)
		}
		m.add("authors", s)
	}
	if len(g.Sources) > 0 {
		s := &yaml.Node{Kind: yaml.SequenceNode}
		for _, src := range g.Sources {
			sm := newMapping()
			sm.add("id", str(src.ID))
			sm.addStr("author", src.Author)
			sm.addStr("title", src.Title)
			if src.Year != "" {
				sm.add("year", numericOrStr(src.Year))
			}
			sm.addStr("publish", src.Publish)
			sm.addStr("city", src.City)
			s.Content = append(s.Content, sm.n)
		}
		m.add("sources", s)
	}
	return m.n
}

func roundNode(r model.Round) *yaml.Node {
	m := newMapping()
	m.add("name", str(r.Name))
	m.addStr("type", r.Type)
	addInfo(m, r.Info)
	themes := &yaml.Node{Kind: yaml.SequenceNode}
	for _, t := range r.Themes {
		themes.Content = append(themes.Content, themeNode(t))
	}
	m.add("themes", themes)
	return m.n
}

func themeNode(t model.Theme) *yaml.Node {
	m := newMapping()
	m.add("name", str(t.Name))
	addInfo(m, t.Info)
	questions := &yaml.Node{Kind: yaml.SequenceNode}
	for _, q := range t.Questions {
		questions.Content = append(questions.Content, questionNode(q))
	}
	m.add("questions", questions)
	return m.n
}

func questionNode(q model.Question) *yaml.Node {
	m := newMapping()
	m.add("price", intNode(q.Price))
	m.addStr("type", q.Type)
	addInfo(m, q.Info)
	if len(q.Params) > 0 {
		m.add("params", paramsNode(q.Params))
	}
	if len(q.Script) > 0 {
		s := &yaml.Node{Kind: yaml.SequenceNode}
		for _, step := range q.Script {
			s.Content = append(s.Content, paramsNode(step))
		}
		m.add("script", s)
	}
	m.add("right", answersNode(q.Right))
	if len(q.Wrong) > 0 {
		m.add("wrong", answersNode(q.Wrong))
	}
	return m.n
}

// answersNode emits a single answer as a scalar and anything else as a list.
func answersNode(answers []string) *yaml.Node {
	if len(answers) == 1 {
		return str(answers[0])
	}
	n := seq(answers)
	if len(answers) == 0 {
		n.Style = yaml.FlowStyle // right: []
	}
	return n
}

func paramsNode(ps []model.Param) *yaml.Node {
	m := newMapping()
	for _, p := range ps {
		m.add(p.Name, paramValue(p))
	}
	return m.n
}

func paramValue(p model.Param) *yaml.Node {
	switch {
	case p.IsRef:
		// falls through to the raw form below
	case p.Type == "":
		return str(p.Value)
	case p.Type == model.ParamContent:
		s := &yaml.Node{Kind: yaml.SequenceNode}
		for _, it := range p.Items {
			s.Content = append(s.Content, itemNode(it))
		}
		if len(p.Items) == 0 {
			s.Style = yaml.FlowStyle
		}
		return s
	case p.Type == model.ParamGroup:
		m := newMapping()
		m.add(model.ParamGroup, paramsNode(p.Group))
		return m.n
	case p.Type == model.ParamNumberSet && p.NumberSet != nil:
		ns := newMapping()
		ns.n.Style = yaml.FlowStyle
		for _, f := range []struct {
			key string
			v   *int
		}{{"minimum", p.NumberSet.Minimum}, {"maximum", p.NumberSet.Maximum}, {"step", p.NumberSet.Step}} {
			if f.v != nil {
				ns.add(f.key, intNode(*f.v))
			}
		}
		m := newMapping()
		m.add(model.ParamNumberSet, ns.n)
		return m.n
	}
	raw := newMapping()
	raw.addStr("type", p.Type)
	if p.IsRef {
		raw.add("ref", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	raw.add("value", str(p.Value))
	m := newMapping()
	m.add("raw", raw.n)
	return m.n
}

// defaultRef reports whether items of this type reference a package file by default in YAML.
func defaultRef(itemType string) bool {
	_, ok := model.MediaFolder(itemType)
	return ok
}

func itemNode(it model.Item) *yaml.Node {
	itemType := it.Type
	if itemType == "" {
		itemType = model.ItemText
	}
	ref := it.IsRef != defaultRef(itemType)
	if it.Type == "" && !ref && it.Placement == "" && it.Duration == "" && !it.NoWait {
		return str(it.Value)
	}
	m := newMapping()
	m.add(itemType, str(it.Value))
	if ref {
		m.add(keyRef, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(it.IsRef)})
	}
	m.addStr(keyPlacement, it.Placement)
	m.addStr(keyDuration, it.Duration)
	if it.NoWait {
		m.add(keyWait, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
	}
	return m.n
}
