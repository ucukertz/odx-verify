package main

import (
	_ "embed"
	"strconv"
	"strings"
)

// Embedded ODX schema files the checker validates against: one per supported
// MODEL-VERSION, plus the rich-text (<DESC>) subset used by each.

//go:embed odx201.xsd
var odx201XSD []byte

//go:embed odx201-xhtml.xsd
var odx201XHTML []byte

//go:embed odx220.xsd
var odx220XSD []byte

//go:embed odx220-xhtml.xsd
var odx220XHTML []byte

type stDef struct {
	base      string
	enums     []string
	patterns  []string
	minIncl   *float64
	maxIncl   *float64
	minExcl   *float64
	maxExcl   *float64
	length    *int
	minLength *int
	maxLength *int
}

type attrDef struct {
	name   string
	typ    string
	use    string
	def    string
	fixed  string
	inline *stDef
}

const (
	kElem = iota
	kSeq
	kGroup
	kChoice
)

type particle struct {
	kind   int
	name   string
	typ    string
	min    int
	max    int // -1 == unbounded
	ref    string
	subs   []particle
	inline *stDef
}

type ctDef struct {
	name          string
	abstract      bool
	mixed         bool
	base          string
	derivation    string
	simpleContent bool
	simpleFacets  *stDef
	content       []particle
	attributes    []attrDef
	attrGroups    []string
}

type xsdSchema struct {
	complex map[string]*ctDef
	simple  map[string]*stDef
	groups  map[string][]particle
	attrs   map[string][]attrDef
}

// odxSchemas holds one parsed schema per supported MODEL-VERSION.
var odxSchemas = map[string]*xsdSchema{
	"2.0.1": buildODXSchema(odx201XSD, odx201XHTML),
	"2.2.0": buildODXSchema(odx220XSD, odx220XHTML),
}

// supportedVersions lists the MODEL-VERSION values we can validate.
var supportedVersions = []string{"2.0.1", "2.2.0"}

func buildODXSchema(xsd, xhtml []byte) *xsdSchema {
	s := &xsdSchema{
		complex: map[string]*ctDef{},
		simple:  map[string]*stDef{},
		groups:  map[string][]particle{},
		attrs:   map[string][]attrDef{},
	}
	s.parse(xsd, false)
	s.parse(xhtml, true)
	return s
}

func (s *xsdSchema) parse(data []byte, merge bool) {
	root, err := parseXML(data)
	if err != nil {
		panic("embedded schema parse error: " + err.Error())
	}
	for _, c := range root.Children {
		name := c.Attrs["name"]
		switch c.Name {
		case "complexType":
			ct := parseCT(c)
			if !merge || s.complex[ct.name] == nil {
				s.complex[ct.name] = ct
			}
		case "simpleType":
			if !merge || s.simple[name] == nil {
				s.simple[name] = parseST(c)
			}
		case "group":
			if !merge || s.groups[name] == nil {
				s.groups[name] = parseModel(c)
			}
		case "attributeGroup":
			if !merge || s.attrs[name] == nil {
				s.attrs[name] = parseAttrGroup(c)
			}
		}
	}
}

// stripPrefix removes a namespace prefix (e.g. "ns:Foo" -> "Foo"). ODX type
// names carry no prefix, but xsi:type values occasionally do.
func stripPrefix(t string) string {
	if i := strings.LastIndexByte(t, ':'); i >= 0 {
		return t[i+1:]
	}
	return t
}

func (s *xsdSchema) knownType(name string) bool {
	if _, ok := s.complex[name]; ok {
		return true
	}
	if _, ok := s.simple[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "xsd:")
}

// derives reports whether name equals base or transitively derives from it via
// the base chain of complex/simple types.
func (s *xsdSchema) derives(name, base string) bool {
	for steps := 0; name != "" && steps < 100; steps++ {
		if name == base {
			return true
		}
		if ct, ok := s.complex[name]; ok {
			name = ct.base
		} else if st, ok := s.simple[name]; ok {
			name = st.base
		} else {
			return false
		}
	}
	return name == base
}

func firstChild(n *node, name string) *node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func parseOcc(n *node) (int, int) {
	min, max := 1, 1
	if v, ok := n.Attrs["minOccurs"]; ok {
		min, _ = strconv.Atoi(v)
	}
	if v, ok := n.Attrs["maxOccurs"]; ok {
		if v == "unbounded" {
			max = -1
		} else {
			max, _ = strconv.Atoi(v)
		}
	}
	return min, max
}

func parseST(n *node) *stDef {
	if r := firstChild(n, "restriction"); r != nil {
		return parseRestriction(r)
	}
	return &stDef{}
}

func parseRestriction(r *node) *stDef {
	st := &stDef{base: r.Attrs["base"]}
	for _, f := range r.Children {
		v := f.Attrs["value"]
		switch f.Name {
		case "enumeration":
			st.enums = append(st.enums, v)
		case "pattern":
			st.patterns = append(st.patterns, v)
		case "minInclusive":
			st.minIncl = parseF(v)
		case "maxInclusive":
			st.maxIncl = parseF(v)
		case "minExclusive":
			st.minExcl = parseF(v)
		case "maxExclusive":
			st.maxExcl = parseF(v)
		case "length":
			st.length = parseI(v)
		case "minLength":
			st.minLength = parseI(v)
		case "maxLength":
			st.maxLength = parseI(v)
		}
	}
	return st
}

