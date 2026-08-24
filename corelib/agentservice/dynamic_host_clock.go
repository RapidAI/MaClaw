package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostProviderKind        = "host"
	reviewedHostClockProviderID     = "core-clock"
	reviewedHostClockImplementation = "local"
	reviewedHostClockAdapterName    = "host_information_current_time"
)

type hostOwnedRuntimeBinding struct {
	execute func(context.Context, Principal, map[string]interface{}) (string, error)
}

type reviewedHostKnowledgeSearcher interface {
	SearchReviewedHostKnowledge(ctx context.Context, principal Principal, query string) (string, error)
}

type reviewedHostAuditReader interface {
	ReadReviewedHostAudit(ctx context.Context, principal Principal, query string) (string, error)
}

type reviewedHostWebFetcher interface {
	FetchReviewedHostWeb(ctx context.Context, principal Principal, rawURL string) (string, error)
}

type reviewedHostFileReader interface {
	ReadReviewedHostFile(ctx context.Context, principal Principal, path, query, filePattern string) (string, error)
}

type reviewedHostRepoInspector interface {
	InspectReviewedHostRepo(ctx context.Context, principal Principal) (string, error)
}

type reviewedHostDocumentReader interface {
	ReadReviewedHostDocument(ctx context.Context, principal Principal, args map[string]interface{}) (string, error)
}

type reviewedHostOwnedServices struct {
	Knowledge         reviewedHostKnowledgeSearcher
	KnowledgeWrite    reviewedHostKnowledgeIngester
	Audit             reviewedHostAuditReader
	WebFetch          reviewedHostWebFetcher
	FileDownload      reviewedHostFileDownloader
	FileRead          reviewedHostFileReader
	FileWrite         reviewedHostFileWriter
	OfficeWrite       reviewedHostOfficeWriter
	DocumentGenerate  reviewedHostDocumentGenerator
	AudioRender       reviewedHostAudioRenderer
	AudioSynthesize   reviewedHostAudioSynthesizer
	VisualCapture     reviewedHostVisualCapturer
	SystemLaunch      reviewedHostSystemLauncher
	URLLaunch         reviewedHostURLLauncher
	Shell             reviewedHostShellExecutor
	BuildVerify       reviewedHostBuildVerifier
	Delegate          reviewedHostDelegateRunner
	SSH               reviewedHostSSHExecutor
	Browser           reviewedHostBrowserController
	ComputerUse       reviewedHostComputerUseController
	RepoInspect       reviewedHostRepoInspector
	DocumentRead      reviewedHostDocumentReader
	AudioTranscribe   reviewedHostAudioTranscriber
	Memory            reviewedHostMemoryManager
	Task              reviewedHostTaskTracker
	Goal              reviewedHostGoalManager
	Template          reviewedHostTemplateManager
	Schedule          reviewedHostScheduleAdministrator
	ScheduleDispatch  reviewedHostScheduleDispatcher
	MessageSend       reviewedHostMessageSender
	FileDeliver       reviewedHostFileDeliverer
	AttachmentDeliver reviewedHostAttachmentDeliverer
	DestinationID     string
	RepoMutate        reviewedHostRepoMutator
	KnowledgeAdmin    reviewedHostKnowledgeAdministrator
	Config            reviewedHostConfigManager
	Session           reviewedHostSessionInspector
}

func reviewedHostClockInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostClockContractDigest() string {
	return coretool.SchemaDigest([]byte("information.current_time:v1:host-clock"))
}

// ProjectReviewedHostClockProvider projects the host-owned local clock. It is
// not a Skill/MCP discovery entry and must not be used to import the GUI
// builtin catalog. The closed schema rejects channel, destination, and every
// other model-supplied field.
func ProjectReviewedHostClockProvider() (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	parameters := reviewedHostClockInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host clock schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostClockContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-clock-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostClockAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostClockProviderID,
			ImplementationID: reviewedHostClockImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityCurrentTime,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostClock}, nil
}

func dynamicHostInvocationDigest(schema map[string]interface{}) (string, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("encode host clock schema: %w", err)
	}
	return coretool.SchemaDigest(data), nil
}

// AttachReviewedHostOwnedProviders adds the reviewed host clock to a dynamic
// catalog. BuildDynamicSemanticCatalog stays MCP/Skill-only so GUI catalog
// families are never imported through this path.
func AttachReviewedHostOwnedProviders(catalog DynamicSemanticCatalog) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostClockProvider()
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

