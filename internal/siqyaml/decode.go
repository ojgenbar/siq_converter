package siqyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"siq_converter/internal/model"
)

// Decode parses YAML produced by Encode or written by hand.
// Unknown keys are errors, so typos never silently lose data.
func Decode(data []byte) (*model.Package, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("empty YAML document")
		}
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, errAt(&extra, "a second YAML document (after ---) is not supported; keep the whole package in one document")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}
	if _, err := resolveAliases(&doc, map[*yaml.Node]bool{}, map[*yaml.Node]int{}); err != nil {
		return nil, err
	}
	if err := checkChars(&doc, map[*yaml.Node]bool{}); err != nil {
		return nil, err
	}
	return decodePackage(doc.Content[0])
}

// checkChars rejects characters XML cannot hold (e.g. form feed from text
// pasted out of a PDF): SIGame could not open a package containing them.
func checkChars(n *yaml.Node, seen map[*yaml.Node]bool) error {
	if seen[n] {
		return nil // shared through an alias, already checked
	}
	seen[n] = true
	if n.Kind == yaml.ScalarNode {
		if r, bad := model.InvalidXMLRune(n.Value); bad {
			return errAt(n, "text contains the control character %U, which SIGame cannot read; remove it: %q", r, n.Value)
		}
	}
	for _, c := range n.Content {
		if err := checkChars(c, seen); err != nil {
			return err
		}
	}
	return nil
}

// maxExpandedNodes caps the document size after aliases are expanded, so a
// few nested aliases cannot make a tiny file decode into a huge package.
const maxExpandedNodes = 1_000_000

// resolveAliases replaces every alias (*name) by the node it refers to, so
// the decoder can treat a referenced mapping or list like a written-out one.
// It rejects aliases that contain themselves and returns the expanded size.
func resolveAliases(n *yaml.Node, active map[*yaml.Node]bool, size map[*yaml.Node]int) (int, error) {
	if s, ok := size[n]; ok {
		return s, nil
	}
	if active[n] {
		return 0, errAt(n, "an alias refers to a value that contains it")
	}
	active[n] = true
	defer delete(active, n)
	total := 1
	for i, c := range n.Content {
		for steps := 0; c.Kind == yaml.AliasNode; steps++ {
			if c.Alias == nil || steps > 100 {
				return 0, errAt(c, "unresolvable alias")
			}
			c = c.Alias
		}
		n.Content[i] = c
		s, err := resolveAliases(c, active, size)
		if err != nil {
			return 0, err
		}
		if total += s; total > maxExpandedNodes {
			return 0, errAt(n, "the document is too large once aliases are expanded")
		}
	}
	size[n] = total
	return total, nil
}

func errAt(n *yaml.Node, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", n.Line, fmt.Sprintf(format, args...))
}

type field struct {
	key *yaml.Node
	val *yaml.Node
}

// fields returns the entries of a mapping in order, rejecting duplicate keys
// and, when allowed is non-nil, keys not in allowed.
func fields(n *yaml.Node, what string, allowed ...string) ([]field, error) {
	if n.Kind != yaml.MappingNode {
		return nil, errAt(n, "%s must be a mapping", what)
	}
	seen := map[string]bool{}
	var out []field
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, errAt(k, "%s: keys must be scalars", what)
		}
		if seen[k.Value] {
			return nil, errAt(k, "%s: duplicate key %q", what, k.Value)
		}
		seen[k.Value] = true
		if allowed != nil && !contains(allowed, k.Value) {
			return nil, errAt(k, "%s: unknown key %q (allowed: %s)", what, k.Value, strings.Join(allowed, ", "))
		}
		out = append(out, field{k, v})
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

// scalar returns the literal text of a scalar; null yields "".
func scalar(n *yaml.Node, what string) (string, error) {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind != yaml.ScalarNode {
		return "", errAt(n, "%s must be a single value", what)
	}
	if isNull(n) {
		return "", nil
	}
	return n.Value, nil
}

func integer(n *yaml.Node, what string) (int, error) {
	s, err := scalar(n, what)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, errAt(n, "%s must be an integer, got %q", what, s)
	}
	return v, nil
}

func boolean(n *yaml.Node, what string) (bool, error) {
	s, err := scalar(n, what)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, errAt(n, "%s must be true or false, got %q", what, s)
}