func parseF(v string) *float64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

func parseI(v string) *int {
	i, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &i
}

func parseCT(n *node) *ctDef {
	ct := &ctDef{
		name:     n.Attrs["name"],
		abstract: n.Attrs["abstract"] == "true",
		mixed:    n.Attrs["mixed"] == "true",
	}
	cc := firstChild(n, "complexContent")
	sc := firstChild(n, "simpleContent")
	switch {
	case cc != nil:
		ext := firstChild(cc, "extension")
		ct.derivation = "extension"
		if ext == nil {
			ext = firstChild(cc, "restriction")
			ct.derivation = "restriction"
		}
		if ext != nil {
			ct.base = ext.Attrs["base"]
			ct.content = parseModel(ext)
			parseAttrs(ext, ct)
		}
	case sc != nil:
		ct.simpleContent = true
		ext := firstChild(sc, "extension")
		ct.derivation = "extension"
		if ext == nil {
			ext = firstChild(sc, "restriction")
			ct.derivation = "restriction"
		}
		if ext != nil {
			ct.base = ext.Attrs["base"]
			if ct.derivation == "restriction" {
				ct.simpleFacets = parseRestriction(ext)
			}
			parseAttrs(ext, ct)
		}
	default:
		ct.content = parseModel(n)
		parseAttrs(n, ct)
	}
	return ct
}

func parseAttrGroup(n *node) []attrDef {
	var out []attrDef
	for _, c := range n.Children {
		if c.Name == "attribute" {
			out = append(out, parseAttr(c))
		}
	}
	return out
}

func parseAttrs(container *node, ct *ctDef) {
	for _, c := range container.Children {
		switch c.Name {
		case "attribute":
			ct.attributes = append(ct.attributes, parseAttr(c))
		case "attributeGroup":
			ct.attrGroups = append(ct.attrGroups, c.Attrs["ref"])
		}
	}
}

func parseAttr(c *node) attrDef {
	a := attrDef{
		name:  c.Attrs["name"],
		typ:   c.Attrs["type"],
		use:   c.Attrs["use"],
		def:   c.Attrs["default"],
		fixed: c.Attrs["fixed"],
	}
	if a.use == "" {
		a.use = "optional"
	}
	if st := firstChild(c, "simpleType"); st != nil {
		a.inline = parseST(st)
	}
	return a
}

// parseModel flattens a sequence context (complexType, extension, group) into a
// list of particles. Nested xsd:sequence is spliced in place.
func parseModel(container *node) []particle {
	var out []particle
	for _, c := range container.Children {
		switch c.Name {
		case "element":
			out = append(out, elemParticle(c))
		case "group":
			out = append(out, groupParticle(c))
		case "choice":
			out = append(out, choiceParticle(c))
		case "sequence", "all":
			out = append(out, parseModel(c)...)
		}
	}
	return out
}

func parseAlternatives(ch *node) []particle {
	var out []particle
	for _, c := range ch.Children {
		switch c.Name {
		case "element":
			out = append(out, elemParticle(c))
		case "group":
			out = append(out, groupParticle(c))
		case "choice":
			out = append(out, choiceParticle(c))
		case "sequence", "all":
			min, max := parseOcc(c)
			out = append(out, particle{kind: kSeq, min: min, max: max, subs: parseModel(c)})
		}
	}
	return out
}

func elemParticle(c *node) particle {
	p := particle{kind: kElem, name: c.Attrs["name"], typ: c.Attrs["type"]}
	if p.name == "" {
		p.name = c.Attrs["ref"] // global element reference (xhtml)
	}
	p.min, p.max = parseOcc(c)
	if st := firstChild(c, "simpleType"); st != nil {
		p.inline = parseST(st)
	}
	return p
}

func groupParticle(c *node) particle {
	min, max := parseOcc(c)
	return particle{kind: kGroup, ref: c.Attrs["ref"], min: min, max: max}
}

func choiceParticle(c *node) particle {
	min, max := parseOcc(c)
	return particle{kind: kChoice, min: min, max: max, subs: parseAlternatives(c)}
}

func (s *xsdSchema) allAttrs(ct *ctDef) []attrDef {
	var out []attrDef
	if ct.base != "" {
		if b, ok := s.complex[ct.base]; ok {
			out = append(out, s.allAttrs(b)...)
		}
	}
	out = append(out, ct.attributes...)
	for _, ag := range ct.attrGroups {
		out = append(out, s.attrs[ag]...)
	}
	return out
}

// allContent returns the effective child sequence: inherited children first
// (complexContent extension), then the type's own children.
func (s *xsdSchema) allContent(ct *ctDef) []particle {
	var out []particle
	if ct.base != "" {
		if b, ok := s.complex[ct.base]; ok && !b.simpleContent {
			out = append(out, s.allContent(b)...)
		}
	}
	out = append(out, ct.content...)
	return out
}
