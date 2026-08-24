package routingeval

import (
	"fmt"
	"sync"

	tool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// RegistryVersion is the registry version every evaluation snapshot is
// published against. Samples may override it on the snapshot to exercise
// registry-mismatch rejection.
const RegistryVersion = "routingeval-v1"

const evalExtensionOwner = "routingeval"

const (
	// CapabilityEvalCaptureDesktop produces a desktop image artifact. It
	// mirrors the visual.capture.desktop contract used by the planner unit
	// tests and exists so artifact-chain samples can run end to end.
	CapabilityEvalCaptureDesktop tool.CapabilityID = "visual.capture.desktop"
	// CapabilityEvalDeliverCurrentChannel consumes an image artifact and
	// delivers it to the current channel. Its "artifact.deliver." prefix
	// places selections into the delivery plan phase.
	CapabilityEvalDeliverCurrentChannel tool.CapabilityID = "artifact.deliver.current_channel"
	// CapabilityEvalLegacyCapture is a deprecated contract retained so
	// version-migration samples can assert both planner rejection
	// (unknown_capability) and publish rejection.
	CapabilityEvalLegacyCapture tool.CapabilityID = "eval.legacy.capture"
	// CapabilityEvalSearchWeb is the lookup contract used by mixed
	// search+generate samples (design 10.1 item 3 / mixed request).
	CapabilityEvalSearchWeb tool.CapabilityID = "information.search.web"
	// CapabilityEvalGenerateDocument is the PDF generate contract for the
	// document.generate product slice. It produces a document ArtifactRef.
	CapabilityEvalGenerateDocument tool.CapabilityID = "document.generate.file"
)

var (
	registryOnce   sync.Once
	sharedRegistry *tool.CapabilityRegistry
	registryErr    error
)

// evalRegistry returns the shared sealed registry: the builtin capability
// ontology plus the small evaluation extension vocabulary below. Extension
// descriptors exist only because the builtin ontology intentionally declares
// no artifact-producing/consuming contracts.
func evalRegistry() (*tool.CapabilityRegistry, error) {
	registryOnce.Do(func() {
		registry := tool.NewCapabilityRegistry(RegistryVersion)
		if err := tool.RegisterBuiltinCapabilityOntology(registry); err != nil {
			registryErr = err
			return
		}
		extensions := []tool.CapabilityDescriptor{
			{
				ID:      CapabilityEvalCaptureDesktop,
				Version: "v1",
				Owner:   evalExtensionOwner,
				Summary: "Capture the desktop image as an artifact.",
				Qualifiers: map[string]tool.QualifierConstraint{
					"display": {Values: []string{"primary", "all"}, Required: true},
				},
				Effects:  []tool.EffectClass{tool.EffectReadOnly},
				Produces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png"}},
			},
			{
				ID:      CapabilityEvalDeliverCurrentChannel,
				Version: "v1",
				Owner:   evalExtensionOwner,
				Summary: "Deliver an image artifact to the current channel.",
				Qualifiers: map[string]tool.QualifierConstraint{
					"format": {Values: []string{"image", "file"}, Required: true},
				},
				Effects:  []tool.EffectClass{tool.EffectExternalEffect},
				Consumes: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
			},
			{
				ID:          CapabilityEvalLegacyCapture,
				Version:     "v1",
				Owner:       evalExtensionOwner,
				Summary:     "Deprecated capture contract retained for migration regression samples.",
				Effects:     []tool.EffectClass{tool.EffectReadOnly},
				Deprecated:  true,
				Replacement: CapabilityEvalCaptureDesktop,
			},
			{
				ID:      CapabilityEvalSearchWeb,
				Version: "v1",
				Owner:   evalExtensionOwner,
				Summary: "Search public web information.",
				Qualifiers: map[string]tool.QualifierConstraint{
					"freshness": {Values: []string{"reference", "current"}, Required: true},
				},
				Effects: []tool.EffectClass{tool.EffectReadOnly},
			},
			{
				ID:      CapabilityEvalGenerateDocument,
				Version: "v1",
				Owner:   evalExtensionOwner,
				Summary: "Render current facts into a PDF artifact without delivering it.",
				Qualifiers: map[string]tool.QualifierConstraint{
					"format": {Values: []string{"pdf"}, Required: true},
				},
				Effects:  []tool.EffectClass{tool.EffectLocalMutation},
				Produces: []tool.ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}},
			},
		}
		for _, descriptor := range extensions {
			if err := registry.Register(descriptor); err != nil {
				registryErr = fmt.Errorf("register eval extension %q: %w", descriptor.ID, err)
				return
			}
		}
		registryErr = registry.Seal()
		sharedRegistry = registry
	})
	return sharedRegistry, registryErr
}
