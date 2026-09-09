package tool

import (
	"strings"
	"testing"
	"time"
)

func TestArtifactFileNameUsesSafeProducerNameAndMIMEFallback(t *testing.T) {
	if got := ArtifactFileName(ArtifactRef{Name: `..\\secret\\report.pdf`}); got != "report.pdf" {
		t.Fatalf("safe artifact name=%q", got)
	}
	if got := ArtifactFileName(ArtifactRef{MIMEType: "application/pdf"}); got != "attachment.pdf" {
		t.Fatalf("PDF fallback=%q", got)
	}
	if got := ArtifactFileName(ArtifactRef{MIMEType: "application/octet-stream"}); got != "attachment.bin" {
		t.Fatalf("binary fallback=%q", got)
	}
}

func TestDocumentTempSuffixAndRequiredContract(t *testing.T) {
	selection := PlannedSelection{FitProof: FitProof{QualifierBindings: map[string]string{"format": "presentation"}}}
	if got := DocumentTempSuffixForSelection(selection); got != ".pptx" {
		t.Fatalf("presentation suffix=%q", got)
	}
	contracts := []ArtifactContract{{Kind: "optional"}, {Kind: "required", Required: true}}
	if got := FirstRequiredArtifactContract(contracts); got.Kind != "required" {
		t.Fatalf("required contract=%#v", got)
	}
}

func TestCurrentChannelDeliverNeedAndAccepts(t *testing.T) {
	file := CapabilityNeed{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}}
	image := CapabilityNeed{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}}
	empty := CapabilityNeed{Capability: "artifact.deliver.current_channel"}
	read := CapabilityNeed{Capability: "document.read.local", Qualifiers: map[string]string{"format": "file"}}
	if !CurrentChannelDeliverNeed(file) || CurrentChannelDeliverNeed(read) {
		t.Fatal("capability membership drifted")
	}
	if !CurrentChannelDeliverAccepts(file, "file") || CurrentChannelDeliverAccepts(image, "file") {
		t.Fatal("GUI file-only accept drifted")
	}
	if !CurrentChannelDeliverAccepts(image, "file", "image", "voice", "") || !CurrentChannelDeliverAccepts(empty, "file", "image", "voice", "") {
		t.Fatal("headless reviewed formats drifted")
	}
	if CurrentChannelDeliverAccepts(read, "file") {
		t.Fatal("document-read must not count as channel deliver")
	}
}

func TestChannelSelectionProjections(t *testing.T) {
	selection := PlannedSelection{Provider: ProviderBinding{Kind: "channel"}, FitProof: FitProof{MatchedCapability: "artifact.deliver.current_channel"}}
	if !CurrentChannelArtifactDeliverySelection(selection) || ScheduleChannelDispatchSelection(selection) {
		t.Fatal("artifact delivery classification mismatch")
	}
	selection.FitProof.MatchedCapability = CapabilityScheduleDispatchChannel
	if !ScheduleChannelDispatchSelection(selection) || CurrentChannelArtifactDeliverySelection(selection) {
		t.Fatal("schedule dispatch classification mismatch")
	}
}

func TestTurnRequiredDeliveryCompleteIgnoresOptionalAndRequiresDelivery(t *testing.T) {
	deliver := PlannedSelection{
		ID: "deliver", NeedID: "need-deliver",
		Provider: ProviderBinding{Kind: "channel"},
		FitProof: FitProof{MatchedCapability: "artifact.deliver.current_channel"},
	}
	search := PlannedSelection{ID: "search", NeedID: "need-search"}
	plan := ToolPlan{Selections: []PlannedSelection{search, deliver}}
	if TurnRequiredDeliveryComplete(plan, map[string]bool{"deliver": true}, nil) {
		t.Fatal("required search still open must not complete")
	}
	if !TurnRequiredDeliveryComplete(plan, map[string]bool{"deliver": true}, map[string]bool{"need-search": true}) {
		t.Fatal("optional search must not gate a delivered turn")
	}
	if TurnRequiredDeliveryComplete(plan, map[string]bool{"search": true, "deliver": false}, map[string]bool{"need-search": true}) {
		t.Fatal("exhausted required work without delivery must not complete")
	}
	if TurnRequiredDeliveryComplete(ToolPlan{}, nil, nil) {
		t.Fatal("empty plan must not complete")
	}
}

