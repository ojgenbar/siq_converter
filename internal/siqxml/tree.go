package siqxml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// node is a namespace-agnostic XML element.
type node struct {
	Name     string
	Attrs    []xml.Attr
	Children []*node
	Text     string // all character data directly inside the element
	Path     string // e.g. /package/rounds/round[2]
}

var bom = []byte{0xEF, 0xBB, 0xBF}

func parseTree(data []byte) (*node, error) {
	data = bytes.TrimPrefix(data, bom)
	dec := xml.NewDecoder(bytes.NewReader(data))
	var root *node
	var stack []*node
	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("parse content.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{Name: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: a.Name.Local}, Value: a.Value})
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("parse content.xml: multiple root elements")
				}
				n.Path = "/" + n.Name
				root = n
			} else {
				parent := stack[len(stack)-1]
				idx := 1
				for _, c := range parent.Children {
					if c.Name == n.Name {
						idx++
					}
				}
				n.Path = fmt.Sprintf("%s/%s[%d]", parent.Path, n.Name, idx)
				parent.Children = append(parent.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("parse content.xml: no root element")
	}
	return root, nil
}

func (n *node) attr(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

// checkAttrs fails on any attribute not in the allowed list.
func (n *node) checkAttrs(allowed ...string) error {
	for _, a := range n.Attrs {
		ok := false
		for _, name := range allowed {
			if a.Name.Local == name {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s: unsupported attribute %q", n.Path, a.Name.Local)
		}
	}
	return nil
}

// checkChildren fails on any child element not in the allowed list,
// and on non-whitespace text mixed in between child elements.
func (n *node) checkChildren(allowed ...string) error {
	if strings.TrimSpace(n.Text) != "" {
		return fmt.Errorf("%s: unexpected text %q", n.Path, strings.TrimSpace(n.Text))
	}
	for _, c := range n.Children {
		ok := false
		for _, name := range allowed {
			if c.Name == name {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s: unsupported element <%s>", n.Path, c.Name)
		}
	}
	return nil
}

// single returns the only child with the given name, or nil.
func (n *node) single(name string) (*node, error) {
	var found *node
	for _, c := range n.Children {
		if c.Name == name {
			if found != nil {
				return nil, fmt.Errorf("%s: duplicate element <%s>", n.Path, name)
			}
			found = c
		}
	}
	return found, nil
}

// text returns the element's text, failing if it has child elements.
func (n *node) text() (string, error) {
	if len(n.Children) > 0 {
		return "", fmt.Errorf("%s: unexpected element <%s> inside text element", n.Path, n.Children[0].Name)
	}
	return n.Text, nil
}

// list reads a container whose children are all text elements named item.
func (n *node) list(item string) ([]string, error) {
	if n == nil {
		return nil, nil
	}
	if err := n.checkAttrs(); err != nil {
		return nil, err
	}
	if err := n.checkChildren(item); err != nil {
		return nil, err
	}
	var out []string
	for _, c := range n.Children {
		if err := c.checkAttrs(); err != nil {
			return nil, err
		}
		s, err := c.text()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
