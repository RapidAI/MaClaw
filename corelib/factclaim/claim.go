// Package factclaim is the shared model for verified knowledge updates.
// A claim is a (subject, predicate, value) assertion. Reachability is one
// predicate; distilled triples such as 是/使用/uses are others. Stores
// supersede a record only when subject and predicate match and the value
// changed.
package factclaim

import (
	"net"
	"regexp"
	"strings"
	"unicode"
)

const (
	PredicateReachability = "reachability"
	maxExtractSentences   = 4
	maxClaimRunes         = 200
)

// Polarity is reachability polarity.
type Polarity int

const (
	PolarityUnknown Polarity = iota
	PolarityReachable
	PolarityUnreachable
)

// Claim is one verified or stored assertion.
type Claim struct {
	Subject   string
	Predicate string
	Value     string
	Text      string
	Aliases   []string
}

// Extract pulls reachability and subject-predicate-object claims from text.
func Extract(text string) []Claim {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var out []Claim
	seen := map[string]struct{}{}
	add := func(c Claim) {
		c.Subject = strings.TrimSpace(c.Subject)
		c.Predicate = normalizePredicate(c.Predicate)
		c.Value = strings.TrimSpace(c.Value)
		c.Text = strings.TrimSpace(c.Text)
		if c.Subject == "" || c.Predicate == "" || c.Value == "" {
			return
		}
		key := strings.ToLower(c.Predicate + "\x00" + c.Subject + "\x00" + c.Value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}

	entities := Entities(text)
	if pol := DetectPolarity(text); pol != PolarityUnknown && len(entities) > 0 {
		add(Claim{
			Subject:   entities[0],
			Predicate: PredicateReachability,
			Value:     polarityValue(pol),
			Text:      text,
		})
	}
	for _, sent := range splitClaimSentences(text) {
		if sub, pred, obj, ok := parseSPO(sent); ok {
			sentEntities := Entities(sent)
			aliases := append([]string(nil), sentEntities...)
			subject := sub
			if len(sentEntities) > 0 && !strings.Contains(strings.ToLower(sub), stripEntityPrefix(sentEntities[0])) {
				aliases = appendUnique(aliases, "spo:"+normalizeSubject(sub))
				if looksLikeEntityKey(sentEntities[0]) {
					subject = sentEntities[0]
				}
			} else {
				subject = "spo:" + normalizeSubject(sub)
			}
			add(Claim{
				Subject:   subject,
				Predicate: pred,
				Value:     obj,
				Text:      sent,
				Aliases:   aliases,
			})
		}
	}
	return out
}

// Contradict reports whether newer should replace stored: same predicate,
// overlapping subjects, different value.
func Contradict(newer, stored Claim) bool {
	if normalizePredicate(newer.Predicate) != normalizePredicate(stored.Predicate) {
		return false
	}
	if !SubjectsOverlap(newer, stored) {
		return false
	}
	nv := normalizeValue(newer.Value)
	sv := normalizeValue(stored.Value)
	if nv == "" || sv == "" || nv == sv {
		return false
	}
	if normalizePredicate(newer.Predicate) == PredicateReachability {
		return nv != sv
	}
	return true
}

// Equivalent reports same predicate, overlapping subjects, and same value.
func Equivalent(a, b Claim) bool {
	if normalizePredicate(a.Predicate) != normalizePredicate(b.Predicate) {
		return false
	}
	if !SubjectsOverlap(a, b) {
		return false
	}
	return normalizeValue(a.Value) == normalizeValue(b.Value) && normalizeValue(a.Value) != ""
}

// ContradictsText reports whether newer contradicts any claim extracted from storedText.
func ContradictsText(newer Claim, storedText string) bool {
	return ContradictsAny([]Claim{newer}, storedText)
}

// ContradictsAny reports whether any newer claim contradicts storedText.
func ContradictsAny(newer []Claim, storedText string) bool {
	return ContradictsStored(newer, Extract(storedText))
}

// ContradictsStored reports whether any newer claim contradicts a stored claim.
func ContradictsStored(newer, stored []Claim) bool {
	for _, n := range newer {
		for _, s := range stored {
			if Contradict(n, s) {
				return true
			}
		}
	}
	return false
}

// Isolated reports that every stored claim is the same (subject, predicate)
// as some newer claim. The record can be rewritten as a whole without
// destroying unrelated knowledge.
func Isolated(newer, stored []Claim) bool {
	if len(stored) == 0 {
		return true
	}
	for _, s := range stored {
		related := false
		for _, n := range newer {
			if Contradict(n, s) || Equivalent(n, s) {
				related = true
				break
			}
		}
		if !related {
			return false
		}
	}
	return true
}

// SubjectsOverlap reports shared identity (entity keys, aliases, or SPO subject).
func SubjectsOverlap(a, b Claim) bool {
	aset := subjectSet(a)
	for s := range subjectSet(b) {
		if _, ok := aset[s]; ok {
			return true
		}
	}
	return false
}

func subjectSet(c Claim) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return
		}
		out[s] = struct{}{}
		if stripped := stripEntityPrefix(s); stripped != s {
			out[stripped] = struct{}{}
		}
		if strings.HasPrefix(s, "spo:") {
			out[strings.TrimPrefix(s, "spo:")] = struct{}{}
		}
	}
	add(c.Subject)
	for _, a := range c.Aliases {
		add(a)
	}
	return out
}

