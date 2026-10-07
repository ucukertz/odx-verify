package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type validator struct {
	schema *xsdSchema
	errs   *[]string
}

func (v *validator) errf(line int, format string, a ...any) {
	addErr(v.errs, line, format, a...)
}

// ---------------------------------------------------------------------------
// Complex type validation
// ---------------------------------------------------------------------------

func (v *validator) validateComplex(n *node, ct *ctDef) {
	if ct == nil {
		return
	}
	if ct.abstract {
		v.errf(n.Line, "element <%s> instantiates abstract type %s", n.Name, ct.name)
	}

	declared := map[string]attrDef{}
	for _, a := range v.schema.allAttrs(ct) {
		if _, ok := declared[a.name]; !ok {
			declared[a.name] = a
		}
	}
	for name, a := range declared {
		val, ok := n.Attrs[name]
		if !ok {
			if a.use == "required" {
				v.errf(n.Line, "missing required attribute %s on <%s>", name, n.Name)
			}
			continue
		}
		if a.fixed != "" && val != a.fixed {
			v.errf(n.Line, "attribute %s on <%s> must be %q", name, n.Name, a.fixed)
		}
		v.validateAttrValue(n, name, a, val)
	}
	for name := range n.Attrs {
		if _, ok := declared[name]; ok {
			continue
		}
		v.errf(n.Line, "unexpected attribute %s on <%s>", name, n.Name)
	}

	if ct.simpleContent {
		if len(n.Children) > 0 {
			v.errf(n.Children[0].Line, "unexpected element <%s> in <%s> (simple content)", n.Children[0].Name, n.Name)
		}
		v.validateSimpleContentText(n, ct)
		return
	}

	content := v.schema.allContent(ct)
	if len(content) > 0 {
		i, ok := v.matchSeq(n, content, 0)
		if !ok || i < len(n.Children) {
			for ; i < len(n.Children); i++ {
				v.errf(n.Children[i].Line, "unexpected element <%s> in <%s>", n.Children[i].Name, n.Name)
			}
		}
	} else {
		for _, c := range n.Children {
			v.errf(c.Line, "unexpected element <%s> in <%s>", c.Name, n.Name)
		}
	}
	if !ct.mixed && strings.TrimSpace(n.Text) != "" {
		v.errf(n.Line, "character data is not allowed in <%s>", n.Name)
	}
}

// ---------------------------------------------------------------------------
// Content-model matching (xsd:sequence / choice / group)
// ---------------------------------------------------------------------------

func (v *validator) matchSeq(owner *node, parts []particle, i int) (int, bool) {
	for _, p := range parts {
		ni, ok := v.matchParticle(owner, p, i)
		if !ok {
			if ni < len(owner.Children) {
				v.errf(owner.Children[ni].Line, "element <%s> is out of order in <%s>: expected <%s>",
					owner.Children[ni].Name, owner.Name, v.expected(p))
			} else {
				v.errf(owner.Line, "missing required element <%s> in <%s>", v.expected(p), owner.Name)
			}
			return ni, false
		}
		i = ni
	}
	return i, true
}

func (v *validator) matchParticle(owner *node, p particle, i int) (int, bool) {
	switch p.kind {
	case kElem:
		count := 0
		for i < len(owner.Children) && (p.max < 0 || count < p.max) && owner.Children[i].Name == p.name {
			v.validateElement(owner.Children[i], p)
			i++
			count++
		}
		return i, count >= p.min

	case kSeq:
		count := 0
		for i < len(owner.Children) && (p.max < 0 || count < p.max) {
			ni, ok := v.matchSeq(owner, p.subs, i)
			if !ok || ni == i {
				break
			}
			i = ni
			count++
		}
		return i, count >= p.min

	case kGroup:
		g, ok := v.schema.groups[p.ref]
		if !ok {
			// Unknown group (e.g. rich-text helper groups): accept remaining
			// children so that XHTML content is not rejected.
			return len(owner.Children), true
		}
		count := 0
		for i < len(owner.Children) && (p.max < 0 || count < p.max) {
			ni, ok := v.matchSeq(owner, g, i)
			if !ok || ni == i {
				break
			}
			i = ni
			count++
		}
		return i, count >= p.min

	case kChoice:
		count := 0
		for i < len(owner.Children) && (p.max < 0 || count < p.max) {
			chosen := i
			var committed []string
			matched := false
			for _, alt := range p.subs {
				buf := []string{}
				trial := &validator{schema: v.schema, errs: &buf}
				ni, ok := trial.matchParticle(owner, alt, i)
				if ok && ni > i {
					chosen, committed, matched = ni, buf, true
					break
				}
			}
			if !matched {
				break
			}
			*v.errs = append(*v.errs, committed...)
			i = chosen
			count++
		}
		return i, count >= p.min
	}
	return i, false
}

