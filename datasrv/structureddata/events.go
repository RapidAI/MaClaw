package structureddata

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *Service) IngestEvent(ctx context.Context, p Principal, in DataEventInput) (*DataEventResult, error) {
	if strings.TrimSpace(in.BusinessAction) != "" {
		return s.ingestBusinessActionEvent(ctx, p, in)
	}
	return s.ingestRawEvent(ctx, p, in, "")
}

func (s *Service) ingestRawEvent(ctx context.Context, p Principal, in DataEventInput, businessActionID string) (*DataEventResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := strings.TrimSpace(in.Source)
	if source == "" {
		return nil, fmt.Errorf("%w: source is required", ErrInvalidInput)
	}
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" {
		return nil, fmt.Errorf("%w: event_type is required", ErrInvalidInput)
	}
	datasetID := strings.TrimSpace(in.DatasetID)
	if datasetID == "" {
		return nil, fmt.Errorf("%w: dataset_id is required", ErrInvalidInput)
	}
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	operation := strings.ToLower(strings.TrimSpace(in.Operation))
	if operation == "" {
		operation = "upsert_record"
	}
	recordID := strings.TrimSpace(in.RecordID)
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if recordID == "" {
		recordID = idempotencyKey
	}
	if recordID == "" {
		return nil, fmt.Errorf("%w: record_id or idempotency_key is required", ErrInvalidInput)
	}

	if idempotencyKey != "" {
		existingEvent, err := s.store.GetDataEventByIdempotencyKey(ctx, p.TenantID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existingEvent != nil {
			return s.duplicateEventResult(ctx, p, fields, existingEvent)
		}
	}

	appliedAt := s.now().UTC()
	result := &DataEventResult{Status: "applied", Source: source, EventType: eventType, Operation: operation, BusinessAction: strings.TrimSpace(businessActionID), DatasetID: datasetID, RecordID: recordID, IdempotencyKey: idempotencyKey, AppliedAt: appliedAt}
	if in.DryRun {
		return s.dryRunRawEventLocked(ctx, p, in, result, fields)
	}

	switch operation {
	case "upsert", "upsert_record", "create_or_update", "merge", "merge_record", "patch_record":
		if len(in.Data) == 0 {
			return nil, fmt.Errorf("%w: data is required", ErrInvalidInput)
		}
		if existing, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID); err == nil {
			nextData := cloneJSONMap(in.Data)
			if operation == "merge" || operation == "merge_record" || operation == "patch_record" {
				nextData = cloneJSONMap(existing.Data)
				for key, value := range in.Data {
					nextData[key] = value
				}
			}
			if err := validateRecordData(fields, nextData); err != nil {
				return nil, err
			}
			if err := s.validateUniqueConstraints(ctx, p, datasetID, fields, nextData, recordID); err != nil {
				return nil, err
			}
			title := strings.TrimSpace(in.Title)
			if title == "" {
				title = existing.Title
			}
			tags := normalizeTags(in.Tags)
			if in.Tags == nil && (operation == "merge" || operation == "merge_record" || operation == "patch_record") {
				tags = existing.Tags
			}
			record, err := s.store.UpdateRecord(ctx, p.TenantID, datasetID, recordID, UpdateRecordInput{Title: &title, Tags: tags, Data: nextData}, eventActor(p, source), appliedAt)
			if err != nil {
				return nil, err
			}
			result.Status = "updated"
			result.Record = maskSensitiveRecord(record, fields, p)
			s.audit(ctx, p, "record.update", datasetID, "record", recordID, "Updated record from event "+eventType, map[string]any{"source": source, "event_type": eventType, "idempotency_key": idempotencyKey})
			revisionRecord := *record
			revisionRecord.SourceID = firstNonEmpty(idempotencyKey, source+":"+eventType+":"+recordID)
			if err := s.appendRecordRevision(ctx, p, "event.update", revisionRecord); err != nil {
				return nil, err
			}
			if err := s.appendDataEventLog(ctx, p, result); err != nil {
				return nil, err
			}
			return result, nil
		} else if !errors.Is(err, ErrRecordNotFound) {
			return nil, err
		}
		if err := validateRecordData(fields, in.Data); err != nil {
			return nil, err
		}
		if err := s.validateUniqueConstraints(ctx, p, datasetID, fields, in.Data, recordID); err != nil {
			return nil, err
		}
		record := Record{ID: recordID, TenantID: p.TenantID, DatasetID: datasetID, Title: strings.TrimSpace(in.Title), Tags: normalizeTags(in.Tags), Data: cloneJSONMap(in.Data), SourceID: firstNonEmpty(idempotencyKey, source+":"+eventType+":"+recordID), CreatedBy: eventActor(p, source), UpdatedBy: eventActor(p, source), CreatedAt: appliedAt, UpdatedAt: appliedAt}
		created, err := s.store.CreateRecord(ctx, record)
		if err != nil {
			return nil, err
		}
		result.Status = "created"
		result.Record = maskSensitiveRecord(created, fields, p)
		s.audit(ctx, p, "record.create", datasetID, "record", recordID, "Created record from event "+eventType, map[string]any{"source": source, "event_type": eventType, "idempotency_key": idempotencyKey})
		if err := s.appendRecordRevision(ctx, p, "event.create", *created); err != nil {
			return nil, err
		}
		if err := s.appendDataEventLog(ctx, p, result); err != nil {
			return nil, err
		}
		return result, nil
	case "delete", "delete_record":
		before, beforeErr := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID)
		if err := s.store.DeleteRecord(ctx, p.TenantID, datasetID, recordID); err != nil {
			if errors.Is(err, ErrRecordNotFound) {
				result.Status = "already_deleted"
				if err := s.appendDataEventLog(ctx, p, result); err != nil {
					return nil, err
				}
				return result, nil
			}
			return nil, err
		}
		result.Status = "deleted"
		if beforeErr == nil && before != nil {
			revisionRecord := *before
			revisionRecord.SourceID = firstNonEmpty(idempotencyKey, source+":"+eventType+":"+recordID)
			if err := s.appendRecordRevision(ctx, p, "event.delete", revisionRecord); err != nil {
				return nil, err
			}
		}
		s.audit(ctx, p, "record.delete", datasetID, "record", recordID, "Deleted record from event "+eventType, map[string]any{"source": source, "event_type": eventType, "idempotency_key": idempotencyKey})
		if err := s.appendDataEventLog(ctx, p, result); err != nil {
			return nil, err
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%w: unsupported operation", ErrInvalidInput)
	}
}

