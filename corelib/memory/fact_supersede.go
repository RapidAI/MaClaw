package memory

import (
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/factclaim"
)

const maxVerifiedFactClaimRunes = 500

var memoryFactIPv4Pattern = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)

var memoryFactUnreachableMarkers = []string{
	"unreachable", "timed out", "no route to host",
	"network is unreachable", "host is down", "connection refused",
	"100% packet loss", "100% loss", "不通", "无法连接", "连不上",
	"连接失败", "连接超时", "主机不可达", "不可达", "当前不可达",
	"不能通", "不可以通",
	"not connected to", "not connected", "not reachable", "cannot connect", "can't connect",
	"failed to connect", "无法连通",
}

var memoryFactReachableMarkers = []string{
	"0% packet loss", "0% loss", "bytes from", "reply from", "is alive",
	" is reachable", "currently reachable", "当前可达", "已连通", "能通", "可以通",
}

type memoryFactPolarity int

const (
	memoryFactPolarityUnknown memoryFactPolarity = iota
	memoryFactPolarityReachable
	memoryFactPolarityUnreachable
)

// VerifiedFact is a tool-verified claim that should replace contradicting
// warehouse entries for the same IP or host.
type VerifiedFact struct {
	Entity      string
	Predicate   string
	Claim       string
	Evidence    string
	Aliases     []string
	StrictOwner bool
}

// VerifiedFactSyncResult reports warehouse mutations for a verified claim.
type VerifiedFactSyncResult struct {
	Superseded int
	Saved      bool
}

// ApplyVerifiedFact supersedes active warehouse entries that assert the same
// (subject, predicate) with a different value, then stores the verified claim
// if it is not already present. It does not insert a fact that was never in
// the warehouse.
func (s *Store) ApplyVerifiedFact(fact VerifiedFact, ownerID string) VerifiedFactSyncResult {
	var out VerifiedFactSyncResult
	if s == nil {
		return out
	}
	claim := strings.TrimSpace(fact.Claim)
	if claim == "" || utf8.RuneCountInString(claim) > maxVerifiedFactClaimRunes {
		return out
	}
	newer := claimsFromVerifiedFact(fact)
	if len(newer) == 0 {
		return out
	}
	needles := needlesFromClaims(newer)
	now := time.Now()
	s.mu.RLock()
	var contradicting []string
	hasSame := false
	for _, entry := range s.entries {
		if !entry.IsActive() {
			continue
		}
		if !memoryEntryWritableBy(entry, ownerID, fact.StrictOwner) {
			continue
		}
		stored := factclaim.Extract(entry.Content)
		if contradictsAny(newer, stored) && factclaim.Isolated(newer, stored) {
			contradicting = append(contradicting, entry.ID)
			continue
		}
		if sameClaimValue(newer, stored) {
			hasSame = true
		}
	}
	s.mu.RUnlock()
	for _, id := range contradicting {
		ok, err := s.SupersedeEntryByID(id, now)
		if err == nil && ok {
			out.Superseded++
		}
	}
	if out.Superseded == 0 {
		return out
	}
	if !hasSame {
		content := claim
		if ev := strings.TrimSpace(fact.Evidence); ev != "" {
			content = claim + "\n（已核实: " + ev + "）"
		}
		entry := Entry{
			Content:    content,
			Category:   CategoryProjectKnowledge,
			Tags:       append([]string(nil), needles...),
			OwnerID:    ownerID,
			Title:      deriveToolMemoryTitle(content),
			SourceType: "verified_tool",
			Status:     StatusActive,
		}
		if err := s.Save(entry); err == nil {
			out.Saved = true
		}
	}
	s.InvalidateRecallCaches(ownerID)
	if !fact.StrictOwner && strings.TrimSpace(ownerID) != "" {
		s.InvalidateRecallCaches("")
	}
	return out
}

