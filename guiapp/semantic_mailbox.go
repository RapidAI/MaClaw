package guiapp

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedMailboxSendAdapter             = "semantic_send_trusted_mailbox"
	semanticTrustedMailboxSendImplementation      = "trusted-mailbox-send-v1"
	semanticTrustedMailboxConfigureAdapter        = "semantic_configure_trusted_mailbox"
	semanticTrustedMailboxConfigureImplementation = "trusted-mailbox-configure-v1"
	semanticTrustedMailboxDeleteAdapter           = "semantic_delete_trusted_mailbox"
	semanticTrustedMailboxDeleteImplementation    = "trusted-mailbox-delete-v1"
)

func semanticUnpublishedLegacyMailboxSendProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMessageSendMailbox {
			return true
		}
	}
	return false
}

func semanticUnpublishedLegacyMailboxDeleteProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMessageDeleteMailbox {
			return true
		}
	}
	return false
}

func semanticUnpublishedLegacyMailboxConfigureProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMessageConfigureMailbox {
			return true
		}
	}
	return false
}

func semanticTrustedMailboxSendDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMailboxSendAdapter,
			"description": mailbox.ToolDescriptionSend(),
			"parameters":  semanticTrustedMailboxSendInvocationSchema(),
		},
	}
}

func semanticTrustedMailboxSendInvocationSchema() map[string]interface{} {
	return mailbox.SendInvocationSchema()
}

func semanticTrustedMailboxConfigureDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMailboxConfigureAdapter,
			"description": mailbox.ToolDescriptionConfigure(),
			"parameters":  semanticTrustedMailboxConfigureInvocationSchema(),
		},
	}
}

func semanticTrustedMailboxConfigureInvocationSchema() map[string]interface{} {
	return mailbox.ConfigureInvocationSchema()
}

func semanticTrustedMailboxDeleteDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMailboxDeleteAdapter,
			"description": mailbox.ToolDescriptionDelete(),
			"parameters":  semanticTrustedMailboxDeleteInvocationSchema(),
		},
	}
}

func semanticTrustedMailboxDeleteInvocationSchema() map[string]interface{} {
	return mailbox.DeleteInvocationSchema()
}

// semanticMailboxSendReceiptSelection is the host receipt for message.send.mailbox.
// The same process freezes the draft and later observes Approve. The prepare
// return is a confirmation, not the SMTP observation, so this is not the
// host-observed allow-list whose handler result is already the effect.
func semanticMailboxSendReceiptSelection(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") {
		return false
	}
	if strings.TrimSpace(selection.AdapterName) != semanticTrustedMailboxSendAdapter {
		return false
	}
	for _, effect := range selection.Effects {
		if effect == tool.EffectExternalEffect {
			return true
		}
	}
	return false
}

// semanticMailboxDeleteReceiptSelection is the host receipt for
// message.delete.mailbox. The first call freezes a draft. IMAP runs on Approve.
func semanticMailboxDeleteReceiptSelection(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") {
		return false
	}
	if strings.TrimSpace(selection.AdapterName) != semanticTrustedMailboxDeleteAdapter {
		return false
	}
	for _, effect := range selection.Effects {
		if effect == tool.EffectExternalEffect {
			return true
		}
	}
	return false
}

func semanticMailboxConfigureSelection(selection tool.PlannedSelection) bool {
	return strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") &&
		strings.TrimSpace(selection.AdapterName) == semanticTrustedMailboxConfigureAdapter
}