func (s *Service) dryRunRawEventLocked(ctx context.Context, p Principal, in DataEventInput, result *DataEventResult, fields []FieldDefinition) (*DataEventResult, error) {
	result.Status = "dry_run"
	result.DryRun = true
	result.Valid = true
	switch result.Operation {
	case "upsert", "upsert_record", "create_or_update", "merge", "merge_record", "patch_record":
		if len(in.Data) == 0 {
			validation := ValidateRecordResult{Valid: false, DatasetID: result.DatasetID, FieldCount: len(fields), Errors: []string{"data is required"}}
			result.Valid = false
			result.Validation = &validation
			result.Preview = map[string]any{}
			return result, nil
		}
		preview := cloneJSONMap(in.Data)
		if existing, err := s.store.GetRecord(ctx, p.TenantID, result.DatasetID, result.RecordID); err == nil && (result.Operation == "merge" || result.Operation == "merge_record" || result.Operation == "patch_record") {
			preview = cloneJSONMap(existing.Data)
			for key, value := range in.Data {
				preview[key] = value
			}
		} else if err != nil && !errors.Is(err, ErrRecordNotFound) {
			return nil, err
		}
		validation := validateRecordDataResult(result.DatasetID, fields, preview)
		if uniqueErrors, err := s.uniqueConstraintErrors(ctx, p, result.DatasetID, fields, preview, result.RecordID); err != nil {
			return nil, err
		} else if len(uniqueErrors) > 0 {
			validation.Valid = false
			validation.Errors = append(validation.Errors, uniqueErrors...)
		}
		result.Valid = validation.Valid
		result.Validation = &validation
		result.Preview = preview
		return result, nil
	case "delete", "delete_record":
		_, err := s.store.GetRecord(ctx, p.TenantID, result.DatasetID, result.RecordID)
		if err != nil && !errors.Is(err, ErrRecordNotFound) {
			return nil, err
		}
		result.Preview = map[string]any{"record_exists": err == nil}
		return result, nil
	default:
		validation := ValidateRecordResult{Valid: false, DatasetID: result.DatasetID, FieldCount: len(fields), Errors: []string{"unsupported operation"}}
		result.Valid = false
		result.Validation = &validation
		result.Preview = map[string]any{}
		return result, nil
	}
}