// SupersedeContradictingFacts marks active warehouse entries that share an
// IP/host entity with newContent but state the opposite reachability as
// superseded. The newly saved entry is left active. Returns how many entries
// were superseded.
func (s *Store) SupersedeContradictingFacts(newContent, ownerID string) int {
	if s == nil {
		return 0
	}
	newContent = strings.TrimSpace(newContent)
	if newContent == "" {
		return 0
	}
	newPolarity := detectMemoryFactPolarity(newContent)
	if newPolarity == memoryFactPolarityUnknown {
		return 0
	}
	entities := memoryFactEntities(newContent)
	if len(entities) == 0 {
		return 0
	}
	now := time.Now()
	var superseded int
	s.mu.RLock()
	var targets []string
	for _, entry := range s.entries {
		if !entry.IsActive() {
			continue
		}
		if !memoryEntryWritableBy(entry, ownerID, ownerID != "") {
			continue
		}
		polarity := detectMemoryFactPolarity(entry.Content)
		if polarity == memoryFactPolarityUnknown || polarity == newPolarity {
			continue
		}
		if !memoryFactSharesEntity(entry.Content, entities) {
			continue
		}
		targets = append(targets, entry.ID)
	}
	s.mu.RUnlock()
	for _, id := range targets {
		ok, err := s.SupersedeEntryByID(id, now)
		if err == nil && ok {
			superseded++
		}
	}
	return superseded
}

func claimsFromVerifiedFact(fact VerifiedFact) []factclaim.Claim {
	var out []factclaim.Claim
	out = append(out, factclaim.Extract(fact.Claim)...)
	if strings.TrimSpace(fact.Entity) == "" {
		return out
	}
	c := factclaim.Claim{
		Subject:   strings.TrimSpace(fact.Entity),
		Predicate: strings.TrimSpace(fact.Predicate),
		Text:      strings.TrimSpace(fact.Claim),
		Aliases:   append([]string(nil), fact.Aliases...),
	}
	if c.Predicate == "" {
		if pol := factclaim.DetectPolarity(fact.Claim + "\n" + fact.Evidence); pol != factclaim.PolarityUnknown {
			c.Predicate = factclaim.PredicateReachability
			c.Value = polarityValue(pol)
		} else if extracted := factclaim.Extract(fact.Claim); len(extracted) > 0 {
			return out
		} else {
			c.Value = fact.Claim
		}
	}
	if c.Value == "" {
		c.Value = fact.Claim
	}
	out = append([]factclaim.Claim{c}, out...)
	return out
}

func polarityValue(p factclaim.Polarity) string {
	switch p {
	case factclaim.PolarityReachable:
		return "reachable"
	case factclaim.PolarityUnreachable:
		return "unreachable"
	default:
		return ""
	}
}

func contradictsAny(newer, stored []factclaim.Claim) bool {
	for _, n := range newer {
		for _, s := range stored {
			if factclaim.Contradict(n, s) {
				return true
			}
		}
	}
	return false
}

func sameClaimValue(newer, stored []factclaim.Claim) bool {
	for _, n := range newer {
		for _, s := range stored {
			if factclaim.Equivalent(n, s) {
				return true
			}
		}
	}
	return false
}

func needlesFromClaims(claims []factclaim.Claim) []string {
	var out []string
	for _, c := range claims {
		out = appendUniqueFold(out, strings.TrimPrefix(strings.ToLower(c.Subject), "ip:"))
		out = appendUniqueFold(out, strings.TrimPrefix(strings.ToLower(c.Subject), "host:"))
		for _, a := range c.Aliases {
			out = appendUniqueFold(out, strings.TrimPrefix(strings.ToLower(a), "ip:"))
			out = appendUniqueFold(out, strings.TrimPrefix(strings.ToLower(a), "host:"))
		}
	}
	return out
}

func detectMemoryFactPolarity(text string) memoryFactPolarity {
	lower := strings.ToLower(text)
	_, unreachEnd := lastMarkerMatch(lower, memoryFactUnreachableMarkers)
	_, reachEnd := lastMarkerMatch(lower, memoryFactReachableMarkers)
	switch {
	case unreachEnd >= 0 && reachEnd >= 0:
		if unreachEnd >= reachEnd {
			return memoryFactPolarityUnreachable
		}
		return memoryFactPolarityReachable
	case unreachEnd >= 0:
		return memoryFactPolarityUnreachable
	case reachEnd >= 0:
		return memoryFactPolarityReachable
	default:
		return memoryFactPolarityUnknown
	}
}

