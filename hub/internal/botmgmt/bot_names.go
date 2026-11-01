package botmgmt

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// One rule for bot names, used both to resolve a name a bot or person wrote
// ("bot5", "5号", "小红书") to a bot, and to keep two bots of one user from
// sharing a name.

var (
	// ErrNameTaken: another bot of this user already has this name.
	ErrNameTaken = errors.New("bot name taken")
	// ErrAmbiguousBot: a partial name fits more than one bot equally well.
	ErrAmbiguousBot = errors.New("bot name ambiguous")
)

// BotNameError carries the human message and the candidate names.
type BotNameError struct {
	Kind       error
	Message    string
	Candidates []string
}

func (e *BotNameError) Error() string { return e.Message }
func (e *BotNameError) Unwrap() error { return e.Kind }

var (
	nameNumRe  = regexp.MustCompile(`^(?:no\.?|#|第)?(\d+)(?:号|号机|号机器人|号bot)?$`)
	nameBotNum = regexp.MustCompile(`^(?:bot|机器人)(?:no\.?|#|第)?(\d+)(?:号)?$`)
	nameNumBot = regexp.MustCompile(`^(?:第)?(\d+)号?(?:bot|机器人)$`)
)

// normalizeBotName folds case, full-width forms and spacing, and writes the
// numbered forms "Bot 5", "bot5", "5号bot", "5号机器人", "机器人5" as "bot5".
func normalizeBotName(s string) string {
	s = width.Fold.String(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || (unicode.IsPunct(r) && r != '#') || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	n := b.String()
	for _, re := range []*regexp.Regexp{nameBotNum, nameNumBot} {
		if m := re.FindStringSubmatch(n); m != nil {
			return "bot" + strings.TrimLeft(m[1], "0")
		}
	}
	return n
}

// normalizeBotQuery also reads a bare number ("5", "5号", "#5") as bot5.
func normalizeBotQuery(s string) string {
	n := normalizeBotName(s)
	if m := nameNumRe.FindStringSubmatch(n); m != nil {
		return "bot" + strings.TrimLeft(m[1], "0")
	}
	return n
}

type nameCandidate struct {
	ID   string
	Name string
}

func participantCandidates(rec record, userID string) []nameCandidate {
	userID = strings.TrimSpace(userID)
	var out []nameCandidate
	for _, b := range botsOwnedBy(rec.Bots, userID) {
		out = append(out, nameCandidate{ID: b.ID, Name: strings.TrimSpace(b.Name)})
	}
	for _, b := range rec.LocalBots {
		if b.Owner == userID {
			out = append(out, nameCandidate{ID: b.ID, Name: strings.TrimSpace(b.Name)})
		}
	}
	return out
}

func bigrams(s string) map[string]int {
	r := []rune(s)
	m := map[string]int{}
	if len(r) == 1 {
		m[s]++
	}
	for i := 0; i+1 < len(r); i++ {
		m[string(r[i:i+2])]++
	}
	return m
}

// nameSimilarity is the larger of bigram Dice and 1-edit distance ratio,
// both on runes, so it works for Chinese and English alike.
func nameSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	ga, gb := bigrams(a), bigrams(b)
	inter, total := 0, 0
	for k, v := range ga {
		total += v
		if w := gb[k]; w > 0 {
			inter += min(v, w)
		}
	}
	for _, v := range gb {
		total += v
	}
	dice := 0.0
	if total > 0 {
		dice = 2 * float64(inter) / float64(total)
	}
	ra, rb := []rune(a), []rune(b)
	d := levenshtein(ra, rb)
	l := max(len(ra), len(rb))
	lev := 1 - float64(d)/float64(l)
	return max(dice, lev)
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

const (
	nameSimMin    = 0.6  // weaker than this is no match
	nameSimMargin = 0.08 // best must beat the runner-up by this much
)

// matchBotName finds the one candidate a written name means.
// Order: exact id, exact name, normalized name, prefix, substring (either
// way), then similarity. Each stage that finds exactly one bot wins; one that
// finds several equally good bots is a tie and returns ErrAmbiguousBot with
// the names, never a guess. No match returns ErrNoSuchBot with the names.
func matchBotName(cands []nameCandidate, target string) (string, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return "", nil
	}
	for _, c := range cands {
		if c.ID == t {
			return c.ID, nil
		}
	}
	if id, err, ok := pickOne(cands, t, func(c nameCandidate) bool { return strings.EqualFold(c.Name, t) }); ok {
		return id, err
	}
	q := normalizeBotQuery(t)
	if q == "" {
		return "", noSuchBot(cands, t)
	}
	if id, err, ok := pickOne(cands, t, func(c nameCandidate) bool { return normalizeBotName(c.Name) == q }); ok {
		return id, err
	}
	if id, err, ok := pickOne(cands, t, func(c nameCandidate) bool {
		n := normalizeBotName(c.Name)
		return n != "" && (strings.HasPrefix(n, q) || (strings.HasPrefix(q, n) && coversHalf(n, q)))
	}); ok {
		return id, err
	}
	if id, err, ok := pickShortest(cands, t, q); ok {
		return id, err
	}
	type scored struct {
		c nameCandidate
		s float64
	}
	var all []scored
	for _, c := range cands {
		all = append(all, scored{c, nameSimilarity(q, normalizeBotName(c.Name))})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].s > all[j].s })
	if len(all) == 0 || all[0].s < nameSimMin {
		return "", noSuchBot(cands, t)
	}
	if len(all) > 1 && all[0].s-all[1].s < nameSimMargin {
		var tied []nameCandidate
		for _, s := range all {
			if all[0].s-s.s < nameSimMargin {
				tied = append(tied, s.c)
			}
		}
		return "", ambiguous(tied, t)
	}
	return all[0].c.ID, nil
}

