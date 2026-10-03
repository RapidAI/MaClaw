package guiapp

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// producedDocumentContext is the host-owned file a desktop task already
// materialized. The path never reaches a model schema. Every later delivery
// re-reads the file and refuses a changed or missing one.
type producedDocumentContext struct {
	CanonicalPath string
	Format        string
	MIMEType      string
	Size          int64
	ModTimeNS     int64
	Digest        string
}

// semanticDeliveryConsumesExistingDocument reports a plan whose file delivery
// has no in-turn document producer. Specified-target delivery, and
// current-channel file delivery without generate or office write, must bind
// an already materialized document. A generate or office turn consumes the
// producer it is about to run.
func semanticDeliveryConsumesExistingDocument(needs []tool.CapabilityNeed) bool {
	if tool.InTurnArtifactProducerPresent(needs, tool.CapabilityDocumentWriteOffice) {
		return false
	}
	for _, need := range needs {
		if semanticPlanCompanionNeed(need) {
			continue
		}
		switch need.Capability {
		case agentservice.CapabilityArtifactDeliverSpecified:
			format := ""
			if need.Qualifiers != nil {
				format = need.Qualifiers[agentservice.QualifierArtifactFormat]
			}
			if format == "" || format == agentservice.ArtifactFormatFile {
				return true
			}
		case agentservice.CapabilityArtifactDeliverCurrent:
			if tool.CurrentChannelDeliverAccepts(need, agentservice.ArtifactFormatFile) {
				return true
			}
		}
	}
	return false
}

func producedDocumentPathFromResponse(resp *IMAgentResponse) string {
	if resp == nil {
		return ""
	}
	paths := append([]string(nil), resp.LocalFilePaths...)
	if path := strings.TrimSpace(resp.LocalFilePath); path != "" {
		paths = append(paths, path)
	}
	found := ""
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, _, ok := agent.DocumentAttachmentFormat(filepath.Base(path), ""); !ok {
			continue
		}
		found = path
	}
	return found
}

func snapshotProducedDocument(path string) (producedDocumentContext, error) {
	clean, err := canonicalActiveLocalDocumentPath(path)
	if err != nil {
		return producedDocumentContext{}, err
	}
	format, mimeType, ok := agent.DocumentAttachmentFormat(filepath.Base(clean), "")
	if !ok {
		return producedDocumentContext{}, fmt.Errorf("produced_document_unsupported")
	}
	data, info, err := readStableActiveLocalDocument(clean)
	if err != nil {
		return producedDocumentContext{}, err
	}
	digest := sha256.Sum256(data)
	return producedDocumentContext{
		CanonicalPath: clean,
		Format:        format,
		MIMEType:      mimeType,
		Size:          info.Size(),
		ModTimeNS:     info.ModTime().UnixNano(),
		Digest:        fmt.Sprintf("%x", digest[:]),
	}, nil
}

// captureProducedDocument records the document this turn materialized.
// A delivery-only turn does not carry generate or office write, so it cannot
// replace the product it is sending.
func (h *IMMessageHandler) captureProducedDocument(msg IMUserMessage, needs []tool.CapabilityNeed, resp *IMAgentResponse) {
	if h == nil || resp == nil || !semanticResidueHasProducedDocument(needs) {
		return
	}
	key, ok := semanticResidueSessionKey(msg)
	if !ok {
		return
	}
	path := producedDocumentPathFromResponse(resp)
	if path == "" {
		return
	}
	context, err := snapshotProducedDocument(path)
	if err != nil {
		log.Printf("[semantic-routing] produced document not captured user=%q: %v", msg.UserID, err)
		return
	}
	h.producedDocuments.Store(key, context)
	log.Printf("[semantic-routing] produced document captured user=%q name=%q", msg.UserID, filepath.Base(context.CanonicalPath))
}

// semanticProducedDocumentInputsForTurn admits the captured file as this
// turn's trusted artifact. A missing snapshot is absent, not an error: the
// planner then fails the delivery closed. A changed or missing file is an
// error so planning cannot fall through to a new document.
func (h *IMMessageHandler) semanticProducedDocumentInputsForTurn(rootTaskID, turnID, sessionID, userID, channel string) ([]semanticTrustedArtifactInput, bool, error) {
	if h == nil {
		return nil, false, nil
	}
	key, ok := semanticResidueSessionKey(IMUserMessage{UserID: userID, Platform: channel})
	if !ok {
		return nil, false, nil
	}
	value, ok := h.producedDocuments.Load(key)
	if !ok {
		return nil, false, nil
	}
	context, ok := value.(producedDocumentContext)
	if !ok || strings.TrimSpace(context.CanonicalPath) == "" || len(context.Digest) < 24 {
		h.producedDocuments.Delete(key)
		return nil, true, fmt.Errorf("produced_document_invalid")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(userID) == "" {
		return nil, true, fmt.Errorf("trusted_document_input_identity_required")
	}
	data, info, err := readStableActiveLocalDocument(context.CanonicalPath)
	if err != nil {
		h.producedDocuments.Delete(key)
		return nil, true, fmt.Errorf("produced_document_stale")
	}
	digest := sha256.Sum256(data)
	if info.Size() != context.Size || info.ModTime().UnixNano() != context.ModTimeNS || !strings.EqualFold(fmt.Sprintf("%x", digest[:]), context.Digest) {
		h.producedDocuments.Delete(key)
		return nil, true, fmt.Errorf("produced_document_stale")
	}
	scope := tool.InvocationScope{
		RootTaskID:  rootTaskID,
		PlanID:      tool.TrustedInputPlanID(turnID),
		SessionID:   sessionID,
		TurnID:      turnID,
		PrincipalID: userID,
	}
	payload, err := tool.NewArtifactPayload(scope, "trusted-input:produced-document:"+context.Digest[:24], "document", context.MIMEType, base64.StdEncoding.EncodeToString(data), time.Now().UTC())
	if err != nil {
		return nil, true, fmt.Errorf("produced_document_invalid")
	}
	payload.Ref.Name = filepath.Base(context.CanonicalPath)
	return []semanticTrustedArtifactInput{{
		Payload: payload,
		Format:  context.Format,
		Suffix:  agent.DocumentAttachmentTempSuffix(context.CanonicalPath, context.Format),
	}}, true, nil
}