// strings accepts a single scalar or a list of scalars; null yields nil.
func stringList(n *yaml.Node, what string) ([]string, error) {
	if isNull(n) {
		return nil, nil
	}
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, errAt(n, "%s must be a value or a list of values", what)
	}
	var out []string
	for _, c := range n.Content {
		s, err := scalar(c, what+" entry")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// list returns the entries of a sequence; null yields nil.
func list(n *yaml.Node, what string) ([]*yaml.Node, error) {
	if isNull(n) {
		return nil, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, errAt(n, "%s must be a list", what)
	}
	return n.Content, nil
}

func decodePackage(n *yaml.Node) (*model.Package, error) {
	fs, err := fields(n, "package", "siq", "name", "id", "restriction", "date", "publisher", "contactUri",
		"difficulty", "logo", "language", "generator", "tags", "info", "global", "rounds")
	if err != nil {
		return nil, err
	}
	p := &model.Package{Version: model.Version}
	hasName := false
	for _, f := range fs {
		k, v := f.key.Value, f.val
		switch k {
		case "siq":
			p.Version, err = scalar(v, k)
			if err == nil {
				if fv, perr := strconv.ParseFloat(p.Version, 64); perr != nil || fv != 5 {
					err = errAt(v, "unsupported siq version %q (only 5 is supported)", p.Version)
				}
			}
		case "name":
			hasName = true
			p.Name, err = scalar(v, k)
		case "id":
			p.ID, err = scalar(v, k)
		case "restriction":
			p.Restriction, err = scalar(v, k)
		case "date":
			p.Date, err = scalar(v, k)
		case "publisher":
			p.Publisher, err = scalar(v, k)
		case "contactUri":
			p.ContactURI, err = scalar(v, k)
		case "difficulty":
			if !isNull(v) {
				p.Difficulty, err = integer(v, k)
				if err == nil && p.Difficulty < 0 {
					err = errAt(v, "difficulty must not be negative, got %d", p.Difficulty)
				}
			}
		case "logo":
			p.Logo, err = scalar(v, k)
		case "language":
			p.Language, err = scalar(v, k)
		case "generator":
			p.Generator, err = scalar(v, k)
		case "tags":
			p.Tags, err = stringList(v, k)
		case "info":
			p.Info, err = decodeInfo(v)
		case "global":
			p.Global, err = decodeGlobal(v)
		case "rounds":
			var items []*yaml.Node
			items, err = list(v, k)
			for _, c := range items {
				if err != nil {
					break
				}
				var r model.Round
				r, err = decodeRound(c)
				p.Rounds = append(p.Rounds, r)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if !hasName {
		return nil, errAt(n, "package: missing required key \"name\"")
	}
	return p, nil
}

func decodeInfo(n *yaml.Node) (model.Info, error) {
	var info model.Info
	if isNull(n) {
		return info, nil
	}
	fs, err := fields(n, "info", "authors", "sources", "comments", "showmanComments", "extension")
	if err != nil {
		return info, err
	}
	for _, f := range fs {
		switch f.key.Value {
		case "authors":
			info.Authors, err = stringList(f.val, "authors")
		case "sources":
			info.Sources, err = stringList(f.val, "sources")
		case "comments":
			info.Comments, err = scalar(f.val, "comments")
		case "showmanComments":
			info.ShowmanComments, err = scalar(f.val, "showmanComments")
		case "extension":
			info.Extension, err = scalar(f.val, "extension")
		}
		if err != nil {
			return info, err
		}
	}
	return info, nil
}

func decodeGlobal(n *yaml.Node) (model.Global, error) {
	var g model.Global
	if isNull(n) {
		return g, nil
	}
	fs, err := fields(n, "global", "authors", "sources")
	if err != nil {
		return g, err
	}
	for _, f := range fs {
		entries, err := list(f.val, "global "+f.key.Value)
		if err != nil {
			return g, err
		}
		for _, e := range entries {
			if f.key.Value == "authors" {
				vals, err := stringFields(e, "global author", "id", "name", "secondName", "surname", "country", "city")
				if err != nil {
					return g, err
				}
				g.Authors = append(g.Authors, model.AuthorInfo{ID: vals["id"], Name: vals["name"], SecondName: vals["secondName"], Surname: vals["surname"], Country: vals["country"], City: vals["city"]})
			} else {
				vals, err := stringFields(e, "global source", "id", "author", "title", "year", "publish", "city")
				if err != nil {
					return g, err
				}
				year := 0
				if y := strings.TrimSpace(vals["year"]); y != "" {
					if year, err = strconv.Atoi(y); err != nil {
						return g, errAt(e, "global source %q: year must be an integer (SIGame requirement), got %q", vals["id"], vals["year"])
					}
				}
				g.Sources = append(g.Sources, model.SourceInfo{ID: vals["id"], Author: vals["author"], Title: vals["title"], Year: year, Publish: vals["publish"], City: vals["city"]})
			}
		}
	}
	return g, nil
}

func stringFields(n *yaml.Node, what string, allowed ...string) (map[string]string, error) {
	fs, err := fields(n, what, allowed...)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range fs {
		if out[f.key.Value], err = scalar(f.val, what+" "+f.key.Value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func decodeRound(n *yaml.Node) (model.Round, error) {
	var r model.Round
	fs, err := fields(n, "round", "name", "type", "info", "themes")
	if err != nil {
		return r, err
	}
	for _, f := range fs {
		switch f.key.Value {
		case "name":
			r.Name, err = scalar(f.val, "round name")
		case "type":
			r.Type, err = scalar(f.val, "round type")
		case "info":
			r.Info, err = decodeInfo(f.val)
		case "themes":
			var items []*yaml.Node
			items, err = list(f.val, "themes")
			for _, c := range items {
				if err != nil {
					break
				}
				var t model.Theme
				t, err = decodeTheme(c)
				r.Themes = append(r.Themes, t)
			}
		}
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

func decodeTheme(n *yaml.Node) (model.Theme, error) {
	var t model.Theme
	fs, err := fields(n, "theme", "name", "info", "questions")
	if err != nil {
		return t, err
	}
	for _, f := range fs {
		switch f.key.Value {
		case "name":
			t.Name, err = scalar(f.val, "theme name")
		case "info":
			t.Info, err = decodeInfo(f.val)
		case "questions":
			var items []*yaml.Node
			items, err = list(f.val, "questions")
			for _, c := range items {
				if err != nil {
					break
				}
				var q model.Question
				q, err = decodeQuestion(c)
				t.Questions = append(t.Questions, q)
			}
		}
		if err != nil {
			return t, err
		}
	}
	return t, nil
}

func decodeQuestion(n *yaml.Node) (model.Question, error) {
	var q model.Question
	fs, err := fields(n, "question", "price", "type", "info", "params", "script", "right", "wrong")
	if err != nil {
		return q, err
	}
	hasPrice := false
	for _, f := range fs {
		switch f.key.Value {
		case "price":
			hasPrice = true
			q.Price, err = integer(f.val, "price")
		case "type":
			q.Type, err = scalar(f.val, "question type")
		case "info":
			q.Info, err = decodeInfo(f.val)
		case "params":
			if !isNull(f.val) {
				q.Params, err = decodeParams(f.val)
			}
		case "script":
			var steps []*yaml.Node
			steps, err = list(f.val, "script")
			for _, s := range steps {
				if err != nil {
					break
				}
				var step []model.Param
				if !isNull(s) {
					step, err = decodeParams(s)
				}
				q.Script = append(q.Script, step)
			}
		case "right":
			q.Right, err = stringList(f.val, "right")
		case "wrong":
			q.Wrong, err = stringList(f.val, "wrong")
		}
		if err != nil {
			return q, err
		}
	}
	q.Right = model.NormalizeRight(q.Right)
	if !hasPrice {
		return q, errAt(n, "question: missing required key \"price\"")
	}
	return q, nil
}

func decodeParams(n *yaml.Node) ([]model.Param, error) {
	fs, err := fields(n, "params")
	if err != nil {
		return nil, err
	}
	var out []model.Param
	for _, f := range fs {
		p, err := decodeParam(f.key.Value, f.val)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// decodeParam infers the parameter type from the shape of its value:
// scalar = simple, list = content, {group: ...}, {numberSet: ...} or {raw: ...}.
func decodeParam(name string, n *yaml.Node) (model.Param, error) {
	p := model.Param{Name: name}
	what := fmt.Sprintf("param %q", name)
	var err error
	switch n.Kind {
	case yaml.ScalarNode, yaml.AliasNode:
		p.Value, err = scalar(n, what)
		return p, err
	case yaml.SequenceNode:
		p.Type = model.ParamContent
		for _, c := range n.Content {
			it, err := decodeItem(c)
			if err != nil {
				return p, err
			}
			p.Items = append(p.Items, it)
		}
		return p, nil
	}
	fs, err := fields(n, what, model.ParamGroup, model.ParamNumberSet, "raw")
	if err != nil {
		return p, err
	}
	if len(fs) != 1 {
		return p, errAt(n, "%s: expected exactly one of group, numberSet, raw", what)
	}
	v := fs[0].val
	switch fs[0].key.Value {
	case model.ParamGroup:
		p.Type = model.ParamGroup
		if !isNull(v) {
			p.Group, err = decodeParams(v)
		}
	case model.ParamNumberSet:
		p.Type = model.ParamNumberSet
		p.NumberSet = &model.NumberSet{}
		var nfs []field
		nfs, err = fields(v, what+" numberSet", "minimum", "maximum", "step")
		for _, nf := range nfs {
			if err != nil {
				break
			}
			var x int
			x, err = integer(nf.val, nf.key.Value)
			switch nf.key.Value {
			case "minimum":
				p.NumberSet.Minimum = &x
			case "maximum":
				p.NumberSet.Maximum = &x
			case "step":
				p.NumberSet.Step = &x
			}
		}
	case "raw":
		var rfs []field
		rfs, err = fields(v, what+" raw", "type", "ref", "value")
		for _, rf := range rfs {
			if err != nil {
				break
			}
			switch rf.key.Value {
			case "type":
				p.Type, err = scalar(rf.val, "type")
				if p.Type == model.ParamSimple {
					p.Type = ""
				}
			case "ref":
				p.IsRef, err = boolean(rf.val, "ref")
			case "value":
				p.Value, err = scalar(rf.val, "value")
			}
		}
		// raw is for references and unknown types; a structured value written as
		// raw would be packed empty, so ask for its real form instead.
		if err == nil && !p.IsRef {
			switch {
			case p.Type == model.ParamContent || p.Type == model.ParamGroup:
				err = errAt(v, "%s: a %s parameter cannot be written as raw; use a list of items (content) or {group: ...}", what, p.Type)
			case p.Type == model.ParamNumberSet && p.Value != "":
				err = errAt(v, "%s: a numberSet value cannot be written as raw; use {numberSet: {minimum: ..., maximum: ..., step: ...}}", what)
			}
		}
	}
	return p, err
}

// decodeItem reads a bare string (text item) or a mapping with one type key
// ({image: file.jpg}) plus optional ref, placement, wait and duration.
func decodeItem(n *yaml.Node) (model.Item, error) {
	var it model.Item
	if n.Kind == yaml.ScalarNode || n.Kind == yaml.AliasNode {
		var err error
		it.Value, err = scalar(n, "item")
		return it, err
	}
	fs, err := fields(n, "item")
	if err != nil {
		return it, err
	}
	var typeField *field
	refSet := false
	for i := range fs {
		f := fs[i]
		switch f.key.Value {
		case keyRef:
			refSet = true
			it.IsRef, err = boolean(f.val, keyRef)
		case keyPlacement:
			it.Placement, err = scalar(f.val, keyPlacement)
		case keyDuration:
			it.Duration, err = scalar(f.val, keyDuration)
		case keyWait:
			var wait bool
			wait, err = boolean(f.val, keyWait)
			it.NoWait = !wait
		default:
			if !model.IsItemType(f.key.Value) {
				return it, errAt(f.key, "item: unknown key %q (types: %s; other keys: ref, placement, wait, duration)", f.key.Value, strings.Join(model.ItemTypes, ", "))
			}
			if typeField != nil {
				return it, errAt(f.key, "item has two type keys: %q and %q", typeField.key.Value, f.key.Value)
			}
			typeField = &fs[i]
		}
		if err != nil {
			return it, err
		}
	}
	if typeField == nil {
		return it, errAt(n, "item needs a type key: %s", strings.Join(model.ItemTypes, ", "))
	}
	itemType := typeField.key.Value
	if it.Value, err = scalar(typeField.val, "item "+itemType); err != nil {
		return it, err
	}
	if !refSet {
		it.IsRef = defaultRef(itemType)
	}
	if it.Placement == model.DefaultPlacement(itemType) {
		it.Placement = ""
	}
	if itemType != model.ItemText {
		it.Type = itemType
	}
	return it, nil
}
