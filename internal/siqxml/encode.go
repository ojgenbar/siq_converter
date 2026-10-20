package siqxml

import (
	"strconv"
	"strings"

	"siq_converter/internal/model"
)

// Namespace is the SIQ version 5 XML namespace.
const Namespace = "https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd"

// FileHash is one entry of the package <files> list.
type FileHash struct {
	Name string // e.g. "Images/photo 1.jpg"
	Hash string // uppercase hex SHA-256
}

// Encode writes content.xml the way SIGame does: UTF-8 BOM, a single line,
// SIGame's element and attribute order, and True/False booleans.
// files must already be sorted by name.
func Encode(p *model.Package, files []FileHash) []byte {
	w := &writer{}
	w.b.Write(bom)
	w.b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)

	version := p.Version
	if version == "" {
		version = model.Version
	}
	w.required("package", "name", p.Name)
	w.attrs([]string{
		"version", version,
		"id", p.ID,
		"restriction", p.Restriction,
		"date", p.Date,
		"publisher", p.Publisher,
		"contactUri", p.ContactURI,
		"difficulty", difficulty(p.Difficulty),
		"logo", p.Logo,
		"language", p.Language,
		"generator", p.Generator,
		"xmlns", Namespace,
	})
	w.b.WriteString(">")
	if len(p.Tags) > 0 {
		w.open("tags")
		for _, t := range p.Tags {
			w.leaf("tag", t)
		}
		w.close("tags")
	}
	if !p.Global.IsEmpty() {
		w.global(p.Global)
	}
	if len(files) > 0 {
		w.open("files")
		for _, f := range files {
			w.empty("file", "name", f.Name, "hash", f.Hash)
		}
		w.close("files")
	}
	w.info(p.Info)
	if len(p.Rounds) > 0 {
		w.open("rounds")
		for _, r := range p.Rounds {
			w.round(r)
		}
		w.close("rounds")
	}
	w.close("package")
	return []byte(w.b.String())
}

func difficulty(d int) string {
	if d <= 0 {
		return ""
	}
	return strconv.Itoa(d)
}

type writer struct {
	b strings.Builder
}

// start writes "<name attrs"; attrs are name/value pairs, empty values are skipped.
func (w *writer) start(name string, attrs []string) {
	w.b.WriteString("<")
	w.b.WriteString(name)
	w.attrs(attrs)
}

// required writes "<name attr="value"" even when value is empty.
func (w *writer) required(name, attr, value string) {
	w.b.WriteString("<" + name + " " + attr + `="` + escapeAttr(value) + `"`)
}

func (w *writer) attrs(attrs []string) {
	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i+1] == "" {
			continue
		}
		w.b.WriteString(" ")
		w.b.WriteString(attrs[i])
		w.b.WriteString(`="`)
		w.b.WriteString(escapeAttr(attrs[i+1]))
		w.b.WriteString(`"`)
	}
}

func (w *writer) open(name string, attrs ...string) {
	w.start(name, attrs)
	w.b.WriteString(">")
}

func (w *writer) close(name string) {
	w.b.WriteString("</")
	w.b.WriteString(name)
	w.b.WriteString(">")
}

func (w *writer) empty(name string, attrs ...string) {
	w.start(name, attrs)
	w.b.WriteString(" />")
}

// leafAttrs writes an element with text content (or a self-closing one if text is empty).
func (w *writer) leafAttrs(name, text string, attrs ...string) {
	if text == "" {
		w.empty(name, attrs...)
		return
	}
	w.open(name, attrs...)
	w.b.WriteString(escapeText(text))
	w.close(name)
}

func (w *writer) leaf(name, text string) { w.leafAttrs(name, text) }

func (w *writer) global(g model.Global) {
	w.open("global")
	if len(g.Authors) > 0 {
		w.open("Authors")
		for _, a := range g.Authors {
			w.required("Author", "id", a.ID)
			w.b.WriteString(">")
			w.leaf("Name", a.Name)
			w.leaf("SecondName", a.SecondName)
			w.leaf("Surname", a.Surname)
			w.leaf("Country", a.Country)
			w.leaf("City", a.City)
			w.close("Author")
		}
		w.close("Authors")
	}
	if len(g.Sources) > 0 {
		w.open("Sources")
		for _, s := range g.Sources {
			w.required("Source", "id", s.ID)
			w.b.WriteString(">")
			w.leaf("Author", s.Author)
			w.leaf("Title", s.Title)
			w.leaf("Year", s.Year)
			w.leaf("Publish", s.Publish)
			w.leaf("City", s.City)
			w.close("Source")
		}
		w.close("Sources")
	}
	w.close("global")
}