func (s *Service) QueryDataEvents(ctx context.Context, p Principal, in QueryDataEventsInput) ([]DataEventLog, error) {
	return s.store.QueryDataEvents(ctx, p.TenantID, in)
}

func (s *Service) CreateDataEventDeadLetter(ctx context.Context, p Principal, in DataEventInput, cause error) (*DataEventDeadLetter, error) {
	if in.DryRun {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	payload := dataEventInputPayload(in)
	item := DataEventDeadLetter{
		ID:             newID("deadletter"),
		TenantID:       p.TenantID,
		Status:         "open",
		Source:         strings.TrimSpace(in.Source),
		EventType:      strings.TrimSpace(in.EventType),
		BusinessAction: strings.TrimSpace(in.BusinessAction),
		DatasetID:      strings.TrimSpace(in.DatasetID),
		RecordID:       strings.TrimSpace(in.RecordID),
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		Error:          strings.TrimSpace(cause.Error()),
		Payload:        payload,
		CreatedBy:      p.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	out, err := s.store.CreateDataEventDeadLetter(ctx, item)
	if err == nil {
		s.audit(ctx, p, "event.dead_letter", item.DatasetID, "dead_letter", item.ID, "Captured failed data event", map[string]any{"source": item.Source, "event_type": item.EventType, "business_action_id": item.BusinessAction, "error": item.Error})
	}
	return out, err
}

func (s *Service) QueryDataEventDeadLetters(ctx context.Context, p Principal, in QueryDataEventDeadLettersInput) ([]DataEventDeadLetter, error) {
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validDataEventDeadLetterStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid dead letter status", ErrInvalidInput)
	}
	return s.store.QueryDataEventDeadLetters(ctx, p.TenantID, in)
}

func validDataEventDeadLetterStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "open", "resolved", "retried":
		return true
	default:
		return false
	}
}

func (s *Service) GetDataEventDeadLetter(ctx context.Context, p Principal, deadLetterID string) (*DataEventDeadLetter, error) {
	return s.store.GetDataEventDeadLetter(ctx, p.TenantID, strings.TrimSpace(deadLetterID))
}

func (s *Service) ResolveDataEventDeadLetter(ctx context.Context, p Principal, deadLetterID string, in ResolveDataEventDeadLetterInput) (*DataEventDeadLetter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.store.UpdateDataEventDeadLetterStatus(ctx, p.TenantID, strings.TrimSpace(deadLetterID), "resolved", p.UserID, strings.TrimSpace(in.Resolution), s.now().UTC())
	if err == nil {
		s.audit(ctx, p, "event.dead_letter.resolve", out.DatasetID, "dead_letter", out.ID, "Resolved data event dead letter", map[string]any{"resolution": out.Resolution})
	}
	return out, err
}

