package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestReviewedDynamicCapabilityRegistryIsSealedAndNarrow(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Sealed() || registry.Version() != ReviewedDynamicCapabilityRegistryVersion {
		t.Fatalf("reviewed registry=%#v sealed=%v", registry, registry.Sealed())
	}
	descriptor, ok := registry.Lookup(CapabilityInformationLookup)
	if !ok || len(descriptor.Effects) != 1 || descriptor.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("lookup descriptor=%#v found=%v", descriptor, ok)
	}
	clock, ok := registry.Lookup(CapabilityCurrentTime)
	if !ok || len(clock.Effects) != 1 || clock.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("current_time descriptor=%#v found=%v", clock, ok)
	}
	knowledge, ok := registry.Lookup(CapabilityKnowledgeRead)
	if !ok || len(knowledge.Effects) != 1 || knowledge.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("knowledge.read descriptor=%#v found=%v", knowledge, ok)
	}
	audit, ok := registry.Lookup(CapabilityAuditRead)
	if !ok || len(audit.Effects) != 1 || audit.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("security.audit.read descriptor=%#v found=%v", audit, ok)
	}
	fetch, ok := registry.Lookup(CapabilityWebFetch)
	if !ok || len(fetch.Effects) != 1 || fetch.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("information.fetch.web descriptor=%#v found=%v", fetch, ok)
	}
	fileDownload, ok := registry.Lookup(CapabilityFileDownload)
	if !ok || len(fileDownload.Effects) != 1 || fileDownload.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("artifact.acquire.remote descriptor=%#v found=%v", fileDownload, ok)
	}
	fileRead, ok := registry.Lookup(CapabilityFileRead)
	if !ok || len(fileRead.Effects) != 1 || fileRead.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("fs.read.local descriptor=%#v found=%v", fileRead, ok)
	}
	repo, ok := registry.Lookup(CapabilityRepoInspect)
	if !ok || len(repo.Effects) != 1 || repo.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("repo.inspect.vcs descriptor=%#v found=%v", repo, ok)
	}
	document, ok := registry.Lookup(CapabilityDocumentRead)
	if !ok || len(document.Effects) != 1 || document.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("document.read.local descriptor=%#v found=%v", document, ok)
	}
	audioTranscribe, ok := registry.Lookup(CapabilityAudioTranscribe)
	if !ok || len(audioTranscribe.Effects) != 1 || audioTranscribe.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("audio.transcribe.speech descriptor=%#v found=%v", audioTranscribe, ok)
	}
	fileWrite, ok := registry.Lookup(CapabilityFileWrite)
	if !ok || len(fileWrite.Effects) != 1 || fileWrite.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("fs.write.local descriptor=%#v found=%v", fileWrite, ok)
	}
	officeWrite, ok := registry.Lookup(CapabilityOfficeWrite)
	if !ok || len(officeWrite.Effects) != 1 || officeWrite.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("document.write.office descriptor=%#v found=%v", officeWrite, ok)
	}
	if officeWrite.Qualifiers[QualifierDocumentFormat].Values[0] != DocumentFormatSpreadsheet || !officeWrite.Qualifiers[QualifierDocumentFormat].Required {
		t.Fatalf("document.write.office qualifiers=%#v", officeWrite.Qualifiers)
	}
	generate, ok := registry.Lookup(CapabilityDocumentGenerate)
	if !ok || len(generate.Effects) != 1 || generate.Effects[0] != coretool.EffectLocalMutation {
		t.Fatalf("document.generate.file descriptor=%#v found=%v", generate, ok)
	}
	if generate.Qualifiers[QualifierDocumentFormat].Values[0] != DocumentFormatPDF || !generate.Qualifiers[QualifierDocumentFormat].Required {
		t.Fatalf("document.generate.file qualifiers=%#v", generate.Qualifiers)
	}
	audioRender, ok := registry.Lookup(CapabilityAudioRender)
	if !ok || len(audioRender.Effects) != 1 || audioRender.Effects[0] != coretool.EffectLocalMutation {
		t.Fatalf("audio.render.speech descriptor=%#v found=%v", audioRender, ok)
	}
	audioSynthesize, ok := registry.Lookup(CapabilityAudioSynthesize)
	if !ok || len(audioSynthesize.Effects) != 1 || audioSynthesize.Effects[0] != coretool.EffectLocalMutation {
		t.Fatalf("audio.synthesize.local descriptor=%#v found=%v", audioSynthesize, ok)
	}
	visualCapture, ok := registry.Lookup(CapabilityVisualCapture)
	if !ok || len(visualCapture.Effects) != 1 || visualCapture.Effects[0] != coretool.EffectLocalMutation {
		t.Fatalf("visual.capture.desktop descriptor=%#v found=%v", visualCapture, ok)
	}
	if visualCapture.Qualifiers[QualifierCaptureDisplay].Values[0] != CaptureDisplayPrimary || !visualCapture.Qualifiers[QualifierCaptureDisplay].Required {
		t.Fatalf("visual.capture.desktop qualifiers=%#v", visualCapture.Qualifiers)
	}
	systemLaunch, ok := registry.Lookup(CapabilitySystemLaunch)
	if !ok || len(systemLaunch.Effects) != 1 || systemLaunch.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("system.launch.local descriptor=%#v found=%v", systemLaunch, ok)
	}
	if !systemLaunch.Qualifiers[QualifierLaunchKind].Required || len(systemLaunch.Qualifiers[QualifierLaunchKind].Values) != 2 || systemLaunch.Qualifiers[QualifierLaunchKind].Values[0] != LaunchKindDocument || systemLaunch.Qualifiers[QualifierLaunchKind].Values[1] != LaunchKindURL {
		t.Fatalf("system.launch.local qualifiers=%#v", systemLaunch.Qualifiers)
	}
	shellExecute, ok := registry.Lookup(CapabilityShellExecute)
	if !ok || len(shellExecute.Effects) != 1 || shellExecute.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("shell.execute.local descriptor=%#v found=%v", shellExecute, ok)
	}
	delegate, ok := registry.Lookup(CapabilityDelegateSubtask)
	if !ok || len(delegate.Effects) != 1 || delegate.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("agent.delegate.subtask descriptor=%#v found=%v", delegate, ok)
	}
	sshExecute, ok := registry.Lookup(CapabilitySSHExecute)
	if !ok || len(sshExecute.Effects) != 1 || sshExecute.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("shell.execute.remote_host descriptor=%#v found=%v", sshExecute, ok)
	}
	browserControl, ok := registry.Lookup(CapabilityBrowserControl)
	if !ok || len(browserControl.Effects) != 1 || browserControl.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("browser.control.web descriptor=%#v found=%v", browserControl, ok)
	}
	computerUse, ok := registry.Lookup(CapabilityComputerUse)
	if !ok || len(computerUse.Effects) != 1 || computerUse.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("computer.control.desktop descriptor=%#v found=%v", computerUse, ok)
	}
	knowledgeWrite, ok := registry.Lookup(CapabilityKnowledgeWrite)
	if !ok || len(knowledgeWrite.Effects) != 1 || knowledgeWrite.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("knowledge.ingest.local descriptor=%#v found=%v", knowledgeWrite, ok)
	}
	memoryManage, ok := registry.Lookup(CapabilityMemoryManage)
	if !ok || len(memoryManage.Effects) != 1 || memoryManage.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("memory.manage.agent descriptor=%#v found=%v", memoryManage, ok)
	}
	memoryRecall, ok := registry.Lookup(CapabilityMemoryRecall)
	if !ok || len(memoryRecall.Effects) != 1 || memoryRecall.Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("memory.recall.agent descriptor=%#v found=%v", memoryRecall, ok)
	}
	taskTrack, ok := registry.Lookup(CapabilityTaskTrack)
	if !ok || len(taskTrack.Effects) != 1 || taskTrack.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("task.track.local descriptor=%#v found=%v", taskTrack, ok)
	}
	goalManage, ok := registry.Lookup(CapabilityGoalManage)
	if !ok || len(goalManage.Effects) != 1 || goalManage.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("goal.manage.longrunning descriptor=%#v found=%v", goalManage, ok)
	}
	templateManage, ok := registry.Lookup(CapabilityTemplateManage)
	if !ok || len(templateManage.Effects) != 1 || templateManage.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("template.manage.session descriptor=%#v found=%v", templateManage, ok)
	}
	scheduleAdminister, ok := registry.Lookup(CapabilityScheduleAdminister)
	if !ok || len(scheduleAdminister.Effects) != 1 || scheduleAdminister.Effects[0] != coretool.EffectLocalMutation {
		t.Fatalf("schedule.administer.local descriptor=%#v found=%v", scheduleAdminister, ok)
	}
	knowledgeAdmin, ok := registry.Lookup(CapabilityKnowledgeAdmin)
	if !ok || len(knowledgeAdmin.Effects) != 1 || knowledgeAdmin.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("knowledge.admin.maintenance descriptor=%#v found=%v", knowledgeAdmin, ok)
	}
	configManage, ok := registry.Lookup(CapabilityConfigManage)
	if !ok || len(configManage.Effects) != 1 || configManage.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("config.manage.self descriptor=%#v found=%v", configManage, ok)
	}
	sessionManage, ok := registry.Lookup(CapabilitySessionManage)
	if !ok || len(sessionManage.Effects) != 1 || sessionManage.Effects[0] != coretool.EffectSensitive {
		t.Fatalf("session.manage.coding descriptor=%#v found=%v", sessionManage, ok)
	}
	messageSend, ok := registry.Lookup(CapabilityMessageSend)
	if !ok || len(messageSend.Effects) != 1 || messageSend.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("message.send.im descriptor=%#v found=%v", messageSend, ok)
	}
	if messageSend.Qualifiers[QualifierMessageFormat].Values[0] != MessageFormatText || !messageSend.Qualifiers[QualifierMessageFormat].Required {
		t.Fatalf("message.send.im qualifiers=%#v", messageSend.Qualifiers)
	}
	repoMutate, ok := registry.Lookup(CapabilityRepoMutate)
	if !ok || len(repoMutate.Effects) != 1 || repoMutate.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("repo.mutate.vcs descriptor=%#v found=%v", repoMutate, ok)
	}
	fileDeliver, ok := registry.Lookup(CapabilityArtifactDeliverSpecified)
	if !ok || len(fileDeliver.Effects) != 1 || fileDeliver.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("artifact.deliver.specified_target descriptor=%#v found=%v", fileDeliver, ok)
	}
	if !fileDeliver.Qualifiers[QualifierArtifactFormat].Required {
		t.Fatalf("artifact.deliver.specified_target qualifiers=%#v", fileDeliver.Qualifiers)
	}
	gotSpecifiedFormats := map[string]bool{}
	for _, value := range fileDeliver.Qualifiers[QualifierArtifactFormat].Values {
		gotSpecifiedFormats[value] = true
	}
	if !gotSpecifiedFormats[ArtifactFormatFile] || !gotSpecifiedFormats[ArtifactFormatImage] || !gotSpecifiedFormats[ArtifactFormatVoice] {
		t.Fatalf("artifact.deliver.specified_target qualifiers=%#v", fileDeliver.Qualifiers)
	}
	currentDeliver, ok := registry.Lookup(CapabilityArtifactDeliverCurrent)
	if !ok || len(currentDeliver.Effects) != 1 || currentDeliver.Effects[0] != coretool.EffectExternalEffect {
		t.Fatalf("artifact.deliver.current_channel descriptor=%#v found=%v", currentDeliver, ok)
	}
	if !currentDeliver.Qualifiers[QualifierArtifactFormat].Required {
		t.Fatalf("artifact.deliver.current_channel qualifiers=%#v", currentDeliver.Qualifiers)
	}
	gotFormats := map[string]bool{}
	for _, value := range currentDeliver.Qualifiers[QualifierArtifactFormat].Values {
		gotFormats[value] = true
	}
	if !gotFormats[ArtifactFormatFile] || !gotFormats[ArtifactFormatImage] || !gotFormats[ArtifactFormatVoice] {
		t.Fatalf("artifact.deliver.current_channel qualifiers=%#v", currentDeliver.Qualifiers)
	}
	if err := registry.Register(coretool.CapabilityDescriptor{ID: "provider.discovered", Version: "v1", Effects: []coretool.EffectClass{coretool.EffectReadOnly}}); err == nil {
		t.Fatal("reviewed registry accepted a runtime capability expansion")
	}
}