func (c *sharedAgentLoopCallbacks) executeTrustedMailboxSend(ctx context.Context, canonicalArgs tool.CanonicalRequest) tool.SelectionExecutionResult {
	if c == nil || c.handler == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", Succeeded: false, ReasonCode: "semantic_surface_unavailable"}
	}
	args := canonicalArgs.Values
	if args == nil {
		args = map[string]interface{}{}
	}
	text := c.handler.toolMailboxSend(withMailboxOwner(ctx, c.mailboxPrincipalID()), args)
	switch {
	case strings.HasPrefix(text, "__ASK_USER__"):
		// The draft is frozen. SMTP waits for Approve on the confirmation.
		// This is a prepared external effect, not a failed send: recording it
		// as a failure makes a later call to the same stable name report that
		// the message did not go out. The selection stays incomplete, and the
		// grant leaves the model list so a second call cannot freeze another
		// message.
		return tool.SelectionExecutionResult{Result: text, Succeeded: false, AwaitingReceipt: true, ReasonCode: mailboxSendAwaitingConfirmation}
	case text == mailbox.MsgUncertain || strings.HasPrefix(strings.TrimSpace(text), "[system unknown]"):
		return mailboxTypedFailure(text, "mailbox_send_uncertain", true)
	default:
		return mailboxTypedSendResult(text)
	}
}

const mailboxSendAwaitingConfirmation = "mailbox_send_awaiting_confirmation"
const mailboxDeleteAwaitingConfirmation = "mailbox_delete_awaiting_confirmation"

func (c *sharedAgentLoopCallbacks) executeTrustedMailboxDelete(ctx context.Context, canonicalArgs tool.CanonicalRequest) tool.SelectionExecutionResult {
	if c == nil || c.handler == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", Succeeded: false, ReasonCode: "semantic_surface_unavailable"}
	}
	args := canonicalArgs.Values
	if args == nil {
		args = map[string]interface{}{}
	}
	text := c.handler.toolMailboxDelete(withMailboxOwner(ctx, c.mailboxPrincipalID()), args)
	switch {
	case strings.HasPrefix(text, "__ASK_USER__"):
		return tool.SelectionExecutionResult{Result: text, Succeeded: false, AwaitingReceipt: true, ReasonCode: mailboxDeleteAwaitingConfirmation}
	case text == mailbox.MsgDeleteUncertain || strings.HasPrefix(strings.TrimSpace(text), "[system unknown]"):
		return mailboxTypedFailure(text, "mailbox_delete_uncertain", true)
	default:
		return mailboxTypedDeleteResult(text)
	}
}

func mailboxTypedSendResult(text string) tool.SelectionExecutionResult {
	if mailboxSendSucceeded(text) {
		return tool.SelectionExecutionResult{Result: strings.TrimSpace(text), Succeeded: true}
	}
	return mailboxTypedFailure(text, "mailbox_send_not_sent", false)
}

func mailboxTypedDeleteResult(text string) tool.SelectionExecutionResult {
	if mailboxDeleteSucceeded(text) {
		return tool.SelectionExecutionResult{Result: strings.TrimSpace(text), Succeeded: true}
	}
	return mailboxTypedFailure(text, "mailbox_delete_not_deleted", false)
}

func mailboxSendSucceeded(text string) bool {
	return strings.TrimSpace(text) == "邮件已发送"
}

func mailboxDeleteSucceeded(text string) bool {
	switch strings.TrimSpace(text) {
	case mailbox.MsgDeleteMoved, mailbox.MsgDeletePurged, mailbox.MsgDeleteMovedAndPurged:
		return true
	default:
		return false
	}
}

func mailboxTypedFailure(text, code string, unknown bool) tool.SelectionExecutionResult {
	text = strings.TrimSpace(text)
	if text == "" {
		text = code
	}
	prefix := "[system rejected]"
	if unknown {
		prefix = "[system unknown]"
	}
	if !strings.HasPrefix(text, prefix) {
		text = prefix + " " + text
	}
	return tool.SelectionExecutionResult{Result: text, Succeeded: false, Unknown: unknown, ReasonCode: code}
}

func (c *sharedAgentLoopCallbacks) executeTrustedMailboxConfigure(ctx context.Context, canonicalArgs tool.CanonicalRequest) tool.SelectionExecutionResult {
	if c == nil || c.handler == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", Succeeded: false, ReasonCode: "semantic_surface_unavailable"}
	}
	args := canonicalArgs.Values
	if args == nil {
		args = map[string]interface{}{}
	}
	if reason := mailbox.ConfigureShapeReason(args); reason != "" {
		return tool.SelectionExecutionResult{Result: "[system rejected] " + reason, Succeeded: false, ReasonCode: reason}
	}
	text, err := c.handler.applyMailboxConfigure(withMailboxOwner(ctx, c.mailboxPrincipalID()), args)
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] " + configureMailboxToolErr(err, stringArg(args, "password")), Succeeded: false, ReasonCode: "mailbox_configure_rejected"}
	}
	return tool.SelectionExecutionResult{Result: text, Succeeded: true}
}

