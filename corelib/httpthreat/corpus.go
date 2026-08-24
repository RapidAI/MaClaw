package httpthreat

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type Corpus struct {
	mu      sync.Mutex
	byID    map[string]Sample
	cap     int
	encoder string
}

func NewCorpus(encoderID string, cap int) *Corpus {
	if cap <= 0 {
		cap = DefaultCorpusCap
	}
	return &Corpus{byID: map[string]Sample{}, cap: cap, encoder: strings.TrimSpace(encoderID)}
}

func (c *Corpus) EncoderID() string {
	if c == nil {
		return ""
	}
	return c.encoder
}

func (c *Corpus) Upsert(now time.Time, s Sample) Sample {
	if c == nil {
		return s
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.ID == "" {
		s.ID = SampleID(s.TenantID, s.EncoderID, s.Preview)
	}
	if cur, ok := c.byID[s.ID]; ok {
		if strings.TrimSpace(cur.TenantID) != "" && strings.TrimSpace(s.TenantID) != "" && cur.TenantID != s.TenantID {
			return cur
		}
		cur.LastSeen = now.UTC().Format(time.RFC3339)
		if s.HeadClass != "" {
			cur.HeadClass = s.HeadClass
			cur.HeadMaxP = s.HeadMaxP
		}
		if len(cur.Embedding) == 0 && len(s.Embedding) > 0 && s.EncoderID == cur.EncoderID {
			cur.Embedding = append([]float32(nil), s.Embedding...)
		}
		c.byID[s.ID] = cur
		return cur
	}
	if s.CreatedAt == "" {
		s.CreatedAt = now.UTC().Format(time.RFC3339)
	}
	s.LastSeen = now.UTC().Format(time.RFC3339)
	c.byID[s.ID] = s
	return s
}

func (c *Corpus) Get(id string) (Sample, bool) {
	if c == nil {
		return Sample{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.byID[id]
	return s, ok
}

func (c *Corpus) Label(id, gold, source string, at time.Time) (Sample, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.byID[id]
	if !ok {
		return Sample{}, false
	}
	if s.GoldSource == GoldHuman && source != GoldHuman {
		return s, true
	}
	s.GoldClass = gold
	s.GoldSource = source
	s.LabeledAt = at.UTC().Format(time.RFC3339)
	s.Abstained = false
	s.NeedHuman = false
	c.byID[id] = s
	return s, true
}

func (c *Corpus) Abstain(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.byID[id]
	if !ok {
		return false
	}
	s.Abstained = true
	s.NeedHuman = false
	c.byID[id] = s
	return true
}

func (c *Corpus) Unlabel(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.byID[id]
	if !ok {
		return false
	}
	s.GoldClass = ""
	s.GoldSource = ""
	s.LabeledAt = ""
	s.NeedHuman = true
	c.byID[id] = s
	return true
}

func (c *Corpus) SetAdvice(id, class, reason string, needHuman bool) (Sample, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.byID[id]
	if !ok {
		return Sample{}, false
	}
	s.LLMClass = class
	s.LLMReason = reason
	s.NeedHuman = needHuman
	c.byID[id] = s
	return s, true
}

func (c *Corpus) Load(samples []Sample) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range samples {
		if strings.TrimSpace(s.ID) == "" {
			continue
		}
		c.byID[s.ID] = s
	}
}

func (c *Corpus) ListTenant(tenant string) []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Sample, 0)
	for _, s := range c.byID {
		if s.TenantID == tenant {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	return out
}

func (c *Corpus) ReviewQueue(tenant string) []Sample {
	items := c.ReviewQueueRanked(tenant)
	out := make([]Sample, 0, len(items))
	for _, item := range items {
		out = append(out, item.Sample)
	}
	return out
}

func (c *Corpus) ReviewQueueRanked(tenant string) []QueueItem {
	items := c.ListTenant(tenant)
	need := map[string]bool{}
	haveHuman := map[string]bool{}
	for _, s := range items {
		if s.GoldSource == GoldHuman && IsTrainableClass(s.GoldClass) {
			haveHuman[s.GoldClass] = true
		}
	}
	for _, class := range TrainableClasses {
		if !haveHuman[class] {
			need[class] = true
		}
	}
	hot := hotCells(items)
	var missing, hotDisagree, disagree, hotHits, rest []QueueItem
	for _, s := range items {
		if s.GoldSource == GoldHuman || s.Abstained || !HeadMayScore(s.RuleSource) {
			continue
		}
		item := QueueItem{Sample: s}
		isDisagree := s.HeadClass != "" && s.HeadClass != ClassUnknown && s.HeadClass != s.RuleClass && s.HeadMaxP >= DefaultTau
		isHot := hot[s.RuleClass+"|"+s.HeadClass]
		switch {
		case s.RuleClass != "" && need[s.RuleClass]:
			item.QueueReason = "coverage"
			missing = append(missing, item)
		case isDisagree && isHot:
			item.QueueReason = "hot"
			hotDisagree = append(hotDisagree, item)
		case isDisagree:
			item.QueueReason = "disagree"
			disagree = append(disagree, item)
		case isHot:
			item.QueueReason = "hot"
			hotHits = append(hotHits, item)
		default:
			item.QueueReason = "recent"
			rest = append(rest, item)
		}
	}
	out := append(missing, hotDisagree...)
	out = append(out, disagree...)
	out = append(out, hotHits...)
	return append(out, rest...)
}

func hotCells(items []Sample) map[string]bool {
	counts := map[string]int{}
	for _, s := range items {
		if !HeadMayScore(s.RuleSource) || s.HeadMaxP < DefaultTau {
			continue
		}
		if !businessPathClass(s.RuleClass) || !highRiskClass(s.HeadClass) {
			continue
		}
		counts[s.RuleClass+"|"+s.HeadClass]++
	}
	out := map[string]bool{}
	for key, n := range counts {
		if n >= 2 {
			out[key] = true
		}
	}
	return out
}

func businessPathClass(class string) bool {
	switch strings.TrimSpace(class) {
	case ClassBenign, ClassScan, ClassAbuse, ClassUnknown, "":
		return true
	default:
		return false
	}
}

func highRiskClass(class string) bool {
	switch strings.TrimSpace(class) {
	case ClassExploit, ClassMalware, ClassExfil, ClassFraud:
		return true
	default:
		return false
	}
}

func (c *Corpus) SetCap(n int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= 0 {
		n = DefaultCorpusCap
	}
	c.cap = n
}

func (c *Corpus) TrimTenant(tenant string, cap int) {
	if c == nil || strings.TrimSpace(tenant) == "" {
		return
	}
	if cap <= 0 {
		cap = DefaultCorpusCap
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var ids []string
	for id, s := range c.byID {
		if s.TenantID == tenant {
			ids = append(ids, id)
		}
	}
	if len(ids) <= cap {
		return
	}
	type row struct {
		id   string
		rank int
		seen string
	}
	rows := make([]row, 0, len(ids))
	for _, id := range ids {
		s := c.byID[id]
		r := 0
		if s.GoldSource == GoldHuman {
			r = 2
		} else if s.GoldSource == GoldAuto {
			r = 1
		}
		rows = append(rows, row{id: id, rank: r, seen: s.LastSeen})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].rank != rows[j].rank {
			return rows[i].rank < rows[j].rank
		}
		return rows[i].seen < rows[j].seen
	})
	drop := len(rows) - cap
	for i := 0; i < drop; i++ {
		delete(c.byID, rows[i].id)
	}
}

func (c *Corpus) RemapAuto(tenant, ruleID, class string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ruleID = strings.TrimSpace(ruleID)
	for id, s := range c.byID {
		if s.TenantID != tenant || s.RuleID != ruleID || s.GoldSource != GoldAuto {
			continue
		}
		if !IsTrainableClass(class) {
			s.GoldClass = ""
			s.GoldSource = ""
			s.LabeledAt = ""
		} else {
			s.GoldClass = class
		}
		c.byID[id] = s
	}
}
