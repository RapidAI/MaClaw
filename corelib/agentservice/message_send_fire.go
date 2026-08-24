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

const reviewedHostMessageSendFireSelectionID = "selection:message-send"

type reviewedHostMessageSendFireDeps struct {
	Coordinator   *coretool.SQLiteSemanticExecutionCoordinator
	Send          func(context.Context, string, []scheduler.DeliveryTarget, string) error
	ChannelScope  string
	DestinationID string
	PrincipalID   string
	Text          string
	Now           func() time.Time
}

func reviewedHostMessageSendFireOutcome(err error) coretool.DeliveryState {
	if err == nil {
		return coretool.DeliveryUnknown
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "message text is empty") || strings.Contains(message, "no delivery targets") || strings.Contains(message, "unsupported channel") || strings.Contains(message, "unavailable") {
		return coretool.DeliveryFailed
	}
	return coretool.DeliveryUnknown
}

// FireReviewedHostMessageSend claims one prepared IM text send and performs
// channel I/O. A nil send error is not a channel receipt and must settle
// unknown. The same prepared delivery is never resent.
func FireReviewedHostMessageSend(ctx context.Context, deps reviewedHostMessageSendFireDeps) error {
	if deps.Coordinator == nil || deps.Send == nil {
		return fmt.Errorf("host_message_send_unavailable")
	}
	channel, ok := reviewedHostScheduleDispatchFireChannel(deps.ChannelScope)
	if !ok {
		return fmt.Errorf("trusted_dispatch_channel_unavailable")
	}
	target, err := trustedDestinationToSchedulerTarget(deps.DestinationID)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(deps.Text)
	if text == "" {
		return fmt.Errorf("host_message_send_text_required")
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	principal := strings.TrimSpace(deps.PrincipalID)
	if principal == "" {
		principal = "message-send"
	}
	runID := now.Format("20060102T150405.000000000Z")
	textKey := coretool.SchemaDigest([]byte(text))[:16]
	scope := coretool.InvocationScope{
		RootTaskID:  "message-send:" + strings.TrimSpace(deps.DestinationID),
		PlanID:      "run:" + runID,
		SessionID:   principal,
		TurnID:      "send:" + runID + ":" + textKey,
		PrincipalID: principal,
	}
	payload, err := coretool.NewArtifactPayload(scope, reviewedHostMessageSendFireSelectionID, "document", "text/plain", base64.StdEncoding.EncodeToString([]byte(text)), now)
	if err != nil {
		return err
	}
	if _, err := deps.Coordinator.Artifacts.Publish(payload); err != nil {
		return err
	}
	if _, err := deps.Coordinator.PrepareStandaloneDelivery(coretool.DeliveryRecord{
		Scope: scope, SelectionID: reviewedHostMessageSendFireSelectionID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: payload.Ref.Scope,
		ChannelScope: deps.ChannelScope, DestinationID: deps.DestinationID, State: coretool.DeliveryPrepared, CreatedAt: now,
	}, now); err != nil {
		return err
	}
	_, claimed, err := deps.Coordinator.ClaimDelivery(scope, reviewedHostMessageSendFireSelectionID, now)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("host_message_send_unknown")
	}
	sendErr := deps.Send(ctx, channel, []scheduler.DeliveryTarget{target}, text)
	outcome := reviewedHostMessageSendFireOutcome(sendErr)
	if _, recErr := deps.Coordinator.SettleStandaloneDelivery(scope, reviewedHostMessageSendFireSelectionID, outcome, "", "message_send_"+string(outcome), now); recErr != nil && sendErr == nil {
		return recErr
	}
	if sendErr != nil {
		return sendErr
	}
	return fmt.Errorf("host_message_send_unknown")
}
