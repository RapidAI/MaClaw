package structureddata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkHTTPConcurrentQueryRecords(b *testing.B) {
	svc, p, datasetID := newBenchmarkRecordStore(b, 1500)
	server := NewHTTPServer(svc, "bench-token-0123456789012345", "test")
	body := []byte(`{"q":"renewal","tag":"finance","limit":25}`)
	path := "/api/v1/data/datasets/" + datasetID + "/records/query"
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer bench-token-0123456789012345")
			req.Header.Set("X-MaClaw-Tenant-ID", p.TenantID)
			req.Header.Set("X-MaClaw-User-ID", p.UserID)
			req.Header.Set("X-MaClaw-Role", p.Role)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				b.Fatalf("query records status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Body.Len() == 0 {
				b.Fatal("query records returned empty response body")
			}
		}
	})
}

func BenchmarkHTTPConcurrentBatchImportRecords(b *testing.B) {
	svc, p, datasetID := newBenchmarkImportHTTPStore(b)
	server := NewHTTPServer(svc, "bench-token-0123456789012345", "test")
	path := "/api/v1/data/datasets/" + datasetID + "/records/batch"
	var seq atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := seq.Add(1)
			body := benchmarkBatchImportBody(b, n, 5)
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer bench-token-0123456789012345")
			req.Header.Set("X-MaClaw-Tenant-ID", p.TenantID)
			req.Header.Set("X-MaClaw-User-ID", p.UserID)
			req.Header.Set("X-MaClaw-Role", p.Role)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				b.Fatalf("batch import status=%d body=%s", w.Code, w.Body.String())
			}
			var out BatchImportRecordsResult
			if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
				b.Fatalf("decode batch import response: %v", err)
			}
			if !out.Valid || out.Imported != 5 {
				b.Fatalf("unexpected batch import result: %#v", out)
			}
		}
	})
}

func BenchmarkHTTPExportJSONLRecords(b *testing.B) {
	svc, p, datasetID := newBenchmarkRecordStore(b, 1500)
	server := NewHTTPServer(svc, "bench-token-0123456789012345", "test")
	body := []byte(`{"q":"renewal","tag":"finance","limit":500}`)
	path := "/api/v1/data/datasets/" + datasetID + "/records/export.jsonl"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer bench-token-0123456789012345")
		req.Header.Set("X-MaClaw-Tenant-ID", p.TenantID)
		req.Header.Set("X-MaClaw-User-ID", p.UserID)
		req.Header.Set("X-MaClaw-Role", p.Role)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("export jsonl status=%d body=%s", w.Code, w.Body.String())
		}
		if w.Body.Len() == 0 {
			b.Fatal("export jsonl returned empty response body")
		}
	}
}

func BenchmarkHTTPRecoveryAndLongRunningJobWorkflow(b *testing.B) {
	svc, p, datasetID := newBenchmarkRecordStore(b, 250)
	server := NewHTTPServer(svc, "bench-token-0123456789012345", "test")
	workflowSeq := atomic.Int64{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runHTTPRecoveryAndLongRunningJobWorkflow(b, server, p, datasetID, workflowSeq.Add(1))
	}
}

func TestHTTPRecoveryAndLongRunningJobWorkflow(t *testing.T) {
	svc, p, datasetID := newBenchmarkRecordStore(t, 40)
	server := NewHTTPServer(svc, "bench-token-0123456789012345", "test")
	runHTTPRecoveryAndLongRunningJobWorkflow(t, server, p, datasetID, 1)
}

func runHTTPRecoveryAndLongRunningJobWorkflow(tb testing.TB, server *HTTPServer, p Principal, datasetID string, n int64) {
	tb.Helper()
	backup := benchmarkHTTPJSON[BackupInfo](tb, server, p, http.MethodPost, "/api/v1/data/backups", CreateBackupInput{
		Name: fmt.Sprintf("workflow-%06d", n),
		Note: "benchmark recovery checkpoint",
	}, http.StatusCreated)
	if backup.ID == "" || backup.SizeBytes == 0 || backup.SHA256 == "" {
		tb.Fatalf("unexpected backup: %#v", backup)
	}
	maintenance := benchmarkHTTPJSON[MaintenanceResult](tb, server, p, http.MethodPost, "/api/v1/data/maintenance/run", RunMaintenanceInput{
		Tasks: []string{"integrity_check", "optimize"},
	}, http.StatusOK)
	if !maintenance.Valid || len(maintenance.Tasks) != 2 {
		tb.Fatalf("unexpected maintenance result: %#v", maintenance)
	}
	importJob := benchmarkHTTPJSON[ImportJob](tb, server, p, http.MethodPost, "/api/v1/data/datasets/"+datasetID+"/records/import.jsonl/jobs", ImportJSONLInput{
		JSONLText: fmt.Sprintf("{\"id\":\"workflow-import-%06d\",\"title\":\"Workflow import %06d\",\"tags\":[\"workflow\"],\"data\":{\"amount\":%d,\"department\":\"ops\",\"customer\":\"WorkflowCo\"}}\n", n, n, n),
	}, http.StatusAccepted)
	importJob = benchmarkWaitImportJob(tb, server, p, importJob.ID)
	if importJob.Status != importJobStatusCompleted || importJob.Imported != 1 {
		tb.Fatalf("unexpected import job: %#v", importJob)
	}
	exportJob := benchmarkHTTPJSON[ExportJob](tb, server, p, http.MethodPost, "/api/v1/data/datasets/"+datasetID+"/records/export.jsonl/jobs", StartExportJobInput{
		QueryRecordsInput: QueryRecordsInput{Tag: "workflow", Limit: 25},
	}, http.StatusAccepted)
	exportJob = benchmarkWaitExportJob(tb, server, p, exportJob.ID)
	if exportJob.Status != exportJobStatusCompleted || exportJob.Bytes == 0 || exportJob.DownloadPath == "" {
		tb.Fatalf("unexpected export job: %#v", exportJob)
	}
	download := benchmarkHTTPRequest(tb, server, p, http.MethodGet, "/api/v1/data/export-jobs/"+exportJob.ID+"/download", nil, http.StatusOK)
	if download.Body.Len() == 0 {
		tb.Fatal("export job download returned empty body")
	}
	backupDownload := benchmarkHTTPRequest(tb, server, p, http.MethodGet, "/api/v1/data/backups/"+backup.ID+"/download", nil, http.StatusOK)
	if backupDownload.Body.Len() == 0 || backupDownload.Header().Get("X-Content-Type-Options") != "nosniff" {
		tb.Fatalf("unexpected backup download headers/body: headers=%v len=%d", backupDownload.Header(), backupDownload.Body.Len())
	}
}

