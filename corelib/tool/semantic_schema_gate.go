package tool

import (
	"fmt"
	"sort"
	"strings"
)

// The design fixes one closure for model-supplied parameters: a model may not
// write provider, server, tool, selection, credential, artifact location,
// receipt, policy or confirmation fields, may not substitute a server-bound
// identifier with an arbitrary *_id, and may not hand a provider a raw local
// path. Each adapter used to re-state that rule in its own hand-written test,
// which meant a new managed adapter silently started with no such test at all.
// This gate is the generic form: it reads a canonical invocation schema and
// reports every field that crosses the closure, so the per-capability tests
// become a reviewed exception list instead of the only line of defence.

// Stable gate codes. They are diagnostic identifiers for repository tests, not
// model-visible reason codes.
const (
	SchemaGateReservedField   = "reserved_field"
	SchemaGateIdentifierField = "identifier_field"
	SchemaGateLocationField   = "location_field"
	SchemaGateOpenObject      = "open_object"
	SchemaGateMissingSchema   = "missing_schema"
)

// SchemaGateFinding is one place where a model-facing schema crosses the
// parameter-authorization closure. Pointer is the dotted position inside the
// schema so a nested array element is distinguishable from a root field.
type SchemaGateFinding struct {
	Pointer string
	Field   string
	Code    string
}

func (f SchemaGateFinding) String() string {
	if f.Field == "" {
		return fmt.Sprintf("%s at %s", f.Code, f.Pointer)
	}
	return fmt.Sprintf("%s %q at %s", f.Code, f.Field, f.Pointer)
}

// reservedModelParameterFields are names that name a server-bound decision.
// A managed adapter receives these from its binding, grant and broker; a model
// that can write them can redirect the call, forge authority, or address an
// artifact the plan never authorized.
var reservedModelParameterFields = map[string]bool{
	"provider": true, "providers": true, "server": true, "servers": true,
	"tool": true, "tools": true, "function": true, "adapter": true,
	"implementation": true, "binding": true, "selection": true, "plan": true,
	"revision": true, "skill": true, "skills": true, "mcp": true,
	"credential": true, "credentials": true, "secret": true, "secrets": true,
	"token": true, "password": true, "apikey": true, "api_key": true,
	"auth": true, "authorization": true, "access_token": true, "signature": true,
	"grant": true, "nonce": true, "receipt": true, "policy": true,
	"confirmation": true, "confirm": true, "confirmed": true, "approval": true,
	"artifact": true, "artifacts": true, "artifact_ref": true, "artifactref": true,
	"principal": true, "tenant": true, "session": true, "scope": true,
	"effect": true, "effects": true, "capability": true, "digest": true,
}

// locationBearingFields address something outside the call: a filesystem
// location or a network endpoint. They are not unconditionally illegal, but
// each one must be resolved by a trusted server-side canonicalizer rather than
// passed to a provider as written, so every occurrence needs a review.
var locationBearingFields = map[string]bool{
	"path": true, "paths": true, "file": true, "files": true,
	"file_path": true, "filepath": true, "filename": true, "file_name": true,
	"dir": true, "directory": true, "folder": true, "cwd": true,
	"workdir": true, "working_dir": true, "working_directory": true,
	"output_path": true, "target_path": true, "source_path": true,
	"destination": true, "destination_path": true, "dest": true,
	"url": true, "uri": true, "href": true, "endpoint": true,
	"address": true, "host": true, "hostname": true, "location": true,
	// Where a provider writes is as much a location as what it reads. The
	// legacy downloader accepted five spellings of its destination, so a
	// single canonical name is not enough to hold this boundary.
	"save_path": true, "savepath": true, "save_to": true, "save_dir": true,
	"save_directory": true, "output_dir": true, "output_directory": true,
	"output_file": true, "input_path": true, "download_path": true,
	"upload_path": true, "local_path": true, "remote_path": true,
	"abs_path": true, "abspath": true, "full_path": true, "fullpath": true,
	"relative_path": true, "rel_path": true, "pathname": true,
	"src_path": true, "dst_path": true, "dest_path": true, "base_dir": true,
	"basedir": true, "root_dir": true, "write_to": true, "read_from": true,
	// Network endpoints under other spellings.
	"urls": true, "link": true, "links": true, "base_url": true,
	"baseurl": true, "webhook": true, "webhook_url": true,
	"callback_url": true, "origin": true, "ip": true, "ip_address": true,
}

