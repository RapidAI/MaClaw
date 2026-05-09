package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

const (
	groupDiscussionMemorySourceType  = "group_discussion"
	groupDiscussionResultTag         = "a2a_discussion_result"
	groupDiscussionConflictTag       = "a2a_conflict_review"
	groupDiscussionRollbackTag       = "has_rollback"
	groupDiscussionRollbackTriggered = "rollback_triggered"
	groupDiscussionRollbackTagPrefix = "rollback_condition:"
	groupDiscussionRollbackMatchPref = "rollback_matched:"
	experienceReviewRequiredTag      = "review_required"
	experienceReviewResolvedTag      = "review_resolved"
	experienceReviewStatusTagPrefix  = "review_status:"
)

func (a *App) promoteGroupDiscussionResultToMemory(detail a2a.HubDiscussionDetail, result GroupDiscussionSummarizeResult) {
	if a == nil || strings.TrimSpace(result.Summary) == "" {
		return
	}
	readiness := groupDiscussionReadiness(detail)
	if !readiness.Ready && detail.Decision == nil {
		return
	}
	if result.AnswerCount == 0 && detail.Decision == nil && strings.TrimSpace(detail.Discussion.ResultSummary) == "" {
		return
	}
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return
	}

	content := formatGroupDiscussionMemoryEntry(detail, result)
	tags := buildGroupDiscussionMemoryTags(detail, result)
	if strings.TrimSpace(content) == "" || len(tags) == 0 {
		return
	}

	existingConflict, hasConflict := findGroupDiscussionMemoryConflict(a.memoryStore, tags, content)
	_, _ = upsertGroupDiscussionMemory(a.memoryStore, content, tags, groupDiscussionMemoryIdentityTagCount(tags), groupDiscussionMemoryTitle(detail))
	if hasConflict {
		reviewContent, reviewTags := formatGroupDiscussionConflictReview(detail, result, existingConflict, tags)
		_, _ = upsertGroupDiscussionMemory(a.memoryStore, reviewContent, reviewTags, groupDiscussionMemoryIdentityTagCount(reviewTags), "A2A conflict review")
	}
}

func upsertGroupDiscussionMemory(store *memory.Store, content string, tags []string, identityCount int, title string) (created bool, updated bool) {
	content = strings.TrimSpace(content)
	tags = normalizeUsageMemoryTags(tags)
	if store == nil || content == "" || len(tags) == 0 {
		return false, false
	}
	if identityCount <= 0 || identityCount > len(tags) {
		identityCount = len(tags)
	}
	allEntries := store.List(memory.CategoryProjectKnowledge, "")
	for _, entry := range allEntries {
		if !hasAllTags(entry.Tags, tags[:identityCount]...) {
			continue
		}
		if strings.TrimSpace(entry.Content) == content {
			store.TouchAccess([]string{entry.ID})
			return false, false
		}
		_ = store.Update(entry.ID, content, memory.CategoryProjectKnowledge, resetExperienceReviewTagsForChangedContent(entry.Tags, tags))
		return false, true
	}
	entry := memory.Entry{
		Title:      strings.TrimSpace(title),
		Content:    content,
		Category:   memory.CategoryProjectKnowledge,
		Tags:       tags,
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  groupDiscussionSourceURL(tags),
	}
	if err := store.Save(entry); err != nil {
		return false, false
	}
	return true, false
}

func groupDiscussionMemoryIdentityTagCount(tags []string) int {
	if len(tags) < 2 {
		return len(tags)
	}
	switch tags[0] {
	case groupDiscussionConflictTag:
		if len(tags) < 4 {
			return len(tags)
		}
		return 4
	default:
		return 2
	}
}

