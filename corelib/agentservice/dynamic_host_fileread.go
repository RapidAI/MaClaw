package agentservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostFileReadProviderID     = "core-fileread"
	reviewedHostFileReadImplementation = "local"
	reviewedHostFileReadAdapterName    = "host_fs_read_local"
)

func reviewedHostFileReadInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":  map[string]interface{}{"type": "string"},
			"query": map[string]interface{}{"type": "string"},
			// file_pattern names an outcome the other two cannot reach: which
			// files exist under this name shape. It is not the legacy
			// search_files knob of the same name, and it carries none of that
			// tool's other arguments.
			"file_pattern": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostFileReadContractDigest() string {
	return coretool.SchemaDigest([]byte("fs.read.local:v3:host-fileread"))
}

// ProjectReviewedHostFileReadProvider projects the host-owned workspace
// filesystem inspect. It is not a Skill/MCP discovery entry and must not
// import GUI read_file / list_directory / search_files. The closed schema
// accepts optional path, query, and file_pattern. Empty path lists the
// workspace root; query searches workspace contents; file_pattern locates
// files by name and narrows a query to the files it matches. Office/PDF files
// use the native document reader; the filesystem type decides, not user
// keywords. This is not knowledge.read.local. Writes, channel, and destination
// are rejected.
func ProjectReviewedHostFileReadProvider(reader reviewedHostFileReader) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if reader == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host file reader is unavailable")
	}
	parameters := reviewedHostFileReadInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host file read schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostFileReadContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-fileread-path-query-pattern-v3", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostFileReadAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostFileReadProviderID,
			ImplementationID: reviewedHostFileReadImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityFileRead,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostFileRead(reader)}, nil
}

func AttachReviewedHostFileReadProvider(catalog DynamicSemanticCatalog, reader reviewedHostFileReader) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostFileReadProvider(reader)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

// reviewedHostFileReadLocated turns a name walk into either its matches or a
// failure.
//
// The walk states trouble by putting the reason in Text and marking Outcome,
// so a caller that reads only Text hands that reason back as though it were
// the list of matching files -- and "no such pattern" then reads to the model
// exactly like "no such file". Finding nothing is a different fact: it is a
// complete answer, and must stay a success. The GUI carries its own copy of
// this guard; the two hosts share the walk, not the judgement about it.
func reviewedHostFileReadLocated(found agent.SearchToolResult) (string, error) {
	if found.Outcome == agent.SearchToolOutcomeError {
		return "", fmt.Errorf("host_file_read_locate_failed")
	}
	return found.Text, nil
}

func executeReviewedHostFileRead(reader reviewedHostFileReader) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if reader == nil {
			return "", fmt.Errorf("host_file_read_unavailable")
		}
		if len(args) > 3 {
			return "", fmt.Errorf("host_file_read_arguments_rejected")
		}
		path, query, filePattern := "", "", ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_file_read_arguments_rejected")
			}
			switch key {
			case "path":
				path = value
			case "query":
				query = value
			case "file_pattern":
				filePattern = value
			default:
				return "", fmt.Errorf("host_file_read_arguments_rejected")
			}
		}
		return reader.ReadReviewedHostFile(ctx, principal, strings.TrimSpace(path), strings.TrimSpace(query), strings.TrimSpace(filePattern))
	}
}

func (c *coreAgentCallbacks) ReadReviewedHostFile(ctx context.Context, principal Principal, path, query, filePattern string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_file_read_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_file_read_principal_mismatch")
	}
	path = strings.TrimSpace(path)
	query = strings.TrimSpace(query)
	filePattern = strings.TrimSpace(filePattern)
	if path == "" {
		path = "."
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	absPath, err := c.resolveWorkspacePath(path)
	if err != nil {
		return "", err
	}
	if query != "" {
		// Given both, the name shape narrows the content search rather than
		// describing a second, separate search.
		found := coretool.SearchFilesInProjectCtx(ctx, absPath, query, filePattern)
		// The search reports a cut-short walk only in its prose, and a
		// truncated result reads exactly like an exhaustive one that found
		// less. Re-checking the deadline asks the same question the search
		// asked, rather than trusting it to say so in words.
		if ctx != nil && ctx.Err() != nil {
			return "", fmt.Errorf("host_file_read_search_incomplete")
		}
		return found, nil
	}
	if filePattern != "" {
		// Locating files by name was the one thing this surface could not do:
		// path lists a single directory and query reads contents, so a plan
		// holding only fs.read.local had no way to discover what to read. The
		// walk is the reviewed one the legacy tool uses, so the model gets no
		// knob to widen matching, exclusions, or result bounds.
		return reviewedHostFileReadLocated(agent.ToolGlobDetailedCtx(ctx, map[string]interface{}{"pattern": filePattern, "path": absPath}))
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("file not found or inaccessible: %w", err)
	}
	if info.IsDir() {
		// The listing states trouble in prose the same way the walk does, and
		// the caller-side checks above cannot stand in for its own: the
		// directory can vanish between the stat and the read.
		listed, err := c.listDirectoryDetailed(map[string]interface{}{"path": path})
		if err != nil {
			return "", err
		}
		return listed, nil
	}
	if reviewedHostFileReadUsesDocumentReader(absPath) {
		document := c.executeReviewedHostDocumentRead(absPath)
		if class, failed := agent.DocumentReadFailure(document); failed {
			return "", fmt.Errorf("host_document_read_failed_%s", class)
		}
		return document, nil
	}
	if reviewedHostFileReadUsesTailDefault(absPath) {
		return c.executeReadFile(map[string]interface{}{
			"path":   path,
			"offset": float64(srvReadFileMaxLines),
		}), nil
	}
	read, err := c.readFileDetailed(map[string]interface{}{"path": path})
	if err != nil {
		return "", err
	}
	return read, nil
}

func reviewedHostFileReadUsesDocumentReader(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx":
		return true
	default:
		return false
	}
}

func reviewedHostFileReadUsesTailDefault(path string) bool {
	return strings.ToLower(filepath.Ext(strings.TrimSpace(path))) == ".log"
}

func (c *coreAgentCallbacks) executeReviewedHostDocumentRead(absPath string) string {
	args := map[string]interface{}{"file_path": absPath}
	contextTokens := 0
	if c != nil {
		contextTokens = c.llmCfg.EffectiveContextTokens()
	}
	var config agent.OfficeReadConfig
	if c != nil {
		config = officeReadConfigFromAppConfig(c.appCfg)
	}
	return agent.ToolReadDocumentWithOfficeReadConfigAndContext(args, config, contextTokens)
}
