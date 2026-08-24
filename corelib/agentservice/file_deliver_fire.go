package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostFileDeliverFireSelectionID  = "selection:file-deliver"
	reviewedHostImageDeliverFireSelectionID = "selection:image-deliver"
	reviewedHostVoiceDeliverFireSelectionID = "selection:voice-deliver"
)

type reviewedHostFileDeliverFireDeps struct {
	Coordinator   *coretool.SQLiteSemanticExecutionCoordinator
	Send          func(context.Context, string, []scheduler.DeliveryTarget, []byte, string, string) error
	ChannelScope  string
	DestinationID string
	PrincipalID   string
	FileName      string
	MIMEType      string
	Data          []byte
	Now           func() time.Time
}

func reviewedHostFileDeliverFireOutcome(err error) coretool.DeliveryState {
	if err == nil {
		return coretool.DeliveryUnknown
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "file payload is empty") || strings.Contains(message, "no delivery targets") || strings.Contains(message, "unsupported channel") || strings.Contains(message, "unavailable") {
		return coretool.DeliveryFailed
	}
	return coretool.DeliveryUnknown
}

// FireReviewedHostFileDeliver claims one prepared trusted document,
// image, or voice file and performs channel I/O. A nil send error or
// missing media id is not a channel receipt and must settle unknown.
// The same prepared delivery is never resent.
func FireReviewedHostFileDeliver(ctx context.Context, deps reviewedHostFileDeliverFireDeps) error {
	if deps.Coordinator == nil || deps.Send == nil {
		return fmt.Errorf("host_file_deliver_unavailable")
	}
	channel, ok := reviewedHostScheduleDispatchFireChannel(deps.ChannelScope)
	if !ok {
		return fmt.Errorf("trusted_dispatch_channel_unavailable")
	}
	target, err := trustedDestinationToSchedulerTarget(deps.DestinationID)
	if err != nil {
		return err
	}
	if len(deps.Data) == 0 {
		return fmt.Errorf("host_file_deliver_size_rejected")
	}
	fileName, mimeType, kind, ok := reviewedHostDeliverableMedia(deps.FileName, deps.MIMEType)
	if !ok {
		return fmt.Errorf("host_file_deliver_document_required")
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	principal := strings.TrimSpace(deps.PrincipalID)
	if principal == "" {
		principal = "file-deliver"
	}
	runID := now.Format("20060102T150405.000000000Z")
	contentKey := coretool.SchemaDigest(deps.Data)[:16]
	selectionID := reviewedHostFileDeliverFireSelectionID
	switch kind {
	case "image":
		selectionID = reviewedHostImageDeliverFireSelectionID
	case "voice":
		selectionID = reviewedHostVoiceDeliverFireSelectionID
	}
	scope := coretool.InvocationScope{
		RootTaskID:  "file-deliver:" + strings.TrimSpace(deps.DestinationID),
		PlanID:      "run:" + runID,
		SessionID:   principal,
		TurnID:      "deliver:" + runID + ":" + contentKey,
		PrincipalID: principal,
	}
	payload, err := coretool.NewArtifactPayload(scope, selectionID, kind, mimeType, base64.StdEncoding.EncodeToString(deps.Data), now)
	if err != nil {
		return err
	}
	if _, err := deps.Coordinator.Artifacts.Publish(payload); err != nil {
		return err
	}
	if _, err := deps.Coordinator.PrepareStandaloneDelivery(coretool.DeliveryRecord{
		Scope: scope, SelectionID: selectionID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: payload.Ref.Scope,
		ChannelScope: deps.ChannelScope, DestinationID: deps.DestinationID, State: coretool.DeliveryPrepared, CreatedAt: now,
	}, now); err != nil {
		return err
	}
	_, claimed, err := deps.Coordinator.ClaimDelivery(scope, selectionID, now)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("host_file_deliver_unknown")
	}
	sendErr := deps.Send(ctx, channel, []scheduler.DeliveryTarget{target}, deps.Data, fileName, mimeType)
	outcome := reviewedHostFileDeliverFireOutcome(sendErr)
	if _, recErr := deps.Coordinator.SettleStandaloneDelivery(scope, selectionID, outcome, "", "file_deliver_"+string(outcome), now); recErr != nil && sendErr == nil {
		return recErr
	}
	if sendErr != nil {
		return sendErr
	}
	return fmt.Errorf("host_file_deliver_unknown")
}
