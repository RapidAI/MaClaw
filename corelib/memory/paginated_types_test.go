package memory

import (
	"testing"
	"time"
)

func TestEncodeCursor_RoundTrip(t *testing.T) {
	cursorID := "cur_abc123"
	userID := "user_xyz"
	createdAt := time.Now()

	token := EncodeCursor(cursorID, userID, createdAt)
	if token == "" {
		t.Fatal("EncodeCursor returned empty token")
	}

	payload, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor failed: %v", err)
	}
	if payload.CursorID != cursorID {
		t.Errorf("CursorID mismatch: got %q, want %q", payload.CursorID, cursorID)
	}
	if payload.UserID != userID {
		t.Errorf("UserID mismatch: got %q, want %q", payload.UserID, userID)
	}
	if payload.CreatedAt != createdAt.Unix() {
		t.Errorf("CreatedAt mismatch: got %d, want %d", payload.CreatedAt, createdAt.Unix())
	}
}

func TestDecodeCursor_Expired(t *testing.T) {
	// Create a cursor that was made 10 minutes ago (beyond 5-min TTL).
	cursorID := "cur_expired"
	userID := "user_old"
	createdAt := time.Now().Add(-10 * time.Minute)

	token := EncodeCursor(cursorID, userID, createdAt)
	_, err := DecodeCursor(token)
	if err != ErrCursorExpired {
		t.Errorf("expected ErrCursorExpired, got: %v", err)
	}
}

func TestDecodeCursor_Empty(t *testing.T) {
	_, err := DecodeCursor("")
	if err != ErrCursorNotFound {
		t.Errorf("expected ErrCursorNotFound for empty token, got: %v", err)
	}
}

func TestDecodeCursor_Malformed(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err != ErrCursorExpired {
		t.Errorf("expected ErrCursorExpired for malformed token, got: %v", err)
	}
}

func TestDecodeCursor_InvalidJSON(t *testing.T) {
	// Valid base64 but not valid JSON.
	_, err := DecodeCursor("aGVsbG8gd29ybGQ") // "hello world" in base64
	if err != ErrCursorExpired {
		t.Errorf("expected ErrCursorExpired for invalid JSON, got: %v", err)
	}
}

func TestDecodeCursor_MissingFields(t *testing.T) {
	// Valid base64 + valid JSON but missing required fields.
	token := EncodeCursor("", "user_x", time.Now())
	_, err := DecodeCursor(token)
	if err != ErrCursorExpired {
		t.Errorf("expected ErrCursorExpired for missing CursorID, got: %v", err)
	}

	token2 := EncodeCursor("cur_x", "", time.Now())
	_, err2 := DecodeCursor(token2)
	if err2 != ErrCursorExpired {
		t.Errorf("expected ErrCursorExpired for missing UserID, got: %v", err2)
	}
}

func TestConstants_Values(t *testing.T) {
	// Verify constants match the spec requirements.
	if defaultMaxEntries != 12 {
		t.Errorf("defaultMaxEntries = %d, want 12", defaultMaxEntries)
	}
	if expandedMaxEntries != 24 {
		t.Errorf("expandedMaxEntries = %d, want 24", expandedMaxEntries)
	}
	if defaultMaxTokens != 2500 {
		t.Errorf("defaultMaxTokens = %d, want 2500", defaultMaxTokens)
	}
	if expandedMaxTokens != 5000 {
		t.Errorf("expandedMaxTokens = %d, want 5000", expandedMaxTokens)
	}
	if topicDensityThreshold != 0.15 {
		t.Errorf("topicDensityThreshold = %f, want 0.15", topicDensityThreshold)
	}
	if expansionFactor != 0.4 {
		t.Errorf("expansionFactor = %f, want 0.4", expansionFactor)
	}
	if exhaustiveMaxEntries != 100 {
		t.Errorf("exhaustiveMaxEntries = %d, want 100", exhaustiveMaxEntries)
	}
	if exhaustiveMaxTokens != 15000 {
		t.Errorf("exhaustiveMaxTokens = %d, want 15000", exhaustiveMaxTokens)
	}
	if cursorTTL != 5*time.Minute {
		t.Errorf("cursorTTL = %v, want 5m", cursorTTL)
	}
	if maxCursorsPerUser != 10 {
		t.Errorf("maxCursorsPerUser = %d, want 10", maxCursorsPerUser)
	}
	if scrollSessionMaxCache != 200 {
		t.Errorf("scrollSessionMaxCache = %d, want 200", scrollSessionMaxCache)
	}
	if perPageTokenBudget != 2500 {
		t.Errorf("perPageTokenBudget = %d, want 2500", perPageTokenBudget)
	}
}