func (v *validator) names(p particle) []string {
	switch p.kind {
	case kElem:
		return []string{p.name}
	case kSeq:
		var out []string
		for _, s := range p.subs {
			out = append(out, v.names(s)...)
		}
		return out
	case kGroup:
		var out []string
		for _, s := range v.schema.groups[p.ref] {
			out = append(out, v.names(s)...)
		}
		if len(out) == 0 {
			return []string{p.ref}
		}
		return out
	case kChoice:
		var out []string
		for _, s := range p.subs {
			out = append(out, v.names(s)...)
		}
		return out
	}
	return nil
}

func (v *validator) expected(p particle) string {
	names := v.names(p)
	if len(names) == 0 {
		return p.ref
	}
	return strings.Join(names, " or ")
}

func (v *validator) validateElement(n *node, p particle) {
	if p.inline != nil {
		if len(n.Children) > 0 {
			v.errf(n.Children[0].Line, "unexpected element <%s> in <%s>", n.Children[0].Name, n.Name)
			return
		}
		v.checkFacets(n.Line, "<"+n.Name+">", p.inline, strings.TrimSpace(n.Text))
		return
	}
	t := p.typ
	// xsi:type selects the concrete type for an element declared with an
	// abstract type (e.g. <PARAM xsi:type="CODED-CONST">) and, in 2.0.1, may
	// also select a subtype of a concrete type. It must name a known type that
	// is the declared type or derives from it.
	if xt, ok := n.NSAttrs["type"]; ok {
		xt = stripPrefix(xt)
		if !v.schema.knownType(xt) {
			v.errf(n.Line, "unknown xsi:type %q on <%s>", xt, n.Name)
			return
		}
		if p.typ != "" && xt != p.typ && !v.schema.derives(xt, p.typ) {
			v.errf(n.Line, "xsi:type %q on <%s> is not a subtype of %q", xt, n.Name, p.typ)
			return
		}
		t = xt
	}
	if t == "" {
		return // opaque (xhtml global element reference)
	}
	if st, ok := v.schema.simple[t]; ok {
		v.validateSimpleValue(n, st)
		return
	}
	if ct, ok := v.schema.complex[t]; ok {
		v.validateComplex(n, ct)
		return
	}
	if strings.HasPrefix(t, "xsd:") {
		v.validatePrimitive(n, t)
	}
}

// ---------------------------------------------------------------------------
// Simple content / facets
// ---------------------------------------------------------------------------

func (v *validator) validateSimpleValue(n *node, st *stDef) {
	if len(n.Children) > 0 {
		v.errf(n.Children[0].Line, "unexpected element <%s> in <%s>", n.Children[0].Name, n.Name)
		return
	}
	v.applySimpleChain(n, st)
}

func (v *validator) applySimpleChain(n *node, st *stDef) {
	if st == nil {
		return
	}
	val := strings.TrimSpace(n.Text)
	v.checkFacets(n.Line, "<"+n.Name+">", st, val)
	if st.base == "" {
		return
	}
	if bs, ok := v.schema.simple[st.base]; ok {
		v.applySimpleChain(n, bs)
	} else if strings.HasPrefix(st.base, "xsd:") {
		v.checkPrimitive(n.Line, "<"+n.Name+">", st.base, val)
	}
}

func (v *validator) validateSimpleContentText(n *node, ct *ctDef) {
	val := strings.TrimSpace(n.Text)
	if ct.simpleFacets != nil {
		v.checkFacets(n.Line, "<"+n.Name+">", ct.simpleFacets, val)
	}
	if ct.base == "" {
		return
	}
	if bs, ok := v.schema.simple[ct.base]; ok {
		v.applySimpleChain(n, bs)
	} else if strings.HasPrefix(ct.base, "xsd:") {
		v.checkPrimitive(n.Line, "<"+n.Name+">", ct.base, val)
	} else if bct, ok := v.schema.complex[ct.base]; ok {
		v.validateSimpleContentText(n, bct)
	}
}