func (s *Service) RetryDataEventDeadLetter(ctx context.Context, p Principal, deadLetterID string) (*RetryDataEventDeadLetterResult, error) {
	item, err := s.GetDataEventDeadLetter(ctx, p, deadLetterID)
	if err != nil {
		return nil, err
	}
	if item.Status != "open" {
		return nil, fmt.Errorf("%w: dead letter is not open", ErrInvalidInput)
	}
	in, err := dataEventInputFromPayload(item.Payload)
	if err != nil {
		return nil, err
	}
	result, err := s.IngestEvent(ctx, p, in)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	updated, updateErr := s.store.UpdateDataEventDeadLetterStatus(ctx, p.TenantID, item.ID, "retried", p.UserID, "retry succeeded", s.now().UTC())
	if updateErr == nil {
		s.audit(ctx, p, "event.dead_letter.retry", updated.DatasetID, "dead_letter", updated.ID, "Retried data event dead letter", map[string]any{"event_status": result.Status})
	}
	s.mu.Unlock()
	if updateErr != nil {
		return nil, updateErr
	}
	return &RetryDataEventDeadLetterResult{DeadLetter: *updated, Result: result}, nil
}

func (s *Service) ingestBusinessActionEvent(ctx context.Context, p Principal, in DataEventInput) (*DataEventResult, error) {
	source := strings.TrimSpace(in.Source)
	if source == "" {
		return nil, fmt.Errorf("%w: source is required", ErrInvalidInput)
	}
	actionID := strings.TrimSpace(in.BusinessAction)
	action, err := s.GetBusinessAction(ctx, p, actionID)
	if err != nil {
		return nil, err
	}
	if datasetID := strings.TrimSpace(in.DatasetID); datasetID != "" && datasetID != action.DatasetID {
		return nil, fmt.Errorf("%w: dataset_id does not match business action", ErrInvalidInput)
	}
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("%w: data is required", ErrInvalidInput)
	}
	for _, key := range action.RequiredFields {
		if _, ok := in.Data[key]; !ok {
			return nil, fmt.Errorf("%w: missing required business field %s", ErrInvalidInput, key)
		}
	}
	if _, err := s.GetDataset(ctx, p, action.DatasetID); err != nil {
		if errors.Is(err, ErrDatasetNotFound) {
			if in.DryRun {
				validation := validateBusinessActionTemplateData(*action, in.Data)
				return &DataEventResult{Status: "dry_run", DryRun: true, Valid: validation.Valid, Validation: validation, Preview: cloneJSONMap(in.Data), Source: source, EventType: firstNonEmpty(strings.TrimSpace(in.EventType), action.EventType), Operation: action.Operation, BusinessAction: action.ID, DatasetID: action.DatasetID, RecordID: strings.TrimSpace(in.RecordID), IdempotencyKey: strings.TrimSpace(in.IdempotencyKey), AppliedAt: s.now().UTC()}, nil
			}
			if _, createErr := s.CreateDatasetFromTemplate(ctx, p, action.DatasetID, CreateFromTemplateInput{}); createErr != nil {
				return nil, createErr
			}
		} else {
			return nil, err
		}
	}
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" {
		eventType = action.EventType
	}
	tags := normalizeTags(append(append([]string{}, action.SuggestedTags...), in.Tags...))
	if in.DryRun {
		dryRun, err := s.dryRunBusinessAction(ctx, p, *action, ExecuteBusinessActionInput{RecordID: in.RecordID, IdempotencyKey: in.IdempotencyKey, Title: in.Title, Tags: tags, Data: in.Data, OccurredAt: in.OccurredAt, DryRun: true})
		if err != nil {
			return nil, err
		}
		return &DataEventResult{Status: "dry_run", DryRun: true, Valid: dryRun.Valid, Validation: dryRun.Validation, Preview: dryRun.Preview, Source: source, EventType: eventType, Operation: action.Operation, BusinessAction: action.ID, DatasetID: action.DatasetID, RecordID: strings.TrimSpace(in.RecordID), IdempotencyKey: strings.TrimSpace(in.IdempotencyKey), AppliedAt: s.now().UTC()}, nil
	}
	result, err := s.ingestRawEvent(ctx, p, DataEventInput{
		Source:         source,
		EventType:      eventType,
		Operation:      action.Operation,
		DatasetID:      action.DatasetID,
		RecordID:       in.RecordID,
		IdempotencyKey: in.IdempotencyKey,
		Title:          in.Title,
		Tags:           tags,
		Data:           in.Data,
		OccurredAt:     in.OccurredAt,
	}, action.ID)
	if err != nil {
		return nil, err
	}
	result.BusinessAction = action.ID
	return result, nil
}