// InspectManagedInvocationSchema walks one canonical invocation schema and
// returns every closure crossing, sorted for stable comparison against a
// reviewed baseline. A nil schema is itself a finding: a managed adapter
// without a schema has no closed parameter surface at all.
func InspectManagedInvocationSchema(schema map[string]interface{}) []SchemaGateFinding {
	if schema == nil {
		return []SchemaGateFinding{{Pointer: ".", Code: SchemaGateMissingSchema}}
	}
	var findings []SchemaGateFinding
	inspectSchemaNode(schema, "", &findings)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Pointer != findings[j].Pointer {
			return findings[i].Pointer < findings[j].Pointer
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}

func inspectSchemaNode(node map[string]interface{}, pointer string, findings *[]SchemaGateFinding) {
	properties, hasProperties := node["properties"].(map[string]interface{})
	declaredType, _ := node["type"].(string)
	if declaredType == "object" || hasProperties {
		// An absent additionalProperties is an open object in JSON Schema, and
		// an explicit true is open as well. Only an explicit false closes it.
		// A closed object without properties needs no separate finding: it
		// accepts nothing, which is the safest surface an adapter can publish.
		if !hasClosedAdditionalProperties(node) {
			*findings = append(*findings, SchemaGateFinding{Pointer: pointerOrRoot(pointer), Code: SchemaGateOpenObject})
		}
	}
	for field, raw := range properties {
		child := joinSchemaPointer(pointer, field)
		if code := classifyModelParameterField(field); code != "" {
			*findings = append(*findings, SchemaGateFinding{Pointer: child, Field: field, Code: code})
		}
		if nested, ok := raw.(map[string]interface{}); ok {
			inspectSchemaNode(nested, child, findings)
		}
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		inspectSchemaNode(items, pointer+"[]", findings)
	}
}

func hasClosedAdditionalProperties(node map[string]interface{}) bool {
	closed, ok := node["additionalProperties"].(bool)
	return ok && !closed
}

func pointerOrRoot(pointer string) string {
	if pointer == "" {
		return "."
	}
	return pointer
}

func joinSchemaPointer(pointer, field string) string {
	if pointer == "" {
		return field
	}
	return pointer + "." + field
}

// classifyModelParameterField returns the gate code for one property name, or
// an empty string when the name carries no server-bound meaning.
func classifyModelParameterField(field string) string {
	normalized := strings.ToLower(strings.TrimSpace(field))
	if normalized == "" {
		return SchemaGateReservedField
	}
	if reservedModelParameterFields[normalized] {
		return SchemaGateReservedField
	}
	if normalized == "id" || strings.HasSuffix(normalized, "_id") {
		return SchemaGateIdentifierField
	}
	if strings.HasSuffix(normalized, "id") && reservedIdentifierStem(normalized) {
		return SchemaGateIdentifierField
	}
	if locationBearingFields[normalized] {
		return SchemaGateLocationField
	}
	return ""
}

// reservedIdentifierStem keeps names such as "valid" or "grid" out of the
// identifier rule while still catching concatenated forms like "chatid".
func reservedIdentifierStem(normalized string) bool {
	stem := strings.TrimSuffix(normalized, "id")
	if len(stem) < 3 {
		return false
	}
	return reservedModelParameterFields[stem] || locationBearingFields[stem] ||
		stem == "chat" || stem == "user" || stem == "group" || stem == "room" ||
		stem == "message" || stem == "task" || stem == "turn"
}
