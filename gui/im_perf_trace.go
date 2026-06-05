package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

func imRequestID(msg IMUserMessage) string {
	return strings.TrimSpace(msg.RequestID)
}

func imPerfLog(stage string, startedAt time.Time, requestID, userID string, fields ...any) {
	if startedAt.IsZero() {
		return
	}
	elapsed := time.Since(startedAt).Round(time.Millisecond)
	var suffix strings.Builder
	for i := 0; i+1 < len(fields); i += 2 {
		key := strings.TrimSpace(fmt.Sprint(fields[i]))
		if key == "" {
			continue
		}
		suffix.WriteString(" ")
		suffix.WriteString(key)
		suffix.WriteString("=")
		suffix.WriteString(fmt.Sprintf("%q", fmt.Sprint(fields[i+1])))
	}
	log.Printf("[perf] stage=%s request_id=%q user=%q elapsed=%s%s", stage, strings.TrimSpace(requestID), strings.TrimSpace(userID), elapsed, suffix.String())
}