func (c *sharedAgentLoopCallbacks) mailboxPrincipalID() string {
	if c == nil {
		return ""
	}
	if id := strings.TrimSpace(c.userID); id != "" {
		return id
	}
	if c.semanticSurface != nil {
		return strings.TrimSpace(c.semanticSurface.scope.PrincipalID)
	}
	return ""
}

// mailboxIntakeRejectReason refuses a send or configure call before the grant
// is admitted. A shape the adapter cannot freeze, or a configure call with no
// server, must not retire the one-shot grant and must not write config.
func (c *sharedAgentLoopCallbacks) mailboxIntakeRejectReasonForName(name, argsJSON string) string {
	if c == nil || c.semanticSurface == nil {
		return ""
	}
	grant, ok := c.semanticSurface.grants[name]
	if !ok {
		return ""
	}
	selection, found := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !found {
		return ""
	}
	return c.mailboxIntakeRejectReason(argsJSON, selection)
}

func (c *sharedAgentLoopCallbacks) mailboxIntakeRejectReason(argsJSON string, selection tool.PlannedSelection) string {
	isSend := semanticMailboxSendReceiptSelection(selection)
	isDelete := semanticMailboxDeleteReceiptSelection(selection)
	if !isSend && !isDelete && !semanticMailboxConfigureSelection(selection) {
		return ""
	}
	canonical, err := c.semanticCanonicalArguments(selection, argsJSON)
	if err != nil {
		return err.Error()
	}
	args := canonical.Values
	if args == nil {
		var parsed map[string]interface{}
		if json.Unmarshal(canonical.CanonicalJSON, &parsed) != nil {
			return "parameter_schema_invalid"
		}
		args = parsed
	}
	if isSend {
		return mailboxSendIntakeReason(args)
	}
	if isDelete {
		return mailbox.DeleteShapeReason(args)
	}
	return mailboxConfigureIntakeReason(args)
}

func mailboxSendIntakeReason(args map[string]interface{}) string {
	return mailbox.SendShapeReason(args)
}

func mailboxConfigureIntakeReason(args map[string]interface{}) string {
	return mailbox.ConfigureShapeReason(args)
}

// semanticRetiredAwaitingReceipt reports a spent selection whose effect was
// prepared and has not been observed. A frozen mailbox draft uses this state;
// a failed attempt does not.
func semanticRetiredAwaitingReceipt(surface *semanticCallSurface, selectionID string) (bool, string) {
	if surface == nil || surface.executor == nil {
		return false, ""
	}
	record, err := surface.executor.Execution(surface.scope, selectionID)
	if err != nil {
		return false, ""
	}
	reason := strings.TrimSpace(record.ReasonCode)
	switch record.State {
	case tool.PlanExecutionAwaitingReceipt:
		return true, reason
	case tool.PlanExecutionFailed:
		// Older drafts were stored as failures with this reason.
		if reason == mailboxSendAwaitingConfirmation || reason == mailboxDeleteAwaitingConfirmation {
			return true, reason
		}
	}
	return false, ""
}

func mailboxDraftAwaitingConfirmation(reason string) bool {
	switch reason {
	case mailboxSendAwaitingConfirmation, mailboxDeleteAwaitingConfirmation:
		return true
	default:
		return false
	}
}

