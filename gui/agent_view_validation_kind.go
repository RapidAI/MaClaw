package main

import "strings"

type agentViewValidationCodeKind string

const (
	agentViewValidationCodeUnknown         agentViewValidationCodeKind = ""
	agentViewValidationCodeMissingRequired agentViewValidationCodeKind = "missing_required"
)

func normalizeAgentViewValidationCodeKind(value string) agentViewValidationCodeKind {
	switch agentViewValidationCodeKind(strings.TrimSpace(value)) {
	case agentViewValidationCodeMissingRequired:
		return agentViewValidationCodeMissingRequired
	default:
		return agentViewValidationCodeUnknown
	}
}

func (kind agentViewValidationCodeKind) IsMissingRequired() bool {
	return kind == agentViewValidationCodeMissingRequired
}

type agentViewSchemaFormatKind string

const (
	agentViewSchemaFormatUnknown      agentViewSchemaFormatKind = ""
	agentViewSchemaFormatEmail        agentViewSchemaFormatKind = "email"
	agentViewSchemaFormatURI          agentViewSchemaFormatKind = "uri"
	agentViewSchemaFormatURL          agentViewSchemaFormatKind = "url"
	agentViewSchemaFormatURIReference agentViewSchemaFormatKind = "uri-reference"
	agentViewSchemaFormatUUID         agentViewSchemaFormatKind = "uuid"
	agentViewSchemaFormatDate         agentViewSchemaFormatKind = "date"
	agentViewSchemaFormatDateTime     agentViewSchemaFormatKind = "date-time"
	agentViewSchemaFormatDatetime     agentViewSchemaFormatKind = "datetime"
	agentViewSchemaFormatPassword     agentViewSchemaFormatKind = "password"
	agentViewSchemaFormatSecret       agentViewSchemaFormatKind = "secret"
	agentViewSchemaFormatToken        agentViewSchemaFormatKind = "token"
)

func normalizeAgentViewSchemaFormatKind(value string) agentViewSchemaFormatKind {
	switch agentViewSchemaFormatKind(strings.ToLower(strings.TrimSpace(value))) {
	case agentViewSchemaFormatEmail:
		return agentViewSchemaFormatEmail
	case agentViewSchemaFormatURI:
		return agentViewSchemaFormatURI
	case agentViewSchemaFormatURL:
		return agentViewSchemaFormatURL
	case agentViewSchemaFormatURIReference:
		return agentViewSchemaFormatURIReference
	case agentViewSchemaFormatUUID:
		return agentViewSchemaFormatUUID
	case agentViewSchemaFormatDate:
		return agentViewSchemaFormatDate
	case agentViewSchemaFormatDateTime:
		return agentViewSchemaFormatDateTime
	case agentViewSchemaFormatDatetime:
		return agentViewSchemaFormatDatetime
	case agentViewSchemaFormatPassword:
		return agentViewSchemaFormatPassword
	case agentViewSchemaFormatSecret:
		return agentViewSchemaFormatSecret
	case agentViewSchemaFormatToken:
		return agentViewSchemaFormatToken
	default:
		return agentViewSchemaFormatUnknown
	}
}

func (kind agentViewSchemaFormatKind) IsSensitive() bool {
	switch kind {
	case agentViewSchemaFormatPassword, agentViewSchemaFormatSecret, agentViewSchemaFormatToken:
		return true
	default:
		return false
	}
}

func (kind agentViewSchemaFormatKind) IsURLLike() bool {
	return kind == agentViewSchemaFormatURI || kind == agentViewSchemaFormatURL || kind == agentViewSchemaFormatURIReference
}

func (kind agentViewSchemaFormatKind) IsDateTime() bool {
	return kind == agentViewSchemaFormatDateTime || kind == agentViewSchemaFormatDatetime
}
