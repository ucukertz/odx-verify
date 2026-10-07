package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// node is a generic ordered XML element used for both the ODX document and the
// embedded XSD.
type node struct {
	Name     string
	Attrs    map[string]string
	NSAttrs  map[string]string // namespaced attributes, e.g. xsi:type
	Children []*node
	Text     string
	Line     int
}

func parseXML(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var root *node
	var stack []*node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{
				Name:    t.Name.Local,
				Attrs:   map[string]string{},
				NSAttrs: map[string]string{},
				Line:    lineOf(data, dec.InputOffset()),
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				if a.Name.Space != "" {
					n.NSAttrs[a.Name.Local] = a.Value
					continue
				}
				n.Attrs[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple root elements")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no root element")
	}
	return root, nil
}

func lineOf(data []byte, off int64) int {
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	return 1 + bytes.Count(data[:off], []byte{'\n'})
}

// validateODX validates a document against the schema for its MODEL-VERSION
// (ODX 2.0.1 or 2.2.0), targeting Vector ODX Studio behaviour.
func validateODX(data []byte) []string {
	root, err := parseXML(data)
	if err != nil {
		return []string{"invalid XML: " + err.Error()}
	}
	var errs []string
	if root.Name != "ODX" {
		errs = append(errs, fmt.Sprintf("line %d: root element must be <ODX>, got <%s>", root.Line, root.Name))
	}
	// Vector rule: the schema's xsd:choice permits COMPARAM-SPEC as the
	// top-level category, but Vector does not accept it as a root; use
	// <DIAG-LAYER-CONTAINER>.
	for _, c := range root.Children {
		if c.Name == "COMPARAM-SPEC" {
			errs = append(errs, fmt.Sprintf("line %d: <COMPARAM-SPEC> is not supported as the root category by Vector ODX Studio; use <DIAG-LAYER-CONTAINER>", c.Line))
		}
	}

	// Pick the schema from MODEL-VERSION (2.0.1 or 2.2.0).
	version := root.Attrs["MODEL-VERSION"]
	schema, ok := odxSchemas[version]
	if !ok {
		if version == "" {
			errs = append(errs, fmt.Sprintf("line %d: missing required attribute MODEL-VERSION on <ODX> (supported: %s)", root.Line, strings.Join(supportedVersions, ", ")))
		} else {
			errs = append(errs, fmt.Sprintf("line %d: unsupported ODX version %q for attribute MODEL-VERSION (supported: %s)", root.Line, version, strings.Join(supportedVersions, ", ")))
		}
		return errs
	}

	v := &validator{schema: schema, errs: &errs}
	if ct := schema.complex["ODX"]; ct != nil {
		v.validateComplex(root, ct)
	}
	checkIDs(root, &errs)
	return errs
}

func addErr(errs *[]string, line int, format string, a ...any) {
	*errs = append(*errs, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, a...))
}

func checkIDs(n *node, errs *[]string) {
	seen := map[string]int{}
	var walk func(*node)
	walk = func(x *node) {
		if id, ok := x.Attrs["ID"]; ok {
			if prev, dup := seen[id]; dup {
				addErr(errs, x.Line, "duplicate ID %q (first used at line %d)", id, prev)
			} else {
				seen[id] = x.Line
			}
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
}

func containsName(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