func normalizePredicate(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return PredicateReachability
	}
	return p
}

func normalizeValue(v string) string {
	return strings.ToLower(strings.TrimSpace(strings.Trim(v, "。.;；,，")))
}

func normalizeSubject(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func polarityValue(p Polarity) string {
	switch p {
	case PolarityReachable:
		return "reachable"
	case PolarityUnreachable:
		return "unreachable"
	default:
		return ""
	}
}

func looksLikeEntityKey(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "ip:") || strings.HasPrefix(s, "host:")
}

func stripEntityPrefix(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "ip:"):
		return strings.TrimSpace(s[3:])
	case strings.HasPrefix(lower, "host:"):
		return strings.ToLower(strings.TrimSpace(s[5:]))
	case strings.HasPrefix(lower, "spo:"):
		return strings.TrimSpace(s[4:])
	default:
		return s
	}
}

func appendUnique(dst []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return dst
	}
	for _, e := range dst {
		if strings.EqualFold(e, v) {
			return dst
		}
	}
	return append(dst, v)
}

func splitClaimSentences(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '\n' || r == ';' || r == '；'
	})
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if n := len([]rune(f)); n < 4 || n > maxClaimRunes {
			continue
		}
		out = append(out, f)
		if len(out) >= maxExtractSentences {
			break
		}
	}
	return out
}

var ipv4Pattern = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)

// Entities returns canonical ip: and host: keys found in text.
func Entities(text string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(entity string) {
		entity = strings.TrimSpace(entity)
		if entity == "" {
			return
		}
		if _, ok := seen[entity]; ok {
			return
		}
		seen[entity] = struct{}{}
		out = append(out, entity)
	}
	for _, ip := range ipv4Pattern.FindAllString(text, 8) {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			add("ip:" + parsed.To4().String())
		}
	}
	for _, tok := range strings.Fields(text) {
		if len(out) >= 8 {
			break
		}
		if ent := CanonicalEntity(tok); ent != "" {
			add(ent)
		}
	}
	return out
}

// CanonicalEntity maps a network identity token to ip:… or host:….
// user@host and host:port are the same identity as host.
func CanonicalEntity(tok string) string {
	tok = canonicalizeIdentity(tok)
	if tok == "" {
		return ""
	}
	if ip := net.ParseIP(tok); ip != nil && ip.To4() != nil {
		return "ip:" + ip.To4().String()
	}
	if looksLikeHostname(tok) {
		return "host:" + strings.ToLower(tok)
	}
	return ""
}

func canonicalizeIdentity(tok string) string {
	tok = strings.TrimSpace(tok)
	tok = strings.Trim(tok, ".,;()[]\"'`{}")
	if at := strings.LastIndex(tok, "@"); at >= 0 && at+1 < len(tok) {
		tok = tok[at+1:]
	}
	if i := strings.LastIndex(tok, ":"); i > 0 {
		port := tok[i+1:]
		if port != "" && isAllASCIIDigits(port) {
			tok = tok[:i]
		}
	}
	return tok
}

func isAllASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func looksLikeHostname(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " /\\") {
		return false
	}
	if !strings.Contains(s, ".") {
		return false
	}
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			continue
		}
		if unicode.IsDigit(r) || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return hasLetter
}