func (v *validator) validateAttrValue(n *node, name string, a attrDef, val string) {
	subject := "attribute " + name + " on <" + n.Name + ">"
	if a.inline != nil {
		v.checkFacets(n.Line, subject, a.inline, val)
		return
	}
	if a.typ == "" {
		return
	}
	if st, ok := v.schema.simple[a.typ]; ok {
		v.checkFacets(n.Line, subject, st, val)
		return
	}
	if strings.HasPrefix(a.typ, "xsd:") {
		v.checkPrimitive(n.Line, subject, a.typ, val)
	}
}

func (v *validator) checkFacets(line int, subject string, st *stDef, val string) {
	if len(st.enums) > 0 && !containsName(st.enums, val) {
		v.errf(line, "invalid value %q for %s (allowed: %s)", val, subject, strings.Join(st.enums, ", "))
	}
	for _, pat := range st.patterns {
		re, err := regexp.Compile("^(?:" + pat + ")$")
		if err == nil && !re.MatchString(val) {
			v.errf(line, "invalid value %q for %s (does not match pattern %s)", val, subject, pat)
		}
	}
	if st.minIncl != nil || st.maxIncl != nil || st.minExcl != nil || st.maxExcl != nil {
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			v.errf(line, "invalid numeric value %q for %s", val, subject)
		} else {
			if st.minIncl != nil && f < *st.minIncl {
				v.errf(line, "value %q for %s must be >= %v", val, subject, *st.minIncl)
			}
			if st.maxIncl != nil && f > *st.maxIncl {
				v.errf(line, "value %q for %s must be <= %v", val, subject, *st.maxIncl)
			}
			if st.minExcl != nil && f <= *st.minExcl {
				v.errf(line, "value %q for %s must be > %v", val, subject, *st.minExcl)
			}
			if st.maxExcl != nil && f >= *st.maxExcl {
				v.errf(line, "value %q for %s must be < %v", val, subject, *st.maxExcl)
			}
		}
	}
	c := utf8.RuneCountInString(val)
	if st.length != nil && c != *st.length {
		v.errf(line, "value %q for %s must be %d characters long", val, subject, *st.length)
	}
	if st.minLength != nil && c < *st.minLength {
		v.errf(line, "value %q for %s must be at least %d characters", val, subject, *st.minLength)
	}
	if st.maxLength != nil && c > *st.maxLength {
		v.errf(line, "value %q for %s must be at most %d characters", val, subject, *st.maxLength)
	}
}

func (v *validator) validatePrimitive(n *node, typ string) {
	if len(n.Children) > 0 {
		v.errf(n.Children[0].Line, "unexpected element <%s> in <%s>", n.Children[0].Name, n.Name)
		return
	}
	v.checkPrimitive(n.Line, "<"+n.Name+">", typ, strings.TrimSpace(n.Text))
}

func (v *validator) checkPrimitive(line int, subject, typ, val string) {
	badInt := func() {
		v.errf(line, "invalid value %q for %s (expected %s)", val, subject, typ)
	}
	switch typ {
	case "xsd:unsignedInt", "xsd:nonNegativeInteger":
		if _, err := strconv.ParseUint(val, 10, 32); err != nil {
			badInt()
		}
	case "xsd:unsignedByte":
		if _, err := strconv.ParseUint(val, 10, 8); err != nil {
			badInt()
		}
	case "xsd:int", "xsd:integer":
		if _, err := strconv.ParseInt(val, 10, 64); err != nil {
			badInt()
		}
	case "xsd:double", "xsd:float":
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			badInt()
		}
	case "xsd:boolean":
		if val != "true" && val != "false" && val != "1" && val != "0" {
			v.errf(line, "invalid value %q for %s (expected true/false)", val, subject)
		}
	case "xsd:hexBinary":
		if !isHex(val) {
			v.errf(line, "invalid value %q for %s (expected hexBinary)", val, subject)
		}
	}
}

func isHex(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
