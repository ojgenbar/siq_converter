package siqxml

import (
	"fmt"
	"sort"
	"strings"
)

// Canonical renders content.xml in a normalized textual form that ignores
// everything SIQ treats as insignificant: formatting, namespace declarations,
// attribute order, boolean casing, default attribute values, empty optional
// containers, the order of xs:all children, and the regenerated <files> list.
// It is independent of the model, so comparing Canonical(original) with
// Canonical(Encode(Decode(original))) catches data the model drops.
func Canonical(data []byte) (string, error) {
	root, err := parseTree(data)
	if err != nil {
		return "", err
	}
	normalize(root, nil)
	var b strings.Builder
	render(&b, root, 0)
	return b.String(), nil
}

// Elements whose children form an xs:all group (order is not significant).
var unordered = map[string]bool{"package": true, "round": true, "theme": true, "question": true, "info": true, "global": true, "Author": true, "Source": true}

// Elements that never hold text, so whitespace inside them is formatting.
var containers = map[string]bool{
	"package": true, "tags": true, "info": true, "authors": true, "sources": true, "global": true, "Authors": true, "Sources": true,
	"Author": true, "Source": true, "rounds": true, "round": true, "themes": true, "theme": true, "questions": true, "question": true,
	"params": true, "script": true, "step": true, "right": true, "wrong": true, "files": true, "numberSet": true,
}

// Optional elements that mean nothing when empty.
var droppableEmpty = map[string]bool{
	"tags": true, "info": true, "global": true, "rounds": true, "themes": true, "questions": true,
	"params": true, "script": true, "wrong": true, "authors": true, "sources": true,
	"comments": true, "showmanComments": true, "extension": true, "Authors": true, "Sources": true,
}

func normalize(n *node, parent *node) {
	var kept []*node
	for _, c := range n.Children {
		if n.Name == "package" && c.Name == "files" {
			continue
		}
		normalize(c, n)
		if droppableEmpty[c.Name] && len(c.Children) == 0 && c.Text == "" && c.Path == "" {
			continue
		}
		if (n.Name == "Author" || n.Name == "Source") && len(c.Children) == 0 && c.Text == "" {
			continue
		}
		kept = append(kept, c)
	}
	n.Children = kept
	paramType, _ := n.attr("type")
	structuredParam := n.Name == "param" && (paramType == "content" || paramType == "group" || paramType == "numberSet")
	if (len(n.Children) > 0 || containers[n.Name] || structuredParam) && strings.TrimSpace(n.Text) == "" {
		n.Text = ""
	}
	if unordered[n.Name] {
		sort.SliceStable(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
	}

	itemType, _ := n.attr("type")
	if itemType == "" {
		itemType = "text"
	}
	var attrs []string
	for _, a := range n.Attrs {
		name, v := a.Name.Local, a.Value
		switch name {
		case "isRef", "waitForFinish":
			v = strings.ToLower(strings.TrimSpace(v))
		}
		drop := v == "" ||
			(name == "isRef" && v == "false") ||
			(name == "waitForFinish" && v == "true") ||
			(n.Name == "item" && name == "type" && v == "text") ||
			(n.Name == "param" && name == "type" && v == "simple") ||
			(n.Name == "package" && name == "difficulty" && v == "0") ||
			(n.Name == "item" && name == "placement" && v == defaultPlacement(itemType))
		if (name == "name" && (n.Name == "package" || n.Name == "round" || n.Name == "theme" || n.Name == "param")) || name == "price" {
			drop = false // required attributes stay even when empty
		}
		if !drop {
			attrs = append(attrs, fmt.Sprintf("%s=%q", name, v))
		}
	}
	sort.Strings(attrs)
	n.Attrs = nil
	n.Path = strings.Join(attrs, " ")
}

func defaultPlacement(itemType string) string {
	if itemType == "audio" {
		return "background"
	}
	return "screen"
}

func render(b *strings.Builder, n *node, depth int) {
	ind := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%s<%s %s>", ind, n.Name, n.Path)
	if n.Text != "" {
		fmt.Fprintf(b, " %q", n.Text)
	}
	b.WriteString("\n")
	for _, c := range n.Children {
		render(b, c, depth+1)
	}
}