func (w *writer) info(i model.Info) {
	if i.IsEmpty() {
		return
	}
	w.open("info")
	if len(i.Authors) > 0 {
		w.open("authors")
		for _, a := range i.Authors {
			w.leaf("author", a)
		}
		w.close("authors")
	}
	if len(i.Sources) > 0 {
		w.open("sources")
		for _, s := range i.Sources {
			w.leaf("source", s)
		}
		w.close("sources")
	}
	if i.Comments != "" {
		w.leaf("comments", i.Comments)
	}
	if i.ShowmanComments != "" {
		w.leaf("showmanComments", i.ShowmanComments)
	}
	if i.Extension != "" {
		w.leaf("extension", i.Extension)
	}
	w.close("info")
}

func (w *writer) round(r model.Round) {
	// name is required, so it is written even when empty
	w.required("round", "name", r.Name)
	w.attrs([]string{"type", r.Type})
	w.b.WriteString(">")
	w.info(r.Info)
	if len(r.Themes) > 0 {
		w.open("themes")
		for _, t := range r.Themes {
			w.theme(t)
		}
		w.close("themes")
	}
	w.close("round")
}

func (w *writer) theme(t model.Theme) {
	w.required("theme", "name", t.Name)
	w.b.WriteString(">")
	w.info(t.Info)
	if len(t.Questions) > 0 {
		w.open("questions")
		for _, q := range t.Questions {
			w.question(q)
		}
		w.close("questions")
	}
	w.close("theme")
}

func (w *writer) question(q model.Question) {
	w.open("question", "price", strconv.Itoa(q.Price), "type", q.Type)
	w.info(q.Info)
	if len(q.Script) > 0 {
		w.open("script")
		for _, step := range q.Script {
			w.open("step")
			w.params(step)
			w.close("step")
		}
		w.close("script")
	}
	if len(q.Params) > 0 {
		w.open("params")
		w.params(q.Params)
		w.close("params")
	}
	w.open("right")
	for _, a := range q.Right {
		w.leaf("answer", a)
	}
	w.close("right")
	if len(q.Wrong) > 0 {
		w.open("wrong")
		for _, a := range q.Wrong {
			w.leaf("answer", a)
		}
		w.close("wrong")
	}
	w.close("question")
}

func (w *writer) params(ps []model.Param) {
	for _, p := range ps {
		w.required("param", "name", p.Name)
		w.attrs([]string{"type", p.Type, "isRef", boolTrue(p.IsRef)})
		w.b.WriteString(">")
		switch {
		case p.IsRef:
			w.b.WriteString(escapeText(p.Value))
		case p.Type == model.ParamContent:
			for _, it := range p.Items {
				w.item(it)
			}
		case p.Type == model.ParamGroup:
			w.params(p.Group)
		case p.Type == model.ParamNumberSet:
			if ns := p.NumberSet; ns != nil {
				w.empty("numberSet", "minimum", optInt(ns.Minimum), "maximum", optInt(ns.Maximum), "step", optInt(ns.Step))
			}
		default:
			w.b.WriteString(escapeText(p.Value))
		}
		w.close("param")
	}
}

func (w *writer) item(it model.Item) {
	effType := it.Type
	if effType == "" {
		effType = model.ItemText
	}
	placement := it.Placement
	if placement == "" && effType == model.ItemAudio {
		// SIGame always writes the placement of audio items
		placement = model.DefaultPlacement(effType)
	}
	wait := ""
	if it.NoWait {
		wait = "False"
	}
	w.leafAttrs("item", it.Value,
		"type", it.Type,
		"isRef", boolTrue(it.IsRef),
		"placement", placement,
		"duration", it.Duration,
		"waitForFinish", wait,
	)
}

func boolTrue(b bool) string {
	if b {
		return "True"
	}
	return ""
}

func optInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\r", "&#xD;", "\n", "&#xA;", "\t", "&#x9;")
)

func escapeText(s string) string { return textEscaper.Replace(s) }
func escapeAttr(s string) string { return attrEscaper.Replace(s) }