func TestDocumentGenerateSelectionAndUnissuedReady(t *testing.T) {
	byCapability := PlannedSelection{ID: "g1", FitProof: FitProof{MatchedCapability: "document.generate.file"}}
	byLegacyName := PlannedSelection{ID: "g2", AdapterName: "generate_pdf"}
	byHostName := PlannedSelection{ID: "g3", AdapterName: "host_document_generate_file"}
	search := PlannedSelection{ID: "s1", FitProof: FitProof{MatchedCapability: "information.search.web"}}
	if !DocumentGenerateSelection(byCapability) || !DocumentGenerateSelection(byLegacyName) || !DocumentGenerateSelection(byHostName) || DocumentGenerateSelection(search) {
		t.Fatal("document generate classification mismatch")
	}
	plan := ToolPlan{Selections: []PlannedSelection{search, byCapability}}
	if !HasUnissuedReadyDocumentGenerate(plan, nil, nil) {
		t.Fatal("ready generate without a grant must be unissued")
	}
	if HasUnissuedReadyDocumentGenerate(plan, map[string]bool{"g1": true}, nil) {
		t.Fatal("completed generate must not count as unissued")
	}
	if HasUnissuedReadyDocumentGenerate(plan, nil, map[string]bool{"g1": true}) {
		t.Fatal("materialized generate must not count as unissued")
	}
}

func TestForEachHostSatisfiedLookupForGenerateVisitsLookupsAndFailsClosed(t *testing.T) {
	search := PlannedSelection{ID: "search", FitProof: FitProof{MatchedCapability: "information.search.web"}}
	other := PlannedSelection{ID: "other", FitProof: FitProof{MatchedCapability: "fs.read.local"}}
	generate := PlannedSelection{
		ID: "generate", AdapterName: "generate_pdf",
		FitProof: FitProof{MatchedCapability: "document.generate.file"},
		Requires: []string{"search"},
	}
	visited := []string{}
	if err := ForEachHostSatisfiedLookupForGenerate(ToolPlan{Selections: []PlannedSelection{search, generate}}, nil, func(id string) error {
		visited = append(visited, id)
		return nil
	}); err != nil || len(visited) != 1 || visited[0] != "search" {
		t.Fatalf("lookup visit=%v err=%v", visited, err)
	}
	if err := ForEachHostSatisfiedLookupForGenerate(ToolPlan{Selections: []PlannedSelection{search, generate}}, map[string]bool{"search": true}, func(string) error {
		t.Fatal("completed lookup must not be visited")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	blocked := generate
	blocked.Requires = []string{"other"}
	if err := ForEachHostSatisfiedLookupForGenerate(ToolPlan{Selections: []PlannedSelection{other, blocked}}, nil, func(string) error { return nil }); err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatalf("non-lookup blocker err=%v", err)
	}
	confirm := generate
	confirm.Requires = []string{"confirmation:need"}
	if err := ForEachHostSatisfiedLookupForGenerate(ToolPlan{Selections: []PlannedSelection{confirm}}, nil, func(string) error { return nil }); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("confirmation blocker err=%v", err)
	}
}

func TestCurrentChannelDeliveryDependencyAndKind(t *testing.T) {
	dep := ArtifactDependency{Contract: ArtifactContract{Kind: "document"}, Artifact: ArtifactBinding{Kind: "image"}}
	if got := ArtifactDependencyKind(dep); got != "image" {
		t.Fatalf("binding kind=%q", got)
	}
	dep.Artifact.Kind = ""
	if got := ArtifactDependencyKind(dep); got != "document" {
		t.Fatalf("contract kind=%q", got)
	}
	plan := ToolPlan{Selections: []PlannedSelection{{
		ID:                   "deliver",
		Provider:             ProviderBinding{Kind: "channel"},
		FitProof:             FitProof{MatchedCapability: "artifact.deliver.current_channel"},
		ArtifactDependencies: []ArtifactDependency{{ArtifactID: "art-1", Contract: ArtifactContract{Kind: "document"}}},
	}}}
	got, ok := CurrentChannelDeliveryDependency(plan, InvocationGrant{SelectionID: "deliver"})
	if !ok || got.ArtifactID != "art-1" || ArtifactDependencyKind(got) != "document" {
		t.Fatalf("delivery dep=%#v ok=%t", got, ok)
	}
	if _, ok := CurrentChannelDeliveryDependency(plan, InvocationGrant{}); ok {
		t.Fatal("empty grant must fail closed")
	}
}