func reviewedHostOwnedCatalogLifecycle() DynamicCatalogLifecycle {
	return dynamicCatalogLifecycleForKind(reviewedHostProviderKind, CompleteDynamicCatalogLifecycle())
}

func mergeHostOwnedCatalogLifecycle(observed DynamicCatalogLifecycle) DynamicCatalogLifecycle {
	values := []DynamicCatalogLifecycle{reviewedHostOwnedCatalogLifecycle()}
	if len(observed.Coverage.Families) > 0 {
		for _, family := range observed.Coverage.Families {
			values = append(values, DynamicCatalogLifecycle{
				Kind: family.Kind,
				Coverage: coretool.CatalogCoverage{
					State: family.State, ReasonCode: family.ReasonCode,
					ObservedAt: family.ObservedAt, StaleUntil: family.StaleUntil,
				},
			})
		}
	} else if strings.TrimSpace(observed.Kind) != "" {
		values = append(values, observed)
	}
	return mergeDynamicCatalogLifecycles(values)
}

func prepareReviewedDynamicSemanticCatalog(registry *coretool.CapabilityRegistry, mcpEntries []MCPToolEntry, skillEntries []SkillToolEntry, observed DynamicCatalogLifecycle, services reviewedHostOwnedServices) (DynamicSemanticCatalog, DynamicCatalogLifecycle, error) {
	catalog, err := BuildDynamicSemanticCatalog(mcpEntries, skillEntries)
	if err != nil {
		return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
	}
	if registry == nil {
		return catalog, observed, nil
	}
	attached := false
	if _, ok := registry.Lookup(CapabilityCurrentTime); ok {
		catalog, err = AttachReviewedHostOwnedProviders(catalog)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityKnowledgeRead); ok && services.Knowledge != nil {
		catalog, err = AttachReviewedHostKnowledgeProvider(catalog, services.Knowledge)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityAuditRead); ok && services.Audit != nil {
		catalog, err = AttachReviewedHostAuditProvider(catalog, services.Audit)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityWebFetch); ok && services.WebFetch != nil {
		catalog, err = AttachReviewedHostWebFetchProvider(catalog, services.WebFetch)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityFileDownload); ok && services.FileDownload != nil {
		catalog, err = AttachReviewedHostFileDownloadProvider(catalog, services.FileDownload)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityFileRead); ok && services.FileRead != nil {
		catalog, err = AttachReviewedHostFileReadProvider(catalog, services.FileRead)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityRepoInspect); ok && services.RepoInspect != nil {
		catalog, err = AttachReviewedHostRepoInspectProvider(catalog, services.RepoInspect)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityDocumentRead); ok && services.DocumentRead != nil {
		catalog, err = AttachReviewedHostDocumentReadProvider(catalog, services.DocumentRead)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityAudioTranscribe); ok && services.AudioTranscribe != nil {
		catalog, err = AttachReviewedHostAudioTranscribeProvider(catalog, services.AudioTranscribe)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityFileWrite); ok && services.FileWrite != nil {
		catalog, err = AttachReviewedHostFileWriteProvider(catalog, services.FileWrite)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityOfficeWrite); ok && services.OfficeWrite != nil {
		catalog, err = AttachReviewedHostOfficeWriteProvider(catalog, services.OfficeWrite)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityDocumentGenerate); ok && services.DocumentGenerate != nil {
		catalog, err = AttachReviewedHostDocumentGenerateProvider(catalog, services.DocumentGenerate)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityAudioRender); ok && services.AudioRender != nil {
		catalog, err = AttachReviewedHostAudioRenderProvider(catalog, services.AudioRender)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityAudioSynthesize); ok && services.AudioSynthesize != nil {
		catalog, err = AttachReviewedHostAudioSynthesizeProvider(catalog, services.AudioSynthesize)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityVisualCapture); ok && services.VisualCapture != nil {
		catalog, err = AttachReviewedHostVisualCaptureProvider(catalog, services.VisualCapture)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilitySystemLaunch); ok && services.SystemLaunch != nil {
		catalog, err = AttachReviewedHostSystemLaunchProvider(catalog, services.SystemLaunch)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilitySystemLaunch); ok && services.URLLaunch != nil {
		catalog, err = AttachReviewedHostURLLaunchProvider(catalog, services.URLLaunch)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityShellExecute); ok && services.Shell != nil {
		catalog, err = AttachReviewedHostShellProvider(catalog, services.Shell)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityBuildVerify); ok && services.BuildVerify != nil {
		catalog, err = AttachReviewedHostBuildVerifyProvider(catalog, services.BuildVerify)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityDelegateSubtask); ok && services.Delegate != nil {
		catalog, err = AttachReviewedHostDelegateProvider(catalog, services.Delegate)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilitySSHExecute); ok && services.SSH != nil {
		catalog, err = AttachReviewedHostSSHProvider(catalog, services.SSH)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityBrowserControl); ok && services.Browser != nil {
		catalog, err = AttachReviewedHostBrowserProvider(catalog, services.Browser)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityComputerUse); ok && services.ComputerUse != nil {
		catalog, err = AttachReviewedHostComputerUseProvider(catalog, services.ComputerUse)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityKnowledgeWrite); ok && services.KnowledgeWrite != nil {
		catalog, err = AttachReviewedHostKnowledgeWriteProvider(catalog, services.KnowledgeWrite)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityMemoryManage); ok && services.Memory != nil {
		catalog, err = AttachReviewedHostMemoryProvider(catalog, services.Memory)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityMemoryRecall); ok && services.Memory != nil {
		catalog, err = AttachReviewedHostMemoryRecallProvider(catalog, services.Memory)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityTaskTrack); ok && services.Task != nil {
		catalog, err = AttachReviewedHostTaskProvider(catalog, services.Task)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityGoalManage); ok && services.Goal != nil {
		catalog, err = AttachReviewedHostGoalProvider(catalog, services.Goal)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityTemplateManage); ok && services.Template != nil {
		catalog, err = AttachReviewedHostTemplateProvider(catalog, services.Template)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityScheduleAdminister); ok && services.Schedule != nil {
		catalog, err = AttachReviewedHostScheduleProvider(catalog, services.Schedule)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityScheduleDispatch); ok && services.ScheduleDispatch != nil && reviewedHostTrustedDestination(services.DestinationID) {
		catalog, err = AttachReviewedHostScheduleDispatchProvider(catalog, services.ScheduleDispatch, services.DestinationID)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityMessageSend); ok && services.MessageSend != nil && reviewedHostTrustedDestination(services.DestinationID) {
		catalog, err = AttachReviewedHostMessageSendProvider(catalog, services.MessageSend, services.DestinationID)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityArtifactDeliverSpecified); ok && services.FileDeliver != nil && reviewedHostTrustedDestination(services.DestinationID) {
		catalog, err = AttachReviewedHostFileDeliverProvider(catalog, services.FileDeliver, services.DestinationID)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityArtifactDeliverCurrent); ok && services.AttachmentDeliver != nil && reviewedHostTrustedDestination(services.DestinationID) {
		catalog, err = AttachReviewedHostAttachmentDeliverProvider(catalog, services.AttachmentDeliver, services.DestinationID)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityRepoMutate); ok && services.RepoMutate != nil {
		catalog, err = AttachReviewedHostRepoMutateProvider(catalog, services.RepoMutate)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityKnowledgeAdmin); ok && services.KnowledgeAdmin != nil {
		catalog, err = AttachReviewedHostKnowledgeAdminProvider(catalog, services.KnowledgeAdmin)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilityConfigManage); ok && services.Config != nil {
		catalog, err = AttachReviewedHostConfigProvider(catalog, services.Config)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if _, ok := registry.Lookup(CapabilitySessionManage); ok && services.Session != nil {
		catalog, err = AttachReviewedHostSessionProvider(catalog, services.Session)
		if err != nil {
			return DynamicSemanticCatalog{}, DynamicCatalogLifecycle{}, err
		}
		attached = true
	}
	if !attached {
		return catalog, observed, nil
	}
	return catalog, mergeHostOwnedCatalogLifecycle(observed), nil
}

func executeReviewedHostClock(_ context.Context, _ Principal, args map[string]interface{}) (string, error) {
	if len(args) > 0 {
		return "", fmt.Errorf("host_clock_arguments_rejected")
	}
	now := time.Now()
	isoYear, isoWeek := now.ISOWeek()
	return fmt.Sprintf(
		"%04d-%02d-%02d %s ISO week %04d-W%02d %02d:%02d:%02d (timezone: %s)",
		now.Year(), int(now.Month()), now.Day(),
		now.Weekday().String(), isoYear, isoWeek,
		now.Hour(), now.Minute(), now.Second(),
		now.Location().String(),
	), nil
}