func buildGroupDiscussionMemoryTags(detail a2a.HubDiscussionDetail, result GroupDiscussionSummarizeResult) []string {
	discussionID := firstNonEmptyGroupString(result.ConsultationID, detail.Discussion.ID)
	if discussionID == "" && detail.Session != nil {
		discussionID = detail.Session.ID
	}
	if discussionID == "" {
		discussionID = "fingerprint:" + shortGroupDiscussionHash(formatGroupDiscussionMemoryEntry(detail, result))
	}
	tags := []string{groupDiscussionResultTag, "discussion:" + discussionID}
	if topic := groupDiscussionTopic(detail); topic != "" {
		tags = append(tags, "topic:"+shortGroupDiscussionHash(normalizeGroupDiscussionKey(topic)))
	}
	if question := groupDiscussionQuestion(detail); question != "" {
		tags = append(tags, "question:"+shortGroupDiscussionHash(normalizeGroupDiscussionKey(question)))
	}
	if detail.Decision != nil || strings.TrimSpace(detail.Discussion.ResultSummary) != "" {
		tags = append(tags, "a2a_decision")
	} else {
		tags = append(tags, "a2a_summary")
	}
	if len(result.Risks) > 0 {
		tags = append(tags, "has_risks")
	}
	if len(result.Disagreements) > 0 {
		tags = append(tags, "has_disagreements")
	}
	if len(result.OpenQuestions) > 0 {
		tags = append(tags, "has_open_questions")
	}
	if detail.Decision != nil && len(detail.Decision.RollbackOn) > 0 {
		tags = append(tags, groupDiscussionRollbackTag)
		tags = append(tags, experienceReviewRequiredTag)
		tags = append(tags, groupDiscussionRollbackConditionTags(detail.Decision.RollbackOn)...)
		readiness := groupDiscussionRollbackReadiness(detail, "")
		if readiness.Suggested {
			tags = append(tags, groupDiscussionRollbackTriggered)
			tags = append(tags, groupDiscussionRollbackMatchedTags(readiness.MatchedTriggers)...)
		}
	}
	if len(detail.ReviewSummaries) > 0 {
		tags = append(tags, "has_review_summary")
	}
	if escalation := groupDiscussionEscalation(detail); escalation != nil {
		tags = append(tags, "has_escalation")
		if target := strings.TrimSpace(escalation.Target); target != "" {
			tags = append(tags, "escalation_target:"+shortGroupDiscussionHash(normalizeGroupDiscussionKey(target)))
		}
	}
	return normalizeUsageMemoryTags(tags)
}

func groupDiscussionRollbackConditionTags(values []string) []string {
	values = dedupeGroupDiscussionStrings(values)
	if len(values) == 0 {
		return nil
	}
	if len(values) > 4 {
		values = values[:4]
	}
	tags := make([]string, 0, len(values))
	for _, value := range values {
		if key := normalizeGroupDiscussionKey(value); key != "" {
			tags = append(tags, groupDiscussionRollbackTagPrefix+shortGroupDiscussionHash(key))
		}
	}
	return tags
}

func groupDiscussionRollbackMatchedTags(values []string) []string {
	values = dedupeGroupDiscussionStrings(values)
	if len(values) == 0 {
		return nil
	}
	if len(values) > 4 {
		values = values[:4]
	}
	tags := make([]string, 0, len(values))
	for _, value := range values {
		if key := normalizeGroupDiscussionKey(value); key != "" {
			tags = append(tags, groupDiscussionRollbackMatchPref+shortGroupDiscussionHash(key))
		}
	}
	return tags
}