func memoryEntryWritableBy(entry Entry, ownerID string, strict bool) bool {
	if strict {
		return ownerID != "" && entry.OwnerID == ownerID
	}
	if ownerID == "" {
		return entry.OwnerID == ""
	}
	return entry.OwnerID == ownerID || entry.OwnerID == ""
}

func memoryFactEntities(text string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, ip := range memoryFactIPv4Pattern.FindAllString(text, 8) {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			continue
		}
		key := parsed.To4().String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func memoryFactSharesEntity(content string, entities []string) bool {
	if len(entities) == 0 {
		return false
	}
	found := memoryFactEntities(content)
	if len(found) == 0 {
		lower := strings.ToLower(content)
		for _, entity := range entities {
			if containsStandaloneIP(lower, entity) {
				return true
			}
		}
		return false
	}
	set := make(map[string]struct{}, len(found))
	for _, ip := range found {
		set[ip] = struct{}{}
	}
	for _, entity := range entities {
		if _, ok := set[entity]; ok {
			return true
		}
	}
	return false
}

func containsStandaloneIP(text, ip string) bool {
	if ip == "" {
		return false
	}
	start := 0
	for {
		i := strings.Index(text[start:], ip)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isIPChar(text[i-1])
		after := i + len(ip)
		afterOK := after >= len(text) || !isIPChar(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
}

func isIPChar(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.'
}

func verifiedFactNeedles(entity, content string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(needle string) {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle == "" {
			return
		}
		if _, ok := seen[needle]; ok {
			return
		}
		seen[needle] = struct{}{}
		out = append(out, needle)
	}
	entity = strings.TrimSpace(entity)
	lowerEntity := strings.ToLower(entity)
	switch {
	case strings.HasPrefix(lowerEntity, "ip:"):
		add(strings.TrimSpace(entity[3:]))
	case strings.HasPrefix(lowerEntity, "host:"):
		add(strings.TrimSpace(entity[5:]))
	default:
		add(entity)
	}
	for _, ip := range memoryFactEntities(content) {
		add(ip)
	}
	return out
}

func appendUniqueFold(dst []string, n string) []string {
	n = strings.ToLower(strings.TrimSpace(n))
	if n == "" {
		return dst
	}
	for _, e := range dst {
		if e == n {
			return dst
		}
	}
	return append(dst, n)
}

func memoryEntryMatchesNeedles(content string, needles []string) bool {
	if strings.TrimSpace(content) == "" || len(needles) == 0 {
		return false
	}
	ips := memoryFactEntities(content)
	ipSet := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		ipSet[ip] = struct{}{}
	}
	lower := strings.ToLower(content)
	for _, needle := range needles {
		if _, ok := ipSet[needle]; ok {
			return true
		}
		if parsed := net.ParseIP(needle); parsed != nil && parsed.To4() != nil {
			continue
		}
		if hostnameBoundaryContains(lower, needle) {
			return true
		}
	}
	return false
}

func hostnameBoundaryContains(lower, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(lower[start:], host)
		if idx < 0 {
			return false
		}
		idx += start
		if hostBodyBoundary(lower, idx, len(host)) {
			return true
		}
		start = idx + 1
	}
}

func hostBodyBoundary(lower string, idx, nlen int) bool {
	if idx > 0 && isHostBodyByte(lower[idx-1]) {
		return false
	}
	end := idx + nlen
	if end < len(lower) && isHostBodyByte(lower[end]) {
		return false
	}
	return true
}

func isHostBodyByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

func lastMarkerMatch(lower string, markers []string) (start, end int) {
	start, end = -1, -1
	bestLen := 0
	for _, marker := range markers {
		if marker == "" {
			continue
		}
		idx := strings.LastIndex(lower, marker)
		if idx < 0 {
			continue
		}
		markerEnd := idx + len(marker)
		if markerEnd > end || (markerEnd == end && len(marker) > bestLen) {
			start = idx
			end = markerEnd
			bestLen = len(marker)
		}
	}
	return start, end
}