// pickOne: the stage's matches (ok=false: none).
func pickOne(cands []nameCandidate, t string, f func(nameCandidate) bool) (string, error, bool) {
	var hit []nameCandidate
	for _, c := range cands {
		if f(c) {
			hit = append(hit, c)
		}
	}
	switch len(hit) {
	case 0:
		return "", nil, false
	case 1:
		return hit[0].ID, nil, true
	}
	return "", ambiguous(hit, t), true
}

// pickShortest: substring either way; the tightest fit (smallest length
// difference) wins, a tie is ambiguous.
func pickShortest(cands []nameCandidate, t, q string) (string, error, bool) {
	best := -1
	var hit []nameCandidate
	for _, c := range cands {
		n := normalizeBotName(c.Name)
		if n == "" || !(strings.Contains(n, q) || (strings.Contains(q, n) && coversHalf(n, q))) {
			continue
		}
		d := len([]rune(n)) - len([]rune(q))
		if d < 0 {
			d = -d
		}
		switch {
		case best < 0 || d < best:
			best, hit = d, []nameCandidate{c}
		case d == best:
			hit = append(hit, c)
		}
	}
	return pickOne(hit, t, func(nameCandidate) bool { return true })
}

// coversHalf: a bot name found inside a longer query counts only when it is
// at least two characters and half the query ("A" is not in "nobody").
func coversHalf(n, q string) bool {
	ln, lq := len([]rune(n)), len([]rune(q))
	return ln >= 2 && 2*ln >= lq
}

func candidateNames(cs []nameCandidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, "「"+c.Name+"」")
	}
	return out
}

func ambiguous(cs []nameCandidate, t string) error {
	names := candidateNames(cs)
	return &BotNameError{Kind: ErrAmbiguousBot, Candidates: names,
		Message: fmt.Sprintf("「%s」能对上好几个 Bot：%s。请问是哪一个？", t, strings.Join(names, "、"))}
}

func noSuchBot(cs []nameCandidate, t string) error {
	names := candidateNames(cs)
	msg := fmt.Sprintf("没有叫「%s」的 Bot。", t)
	if len(names) > 0 {
		msg += "现有的是：" + strings.Join(names, "、") + "。"
	}
	return &BotNameError{Kind: ErrNoSuchBot, Candidates: names, Message: msg}
}

// resolveParticipantErr turns a written name into a Hub or local bot id of
// this user, or says why it cannot (none / several fit).
func resolveParticipantErr(rec record, userID, target string) (string, error) {
	t := strings.TrimSpace(target)
	if t == "" || participantOwned(rec, userID, t) {
		return t, nil
	}
	return matchBotName(participantCandidates(rec, userID), t)
}

// botNameConflict: the name another bot of this user already uses (same
// normalized form), skipping selfID. nil when free.
func botNameConflict(rec record, userID, name, selfID string) error {
	n := normalizeBotName(name)
	if n == "" {
		return nil
	}
	for _, c := range participantCandidates(rec, userID) {
		if c.ID != selfID && normalizeBotName(c.Name) == n {
			return &BotNameError{Kind: ErrNameTaken, Candidates: []string{c.Name},
				Message: fmt.Sprintf("已经有一个叫「%s」的 Bot 了，换个名字吧。", c.Name)}
		}
	}
	return nil
}
