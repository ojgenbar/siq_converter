// Package siqxml converts between SIQ content.xml and the package model.
package siqxml

import (
	"fmt"
	"strconv"
	"strings"

	"siq_converter/internal/model"
)

// Decode parses content.xml. It is strict: any element or attribute the model
// cannot represent is an error, so nothing is ever dropped silently. The
// <files> hash list is read and discarded, since it is regenerated on encode.
func Decode(data []byte) (*model.Package, error) {
	root, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	if root.Name != "package" {
		return nil, fmt.Errorf("%s: root element must be <package>", root.Path)
	}
	return decodePackage(root)
}

func decodePackage(n *node) (*model.Package, error) {
	if err := n.checkAttrs("name", "version", "id", "restriction", "date", "publisher", "contactUri", "difficulty", "logo", "language", "generator"); err != nil {
		return nil, err
	}
	if err := n.checkChildren("tags", "files", "info", "global", "rounds"); err != nil {
		return nil, err
	}
	p := &model.Package{}
	p.Version, _ = n.attr("version")
	if v, err := strconv.ParseFloat(p.Version, 64); err != nil || v < 5 {
		return nil, fmt.Errorf("%s: unsupported package version %q (only SIQ version 5 is supported)", n.Path, p.Version)
	}
	p.Name, _ = n.attr("name")
	p.ID, _ = n.attr("id")
	p.Restriction, _ = n.attr("restriction")
	p.Date, _ = n.attr("date")
	p.Publisher, _ = n.attr("publisher")
	p.ContactURI, _ = n.attr("contactUri")
	if d, ok := n.attr("difficulty"); ok && d != "" {
		v, err := strconv.Atoi(d)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("%s: invalid difficulty %q", n.Path, d)
		}
		p.Difficulty = v
	}
	p.Logo, _ = n.attr("logo")
	p.Language, _ = n.attr("language")
	p.Generator, _ = n.attr("generator")

	tags, err := n.single("tags")
	if err != nil {
		return nil, err
	}
	if p.Tags, err = tags.list("tag"); err != nil {
		return nil, err
	}
	files, err := n.single("files")
	if err != nil {
		return nil, err
	}
	if files != nil {
		if err := checkFiles(files); err != nil {
			return nil, err
		}
	}
	if p.Info, err = decodeInfoOf(n); err != nil {
		return nil, err
	}
	global, err := n.single("global")
	if err != nil {
		return nil, err
	}
	if global != nil {
		if p.Global, err = decodeGlobal(global); err != nil {
			return nil, err
		}
	}
	rounds, err := n.single("rounds")
	if err != nil {
		return nil, err
	}
	if rounds != nil {
		if err := rounds.checkAttrs(); err != nil {
			return nil, err
		}
		if err := rounds.checkChildren("round"); err != nil {
			return nil, err
		}
		for _, c := range rounds.Children {
			r, err := decodeRound(c)
			if err != nil {
				return nil, err
			}
			p.Rounds = append(p.Rounds, r)
		}
	}
	return p, nil
}

func checkFiles(n *node) error {
	if err := n.checkAttrs(); err != nil {
		return err
	}
	if err := n.checkChildren("file"); err != nil {
		return err
	}
	for _, c := range n.Children {
		if err := c.checkAttrs("name", "hash"); err != nil {
			return err
		}
	}
	return nil
}

func decodeInfoOf(parent *node) (model.Info, error) {
	n, err := parent.single("info")
	if err != nil || n == nil {
		return model.Info{}, err
	}
	var info model.Info
	if err := n.checkAttrs(); err != nil {
		return info, err
	}
	if err := n.checkChildren("authors", "sources", "comments", "showmanComments", "extension"); err != nil {
		return info, err
	}
	authors, err := n.single("authors")
	if err != nil {
		return info, err
	}
	if info.Authors, err = authors.list("author"); err != nil {
		return info, err
	}
	sources, err := n.single("sources")
	if err != nil {
		return info, err
	}
	if info.Sources, err = sources.list("source"); err != nil {
		return info, err
	}
	for name, dst := range map[string]*string{"comments": &info.Comments, "showmanComments": &info.ShowmanComments, "extension": &info.Extension} {
		c, err := n.single(name)
		if err != nil {
			return info, err
		}
		if c == nil {
			continue
		}
		if err := c.checkAttrs(); err != nil {
			return info, err
		}
		if *dst, err = c.text(); err != nil {
			return info, err
		}
	}
	return info, nil
}

