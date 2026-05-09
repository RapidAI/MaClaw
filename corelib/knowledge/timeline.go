package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *SQLiteStore) SourceTimeline(ctx context.Context, sourceID string, limit int) (SourceTimelineResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SourceTimelineResult{}, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	source, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return SourceTimelineResult{}, err
	}
	result := SourceTimelineResult{
		SourceID:    sourceID,
		Source:      source,
		Limit:       limit,
		Notes:       []string{"local_source_timeline_no_llm"},
		GeneratedAt: time.Now().UTC(),
	}
	events := make([]SourceTimelineEvent, 0, limit+2)
	if !source.CreatedAt.IsZero() {
		events = append(events, SourceTimelineEvent{
			ID:         "source_created:" + source.ID,
			SourceID:   source.ID,
			Kind:       "source_created",
			Action:     "created",
			Title:      firstNonEmpty(source.Title, source.RelativePath, source.CanonicalURI, source.URI, source.ID),
			Detail:     source.Kind,
			Status:     source.Status,
			NodeCount:  source.NodeCount,
			CardCount:  source.CardCount,
			FactCount:  source.FactCount,
			OccurredAt: source.CreatedAt,
		})
	}
	if !source.UpdatedAt.IsZero() && !sameTimelineInstant(source.UpdatedAt, source.CreatedAt) {
		events = append(events, SourceTimelineEvent{
			ID:         "source_updated:" + source.ID,
			SourceID:   source.ID,
			Kind:       "source_updated",
			Action:     "updated",
			Title:      firstNonEmpty(source.Title, source.RelativePath, source.CanonicalURI, source.URI, source.ID),
			Detail:     source.ErrorMessage,
			Status:     source.Status,
			NodeCount:  source.NodeCount,
			CardCount:  source.CardCount,
			FactCount:  source.FactCount,
			OccurredAt: source.UpdatedAt,
		})
	}
	versions, err := s.ListSourceVersions(ctx, sourceID, limit)
	if err != nil {
		return SourceTimelineResult{}, err
	}
	for _, version := range versions {
		action := strings.TrimSpace(version.Reason)
		if action == "" {
			action = "version"
		}
		events = append(events, SourceTimelineEvent{
			ID:          "source_version:" + version.ID,
			SourceID:    version.SourceID,
			Kind:        "source_version",
			Action:      action,
			Title:       firstNonEmpty(version.Title, source.Title, source.RelativePath, source.ID),
			Detail:      version.Kind,
			Status:      version.Status,
			VersionID:   version.ID,
			ContentHash: version.ContentHash,
			NodeCount:   version.NodeCount,
			CardCount:   version.CardCount,
			FactCount:   version.FactCount,
			OccurredAt:  version.CreatedAt,
		})
	}
	linkEvents, err := s.ListSourceLinkEvents(ctx, sourceID, limit)
	if err != nil {
		return SourceTimelineResult{}, err
	}
	for _, event := range linkEvents {
		events = append(events, SourceTimelineEvent{
			ID:              "source_link_event:" + event.ID,
			SourceID:        event.SourceID,
			Kind:            "source_link_event",
			Action:          event.Action,
			Title:           strings.TrimSpace(event.Note),
			Detail:          event.Relation,
			Relation:        event.Relation,
			RelatedSourceID: event.RelatedSourceID,
			Score:           event.Score,
			Terms:           append([]string(nil), event.Terms...),
			Evidence:        append([]string(nil), event.Evidence...),
			OccurredAt:      event.CreatedAt,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !sameTimelineInstant(events[i].OccurredAt, events[j].OccurredAt) {
			return events[i].OccurredAt.After(events[j].OccurredAt)
		}
		if events[i].Kind != events[j].Kind {
			return events[i].Kind < events[j].Kind
		}
		return events[i].ID < events[j].ID
	})
	if len(events) > limit {
		events = events[:limit]
	}
	result.Events = events
	result.Count = len(events)
	return result, nil
}

func sameTimelineInstant(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() && b.IsZero()
	}
	return a.Equal(b)
}