// semanticGovernedEffectPlanningRewrite applies the governing act before an
// immediate mailbox delete. A schedule whose body is a mail delete plans
// schedule administration. An immediate mail delete still plans mailbox_delete.
// File delete is decided by the caller first; both rewrites also leave it alone.
func semanticGovernedEffectPlanningRewrite(current *intent.ClassificationResult, userText string) *intent.ClassificationResult {
	if lifted := semanticSchedulePayloadPlanningRewrite(current, userText); lifted != nil {
		return lifted
	}
	if lifted := semanticMailboxDeletePlanningRewrite(current, userText); lifted != nil {
		return lifted
	}
	return semanticMailboxSendPlanningRewrite(current, userText)
}

func logGovernedEffectPlanning(userID, reason string) {
	switch {
	case strings.Contains(reason, "schedule payload planning"):
		log.Printf("[semantic-routing] schedule payload planning user=%q", userID)
	case strings.Contains(reason, "mailbox send planning"):
		log.Printf("[semantic-routing] mailbox send planning user=%q", userID)
	default:
		log.Printf("[semantic-routing] mailbox delete planning user=%q", userID)
	}
}

// semanticSchedulePayloadPlanningRewrite projects a mail delete that is only
// the body of a schedule onto schedule_manage. A settled schedule
// administration verdict is already that act and is left alone, including
// schedule_dispatch. The projection is schedule administration, not dispatch.
func semanticSchedulePayloadPlanningRewrite(current *intent.ClassificationResult, userText string) *intent.ClassificationResult {
	if current == nil {
		return nil
	}
	query := acpInnerUserRequest(userText)
	if tool.QueryWantsLocalFileDelete(query) || !mailbox.QuerySchedulesMessageDelete(query) {
		return nil
	}
	if scheduleAdministrationSettled(*current) {
		return nil
	}
	out := semanticSchedulePayloadPlanningClassification(*current)
	return &out
}

func scheduleAdministrationSettled(result intent.ClassificationResult) bool {
	if result.Degraded || result.Confidence < imSemanticMinimumConfidence {
		return false
	}
	return result.Primary == intent.LabelScheduleManage || result.Primary == intent.LabelScheduleDispatch
}

func semanticSchedulePayloadPlanningClassification(result intent.ClassificationResult) intent.ClassificationResult {
	out := semanticLookupClassificationForPlanning(result)
	out.Primary = intent.LabelScheduleManage
	out.Secondary = nil
	out.ToolNames = nil
	if out.Layer != 23 && out.Layer < 3 {
		out.Layer = 3
	}
	if !strings.Contains(out.Reason, "schedule payload planning") {
		if strings.TrimSpace(out.Reason) != "" {
			out.Reason += "; "
		}
		out.Reason += "schedule payload planning"
	}
	return out
}

// semanticMailboxDeletePlanningRewrite lifts a mail-deletion sentence onto
// mailbox_delete before the profile and the plan floor see it. A local file
// delete stays a file delete. A settled schedule administration verdict is
// the governing act, so a mail verb in its payload does not replace it. A
// tree score already at the resolver floor is left alone. A non-degraded
// tree pick below that floor is raised so the plan can publish the tool.
func semanticMailboxDeletePlanningRewrite(current *intent.ClassificationResult, userText string) *intent.ClassificationResult {
	if current == nil {
		return nil
	}
	query := acpInnerUserRequest(userText)
	if tool.QueryWantsLocalFileDelete(query) {
		return nil
	}
	if mailbox.QueryWantsMessageDelete(query) {
		if mailboxDeletePlanningSettled(*current) || scheduleAdministrationSettled(*current) {
			return nil
		}
		out := semanticMailboxDeletePlanningClassification(*current)
		return &out
	}
	if current.Primary != intent.LabelMailboxDelete || current.Degraded {
		return nil
	}
	if current.Layer != 3 && current.Layer != 23 {
		return nil
	}
	if current.Confidence >= semanticLookupHintFloor {
		return nil
	}
	out := semanticMailboxDeletePlanningClassification(*current)
	return &out
}

func mailboxDeletePlanningSettled(result intent.ClassificationResult) bool {
	return result.Primary == intent.LabelMailboxDelete &&
		!result.Degraded &&
		result.Confidence >= imSemanticMinimumConfidence &&
		len(result.Secondary) == 0 &&
		len(result.ToolNames) == 0
}

