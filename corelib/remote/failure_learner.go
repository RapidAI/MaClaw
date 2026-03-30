package remote

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LearnedConstraint 表示一条从失败中学到的约束规则。
type LearnedConstraint struct {
	Rule          string    `json:"rule"`
	TriggerCount  int       `json:"trigger_count"`
	CreatedAt     time.Time `json:"created_at"`
	LastTriggered time.Time `json:"last_triggered"`
}

// FailureLearner 从重复失败中提取约束规则。
type FailureLearner struct {
	projectPath   string
	threshold     int            // 重复失败阈值 (默认 3)
	expiryDays    int            // 约束过期天数 (默认 7)
	maxTokens     int            // 约束块 token 上限 (默认 1500)
	errorPatterns map[string]int // errorKey → 出现次数
	mu            sync.Mutex
}

// NewFailureLearner 创建失败学习器。
func NewFailureLearner(projectPath string) *FailureLearner {
	return &FailureLearner{
		projectPath:   projectPath,
		threshold:     3,
		expiryDays:    7,
		maxTokens:     1500,
		errorPatterns: make(map[string]int),
	}
}

// constraintsFilePath returns the path to the learned-constraints.md file.
func (l *FailureLearner) constraintsFilePath() string {
	return filepath.Join(l.projectPath, ".maclaw", "learned-constraints.md")
}

// RecordError 记录一次错误事件，达到阈值时自动生成约束。
func (l *FailureLearner) RecordError(errorKey, errorDetail string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.errorPatterns[errorKey]++
	count := l.errorPatterns[errorKey]

	if count == l.threshold {
		constraint := LearnedConstraint{
			Rule:          errorDetail,
			TriggerCount:  count,
			CreatedAt:     time.Now(),
			LastTriggered: time.Now(),
		}
		l.appendConstraint(constraint)
	} else if count > l.threshold {
		// Already generated constraint; bump trigger count in file.
		l.bumpConstraint(errorKey, errorDetail)
	}
}

// appendConstraint appends a new constraint to the file.
func (l *FailureLearner) appendConstraint(c LearnedConstraint) {
	path := l.constraintsFilePath()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[FailureLearner] mkdir %s: %v", dir, err)
		return
	}

	// If file doesn't exist, write header first.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("# Learned Constraints (Auto-generated)\n"), 0o644); err != nil {
			log.Printf("[FailureLearner] create file: %v", err)
			return
		}
	}

	line, err := json.Marshal(c)
	if err != nil {
		log.Printf("[FailureLearner] marshal constraint: %v", err)
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[FailureLearner] open file: %v", err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "%s\n", line)
}

// bumpConstraint increments trigger count for an existing constraint matching errorDetail.
func (l *FailureLearner) bumpConstraint(errorKey, errorDetail string) {
	constraints := l.loadConstraintsInternal()
	found := false
	for i := range constraints {
		if constraints[i].Rule == errorDetail {
			constraints[i].TriggerCount++
			constraints[i].LastTriggered = time.Now()
			found = true
			break
		}
	}
	if !found {
		return
	}
	l.saveConstraints(constraints)
}

// LoadConstraints 从 .maclaw/learned-constraints.md 加载约束。
func (l *FailureLearner) LoadConstraints() []LearnedConstraint {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loadConstraintsInternal()
}

// loadConstraintsInternal loads constraints without locking (caller must hold lock).
func (l *FailureLearner) loadConstraintsInternal() []LearnedConstraint {
	path := l.constraintsFilePath()

	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[FailureLearner] open %s: %v", path, err)
		}
		return nil
	}
	defer f.Close()

	var constraints []LearnedConstraint
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c LearnedConstraint
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			log.Printf("[FailureLearner] parse line: %v", err)
			continue
		}
		constraints = append(constraints, c)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[FailureLearner] scan %s: %v", path, err)
	}
	return constraints
}

// saveConstraints writes all constraints back to the file.
func (l *FailureLearner) saveConstraints(constraints []LearnedConstraint) {
	path := l.constraintsFilePath()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[FailureLearner] mkdir %s: %v", dir, err)
		return
	}

	var b strings.Builder
	b.WriteString("# Learned Constraints (Auto-generated)\n")
	for _, c := range constraints {
		line, err := json.Marshal(c)
		if err != nil {
			log.Printf("[FailureLearner] marshal: %v", err)
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		log.Printf("[FailureLearner] write %s: %v", path, err)
	}
}

// BuildConstraintBlock 生成约束注入内容。
// 按触发次数降序排列，超出 token 限制时截断低频约束。
func (l *FailureLearner) BuildConstraintBlock() string {
	l.mu.Lock()
	constraints := l.loadConstraintsInternal()
	l.mu.Unlock()

	if len(constraints) == 0 {
		return ""
	}

	// Sort by TriggerCount descending.
	sort.SliceStable(constraints, func(i, j int) bool {
		return constraints[i].TriggerCount > constraints[j].TriggerCount
	})

	header := "[📚 已学习约束]\n"
	footer := "[/约束]\n"

	var b strings.Builder
	b.WriteString(header)

	for _, c := range constraints {
		line := fmt.Sprintf("- (触发 %d 次) %s\n", c.TriggerCount, c.Rule)
		candidate := b.String() + line + footer
		if estimateTokens(candidate) > l.maxTokens {
			break
		}
		b.WriteString(line)
	}

	b.WriteString(footer)

	result := b.String()
	// If only header+footer (no constraints fit), return empty.
	if result == header+footer {
		return ""
	}
	return result
}

// PruneExpired 移除 expiryDays 天内未触发的过期约束。
func (l *FailureLearner) PruneExpired() {
	l.mu.Lock()
	defer l.mu.Unlock()

	constraints := l.loadConstraintsInternal()
	if len(constraints) == 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -l.expiryDays)
	var kept []LearnedConstraint
	for _, c := range constraints {
		if c.LastTriggered.After(cutoff) {
			kept = append(kept, c)
		}
	}

	if len(kept) != len(constraints) {
		l.saveConstraints(kept)
	}
}
