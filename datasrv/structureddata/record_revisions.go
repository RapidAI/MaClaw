package structureddata

import (
	"context"
	"sort"
	"strings"
	"time"
)

func (s *Service) appendRecordRevision(ctx context.Context, p Principal, action string, record Record) error {
	_, err := s.store.AppendRecordRevision(ctx, RecordRevision{
		ID:        newID("revision"),
		TenantID:  record.TenantID,
		DatasetID: record.DatasetID,
		RecordID:  record.ID,
		Action:    strings.TrimSpace(action),
		Title:     record.Title,
		Tags:      append([]string(nil), record.Tags...),
		Data:      cloneJSONMap(record.Data),
		SourceID:  record.SourceID,
		CreatedBy: firstNonEmpty(p.UserID, record.UpdatedBy, record.CreatedBy),
		CreatedAt: s.now().UTC(),
	})
	return err
}

func maskSensitiveRevisions(revisions []RecordRevision, fields []FieldDefinition, p Principal) []RecordRevision {
	if canViewSensitive(p) || len(revisions) == 0 {
		return revisions
	}
	sensitive := sensitiveFieldSet(fields)
	if len(sensitive) == 0 {
		return revisions
	}
	out := make([]RecordRevision, len(revisions))
	for i := range revisions {
		out[i] = revisions[i]
		out[i].Tags = append([]string(nil), revisions[i].Tags...)
		out[i].Data = cloneJSONMap(revisions[i].Data)
		for key := range sensitive {
			if _, ok := out[i].Data[key]; ok {
				out[i].Data[key] = maskedValue
			}
		}
	}
	return out
}

func (s *Service) GetRecordTimeline(ctx context.Context, p Principal, datasetID, recordID string, in QueryRecordTimelineInput) (*RecordTimelineResult, error) {
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	revisions, err := s.store.QueryRecordRevisions(ctx, p.TenantID, datasetID, recordID, QueryRecordRevisionsInput{Limit: limit, Before: strings.TrimSpace(in.Before), BeforeID: strings.TrimSpace(in.BeforeID)})
	if err != nil {
		return nil, err
	}
	revisions = maskSensitiveRevisions(revisions, fields, p)
	events, err := s.store.QueryDataEvents(ctx, p.TenantID, QueryDataEventsInput{DatasetID: datasetID, RecordID: recordID, Limit: limit, Before: strings.TrimSpace(in.Before), BeforeID: strings.TrimSpace(in.BeforeID)})
	if err != nil {
		return nil, err
	}
	audit, err := s.store.QueryAuditLogs(ctx, p.TenantID, QueryAuditLogsInput{DatasetID: datasetID, TargetType: "record", TargetID: recordID, Limit: limit, Before: strings.TrimSpace(in.Before), BeforeID: strings.TrimSpace(in.BeforeID)})
	if err != nil {
		return nil, err
	}
	approvals, err := s.store.ListRecordApprovals(ctx, p.TenantID, QueryRecordApprovalsInput{DatasetID: datasetID, RecordID: recordID, Limit: limit, Before: strings.TrimSpace(in.Before), BeforeID: strings.TrimSpace(in.BeforeID)})
	if err != nil {
		return nil, err
	}
	items := make([]RecordTimelineItem, 0, len(revisions)+len(events)+len(audit)+len(approvals))
	for _, revision := range revisions {
		items = append(items, RecordTimelineItem{
			ID:        revision.ID,
			Type:      "revision",
			Action:    revision.Action,
			UserID:    revision.CreatedBy,
			Summary:   "Record revision " + revision.Action,
			Data:      cloneJSONMap(revision.Data),
			CreatedAt: revision.CreatedAt,
		})
	}
	for _, event := range events {
		items = append(items, RecordTimelineItem{
			ID:        event.ID,
			Type:      "event",
			Action:    event.EventType,
			UserID:    event.CreatedBy,
			Source:    event.Source,
			Summary:   event.Operation + " " + event.ResultStatus,
			Metadata:  map[string]any{"business_action_id": event.BusinessAction, "idempotency_key": event.IdempotencyKey, "result_status": event.ResultStatus},
			CreatedAt: event.AppliedAt,
		})
	}
	for _, entry := range audit {
		items = append(items, RecordTimelineItem{
			ID:        entry.ID,
			Type:      "audit",
			Action:    entry.Action,
			UserID:    entry.UserID,
			Summary:   entry.Summary,
			Metadata:  cloneJSONMap(entry.Metadata),
			CreatedAt: entry.CreatedAt,
		})
	}
	for _, approval := range approvals {
		items = append(items, RecordTimelineItem{
			ID:        approval.ID,
			Type:      "approval",
			Action:    approval.Status,
			UserID:    firstNonEmpty(approval.ReviewedBy, approval.CreatedBy),
			Summary:   approval.Summary,
			Metadata:  recordApprovalMetadata(approval),
			CreatedAt: firstNonZeroTime(approval.ReviewedAt, approval.CreatedAt),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := &RecordTimelineResult{DatasetID: datasetID, RecordID: recordID, Items: items, Limit: limit, HasMore: hasMore}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		result.NextBefore = last.CreatedAt.Format(time.RFC3339Nano)
		result.NextBeforeID = last.ID
	}
	return result, nil
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