func formatGroupDiscussionMemoryEntry(detail a2a.HubDiscussionDetail, result GroupDiscussionSummarizeResult) string {
	var b strings.Builder
	b.WriteString("A2A discussion result")
	if id := firstNonEmptyGroupString(result.ConsultationID, detail.Discussion.ID); id != "" {
		b.WriteString(" (")
		b.WriteString(id)
		b.WriteString(")")
	}
	if topic := groupDiscussionTopic(detail); topic != "" {
		b.WriteString("\nTopic: ")
		b.WriteString(topic)
	}
	if question := groupDiscussionQuestion(detail); question != "" {
		b.WriteString("\nQuestion: ")
		b.WriteString(question)
	}
	b.WriteString("\nSummary: ")
	b.WriteString(strings.TrimSpace(result.Summary))
	if result.Rationale != "" {
		b.WriteString("\nRationale: ")
		b.WriteString(strings.TrimSpace(result.Rationale))
	}
	writeGroupDiscussionMemoryList(&b, "Risks", result.Risks)
	writeGroupDiscussionMemoryList(&b, "Disagreements", result.Disagreements)
	writeGroupDiscussionMemoryList(&b, "Open questions", result.OpenQuestions)
	if detail.Decision != nil && len(detail.Decision.RollbackOn) > 0 {
		writeGroupDiscussionMemoryList(&b, "Rollback on", detail.Decision.RollbackOn)
		readiness := groupDiscussionRollbackReadiness(detail, "")
		if len(readiness.MatchedTriggers) > 0 {
			writeGroupDiscussionMemoryList(&b, "Matched rollback triggers", readiness.MatchedTriggers)
		}
	}
	if detail.Decision != nil && strings.TrimSpace(detail.Decision.Rationale) != "" && strings.TrimSpace(detail.Decision.Rationale) != strings.TrimSpace(result.Rationale) {
		b.WriteString("\nDecision rationale: ")
		b.WriteString(truncateGroupDiscussionText(detail.Decision.Rationale, 500))
	}
	writeGroupDiscussionReviewSummaries(&b, detail.ReviewSummaries)
	writeGroupDiscussionEscalation(&b, groupDiscussionEscalation(detail))
	if len(result.ParticipantContributions) > 0 {
		keys := make([]string, 0, len(result.ParticipantContributions))
		for k := range result.ParticipantContributions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("\nParticipant contributions:")
		for _, participant := range keys {
			contribution := strings.TrimSpace(result.ParticipantContributions[participant])
			if contribution == "" {
				continue
			}
			b.WriteString("\n- ")
			b.WriteString(participant)
			b.WriteString(": ")
			b.WriteString(truncateGroupDiscussionText(contribution, 360))
		}
	}
	if result.Confidence > 0 {
		b.WriteString("\nConfidence: ")
		b.WriteString(fmt.Sprintf("%.2f", result.Confidence))
	}
	return strings.TrimSpace(b.String())
}

func groupDiscussionEscalation(detail a2a.HubDiscussionDetail) *a2a.Escalation {
	if detail.Session == nil || detail.Session.Escalation == nil {
		return nil
	}
	escalation := *detail.Session.Escalation
	escalation.RaisedBy = strings.TrimSpace(escalation.RaisedBy)
	escalation.Reason = strings.TrimSpace(escalation.Reason)
	escalation.Target = strings.TrimSpace(escalation.Target)
	if escalation.RaisedBy == "" && escalation.Reason == "" && escalation.Target == "" {
		return nil
	}
	return &escalation
}

func writeGroupDiscussionEscalation(b *strings.Builder, escalation *a2a.Escalation) {
	if escalation == nil {
		return
	}
	b.WriteString("\nEscalation:")
	if escalation.Reason != "" {
		b.WriteString("\n- Reason: ")
		b.WriteString(truncateGroupDiscussionText(escalation.Reason, 500))
	}
	if escalation.Target != "" {
		b.WriteString("\n- Target: ")
		b.WriteString(truncateGroupDiscussionText(escalation.Target, 200))
	}
	if escalation.RaisedBy != "" {
		b.WriteString("\n- Raised by: ")
		b.WriteString(truncateGroupDiscussionText(escalation.RaisedBy, 200))
	}
}

func writeGroupDiscussionReviewSummaries(b *strings.Builder, summaries map[string]a2a.ReviewSummary) {
	if len(summaries) == 0 {
		return
	}
	proposalIDs := make([]string, 0, len(summaries))
	for proposalID, summary := range summaries {
		proposalID = strings.TrimSpace(proposalID)
		if proposalID == "" || groupDiscussionReviewSummaryEmpty(summary) {
			continue
		}
		proposalIDs = append(proposalIDs, proposalID)
	}
	if len(proposalIDs) == 0 {
		return
	}
	sort.Strings(proposalIDs)
	b.WriteString("\nReview summaries:")
	for _, proposalID := range proposalIDs {
		summary := summaries[proposalID]
		reviewedBy := dedupeGroupDiscussionStrings(summary.ReviewedBy)
		b.WriteString("\n- ")
		b.WriteString(proposalID)
		b.WriteString(": approvals=")
		b.WriteString(fmt.Sprintf("%d", summary.Approvals))
		b.WriteString(", rejections=")
		b.WriteString(fmt.Sprintf("%d", summary.Rejections))
		b.WriteString(", concerns=")
		b.WriteString(fmt.Sprintf("%d", summary.Concerns))
		b.WriteString(", abstains=")
		b.WriteString(fmt.Sprintf("%d", summary.Abstains))
		if len(reviewedBy) > 0 {
			sort.Strings(reviewedBy)
			b.WriteString(", reviewed_by=")
			b.WriteString(strings.Join(reviewedBy, ", "))
		}
	}
}

