package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// TrustedInputPlanID is the RouteState plan identity for ingress artifacts
// that exist before a governed plan is published. GUI IM and headless reviewed
// host attachments share this so a later consumer cannot bind a different
// input family.
func TrustedInputPlanID(turnID string) string {
	return "input:" + strings.TrimSpace(turnID)
}

// TrustedAttachmentSourceID is the stable producer key for one ingress
// attachment. SourceMediaID wins when the channel supplied a handle; otherwise
// the slot index, basename and MIME form the identity. GUI and headless hosts
// share this so a missing media handle cannot mint a second ArtifactRef.
func TrustedAttachmentSourceID(index int, fileName, mimeType, sourceMediaID string) string {
	if sourceID := strings.TrimSpace(sourceMediaID); sourceID != "" {
		return sourceID
	}
	return fmt.Sprintf("attachment:%d:%s:%s", index, filepath.Base(fileName), mimeType)
}

const (
	// TrustedInputMissing is the fail-closed code when a document-read or
	// current-channel deliver need required an ingress artifact and none arrived.
	TrustedInputMissing = "trusted_document_input_missing"
	// TrustedInputAmbiguous is the fail-closed code when more than one ingress
	// artifact could bind that need.
	TrustedInputAmbiguous = "trusted_document_input_ambiguous"
)

// UniqueTrustedInputCount reports whether ingress supplied exactly one artifact
// for a need that cannot choose among alternatives. GUI IM document binding and
// headless reviewed document/image/voice deliver share this so a host cannot
// pick a different missing/ambiguous spelling.
func UniqueTrustedInputCount(n int) error {
	if n == 0 {
		return fmt.Errorf("%s", TrustedInputMissing)
	}
	if n != 1 {
		return fmt.Errorf("%s", TrustedInputAmbiguous)
	}
	return nil
}

// IsTrustedInputMissingOrAmbiguous reports the uniqueness fail-closed pair.
func IsTrustedInputMissingOrAmbiguous(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, TrustedInputMissing) || strings.Contains(msg, TrustedInputAmbiguous)
}
