package main

import (
	"encoding/json"
	"strings"
)

type craftAPIErrorKind int

const (
	craftAPIErrorNone craftAPIErrorKind = iota
	craftAPIErrorCode1234
	craftAPIErrorCode1234Transient
	craftAPIErrorResponse
	craftAPIErrorRateLimit
)

func classifyCraftAPIError(message string) craftAPIErrorKind {
	payload, hasPayload := parseCraftAPIErrorPayload(message)
	if hasPayload && payload.Code == "1234" {
		if strings.Contains(message, "缂冩垹绮堕柨娆掝嚖") {
			return craftAPIErrorCode1234Transient
		}
		return craftAPIErrorCode1234
	}
	if hasPayload && payload.Type == "error" {
		return craftAPIErrorResponse
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "http 429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") || strings.Contains(message, "429") {
		return craftAPIErrorRateLimit
	}
	return craftAPIErrorNone
}

func hasCraftAPIErrorCode1234(message string) bool {
	payload, ok := parseCraftAPIErrorPayload(message)
	return ok && payload.Code == "1234"
}

type craftAPIErrorPayload struct {
	Type string
	Code string
}

func parseCraftAPIErrorPayload(message string) (craftAPIErrorPayload, bool) {
	var wire struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Error *struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(message), &wire); err != nil {
		return craftAPIErrorPayload{}, false
	}
	payload := craftAPIErrorPayload{Type: wire.Type, Code: wire.Code}
	if wire.Error != nil {
		if payload.Type == "" {
			payload.Type = wire.Error.Type
		}
		if payload.Code == "" {
			payload.Code = wire.Error.Code
		}
	}
	return payload, payload.Type != "" || payload.Code != ""
}
