package structureddata

import (
	"encoding/csv"
	"io"
)

func writeAuditLogsCSV(w io.Writer, items []AuditLog) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"id", "created_at", "tenant_id", "user_id", "action", "dataset_id", "target_type", "target_id", "summary", "metadata_json"}); err != nil {
		return err
	}
	for _, item := range items {
		if err := writer.Write([]string{
			item.ID,
			formatTime(item.CreatedAt),
			item.TenantID,
			item.UserID,
			item.Action,
			item.DatasetID,
			item.TargetType,
			item.TargetID,
			item.Summary,
			jsonString(item.Metadata),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
