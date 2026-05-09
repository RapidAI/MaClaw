package structureddata

import "strings"

const maskedValue = "***MASKED***"

func canViewSensitive(p Principal) bool {
	return principalCanReadSensitive(p) || strings.EqualFold(strings.TrimSpace(p.Role), "admin") || strings.EqualFold(strings.TrimSpace(p.Role), "auditor")
}

func maskSensitiveRecord(record *Record, fields []FieldDefinition, p Principal) *Record {
	if record == nil || canViewSensitive(p) {
		return record
	}
	sensitive := sensitiveFieldSet(fields)
	if len(sensitive) == 0 {
		return record
	}
	clone := *record
	clone.Data = cloneJSONMap(record.Data)
	for key := range sensitive {
		if _, ok := clone.Data[key]; ok {
			clone.Data[key] = maskedValue
		}
	}
	return &clone
}

func maskSensitiveRecords(records []Record, fields []FieldDefinition, p Principal) []Record {
	if canViewSensitive(p) || len(records) == 0 {
		return records
	}
	sensitive := sensitiveFieldSet(fields)
	if len(sensitive) == 0 {
		return records
	}
	out := make([]Record, len(records))
	for i := range records {
		out[i] = records[i]
		out[i].Data = cloneJSONMap(records[i].Data)
		for key := range sensitive {
			if _, ok := out[i].Data[key]; ok {
				out[i].Data[key] = maskedValue
			}
		}
	}
	return out
}

func sensitiveFieldSet(fields []FieldDefinition) map[string]struct{} {
	out := map[string]struct{}{}
	for _, field := range fields {
		if field.Sensitive && strings.TrimSpace(field.Key) != "" {
			out[strings.TrimSpace(field.Key)] = struct{}{}
		}
	}
	return out
}
