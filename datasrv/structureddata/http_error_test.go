package structureddata

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestHTTPStatusForSentinelErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "unauthorized", err: ErrUnauthorized, status: http.StatusUnauthorized},
		{name: "forbidden", err: ErrForbidden, status: http.StatusForbidden},
		{name: "dataset not found", err: ErrDatasetNotFound, status: http.StatusNotFound},
		{name: "record not found", err: ErrRecordNotFound, status: http.StatusNotFound},
		{name: "backup not found", err: ErrBackupNotFound, status: http.StatusNotFound},
		{name: "already exists", err: ErrAlreadyExists, status: http.StatusConflict},
		{name: "invalid input", err: ErrInvalidInput, status: http.StatusBadRequest},
		{name: "unknown", err: fmt.Errorf("other failure"), status: http.StatusBadRequest},
		{name: "wrapped forbidden", err: fmt.Errorf("wrapped: %w", ErrForbidden), status: http.StatusForbidden},
		{name: "business invalid input", err: newBusinessError(ErrInvalidInput, "approval_not_pending", "approval is not pending"), status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpStatusForError(tc.err); got != tc.status {
				t.Fatalf("httpStatusForError(%v)=%d, want %d", tc.err, got, tc.status)
			}
			if businessErr, ok := tc.err.(*businessError); ok && !errors.Is(businessErr, ErrInvalidInput) {
				t.Fatalf("business error should wrap ErrInvalidInput: %v", businessErr)
			}
		})
	}
}