func groupDiscussionReviewSummaryEmpty(summary a2a.ReviewSummary) bool {
	return summary.Approvals == 0 && summary.Rejections == 0 && summary.Concerns == 0 && summary.Abstains == 0 && len(summary.ReviewedBy) == 0
}

func writeGroupDiscussionMemoryList(b *strings.Builder, label string, values []string) {
	values = dedupeGroupDiscussionStrings(values)
	if len(values) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(label)
	b.WriteString(":")
	for _, value := range values {
		b.WriteString("\n- ")
		b.WriteString(truncateGroupDiscussionText(value, 360))
	}
}

func findGroupDiscussionMemoryConflict(store *memory.Store, newTags []string, newContent string) (memory.Entry, bool) {
	if store == nil || strings.TrimSpace(newContent) == "" {
		return memory.Entry{}, false
	}
	newDiscussionTag := groupDiscussionDiscussionTag(newTags)
	entries := store.List(memory.CategoryProjectKnowledge, "")
	for _, entry := range entries {
		if !hasTag(entry.Tags, groupDiscussionResultTag) || entry.SourceType != groupDiscussionMemorySourceType {
			continue
		}
		if newDiscussionTag != "" && hasTag(entry.Tags, newDiscussionTag) {
			continue
		}
		if !sharesGroupDiscussionTopicOrQuestion(entry.Tags, newTags) {
			continue
		}
		if groupDiscussionOpposingDecisionSignals(entry.Content, newContent) {
			return entry, true
		}
	}
	return memory.Entry{}, false
}

func formatGroupDiscussionConflictReview(detail a2a.HubDiscussionDetail, result GroupDiscussionSummarizeResult, existing memory.Entry, resultTags []string) (string, []string) {
	newID := strings.TrimPrefix(groupDiscussionDiscussionTag(resultTags), "discussion:")
	if newID == "" {
		newID = "new:" + shortGroupDiscussionHash(result.Summary)
	}
	existingID := firstNonEmptyGroupString(groupDiscussionDiscussionTag(existing.Tags), existing.ID)
	if existingID == "" {
		existingID = "existing:" + shortGroupDiscussionHash(existing.Content)
	}
	var b strings.Builder
	b.WriteString("A2A conflict review candidate")
	if topic := groupDiscussionTopic(detail); topic != "" {
		b.WriteString("\nTopic: ")
		b.WriteString(topic)
	}
	if question := groupDiscussionQuestion(detail); question != "" {
		b.WriteString("\nQuestion: ")
		b.WriteString(question)
	}
	b.WriteString("\nNew discussion: ")
	b.WriteString(newID)
	b.WriteString("\nNew summary: ")
	b.WriteString(truncateGroupDiscussionText(result.Summary, 500))
	b.WriteString("\nExisting memory: ")
	b.WriteString(existingID)
	b.WriteString("\nExisting summary: ")
	b.WriteString(truncateGroupDiscussionText(existing.Content, 500))
	b.WriteString("\nReview before treating either A2A result as durable project policy; opposite decision signals were detected for the same topic or question.")
	tags := []string{groupDiscussionConflictTag}
	for _, tag := range resultTags {
		if strings.HasPrefix(tag, "topic:") || strings.HasPrefix(tag, "question:") {
			tags = append(tags, tag)
		}
	}
	tags = append(tags, "new:"+shortGroupDiscussionHash(newID), "existing:"+shortGroupDiscussionHash(existingID), experienceReviewRequiredTag)
	return strings.TrimSpace(b.String()), normalizeUsageMemoryTags(tags)
}

func groupDiscussionOpposingDecisionSignals(a, b string) bool {
	posA, negA := groupDiscussionDecisionTargets(a)
	posB, negB := groupDiscussionDecisionTargets(b)
	return groupDiscussionTargetOverlap(posA, negB) || groupDiscussionTargetOverlap(negA, posB)
}

