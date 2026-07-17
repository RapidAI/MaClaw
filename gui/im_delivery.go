package main

import (
	"context"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// DeliverIMText immediately pushes text to one or more IM channel targets.
// Targets should already be name-resolved (group_id filled). channel defaults to lansenger.
// Remembers successful private peers for user_id=self resolution (same as schedule delivery).
func (a *App) DeliverIMText(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	text = scheduler.TruncateDeliveryBody(text)
	if text == "" {
		return fmt.Errorf("message text is empty")
	}
	if len(targets) == 0 {
		return fmt.Errorf("no delivery targets")
	}
	channel = scheduler.DefaultDeliveryChannel(channel)

	ctx, cancel := scheduler.WithDeliveryTimeout(ctx, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()

	store := a.deliveryStateStore()
	_, err := scheduler.FanOutDeliveryTargets(targets, func(i int, target scheduler.DeliveryTarget) error {
		peer, sendErr := a.deliverScheduledTaskTarget(ctx, channel, target, text)
		if sendErr != nil {
			log.Printf("[im-delivery] failed channel=%s target=%d: %v", channel, i, sendErr)
			return sendErr
		}
		if store != nil && peer != "" && scheduler.CanRememberAsSelfPeer(target) {
			store.RememberPeer(channel, peer)
		}
		return nil
	})
	return err
}

// DeliverIMFromTaskDelivery is the shared path for scheduled-task result push and im_message.
// Expects d already Active() with resolved targets. Does not apply FormatBody (caller owns body).
func (a *App) DeliverIMFromTaskDelivery(ctx context.Context, d *scheduler.TaskDelivery, text string) error {
	if d == nil || !d.Active() {
		return fmt.Errorf("delivery not active")
	}
	return a.DeliverIMText(ctx, d.Channel, d.Targets, text)
}