func dataEventInputPayload(in DataEventInput) map[string]any {
	return map[string]any{
		"source":             in.Source,
		"event_type":         in.EventType,
		"operation":          in.Operation,
		"business_action_id": in.BusinessAction,
		"dataset_id":         in.DatasetID,
		"record_id":          in.RecordID,
		"idempotency_key":    in.IdempotencyKey,
		"title":              in.Title,
		"tags":               append([]string(nil), in.Tags...),
		"data":               cloneJSONMap(in.Data),
		"occurred_at":        in.OccurredAt,
	}
}

func dataEventInputFromPayload(payload map[string]any) (DataEventInput, error) {
	in := DataEventInput{
		Source:         stringFromPayload(payload, "source"),
		EventType:      stringFromPayload(payload, "event_type"),
		Operation:      stringFromPayload(payload, "operation"),
		BusinessAction: stringFromPayload(payload, "business_action_id"),
		DatasetID:      stringFromPayload(payload, "dataset_id"),
		RecordID:       stringFromPayload(payload, "record_id"),
		IdempotencyKey: stringFromPayload(payload, "idempotency_key"),
		Title:          stringFromPayload(payload, "title"),
		OccurredAt:     stringFromPayload(payload, "occurred_at"),
	}
	if tags, ok := payload["tags"].([]any); ok {
		for _, tag := range tags {
			in.Tags = append(in.Tags, strings.TrimSpace(fmt.Sprint(tag)))
		}
	}
	if data, ok := payload["data"].(map[string]any); ok {
		in.Data = cloneJSONMap(data)
	}
	return in, nil
}

func stringFromPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (s *Service) duplicateEventResult(ctx context.Context, p Principal, fields []FieldDefinition, event *DataEventLog) (*DataEventResult, error) {
	result := &DataEventResult{
		Status:         "duplicate",
		Duplicate:      true,
		OriginalStatus: event.ResultStatus,
		Source:         event.Source,
		EventType:      event.EventType,
		Operation:      event.Operation,
		BusinessAction: event.BusinessAction,
		DatasetID:      event.DatasetID,
		RecordID:       event.RecordID,
		IdempotencyKey: event.IdempotencyKey,
		AppliedAt:      event.AppliedAt,
	}
	if event.RecordID != "" && event.ResultStatus != "deleted" && event.ResultStatus != "already_deleted" {
		record, err := s.store.GetRecord(ctx, p.TenantID, event.DatasetID, event.RecordID)
		if err != nil && !errors.Is(err, ErrRecordNotFound) {
			return nil, err
		}
		if record != nil {
			result.Record = maskSensitiveRecord(record, fields, p)
		}
	}
	return result, nil
}

func (s *Service) appendDataEventLog(ctx context.Context, p Principal, result *DataEventResult) error {
	if strings.TrimSpace(result.IdempotencyKey) == "" {
		return nil
	}
	_, err := s.store.AppendDataEventLog(ctx, DataEventLog{
		ID:             newID("event"),
		TenantID:       p.TenantID,
		Source:         result.Source,
		EventType:      result.EventType,
		Operation:      result.Operation,
		BusinessAction: result.BusinessAction,
		DatasetID:      result.DatasetID,
		RecordID:       result.RecordID,
		IdempotencyKey: result.IdempotencyKey,
		ResultStatus:   result.Status,
		CreatedBy:      eventActor(p, result.Source),
		AppliedAt:      result.AppliedAt,
	})
	return err
}

func eventActor(p Principal, source string) string {
	if strings.TrimSpace(p.UserID) != "" {
		return strings.TrimSpace(p.UserID)
	}
	return "event:" + strings.TrimSpace(source)
}
