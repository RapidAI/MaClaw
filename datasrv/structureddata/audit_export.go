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
			sanitizeCSVCell(item.ID),
			formatTime(item.CreatedAt),
			sanitizeCSVCell(item.TenantID),
			sanitizeCSVCell(item.UserID),
			sanitizeCSVCell(item.Action),
			sanitizeCSVCell(item.DatasetID),
			sanitizeCSVCell(item.TargetType),
			sanitizeCSVCell(item.TargetID),
			sanitizeCSVCell(item.Summary),
			sanitizeCSVCell(jsonString(item.Metadata)),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
