package agentservice

import (
	"context"
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostKnowledgeProviderID     = "core-knowledge"
	reviewedHostKnowledgeImplementation = "local"
	reviewedHostKnowledgeAdapterName    = "host_knowledge_read_local"
)

func reviewedHostKnowledgeInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func reviewedHostKnowledgeContractDigest() string {
	return coretool.SchemaDigest([]byte("knowledge.read.local:v1:host-knowledge"))
}

// ProjectReviewedHostKnowledgeProvider projects the host-owned knowledge-store
// read. It is not a Skill/MCP discovery entry and must not import the GUI
// knowledge write/admin catalog. The closed schema accepts only query.
func ProjectReviewedHostKnowledgeProvider(searcher reviewedHostKnowledgeSearcher) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if searcher == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host knowledge searcher is unavailable")
	}
	parameters := reviewedHostKnowledgeInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host knowledge schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostKnowledgeContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-knowledge-query-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostKnowledgeAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostKnowledgeProviderID,
			ImplementationID: reviewedHostKnowledgeImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityKnowledgeRead,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostKnowledge(searcher)}, nil
}

func AttachReviewedHostKnowledgeProvider(catalog DynamicSemanticCatalog, searcher reviewedHostKnowledgeSearcher) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostKnowledgeProvider(searcher)
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

func executeReviewedHostKnowledge(searcher reviewedHostKnowledgeSearcher) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if searcher == nil {
			return "", fmt.Errorf("host_knowledge_unavailable")
		}
		query, _ := args["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return "", fmt.Errorf("host_knowledge_query_required")
		}
		if len(args) != 1 {
			return "", fmt.Errorf("host_knowledge_arguments_rejected")
		}
		return searcher.SearchReviewedHostKnowledge(ctx, principal, query)
	}
}

func (c *coreAgentCallbacks) reviewedHostOwnedServices() reviewedHostOwnedServices {
	if c == nil {
		return reviewedHostOwnedServices{}
	}
	out := reviewedHostOwnedServices{}
	if c.knowledgeStore != nil || strings.TrimSpace(c.dataDir) != "" {
		out.Knowledge = c
	}
	if c.knowledgeStore != nil {
		out.KnowledgeWrite = c
		out.KnowledgeAdmin = c
	}
	if c.memory != nil {
		out.Memory = c
	}
	if c.tasks != nil {
		out.Task = c
	}
	if c.goals != nil {
		out.Goal = c
	}
	if c.templates != nil {
		out.Template = c
	}
	if c.schedules != nil {
		out.Schedule = c
	}
	if dest := strings.TrimSpace(c.trustedDestinationID); reviewedHostTrustedDestination(dest) {
		out.ScheduleDispatch = c
		out.MessageSend = c
		out.DestinationID = dest
		if strings.TrimSpace(c.workspace) != "" {
			out.FileDeliver = c
		}
		if c.reviewedHostDocument != nil || c.reviewedHostImage != nil || c.reviewedHostVoice != nil || c.reviewedHostGenerate || c.reviewedHostAudioRender || c.reviewedHostVisualCapture {
			out.AttachmentDeliver = c
		}
	}
	if c.configManager != nil {
		out.Config = c.configManager
	}
	out.Session = c
	if c.auditReader != nil {
		out.Audit = c.auditReader
	}
	out.WebFetch = c
	if strings.TrimSpace(c.workspace) != "" {
		out.FileRead = c
		out.FileWrite = c
		out.FileDownload = c
		out.OfficeWrite = c
		out.DocumentGenerate = c
		out.RepoInspect = c
		if !c.delegateChild && !c.runtimeReadOnlyChild {
			out.RepoMutate = c
			// build.verify.local runs a reviewed argv from a closed table, so
			// it is deliberately not gated on canUseLocalBash: an instance that
			// withholds the shell is exactly the case this capability exists to
			// serve (§11.56), and reusing the shell gate would make the
			// narrowed grant unobtainable wherever it matters most. It is still
			// withheld from read-only and delegate children, because running a
			// build executes project code.
			out.BuildVerify = c
		}
		if c.canUseLocalBash() {
			out.Shell = c
		}
	}
	if reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
		out.AudioRender = c
	}
	if reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) && reviewedHostSpeechPlayerReady(c.speechPlayer) {
		out.AudioSynthesize = c
	}
	if reviewedHostDesktopCapturerReady(c.desktopCapturer) {
		out.VisualCapture = c
	}
	if strings.TrimSpace(c.workspace) != "" && reviewedHostDocumentLauncherReady(c.documentLauncher) {
		out.SystemLaunch = c
	}
	if reviewedHostURLLauncherReady(c.urlLauncher) {
		out.URLLaunch = c
	}
	if c.delegateSubtask != nil && !c.delegateChild && !c.runtimeReadOnlyChild {
		out.Delegate = c
	}
	if !c.delegateChild && !c.runtimeReadOnlyChild {
		if c.trustedSSH != nil || reviewedHostSingleBoundSSHSession(c) != nil {
			out.SSH = c
		}
		if c.trustedBrowser != nil {
			out.Browser = c
		}
		if c.trustedComputerUse != nil {
			out.ComputerUse = c
		}
	}
	return out
}

func (c *coreAgentCallbacks) SearchReviewedHostKnowledge(ctx context.Context, principal Principal, query string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("host_knowledge_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_knowledge_principal_mismatch")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("host_knowledge_query_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	return c.executeKnowledgeSearch(map[string]interface{}{"query": query}), nil
}