func TestArtifactContractMatchesAndProducerPublished(t *testing.T) {
	typed := ArtifactContract{Kind: "document", MIMEType: "application/pdf", Required: true}
	wildcard := ArtifactContract{Kind: "Document"}
	if !ArtifactContractMatches(typed, wildcard) {
		t.Fatal("empty required MIME must wildcard")
	}
	if ArtifactContractMatches(wildcard, typed) {
		t.Fatal("empty produced MIME must not satisfy a typed required contract")
	}
	if ArtifactContractMatches(ArtifactContract{Kind: "image", MIMEType: "application/pdf"}, typed) {
		t.Fatal("kind mismatch must fail")
	}
	if !ArtifactBindingMatchesContract(ArtifactBinding{Kind: "document", MIMEType: "application/pdf"}, wildcard) {
		t.Fatal("binding must match wildcard contract")
	}
	if ArtifactBindingMatchesContract(ArtifactBinding{Kind: "document", MIMEType: "text/plain"}, typed) {
		t.Fatal("binding MIME mismatch must fail")
	}
	refs := []RouteArtifactRef{{ProducerSelection: "office", Kind: "document", MIMEType: "application/pdf"}}
	if !ProducerArtifactPublished(refs, "office", typed) {
		t.Fatal("typed producer ref must satisfy typed contract")
	}
	if ProducerArtifactPublished(refs, "other", typed) {
		t.Fatal("wrong producer must fail closed")
	}
	if ProducerArtifactPublished(nil, "office", typed) {
		t.Fatal("empty refs must fail closed")
	}
	if ProducerArtifactPublished(refs, "", typed) {
		t.Fatal("empty producer must fail closed")
	}
}

func TestUniqueMatchingAndBoundArtifactDependency(t *testing.T) {
	contract := ArtifactContract{Kind: "document", MIMEType: "application/pdf", Required: true}
	bound := ArtifactDependency{
		ArtifactID: "art-1",
		Contract:   contract,
		Artifact:   ArtifactBinding{ID: "art-1", Kind: "document", MIMEType: "application/pdf"},
	}
	unbound := ArtifactDependency{ProducerSelection: "office", Contract: contract}
	got, err := UniqueMatchingArtifactDependency([]ArtifactDependency{unbound}, contract)
	if err != nil || got.ProducerSelection != "office" {
		t.Fatalf("unique unbound=%#v err=%v", got, err)
	}
	if _, err := UniqueMatchingArtifactDependency([]ArtifactDependency{bound, unbound}, contract); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("two matches err=%v", err)
	}
	if _, err := UniqueMatchingArtifactDependency(nil, contract); err == nil || !strings.Contains(err.Error(), "unbound") {
		t.Fatalf("zero matches err=%v", err)
	}
	got, err = UniqueBoundArtifactDependency([]ArtifactDependency{bound, unbound}, contract)
	if err != nil || got.ArtifactID != "art-1" {
		t.Fatalf("bound unique=%#v err=%v", got, err)
	}
	if err := ValidateBoundArtifactDependency(got, contract); err != nil {
		t.Fatal(err)
	}
	mismatch := bound
	mismatch.Artifact.ID = "other"
	if err := ValidateBoundArtifactDependency(mismatch, contract); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("id mismatch err=%v", err)
	}
}

func TestNewestFamilyProducerArtifactPicksLatestSibling(t *testing.T) {
	contract := ArtifactContract{Kind: "document", MIMEType: "application/pdf", Required: true}
	older := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	refs := []RouteArtifactRef{
		{ArtifactID: "draft", ProducerSelection: "office", Kind: "document", MIMEType: "application/pdf", CreatedAt: older},
		{ArtifactID: "revision", ProducerSelection: RepeatSiblingNeedID("office", 1), Kind: "document", MIMEType: "application/pdf", CreatedAt: newer},
		{ArtifactID: "other", ProducerSelection: "search", Kind: "document", MIMEType: "application/pdf", CreatedAt: newer.Add(time.Hour)},
	}
	got, ok := NewestFamilyProducerArtifact(refs, "office", contract)
	if !ok || got.ID != "revision" {
		t.Fatalf("family newest=%#v ok=%t", got, ok)
	}
	if _, ok := NewestFamilyProducerArtifact(refs, "missing", contract); ok {
		t.Fatal("unknown family must fail closed")
	}
	if _, ok := NewestFamilyProducerArtifact(refs, "", contract); ok {
		t.Fatal("empty producer must fail closed")
	}
}
