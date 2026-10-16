// Package toolid defines the explicit ToolID namespace convention for
// FIRST-PARTY tools (docs/design/tool-routing-improvement-plan-zh.md Phase 2,
// decision R1/R2). A ToolID is "{namespace}:{name}" — e.g. "core:bash" — and
// is the canonical identity carried through registration, dispatch, and
// authorization for tools whose provenance the host controls.
//
// Deliberate boundary: third-party/dynamic MCP and skill adapters do NOT get
// ToolIDs and keep their opaque adapter names (R1 — server-controlled names
// must never enter the host namespace) and grant-token model names (R2 —
// one-shot grants stay keyed by rendered name). This package is only for the
// first-party catalog.
package toolid

import (
	"fmt"
	"strings"
)

// Well-known first-party namespaces.
const (
	// NamespaceCore holds built-in host tools (bash, read_file, ...).
	NamespaceCore = "core"
	// NamespaceWorkflow holds workflow-owned tools.
	NamespaceWorkflow = "workflow"
	// NamespaceCoding holds coding-workbench tools.
	NamespaceCoding = "coding"
)

// segmentCharset is the allowed character set for namespace and name
// segments (mirrors the wire-name discipline: no whitespace, no colons, no
// characters that could smuggle prompt syntax).
const segmentCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

// ToolID is a validated first-party tool identity.
type ToolID struct {
	namespace string
	name      string
}

// Parse validates and splits a "{namespace}:{name}" or bare "{name}" ID. A
// bare name is normalized to NamespaceCore. At most one colon is allowed;
// empty segments and characters outside segmentCharset are rejected, so a
// ToolID value can never carry attacker-influenced text beyond the charset.
func Parse(id string) (ToolID, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ToolID{}, fmt.Errorf("toolid: empty id")
	}
	if strings.Count(id, ":") > 1 {
		return ToolID{}, fmt.Errorf("toolid: at most one colon allowed in %q", id)
	}
	ns, name := NamespaceCore, id
	if i := strings.Index(id, ":"); i >= 0 {
		ns, name = id[:i], id[i+1:]
	}
	if !validSegment(ns) {
		return ToolID{}, fmt.Errorf("toolid: invalid namespace segment %q", ns)
	}
	if !validSegment(name) {
		return ToolID{}, fmt.Errorf("toolid: invalid name segment %q", name)
	}
	return ToolID{namespace: ns, name: name}, nil
}

// MustParse is Parse for constants and tests; panics on invalid input.
func MustParse(id string) ToolID {
	t, err := Parse(id)
	if err != nil {
		panic(err)
	}
	return t
}

func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(segmentCharset, r) {
			return false
		}
	}
	return true
}

// New builds a ToolID from parts (convenience over MustParse(fmt) — same
// validation).
func New(namespace, name string) (ToolID, error) {
	return Parse(namespace + ":" + name)
}

// Namespace returns the ID's namespace.
func (t ToolID) Namespace() string { return t.namespace }

// Name returns the ID's bare name (the segment after the colon).
func (t ToolID) Name() string { return t.name }

// String renders the canonical "{namespace}:{name}" form.
func (t ToolID) String() string { return t.namespace + ":" + t.name }

// IsZero reports whether t is the zero value.
func (t ToolID) IsZero() bool { return t.namespace == "" && t.name == "" }