func decodeGlobal(n *node) (model.Global, error) {
	var g model.Global
	if err := n.checkAttrs(); err != nil {
		return g, err
	}
	if err := n.checkChildren("Authors", "Sources"); err != nil {
		return g, err
	}
	authors, err := n.single("Authors")
	if err != nil {
		return g, err
	}
	if authors != nil {
		err := decodeGlobalList(authors, "Author", []string{"Name", "SecondName", "Surname", "Country", "City"}, func(id string, f map[string]string) {
			g.Authors = append(g.Authors, model.AuthorInfo{ID: id, Name: f["Name"], SecondName: f["SecondName"], Surname: f["Surname"], Country: f["Country"], City: f["City"]})
		})
		if err != nil {
			return g, err
		}
	}
	sources, err := n.single("Sources")
	if err != nil {
		return g, err
	}
	if sources != nil {
		err := decodeGlobalList(sources, "Source", []string{"Author", "Title", "Year", "Publish", "City"}, func(id string, f map[string]string) {
			g.Sources = append(g.Sources, model.SourceInfo{ID: id, Author: f["Author"], Title: f["Title"], Year: f["Year"], Publish: f["Publish"], City: f["City"]})
		})
		if err != nil {
			return g, err
		}
	}
	return g, nil
}

func decodeGlobalList(n *node, item string, fields []string, add func(id string, f map[string]string)) error {
	if err := n.checkAttrs(); err != nil {
		return err
	}
	if err := n.checkChildren(item); err != nil {
		return err
	}
	for _, c := range n.Children {
		if err := c.checkAttrs("id"); err != nil {
			return err
		}
		if err := c.checkChildren(fields...); err != nil {
			return err
		}
		f := map[string]string{}
		for _, name := range fields {
			fn, err := c.single(name)
			if err != nil {
				return err
			}
			if fn == nil {
				continue
			}
			if err := fn.checkAttrs(); err != nil {
				return err
			}
			if f[name], err = fn.text(); err != nil {
				return err
			}
		}
		id, _ := c.attr("id")
		add(id, f)
	}
	return nil
}

func decodeRound(n *node) (model.Round, error) {
	var r model.Round
	if err := n.checkAttrs("name", "type"); err != nil {
		return r, err
	}
	if err := n.checkChildren("info", "themes"); err != nil {
		return r, err
	}
	r.Name, _ = n.attr("name")
	r.Type, _ = n.attr("type")
	var err error
	if r.Info, err = decodeInfoOf(n); err != nil {
		return r, err
	}
	themes, err := n.single("themes")
	if err != nil || themes == nil {
		return r, err
	}
	if err := themes.checkAttrs(); err != nil {
		return r, err
	}
	if err := themes.checkChildren("theme"); err != nil {
		return r, err
	}
	for _, c := range themes.Children {
		t, err := decodeTheme(c)
		if err != nil {
			return r, err
		}
		r.Themes = append(r.Themes, t)
	}
	return r, nil
}

func decodeTheme(n *node) (model.Theme, error) {
	var t model.Theme
	if err := n.checkAttrs("name"); err != nil {
		return t, err
	}
	if err := n.checkChildren("info", "questions"); err != nil {
		return t, err
	}
	t.Name, _ = n.attr("name")
	var err error
	if t.Info, err = decodeInfoOf(n); err != nil {
		return t, err
	}
	questions, err := n.single("questions")
	if err != nil || questions == nil {
		return t, err
	}
	if err := questions.checkAttrs(); err != nil {
		return t, err
	}
	if err := questions.checkChildren("question"); err != nil {
		return t, err
	}
	for _, c := range questions.Children {
		q, err := decodeQuestion(c)
		if err != nil {
			return t, err
		}
		t.Questions = append(t.Questions, q)
	}
	return t, nil
}