func newBenchmarkImportHTTPStore(tb testing.TB) (*Service, Principal, string) {
	tb.Helper()
	svc, p, datasetID := newBenchmarkRecordStore(tb, 0)
	if _, err := svc.UpsertFields(tb.Context(), p, datasetID, UpsertFieldsInput{Fields: []FieldDefinition{
		{Key: "ticket_no", Type: "string", Indexed: true},
		{Key: "amount", Type: "number", Indexed: true},
		{Key: "customer", Type: "string", Indexed: true},
	}}); err != nil {
		tb.Fatalf("UpsertFields import benchmark: %v", err)
	}
	return svc, p, datasetID
}

func benchmarkBatchImportBody(tb testing.TB, batch int64, count int) []byte {
	tb.Helper()
	in := BatchImportRecordsInput{Records: make([]BatchRecordInput, 0, count)}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("http_import_%06d_%02d", batch, i)
		in.Records = append(in.Records, BatchRecordInput{
			ID:    id,
			Title: "HTTP import " + id,
			Tags:  []string{"http", "load"},
			Data: map[string]any{
				"ticket_no": id,
				"amount":    int(batch)*10 + i,
				"customer":  fmt.Sprintf("Customer %03d", (int(batch)+i)%250),
			},
		})
	}
	data, err := json.Marshal(in)
	if err != nil {
		tb.Fatalf("marshal batch import body: %v", err)
	}
	return data
}

func benchmarkWaitImportJob(tb testing.TB, server *HTTPServer, p Principal, jobID string) ImportJob {
	tb.Helper()
	for i := 0; i < 80; i++ {
		job := benchmarkHTTPJSON[ImportJob](tb, server, p, http.MethodGet, "/api/v1/data/import-jobs/"+jobID, nil, http.StatusOK)
		if job.Status == importJobStatusCompleted || job.Status == importJobStatusFailed {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	tb.Fatalf("import job %s did not finish", jobID)
	return ImportJob{}
}

func benchmarkWaitExportJob(tb testing.TB, server *HTTPServer, p Principal, jobID string) ExportJob {
	tb.Helper()
	for i := 0; i < 80; i++ {
		job := benchmarkHTTPJSON[ExportJob](tb, server, p, http.MethodGet, "/api/v1/data/export-jobs/"+jobID, nil, http.StatusOK)
		if job.Status == exportJobStatusCompleted || job.Status == exportJobStatusFailed {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	tb.Fatalf("export job %s did not finish", jobID)
	return ExportJob{}
}

func benchmarkHTTPJSON[T any](tb testing.TB, server *HTTPServer, p Principal, method, path string, body any, wantStatus int) T {
	tb.Helper()
	w := benchmarkHTTPRequest(tb, server, p, method, path, body, wantStatus)
	var out T
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		tb.Fatalf("%s %s decode response: %v body=%s", method, path, err, w.Body.String())
	}
	return out
}

func benchmarkHTTPRequest(tb testing.TB, server *HTTPServer, p Principal, method, path string, body any, wantStatus int) *httptest.ResponseRecorder {
	tb.Helper()
	var rbody *bytes.Reader
	if body == nil {
		rbody = bytes.NewReader(nil)
	} else if raw, ok := body.([]byte); ok {
		rbody = bytes.NewReader(raw)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("%s %s marshal request: %v", method, path, err)
		}
		rbody = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, rbody)
	req.Header.Set("Authorization", "Bearer bench-token-0123456789012345")
	req.Header.Set("X-MaClaw-Tenant-ID", p.TenantID)
	req.Header.Set("X-MaClaw-User-ID", p.UserID)
	req.Header.Set("X-MaClaw-Role", p.Role)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != wantStatus {
		tb.Fatalf("%s %s status=%d want=%d body=%s", method, path, w.Code, wantStatus, w.Body.String())
	}
	return w
}
