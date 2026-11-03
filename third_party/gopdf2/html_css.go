package gopdf

import (
	"strings"
)

// ============================================================
// Minimal CSS engine for HTML-to-PDF rendering.
//
// Supports: <style> blocks, tag / .class / #id / tag.class selectors,
// comma-separated selector lists, and the inline style attribute.
// Specificity: inline > #id > .class > tag; later rules win within a tier.
// ============================================================

// cssRule is a single stylesheet rule.
type cssRule struct {
	selector cssSelector
	props    map[string]string
}

// cssSelector is a simple selector (no combinators): tag, .class, #id, tag.class.
type cssSelector struct {
	tag   string // "" = any tag
	class string // "" = none
	id    string // "" = none
}

// specificity ranks rules: 3=id, 2=class, 1=tag.
func (s cssSelector) specificity() int {
	if s.id != "" {
		return 3
	}
	if s.class != "" {
		return 2
	}
	return 1
}

// matches reports whether the selector matches an element node.
func (s cssSelector) matches(node *htmlNode) bool {
	if node == nil || node.Type != htmlNodeElement {
		return false
	}
	if s.tag != "" && node.Tag != s.tag {
		return false
	}
	if s.id != "" && node.Attrs["id"] != s.id {
		return false
	}
	if s.class != "" && !htmlHasClass(node, s.class) {
		return false
	}
	return true
}

func htmlHasClass(node *htmlNode, class string) bool {
	v, ok := node.Attrs["class"]
	if !ok {
		return false
	}
	for _, f := range strings.Fields(v) {
		if f == class {
			return true
		}
	}
	return false
}

// extractStyleBlocks collects the text content of <style> elements in a tree.
func extractStyleBlocks(nodes []*htmlNode) string {
	var sb strings.Builder
	var walk func([]*htmlNode)
	walk = func(ns []*htmlNode) {
		for _, n := range ns {
			if n.Type == htmlNodeElement {
				if n.Tag == "style" {
					for _, c := range n.Children {
						if c.Type == htmlNodeText {
							sb.WriteString(c.Text)
							sb.WriteByte('\n')
						}
					}
				}
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return sb.String()
}

// parseStylesheet parses CSS text into rules. Comments are stripped.
// Media queries and at-rules are skipped.
func parseStylesheet(css string) []cssRule {
	css = stripCSSComments(css)
	var rules []cssRule
	i := 0
	n := len(css)
	for i < n {
		// skip whitespace
		for i < n && (css[i] == ' ' || css[i] == '\n' || css[i] == '\r' || css[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		// at-rule: skip to ';' or matching '{...}'
		if css[i] == '@' {
			for i < n && css[i] != ';' && css[i] != '{' {
				i++
			}
			if i < n && css[i] == '{' {
				depth := 1
				i++
				for i < n && depth > 0 {
					if css[i] == '{' {
						depth++
					} else if css[i] == '}' {
						depth--
					}
					i++
				}
			}
			continue
		}
		// selector list up to '{'
		selStart := i
		for i < n && css[i] != '{' && css[i] != '}' {
			i++
		}
		if i >= n || css[i] == '}' {
			i++
			continue
		}
		selText := strings.TrimSpace(css[selStart:i])
		i++ // consume '{'
		bodyStart := i
		for i < n && css[i] != '}' {
			i++
		}
		body := ""
		if i <= n {
			body = css[bodyStart:min(i, n)]
		}
		if i < n {
			i++ // consume '}'
		}
		if selText == "" || body == "" {
			continue
		}
		props := parseInlineStyle(body)
		if len(props) == 0 {
			continue
		}
		for _, sel := range strings.Split(selText, ",") {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}
			// keep only the last simple selector of the compound (descendant
			// combinator support is out of scope; "div .cls" → ".cls")
			parts := strings.Fields(sel)
			last := parts[len(parts)-1]
			if cs, ok := parseSimpleSelector(last); ok {
				rules = append(rules, cssRule{selector: cs, props: props})
			}
		}
	}
	return rules
}

func stripCSSComments(css string) string {
	var sb strings.Builder
	for {
		idx := strings.Index(css, "/*")
		if idx < 0 {
			sb.WriteString(css)
			break
		}
		sb.WriteString(css[:idx])
		rest := css[idx+2:]
		end := strings.Index(rest, "*/")
		if end < 0 {
			break
		}
		css = rest[end+2:]
	}
	return sb.String()
}

// parseSimpleSelector parses "tag", ".class", "#id", "tag.class", "tag#id".
func parseSimpleSelector(s string) (cssSelector, bool) {
	var cs cssSelector
	if s == "" || s == "*" {
		return cs, true
	}
	// universal selector may combine with classes/ids: *.cls == .cls
	s = strings.TrimPrefix(s, "*")

	for len(s) > 0 {
		switch s[0] {
		case '.':
			s = s[1:]
			end := strings.IndexAny(s, ".#")
			var part string
			if end < 0 {
				part, s = s, ""
			} else {
				part, s = s[:end], s[end:]
			}
			if part == "" {
				return cs, false
			}
			cs.class = part
		case '#':
			s = s[1:]
			end := strings.IndexAny(s, ".#")
			var part string
			if end < 0 {
				part, s = s, ""
			} else {
				part, s = s[:end], s[end:]
			}
			if part == "" {
				return cs, false
			}
			cs.id = part
		default:
			end := strings.IndexAny(s, ".#")
			var part string
			if end < 0 {
				part, s = s, ""
			} else {
				part, s = s[:end], s[end:]
			}
			cs.tag = strings.ToLower(part)
		}
	}
	return cs, true
}

// htmlStyleResolver resolves computed styles for nodes.
// Rules are pre-bucketed by specificity so each resolve is a single pass
// over the matching tier (id > class > tag).
type htmlStyleResolver struct {
	tagRules   []cssRule
	classRules []cssRule
	idRules    []cssRule
}

// newHTMLStyleResolver builds a resolver from <style> blocks found in the tree.
func newHTMLStyleResolver(nodes []*htmlNode) *htmlStyleResolver {
	r := &htmlStyleResolver{}
	for _, rule := range parseStylesheet(extractStyleBlocks(nodes)) {
		switch rule.selector.specificity() {
		case 3:
			r.idRules = append(r.idRules, rule)
		case 2:
			r.classRules = append(r.classRules, rule)
		default:
			r.tagRules = append(r.tagRules, rule)
		}
	}
	return r
}

// resolve returns the merged style properties for a node:
// matching tag rules (spec 1), class rules (spec 2), id rules (spec 3),
// then the inline style attribute.
func (r *htmlStyleResolver) resolve(node *htmlNode) map[string]string {
	out := make(map[string]string, 8)
	if node == nil || node.Type != htmlNodeElement {
		return out
	}
	apply := func(rules []cssRule) {
		for i := range rules {
			if rules[i].selector.matches(node) {
				for k, v := range rules[i].props {
					out[k] = v
				}
			}
		}
	}
	apply(r.tagRules)
	apply(r.classRules)
	apply(r.idRules)
	if styleStr, ok := node.Attrs["style"]; ok && styleStr != "" {
		for k, v := range parseInlineStyle(styleStr) {
			out[k] = v
		}
	}
	return out
}