func decodeQuestion(n *node) (model.Question, error) {
	var q model.Question
	if err := n.checkAttrs("price", "type"); err != nil {
		return q, err
	}
	// The deprecated version 4 <type> and <scenario> elements are deliberately not allowed.
	if err := n.checkChildren("info", "params", "script", "right", "wrong"); err != nil {
		return q, err
	}
	price, _ := n.attr("price")
	v, err := strconv.Atoi(strings.TrimSpace(price))
	if err != nil {
		return q, fmt.Errorf("%s: invalid price %q", n.Path, price)
	}
	q.Price = v
	q.Type, _ = n.attr("type")
	if q.Info, err = decodeInfoOf(n); err != nil {
		return q, err
	}
	params, err := n.single("params")
	if err != nil {
		return q, err
	}
	if params != nil {
		if q.Params, err = decodeParams(params); err != nil {
			return q, err
		}
	}
	script, err := n.single("script")
	if err != nil {
		return q, err
	}
	if script != nil {
		if err := script.checkAttrs(); err != nil {
			return q, err
		}
		if err := script.checkChildren("step"); err != nil {
			return q, err
		}
		for _, c := range script.Children {
			step, err := decodeParams(c)
			if err != nil {
				return q, err
			}
			q.Script = append(q.Script, step)
		}
	}
	right, err := n.single("right")
	if err != nil {
		return q, err
	}
	if q.Right, err = right.list("answer"); err != nil {
		return q, err
	}
	wrong, err := n.single("wrong")
	if err != nil {
		return q, err
	}
	if q.Wrong, err = wrong.list("answer"); err != nil {
		return q, err
	}
	return q, nil
}

// decodeParams reads a parameters container (<params>, <step> or a group <param>).
func decodeParams(n *node) ([]model.Param, error) {
	if n.Name != "param" {
		if err := n.checkAttrs(); err != nil {
			return nil, err
		}
	}
	if err := n.checkChildren("param"); err != nil {
		return nil, err
	}
	var out []model.Param
	seen := map[string]bool{}
	for _, c := range n.Children {
		p, err := decodeParam(c)
		if err != nil {
			return nil, err
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("%s: duplicate parameter name %q", c.Path, p.Name)
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out, nil
}

func decodeParam(n *node) (model.Param, error) {
	var p model.Param
	if err := n.checkAttrs("name", "type", "isRef"); err != nil {
		return p, err
	}
	p.Name, _ = n.attr("name")
	p.Type, _ = n.attr("type")
	if p.Type == model.ParamSimple {
		p.Type = ""
	}
	var err error
	if p.IsRef, err = boolAttr(n, "isRef", false); err != nil {
		return p, err
	}
	if p.IsRef {
		// SIGame stores referenced parameters as a plain value whatever their type.
		p.Value, err = n.text()
		return p, err
	}
	switch p.Type {
	case model.ParamContent:
		if err := n.checkChildren("item"); err != nil {
			return p, err
		}
		for _, c := range n.Children {
			it, err := decodeItem(c)
			if err != nil {
				return p, err
			}
			p.Items = append(p.Items, it)
		}
	case model.ParamGroup:
		if p.Group, err = decodeParams(n); err != nil {
			return p, err
		}
	case model.ParamNumberSet:
		if err := n.checkChildren("numberSet"); err != nil {
			return p, err
		}
		ns, err := n.single("numberSet")
		if err != nil || ns == nil {
			return p, err
		}
		if err := ns.checkAttrs("minimum", "maximum", "step"); err != nil {
			return p, err
		}
		if _, err := ns.text(); err != nil {
			return p, err
		}
		p.NumberSet = &model.NumberSet{}
		for name, dst := range map[string]**int{"minimum": &p.NumberSet.Minimum, "maximum": &p.NumberSet.Maximum, "step": &p.NumberSet.Step} {
			s, ok := ns.attr(name)
			if !ok {
				continue
			}
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return p, fmt.Errorf("%s: invalid %s %q", ns.Path, name, s)
			}
			*dst = &v
		}
	default:
		// simple or unknown type: plain text value
		if p.Value, err = n.text(); err != nil {
			return p, err
		}
	}
	return p, nil
}

func decodeItem(n *node) (model.Item, error) {
	var it model.Item
	if err := n.checkAttrs("type", "isRef", "placement", "duration", "waitForFinish"); err != nil {
		return it, err
	}
	var err error
	if it.Value, err = n.text(); err != nil {
		return it, err
	}
	it.Type, _ = n.attr("type")
	if it.Type == model.ItemText {
		it.Type = ""
	}
	if it.IsRef, err = boolAttr(n, "isRef", false); err != nil {
		return it, err
	}
	effType := it.Type
	if effType == "" {
		effType = model.ItemText
	}
	if pl, ok := n.attr("placement"); ok && pl != model.DefaultPlacement(effType) {
		it.Placement = pl
	}
	it.Duration, _ = n.attr("duration")
	wait, err := boolAttr(n, "waitForFinish", true)
	if err != nil {
		return it, err
	}
	it.NoWait = !wait
	return it, nil
}

func boolAttr(n *node, name string, def bool) (bool, error) {
	s, ok := n.attr(name)
	if !ok {
		return def, nil
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%s: invalid boolean %s=%q", n.Path, name, s)
}