func TestReviewedDynamicIntentRulesIgnoreLegacyToolNames(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelSearch, Confidence: .99,
			ToolNames: []string{"invoke_mcp_everything", "discover_provider_by_keyword"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "find reports"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	need := resolution.Needs[0]
	if need.Capability != CapabilityInformationLookup || need.Qualifiers[QualifierInformationScope] != InformationScopeReference {
		t.Fatalf("need=%#v", need)
	}
}

func TestReviewedDynamicIntentRulesResolveCurrentTimeWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelCurrentTime, Confidence: .99,
			ToolNames: []string{"web_search", "current_datetime"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "what time is it"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityCurrentTime {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("current_time must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveKnowledgeReadWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelKnowledgeRead, Confidence: .99,
			ToolNames: []string{"web_search", "knowledge_search", "knowledge_save"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "search my knowledge base for this topic"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityKnowledgeRead {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("knowledge_read must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveAuditReadWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelAuditRead, Confidence: .99,
			ToolNames: []string{"web_search", "query_audit_log", "session_search", "check_health"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "show the recent security audit log"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityAuditRead {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("audit_read must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveWebFetchWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelWebFetch, Confidence: .99,
			ToolNames: []string{"web_search", "web_fetch", "download_file"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "fetch the content of this URL"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityWebFetch {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("web_fetch must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveFileReadWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelFileRead, Confidence: .99,
			ToolNames: []string{"read_file", "list_directory", "search_files", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "show me what is in the README file"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityFileRead {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("file_read must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveGitInspectWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelGitInspect, Confidence: .99,
			ToolNames: []string{"git_status", "git_diff", "git_commit", "bash"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "show me the current diff"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityRepoInspect {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("git_inspect must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveDocumentReadWithoutFileRead(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelDocumentRead, Confidence: .99,
			ToolNames: []string{"office", "read_file", "read_document"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "read the attached document"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityDocumentRead {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityFileRead || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("document_read must not resolve to fs.read.local or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveAudioTranscribeWithoutLookupOrSynthesize(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelAudioTranscribe, Confidence: .99,
			ToolNames: []string{"asr", "audio_transcribe", "record_audio"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "transcribe this recording"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityAudioTranscribe {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("audio_transcribe must not resolve to information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveFileWriteWithoutReadOrIngest(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelFileWrite, Confidence: .99,
			ToolNames: []string{"write_file", "edit_file", "knowledge_save", "generate_pdf"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "save this text to a local file"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityFileWrite {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityFileRead || resolution.Needs[0].Capability == CapabilityKnowledgeRead || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("file_write must not resolve to fs.read.local, knowledge.read.local, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveOfficeWriteWithoutFileWrite(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelOffice, Confidence: .99,
			ToolNames: []string{"office", "write_excel", "write_file", "generate_pdf"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "写一个表格"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityOfficeWrite || resolution.Needs[0].Qualifiers[QualifierDocumentFormat] != DocumentFormatSpreadsheet {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityFileWrite || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("office write must not resolve to fs.write.local or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveDocumentDeliveryWithoutSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelDocumentDelivery, Confidence: .99,
			ToolNames: []string{"send_file", "send_to_im", "im_message", "office", "write_excel"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "把这个表格发给我"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityArtifactDeliverSpecified || resolution.Needs[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityOfficeWrite || resolution.Needs[0].Capability == CapabilityMessageSend || resolution.Needs[0].Capability == CapabilityFileWrite {
		t.Fatal("document_delivery must not resolve to office write, message.send.im, or fs.write.local")
	}
}

func TestReviewedDynamicIntentRulesResolveAttachmentDeliveryWithoutGenerate(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelAttachmentDelivery, Confidence: .99,
			ToolNames: []string{"send_file", "send_to_im", "generate_pdf", "office"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "把这个附件发回当前会话"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityArtifactDeliverCurrent || resolution.Needs[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityArtifactDeliverSpecified || resolution.Needs[0].Capability == coretool.CapabilityID("document.generate.file") {
		t.Fatal("attachment_delivery must not resolve to specified_target or document.generate.file")
	}
}

func TestReviewedDynamicIntentRulesResolveShellWithoutFileWrite(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelShellCommand, Confidence: .99,
			ToolNames: []string{"bash", "ssh", "write_file"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "运行 echo hi"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityShellExecute {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityFileWrite || resolution.Needs[0].Capability == coretool.CapabilityShellExecuteRemoteHost {
		t.Fatal("shell must not resolve to fs.write.local or shell.execute.remote_host")
	}
}

func TestReviewedDynamicIntentRulesResolveSSHWithoutLocalShell(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelSSH, Confidence: .99,
			ToolNames: []string{"ssh", "bash", "write_file"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "登录服务器查看日志"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilitySSHExecute {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityShellExecute || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("ssh must not resolve to shell.execute.local or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveBrowserWithoutLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelBrowser, Confidence: .99,
			ToolNames: []string{"browser", "web_search", "web_fetch"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "打开网页登录账号然后发布内容"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityBrowserControl {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup || resolution.Needs[0].Capability == CapabilityWebFetch {
		t.Fatal("browser must not resolve to information.lookup or information.fetch.web")
	}
}

func TestReviewedDynamicIntentRulesResolveComputerUseWithoutBrowser(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelComputerUse, Confidence: .99,
			ToolNames: []string{"computer_use", "browser", "screenshot"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "看一下桌面"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityComputerUse {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityBrowserControl {
		t.Fatal("computer_use must not resolve to browser.control.web")
	}
}

func TestReviewedDynamicIntentRulesResolveDelegateWithoutTaskTrack(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelDelegateTask, Confidence: .99,
			ToolNames: []string{"delegate_task", "task", "bash"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "交给子代理"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityDelegateSubtask {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityTaskTrack || resolution.Needs[0].Capability == CapabilitySessionManage {
		t.Fatal("delegate must not resolve to task.track.local or session.manage.coding")
	}
}

func TestReviewedDynamicIntentRulesResolveKnowledgeWriteWithoutReadOrFileWrite(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelKnowledgeWrite, Confidence: .99,
			ToolNames: []string{"knowledge_save_text", "knowledge_save_url", "knowledge_import_files", "write_file", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "save this note into the knowledge base for future retrieval"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityKnowledgeWrite {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityKnowledgeRead || resolution.Needs[0].Capability == CapabilityFileWrite || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("knowledge_write must not resolve to knowledge.read.local, fs.write.local, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveMemoryManageWithoutKnowledge(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelMemoryManage, Confidence: .99,
			ToolNames: []string{"memory", "knowledge_save_text", "knowledge_search", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "remember that I prefer Chinese"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityMemoryManage {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityKnowledgeRead || resolution.Needs[0].Capability == CapabilityKnowledgeWrite || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("memory_manage must not resolve to knowledge.read.local, knowledge.ingest.local, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveTaskTrackWithoutGoalOrDelegate(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelTaskTrack, Confidence: .99,
			ToolNames: []string{"task", "manage_schedule", "delegate_task", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "show my current todo list"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityTaskTrack {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == coretool.CapabilityGoalManageLongRunning || resolution.Needs[0].Capability == coretool.CapabilityAgentDelegateSubtask || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("task_track must not resolve to goal.manage.longrunning, agent.delegate.subtask, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveGoalManageWithoutTaskOrDelegate(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelGoalManage, Confidence: .99,
			ToolNames: []string{"goal", "task", "delegate_task", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "create a long-running goal to keep this documentation up to date"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityGoalManage {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityTaskTrack || resolution.Needs[0].Capability == coretool.CapabilityAgentDelegateSubtask || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("goal_manage must not resolve to task.track.local, agent.delegate.subtask, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveTemplateManageWithoutSessionOrConfig(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelTemplateManage, Confidence: .99,
			ToolNames: []string{"manage_template", "launch_template", "list_sessions", "manage_config", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "create a session template that uses codex"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityTemplateManage {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == coretool.CapabilitySessionManageCoding || resolution.Needs[0].Capability == coretool.CapabilityConfigManageSelf || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("template_manage must not resolve to session.manage.coding, config.manage.self, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveScheduleManageWithoutDispatchOrTask(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelScheduleManage, Confidence: .99,
			ToolNames: []string{"manage_schedule", "schedule_administer", "task", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "list all scheduled tasks"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityScheduleAdminister {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == coretool.CapabilityScheduleDispatchChannel || resolution.Needs[0].Capability == CapabilityTaskTrack || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("schedule_manage must not resolve to schedule.dispatch.channel, task.track.local, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveScheduleDispatchWithoutSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelScheduleDispatch, Confidence: .99,
			ToolNames: []string{"manage_schedule", "send_to_im", "im_message", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "每天早上发给群里"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 2 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	got := map[coretool.CapabilityID]bool{}
	for _, need := range resolution.Needs {
		got[need.Capability] = true
	}
	if !got[CapabilityScheduleAdminister] || !got[CapabilityScheduleDispatch] {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if got[CapabilityTaskTrack] || got[CapabilityMessageSend] || got[CapabilityInformationLookup] {
		t.Fatal("schedule_dispatch must not resolve to task.track.local, message.send.im, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveKnowledgeAdminWithoutReadOrIngest(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelKnowledgeAdmin, Confidence: .99,
			ToolNames: []string{"knowledge_maintain", "knowledge_search", "knowledge_save_text", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "disable this knowledge base source"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityKnowledgeAdmin {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityKnowledgeRead || resolution.Needs[0].Capability == CapabilityKnowledgeWrite || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("knowledge_admin must not resolve to knowledge.read.local, knowledge.ingest.local, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesResolveConfigManageWithoutLookupOrSession(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelConfigManage, Confidence: .99,
			ToolNames: []string{"manage_config", "switch_llm_provider", "set_max_iterations", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "raise the max iteration limit"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityConfigManage {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityInformationLookup || resolution.Needs[0].Capability == coretool.CapabilitySessionManageCoding {
		t.Fatal("config_manage must not resolve to information.lookup or session.manage.coding")
	}
}

func TestReviewedDynamicIntentRulesResolveSessionManageWithoutDriveOrDelegate(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelSessionManage, Confidence: .99,
			ToolNames: []string{"list_sessions", "interrupt_session", "send_input", "delegate_task", "web_search"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "list my running coding sessions"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilitySessionManage {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
	if resolution.Needs[0].Capability == CapabilityTemplateManage || resolution.Needs[0].Capability == coretool.CapabilityAgentDelegateSubtask || resolution.Needs[0].Capability == CapabilityInformationLookup {
		t.Fatal("session_manage must not resolve to template.manage.session, agent.delegate.subtask, or information.lookup")
	}
}

func TestReviewedDynamicIntentRulesDoNotImportGUIMCatalog(t *testing.T) {
	rules := ReviewedDynamicIntentCapabilityNeedRules()
	generateNeeds, ok := rules[intent.LabelDocumentGenerate]
	if !ok || len(generateNeeds) != 2 || generateNeeds[0].Capability != CapabilityDocumentGenerate || generateNeeds[0].Qualifiers[QualifierDocumentFormat] != DocumentFormatPDF || generateNeeds[1].Capability != CapabilityArtifactDeliverCurrent || generateNeeds[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile {
		t.Fatalf("document_generate rule=%#v", generateNeeds)
	}
	if generateNeeds[0].Capability == CapabilityOfficeWrite || generateNeeds[0].Capability == CapabilityFileWrite {
		t.Fatal("document_generate must not map onto document.write.office or fs.write.local")
	}
	audioSynthNeeds, ok := rules[intent.LabelAudioSynthesize]
	if !ok || len(audioSynthNeeds) != 1 || audioSynthNeeds[0].Capability != CapabilityAudioSynthesize {
		t.Fatalf("audio_synthesize rule=%#v", audioSynthNeeds)
	}
	if audioSynthNeeds[0].Capability == CapabilityAudioRender || audioSynthNeeds[0].Capability == CapabilityAudioTranscribe {
		t.Fatal("audio_synthesize must not map onto audio.render.speech or audio.transcribe.speech")
	}
	for _, label := range []intent.IntentLabel{
		intent.LabelAudioRecord,
		intent.LabelBusinessData,
		intent.LabelCoding,
		intent.LabelBugFix,
		intent.LabelMaintenance,
		intent.LabelNonCoding,
		intent.LabelContinuation,
		intent.LabelWorkflowTask,
		intent.LabelAmbiguous,
		intent.LabelUnknown,
	} {
		if _, ok := rules[label]; ok {
			t.Fatalf("dynamic rules must not map %s until a reviewed host receipt exists", label)
		}
	}
	audioDeliverNeeds, ok := rules[intent.LabelAudioDeliver]
	if !ok || len(audioDeliverNeeds) != 2 || audioDeliverNeeds[0].Capability != CapabilityAudioRender || audioDeliverNeeds[1].Capability != CapabilityArtifactDeliverCurrent || audioDeliverNeeds[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatVoice {
		t.Fatalf("audio_deliver rule=%#v", audioDeliverNeeds)
	}
	if audioDeliverNeeds[0].Capability == CapabilityAudioTranscribe {
		t.Fatal("audio_deliver must not map onto audio.transcribe.speech")
	}
	documentOpenNeeds, ok := rules[intent.LabelDocumentOpen]
	if !ok || len(documentOpenNeeds) != 1 || documentOpenNeeds[0].Capability != CapabilitySystemLaunch || documentOpenNeeds[0].Qualifiers[QualifierLaunchKind] != LaunchKindDocument {
		t.Fatalf("document_open rule=%#v", documentOpenNeeds)
	}
	if documentOpenNeeds[0].Capability == CapabilityDocumentRead || documentOpenNeeds[0].Capability == CapabilityArtifactDeliverSpecified {
		t.Fatal("document_open must not map onto document.read.local or specified-target deliver")
	}
	appLaunchNeeds, ok := rules[intent.LabelAppLaunch]
	if !ok || len(appLaunchNeeds) != 1 || appLaunchNeeds[0].Capability != CapabilitySystemLaunch || appLaunchNeeds[0].Qualifiers[QualifierLaunchKind] != LaunchKindURL {
		t.Fatalf("app_launch rule=%#v", appLaunchNeeds)
	}
	if appLaunchNeeds[0].Qualifiers[QualifierLaunchKind] == LaunchKindDocument || appLaunchNeeds[0].Capability == CapabilityBrowserControl {
		t.Fatal("app_launch must not map onto document open or browser.control.web")
	}
	deliverNeeds, ok := rules[intent.LabelDocumentDelivery]
	if !ok || len(deliverNeeds) != 1 || deliverNeeds[0].Capability != CapabilityArtifactDeliverSpecified || deliverNeeds[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile {
		t.Fatalf("document_delivery rule=%#v", deliverNeeds)
	}
	if deliverNeeds[0].Capability == CapabilityOfficeWrite || deliverNeeds[0].Capability == CapabilityFileWrite || deliverNeeds[0].Capability == CapabilityMessageSend {
		t.Fatal("document_delivery must not map onto document.write.office, fs.write.local, or message.send.im")
	}
	attachNeeds, ok := rules[intent.LabelAttachmentDelivery]
	if !ok || len(attachNeeds) != 1 || attachNeeds[0].Capability != CapabilityArtifactDeliverCurrent || attachNeeds[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile {
		t.Fatalf("attachment_delivery rule=%#v", attachNeeds)
	}
	if attachNeeds[0].Capability == CapabilityArtifactDeliverSpecified || attachNeeds[0].Capability == CapabilityOfficeWrite {
		t.Fatal("attachment_delivery must not map onto specified_target or document.write.office")
	}
	ingestNeeds, ok := rules[intent.LabelKnowledgeWrite]
	if !ok || len(ingestNeeds) != 1 || ingestNeeds[0].Capability != CapabilityKnowledgeWrite {
		t.Fatalf("knowledge_write rule=%#v", ingestNeeds)
	}
	if ingestNeeds[0].Capability == CapabilityKnowledgeRead || ingestNeeds[0].Capability == CapabilityFileWrite {
		t.Fatal("knowledge_write must not map onto knowledge.read.local or fs.write.local")
	}
	downloadNeeds, ok := rules[intent.LabelFileDownload]
	if !ok || len(downloadNeeds) != 1 || downloadNeeds[0].Capability != CapabilityFileDownload {
		t.Fatalf("file_download rule=%#v", downloadNeeds)
	}
	if downloadNeeds[0].Capability == CapabilityWebFetch || downloadNeeds[0].Capability == CapabilityFileWrite {
		t.Fatal("file_download must not map onto information.fetch.web or fs.write.local")
	}
	screenshotNeeds, ok := rules[intent.LabelScreenshot]
	if !ok || len(screenshotNeeds) != 2 || screenshotNeeds[0].Capability != CapabilityVisualCapture || screenshotNeeds[0].Qualifiers[QualifierCaptureDisplay] != CaptureDisplayPrimary || screenshotNeeds[1].Capability != CapabilityArtifactDeliverCurrent || screenshotNeeds[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatImage {
		t.Fatalf("screenshot rule=%#v", screenshotNeeds)
	}
	if screenshotNeeds[0].Capability == CapabilityComputerUse {
		t.Fatal("screenshot must not map onto computer.control.desktop")
	}
	browserNeeds, ok := rules[intent.LabelBrowser]
	if !ok || len(browserNeeds) != 1 || browserNeeds[0].Capability != CapabilityBrowserControl {
		t.Fatalf("browser rule=%#v", browserNeeds)
	}
	if browserNeeds[0].Capability == CapabilityInformationLookup || browserNeeds[0].Capability == CapabilityWebFetch {
		t.Fatal("browser must not map onto information.lookup or information.fetch.web")
	}
	writeNeeds, ok := rules[intent.LabelFileWrite]
	if !ok || len(writeNeeds) != 1 || writeNeeds[0].Capability != CapabilityFileWrite {
		t.Fatalf("file_write rule=%#v", writeNeeds)
	}
	officeNeeds, ok := rules[intent.LabelOffice]
	if !ok || len(officeNeeds) != 1 || officeNeeds[0].Capability != CapabilityOfficeWrite || officeNeeds[0].Qualifiers[QualifierDocumentFormat] != DocumentFormatSpreadsheet {
		t.Fatalf("office write rule=%#v", officeNeeds)
	}
	if officeNeeds[0].Capability == CapabilityFileWrite {
		t.Fatal("office write must not map onto fs.write.local")
	}
	if writeNeeds[0].Capability == CapabilityFileRead || writeNeeds[0].Capability == coretool.CapabilityKnowledgeIngestLocal {
		t.Fatal("file_write must not map onto fs.read.local or knowledge.ingest.local")
	}
	documentNeeds, ok := rules[intent.LabelDocumentRead]
	if !ok || len(documentNeeds) != 1 || documentNeeds[0].Capability != CapabilityDocumentRead {
		t.Fatalf("document_read rule=%#v", documentNeeds)
	}
	if documentNeeds[0].Capability == CapabilityFileRead || documentNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("document_read must not map onto fs.read.local or information.lookup")
	}
	audioNeeds, ok := rules[intent.LabelAudioTranscribe]
	if !ok || len(audioNeeds) != 1 || audioNeeds[0].Capability != CapabilityAudioTranscribe {
		t.Fatalf("audio_transcribe rule=%#v", audioNeeds)
	}
	if audioNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("audio_transcribe must not map onto information.lookup")
	}
	shellNeeds, ok := rules[intent.LabelShellCommand]
	if !ok || len(shellNeeds) != 1 || shellNeeds[0].Capability != CapabilityShellExecute {
		t.Fatalf("shell rule=%#v", shellNeeds)
	}
	if shellNeeds[0].Capability == CapabilityFileWrite || shellNeeds[0].Capability == coretool.CapabilityShellExecuteRemoteHost {
		t.Fatal("shell must not map onto fs.write.local or shell.execute.remote_host")
	}
	clockNeeds, ok := rules[intent.LabelCurrentTime]
	if !ok || len(clockNeeds) != 1 || clockNeeds[0].Capability != CapabilityCurrentTime {
		t.Fatalf("current_time rule=%#v", clockNeeds)
	}
	if clockNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("current_time must not map onto information.lookup")
	}
	knowledgeNeeds, ok := rules[intent.LabelKnowledgeRead]
	if !ok || len(knowledgeNeeds) != 1 || knowledgeNeeds[0].Capability != CapabilityKnowledgeRead {
		t.Fatalf("knowledge_read rule=%#v", knowledgeNeeds)
	}
	if knowledgeNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("knowledge_read must not map onto information.lookup")
	}
	auditNeeds, ok := rules[intent.LabelAuditRead]
	if !ok || len(auditNeeds) != 1 || auditNeeds[0].Capability != CapabilityAuditRead {
		t.Fatalf("audit_read rule=%#v", auditNeeds)
	}
	if auditNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("audit_read must not map onto information.lookup")
	}
	fetchNeeds, ok := rules[intent.LabelWebFetch]
	if !ok || len(fetchNeeds) != 1 || fetchNeeds[0].Capability != CapabilityWebFetch {
		t.Fatalf("web_fetch rule=%#v", fetchNeeds)
	}
	if fetchNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("web_fetch must not map onto information.lookup")
	}
	fileNeeds, ok := rules[intent.LabelFileRead]
	if !ok || len(fileNeeds) != 1 || fileNeeds[0].Capability != CapabilityFileRead {
		t.Fatalf("file_read rule=%#v", fileNeeds)
	}
	if fileNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("file_read must not map onto information.lookup")
	}
	gitNeeds, ok := rules[intent.LabelGitInspect]
	if !ok || len(gitNeeds) != 1 || gitNeeds[0].Capability != CapabilityRepoInspect {
		t.Fatalf("git_inspect rule=%#v", gitNeeds)
	}
	if gitNeeds[0].Capability == CapabilityInformationLookup {
		t.Fatal("git_inspect must not map onto information.lookup")
	}
	memoryNeeds, ok := rules[intent.LabelMemoryManage]
	if !ok || len(memoryNeeds) != 1 || memoryNeeds[0].Capability != CapabilityMemoryManage {
		t.Fatalf("memory_manage rule=%#v", memoryNeeds)
	}
	if memoryNeeds[0].Capability == CapabilityKnowledgeRead || memoryNeeds[0].Capability == CapabilityKnowledgeWrite {
		t.Fatal("memory_manage must not map onto knowledge.read.local or knowledge.ingest.local")
	}
	taskNeeds, ok := rules[intent.LabelTaskTrack]
	if !ok || len(taskNeeds) != 1 || taskNeeds[0].Capability != CapabilityTaskTrack {
		t.Fatalf("task_track rule=%#v", taskNeeds)
	}
	if taskNeeds[0].Capability == coretool.CapabilityGoalManageLongRunning || taskNeeds[0].Capability == coretool.CapabilityAgentDelegateSubtask {
		t.Fatal("task_track must not map onto goal.manage.longrunning or agent.delegate.subtask")
	}
	goalNeeds, ok := rules[intent.LabelGoalManage]
	if !ok || len(goalNeeds) != 1 || goalNeeds[0].Capability != CapabilityGoalManage {
		t.Fatalf("goal_manage rule=%#v", goalNeeds)
	}
	if goalNeeds[0].Capability == CapabilityTaskTrack || goalNeeds[0].Capability == coretool.CapabilityAgentDelegateSubtask {
		t.Fatal("goal_manage must not map onto task.track.local or agent.delegate.subtask")
	}
	templateNeeds, ok := rules[intent.LabelTemplateManage]
	if !ok || len(templateNeeds) != 1 || templateNeeds[0].Capability != CapabilityTemplateManage {
		t.Fatalf("template_manage rule=%#v", templateNeeds)
	}
	if templateNeeds[0].Capability == coretool.CapabilitySessionManageCoding || templateNeeds[0].Capability == coretool.CapabilityConfigManageSelf {
		t.Fatal("template_manage must not map onto session.manage.coding or config.manage.self")
	}
	scheduleNeeds, ok := rules[intent.LabelScheduleManage]
	if !ok || len(scheduleNeeds) != 1 || scheduleNeeds[0].Capability != CapabilityScheduleAdminister {
		t.Fatalf("schedule_manage rule=%#v", scheduleNeeds)
	}
	if scheduleNeeds[0].Capability == coretool.CapabilityScheduleDispatchChannel || scheduleNeeds[0].Capability == CapabilityTaskTrack {
		t.Fatal("schedule_manage must not map onto schedule.dispatch.channel or task.track.local")
	}
	dispatchNeeds, ok := rules[intent.LabelScheduleDispatch]
	if !ok || len(dispatchNeeds) != 2 {
		t.Fatalf("schedule_dispatch rule=%#v", dispatchNeeds)
	}
	if dispatchNeeds[0].Capability != CapabilityScheduleAdminister || dispatchNeeds[1].Capability != CapabilityScheduleDispatch {
		t.Fatalf("schedule_dispatch needs=%#v", dispatchNeeds)
	}
	if dispatchNeeds[0].Capability == CapabilityTaskTrack || dispatchNeeds[1].Capability == CapabilityMessageSend {
		t.Fatal("schedule_dispatch must not map onto task.track.local or message.send.im")
	}
	adminNeeds, ok := rules[intent.LabelKnowledgeAdmin]
	if !ok || len(adminNeeds) != 1 || adminNeeds[0].Capability != CapabilityKnowledgeAdmin {
		t.Fatalf("knowledge_admin rule=%#v", adminNeeds)
	}
	if adminNeeds[0].Capability == CapabilityKnowledgeRead || adminNeeds[0].Capability == CapabilityKnowledgeWrite {
		t.Fatal("knowledge_admin must not map onto knowledge.read.local or knowledge.ingest.local")
	}
	configNeeds, ok := rules[intent.LabelConfigManage]
	if !ok || len(configNeeds) != 1 || configNeeds[0].Capability != CapabilityConfigManage {
		t.Fatalf("config_manage rule=%#v", configNeeds)
	}
	if configNeeds[0].Capability == CapabilityInformationLookup || configNeeds[0].Capability == coretool.CapabilitySessionManageCoding {
		t.Fatal("config_manage must not map onto information.lookup or session.manage.coding")
	}
	sessionNeeds, ok := rules[intent.LabelSessionManage]
	if !ok || len(sessionNeeds) != 1 || sessionNeeds[0].Capability != CapabilitySessionManage {
		t.Fatalf("session_manage rule=%#v", sessionNeeds)
	}
	if sessionNeeds[0].Capability == CapabilityTemplateManage || sessionNeeds[0].Capability == coretool.CapabilityAgentDelegateSubtask {
		t.Fatal("session_manage must not map onto template.manage.session or agent.delegate.subtask")
	}
	delegateNeeds, ok := rules[intent.LabelDelegateTask]
	if !ok || len(delegateNeeds) != 1 || delegateNeeds[0].Capability != CapabilityDelegateSubtask {
		t.Fatalf("delegate rule=%#v", delegateNeeds)
	}
	if delegateNeeds[0].Capability == CapabilityTaskTrack || delegateNeeds[0].Capability == CapabilitySessionManage {
		t.Fatal("delegate must not map onto task.track.local or session.manage.coding")
	}
	sshNeeds, ok := rules[intent.LabelSSH]
	if !ok || len(sshNeeds) != 1 || sshNeeds[0].Capability != CapabilitySSHExecute {
		t.Fatalf("ssh rule=%#v", sshNeeds)
	}
	if sshNeeds[0].Capability == CapabilityShellExecute {
		t.Fatal("ssh must not map onto shell.execute.local")
	}
	cuNeeds, ok := rules[intent.LabelComputerUse]
	if !ok || len(cuNeeds) != 1 || cuNeeds[0].Capability != CapabilityComputerUse {
		t.Fatalf("computer_use rule=%#v", cuNeeds)
	}
	if cuNeeds[0].Capability == CapabilityBrowserControl {
		t.Fatal("computer_use must not map onto browser.control.web")
	}
	mutateNeeds, ok := rules[intent.LabelGitMutate]
	if !ok || len(mutateNeeds) != 1 || mutateNeeds[0].Capability != CapabilityRepoMutate {
		t.Fatalf("git_mutate rule=%#v", mutateNeeds)
	}
	if mutateNeeds[0].Capability == CapabilityRepoInspect {
		t.Fatal("git_mutate must not map onto repo.inspect.vcs")
	}
	// message.send.im stays quarantined: pushing a message to an IM channel is
	// an external effect the host cannot read back, so no label may reach it.
	// repo.mutate.vcs left quarantine when its push grew a receipt that is read
	// from the remote ref rather than taken from the push command's exit code.
	for label, needs := range rules {
		for _, need := range needs {
			if need.Capability == CapabilityMessageSend {
				t.Fatalf("UIC label %s must not name quarantined %s", label, need.Capability)
			}
		}
	}
	if len(rules) != 37 {
		t.Fatalf("reviewed dynamic rules=%d, want search, live_data, current_time, knowledge_read, audit_read, web_fetch, file_download, file_read, git_inspect, git_mutate, document_read, audio_transcribe, file_write, office_write, shell, delegate, ssh, browser, computer_use, knowledge_write, memory_manage, task_track, goal_manage, template_manage, schedule_manage, schedule_dispatch, knowledge_admin, config_manage, session_manage, document_delivery, attachment_delivery, document_generate, audio_deliver, audio_synthesize, screenshot, document_open, and app_launch", len(rules))
	}
}

func TestReviewedDynamicPolicyDeniesLookupForOpsControlled(t *testing.T) {
	_, constraints, err := ReviewedDynamicCapabilityPolicyAdapter().DynamicCapabilityConstraints(DynamicCapabilityNeedRequest{WorkflowPolicy: "ops_controlled"})
	if err != nil || len(constraints) != 1 || constraints[0].Capability != CapabilityInformationLookup || constraints[0].Effect != "deny" {
		t.Fatalf("constraints=%#v err=%v", constraints, err)
	}
}