func groupDiscussionDecisionTargets(text string) (positive map[string]struct{}, negative map[string]struct{}) {
	tokens := groupDiscussionDecisionTokens(text)
	positive = map[string]struct{}{}
	negative = map[string]struct{}{}
	positiveMarkers := map[string]struct{}{"use": {}, "prefer": {}, "choose": {}, "enable": {}, "adopt": {}, "allow": {}, "approve": {}, "accept": {}, "keep": {}, "proceed": {}}
	negativeMarkers := map[string]struct{}{"avoid": {}, "disable": {}, "reject": {}, "block": {}, "remove": {}, "abandon": {}, "defer": {}, "skip": {}}
	for i, token := range tokens {
		if token == "do" && i+2 < len(tokens) && tokens[i+1] == "not" {
			if _, ok := positiveMarkers[tokens[i+2]]; ok {
				if target := groupDiscussionDecisionTargetAfter(tokens, i+3); target != "" {
					negative[target] = struct{}{}
				}
			}
			continue
		}
		if _, ok := positiveMarkers[token]; ok {
			if target := groupDiscussionDecisionTargetAfter(tokens, i+1); target != "" {
				positive[target] = struct{}{}
			}
			continue
		}
		if _, ok := negativeMarkers[token]; ok {
			if target := groupDiscussionDecisionTargetAfter(tokens, i+1); target != "" {
				negative[target] = struct{}{}
			}
		}
	}
	return positive, negative
}

func groupDiscussionDecisionTokens(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func groupDiscussionDecisionTargetAfter(tokens []string, start int) string {
	stop := map[string]struct{}{"a": {}, "an": {}, "the": {}, "to": {}, "with": {}, "for": {}, "by": {}, "of": {}, "in": {}, "on": {}, "this": {}, "that": {}, "option": {}, "approach": {}, "strategy": {}, "plan": {}, "and": {}, "or": {}}
	parts := make([]string, 0, 2)
	for i := start; i < len(tokens) && len(parts) < 2; i++ {
		tok := strings.TrimSpace(tokens[i])
		if tok == "" {
			continue
		}
		if _, ok := stop[tok]; ok {
			continue
		}
		parts = append(parts, tok)
	}
	return strings.Join(parts, " ")
}

func groupDiscussionTargetOverlap(a, b map[string]struct{}) bool {
	for left := range a {
		for right := range b {
			if left == right || strings.Contains(left, right) || strings.Contains(right, left) {
				return true
			}
		}
	}
	return false
}

func sharesGroupDiscussionTopicOrQuestion(existingTags, newTags []string) bool {
	for _, tag := range newTags {
		if !(strings.HasPrefix(tag, "topic:") || strings.HasPrefix(tag, "question:")) {
			continue
		}
		if hasTag(existingTags, tag) {
			return true
		}
	}
	return false
}

func groupDiscussionDiscussionTag(tags []string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, "discussion:") {
			return tag
		}
	}
	return ""
}

func groupDiscussionSourceURL(tags []string) string {
	if tag := groupDiscussionDiscussionTag(tags); tag != "" {
		return "a2a://current_hub/" + strings.TrimPrefix(tag, "discussion:")
	}
	return "a2a://current_hub"
}

func groupDiscussionMemoryTitle(detail a2a.HubDiscussionDetail) string {
	label := firstNonEmptyGroupString(groupDiscussionTopic(detail), groupDiscussionQuestion(detail), detail.Discussion.ID, "A2A discussion")
	return "A2A: " + truncateGroupDiscussionText(label, 80)
}

func groupDiscussionTopic(detail a2a.HubDiscussionDetail) string {
	if topic := strings.TrimSpace(detail.Discussion.Topic); topic != "" {
		return topic
	}
	if detail.Session != nil {
		return strings.TrimSpace(detail.Session.Topic)
	}
	return ""
}

func groupDiscussionQuestion(detail a2a.HubDiscussionDetail) string {
	if question := strings.TrimSpace(detail.Discussion.Question); question != "" {
		return question
	}
	if detail.Session != nil {
		return strings.TrimSpace(detail.Session.Goal)
	}
	return ""
}

func normalizeGroupDiscussionKey(value string) string {
	return strings.Join(groupDiscussionDecisionTokens(value), " ")
}

func shortGroupDiscussionHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(value))))
	return hex.EncodeToString(sum[:])[:12]
}