func semanticMailboxDeletePlanningClassification(result intent.ClassificationResult) intent.ClassificationResult {
	out := semanticLookupClassificationForPlanning(result)
	out.Primary = intent.LabelMailboxDelete
	out.Secondary = nil
	out.ToolNames = nil
	if out.Layer != 23 && out.Layer < 3 {
		out.Layer = 3
	}
	if !strings.Contains(out.Reason, "mailbox delete planning") {
		if strings.TrimSpace(out.Reason) != "" {
			out.Reason += "; "
		}
		out.Reason += "mailbox delete planning"
	}
	return out
}

// semanticMailboxSendPlanningRewrite lifts an email send off document
// delivery and off a PDF-skill turn. document_delivery plans an IM file
// handoff, which desktop policy denies, so a sentence that names a mailbox
// never reaches SMTP. A request to generate a PDF and then send it keeps
// document_generate as a companion and does not become a skill-market task:
// the primary is mailbox_send, whose native tools are generate_pdf and
// mailbox_send. Attaching a PDF that already exists does not regenerate it.
func semanticMailboxSendPlanningRewrite(current *intent.ClassificationResult, userText string) *intent.ClassificationResult {
	if current == nil {
		return nil
	}
	query := acpInnerUserRequest(userText)
	if tool.QueryWantsLocalFileDelete(query) || !mailbox.QueryWantsMessageSend(query) {
		return nil
	}
	if mailboxSendPlanningSettled(*current, query) {
		return nil
	}
	out := semanticMailboxSendPlanningClassification(*current, query)
	return &out
}

func mailboxSendPlanningSettled(result intent.ClassificationResult, query string) bool {
	if result.Primary != intent.LabelMailboxSend || result.Degraded || result.Confidence < imSemanticMinimumConfidence {
		return false
	}
	if result.HasLabel(intent.LabelDocumentDelivery) || result.HasLabel(intent.LabelAttachmentDelivery) {
		return false
	}
	return result.HasLabel(intent.LabelDocumentGenerate) == mailbox.QueryWantsNewPDF(query)
}

func semanticMailboxSendPlanningClassification(result intent.ClassificationResult, query string) intent.ClassificationResult {
	out := semanticLookupClassificationForPlanning(result)
	// The displaced primary is a companion. Dropping it would lose a lookup
	// the sentence still needs, such as live_data beside a mailed report.
	displaced := out.Primary
	out.Primary = intent.LabelMailboxSend
	out.ToolNames = nil
	wantsPDF := mailbox.QueryWantsNewPDF(query)
	incoming := make([]intent.IntentLabel, 0, len(out.Secondary)+1)
	if displaced != "" {
		incoming = append(incoming, displaced)
	}
	incoming = append(incoming, out.Secondary...)
	kept := make([]intent.IntentLabel, 0, len(incoming)+1)
	seen := map[intent.IntentLabel]struct{}{intent.LabelMailboxSend: {}}
	seenGenerate := false
	for _, label := range incoming {
		if _, ok := seen[label]; ok {
			continue
		}
		switch label {
		case intent.LabelDocumentDelivery, intent.LabelAttachmentDelivery:
			continue
		case intent.LabelDocumentGenerate:
			if !wantsPDF {
				continue
			}
			seenGenerate = true
		}
		// An unmapped companion such as workflow_task has no capability
		// rule. Coverage then treats the whole send as managed and unmapped,
		// and the turn is rejected before mailbox_send is granted.
		if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) == 0 {
			continue
		}
		seen[label] = struct{}{}
		kept = append(kept, label)
	}
	if wantsPDF && !seenGenerate {
		kept = append(kept, intent.LabelDocumentGenerate)
	}
	out.Secondary = kept
	if out.Layer != 23 && out.Layer < 3 {
		out.Layer = 3
	}
	if !strings.Contains(out.Reason, "mailbox send planning") {
		if strings.TrimSpace(out.Reason) != "" {
			out.Reason += "; "
		}
		out.Reason += "mailbox send planning"
	}
	return out
}
