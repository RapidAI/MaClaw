package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// DeliverIMText immediately pushes text to one or more IM channel targets.
// Targets should already be name-resolved (group_id filled). channel defaults to lansenger.
// Remembers successful private peers for user_id=self resolution (same as schedule delivery).
func (a *App) DeliverIMText(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
	return a.deliverIMTextForBot(ctx, channel, "", targets, text)
}

func (a *App) deliverIMTextForBot(ctx context.Context, channel, botProfileID string, targets []scheduler.DeliveryTarget, text string) error {
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
		peer, sendErr := a.deliverScheduledTaskTarget(ctx, channel, botProfileID, target, text)
		if sendErr != nil {
			log.Printf("[im-delivery] failed channel=%s target=%d: %v", channel, i, sendErr)
			return sendErr
		}
		if store != nil && strings.TrimSpace(botProfileID) == "" && peer != "" && scheduler.CanRememberAsSelfPeer(target) {
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
	return a.deliverIMTextForBot(ctx, d.Channel, d.BotProfileID, d.Targets, text)
}

// DeliverIMFile immediately uploads a local file to one or more IM channel targets.
// caption, when non-empty, is delivered as a text message to the same targets first
// (Lansenger media messages carry no caption field). Returns the display name and
// size actually sent. Currently only the lansenger channel supports file upload.
func (a *App) DeliverIMFile(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, path, fileName, caption string) (string, int64, error) {
	return a.deliverIMFileForBot(ctx, channel, "", targets, path, fileName, caption)
}

func (a *App) deliverIMFileForBot(ctx context.Context, channel, botProfileID string, targets []scheduler.DeliveryTarget, path, fileName, caption string) (string, int64, error) {
	if a == nil {
		return "", 0, fmt.Errorf("app unavailable")
	}
	if len(targets) == 0 {
		return "", 0, fmt.Errorf("no delivery targets")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", 0, fmt.Errorf("file path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("file not found or inaccessible: %w", err)
	}
	if info.IsDir() {
		return "", 0, fmt.Errorf("path is a directory: %s", path)
	}
	if info.Size() == 0 {
		// Fail before any caption text goes out so an unsendable file never
		// leaves a half-delivered caption behind.
		return "", 0, fmt.Errorf("file is empty: %s", path)
	}
	if info.Size() > agent.SendFileMaxSize {
		return "", 0, fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), agent.SendFileMaxSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read file failed: %w", err)
	}
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = filepath.Base(path)
	}
	caption = scheduler.TruncateDeliveryBody(caption)
	channel = scheduler.DefaultDeliveryChannel(channel)
	if channel != scheduler.DeliveryChannelLansenger {
		return "", 0, fmt.Errorf("channel %q 暂不支持 im_message 文件发送（目前仅蓝信 lansenger）", channel)
	}

	ctx, cancel := scheduler.WithDeliveryTimeout(ctx, scheduler.DefaultIMFileDeliveryTimeout)
	defer cancel()

	store := a.deliveryStateStore()
	_, err = scheduler.FanOutDeliveryTargets(targets, func(i int, target scheduler.DeliveryTarget) error {
		peer, sendErr := a.deliverLansengerFileTarget(ctx, botProfileID, target, data, name, caption)
		if sendErr != nil {
			log.Printf("[im-delivery] file failed channel=%s target=%d: %v", channel, i, sendErr)
			return sendErr
		}
		if store != nil && strings.TrimSpace(botProfileID) == "" && peer != "" && scheduler.CanRememberAsSelfPeer(target) {
			store.RememberPeer(channel, peer)
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return name, info.Size(), nil
}

// DeliverIMFileFromTaskDelivery is the send_file counterpart of DeliverIMFromTaskDelivery.
func (a *App) DeliverIMFileFromTaskDelivery(ctx context.Context, d *scheduler.TaskDelivery, path, fileName, caption string) (string, int64, error) {
	if d == nil || !d.Active() {
		return "", 0, fmt.Errorf("delivery not active")
	}
	return a.deliverIMFileForBot(ctx, d.Channel, d.BotProfileID, d.Targets, path, fileName, caption)
}

// deliverLansengerFileTarget uploads one file to a single lansenger target and
// returns the concrete peer id used (for memory), mirroring deliverLansengerScheduledTarget.
func (a *App) deliverLansengerFileTarget(ctx context.Context, botProfileID string, target scheduler.DeliveryTarget, data []byte, name, caption string) (string, error) {
	send := func(gw *lansenger.Gateway, peer string, isGroup bool) error {
		if strings.TrimSpace(caption) != "" {
			text := lansenger.OutgoingText{ToUserID: peer, Text: caption, IsGroup: isGroup}
			if isGroup {
				if target.MentionAll {
					text.Reminder = &lansenger.OutgoingReminder{All: true}
				} else if len(target.MentionUserIDs) > 0 {
					text.Reminder = &lansenger.OutgoingReminder{UserIDs: append([]string(nil), target.MentionUserIDs...)}
				}
			}
			if err := gw.SendText(ctx, text); err != nil {
				return fmt.Errorf("caption send failed: %w", err)
			}
		}
		return gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  peer,
			FileData:  data,
			FileName:  name,
			MediaType: lansengerMediaTypeForFileName(name),
			IsGroup:   isGroup,
			Strict:    true,
		})
	}

	switch target.Kind {
	case scheduler.DeliveryKindGroup:
		if strings.TrimSpace(target.GroupID) == "" {
			return "", fmt.Errorf("lansenger group target missing group_id")
		}
		gw, err := a.lansengerGatewayForSend(botProfileID)
		if err != nil {
			return "", err
		}
		if err := send(gw, target.GroupID, true); err != nil {
			return "", err
		}
		return target.GroupID, nil
	case scheduler.DeliveryKindUser:
		userID := strings.TrimSpace(target.UserID)
		if userID == "" {
			return "", fmt.Errorf("lansenger user target missing user_id")
		}
		// user_id=self → remembered peer, else live private session.
		userID = a.resolveLansengerDeliverySelfPeer(botProfileID, userID)
		if scheduler.IsSelfPeerID(userID) {
			manager, err := a.lansengerGatewayManagerForSend(botProfileID)
			if err != nil {
				return "", err
			}
			if manager == nil {
				return "", fmt.Errorf("lansenger: user_id=self 需要 staffId，或先用蓝信私聊机器人一次")
			}
			peer := manager.LastPrivatePeerID()
			if peer == "" {
				return "", fmt.Errorf("lansenger: user_id=self 需要 staffId，或先用蓝信私聊机器人一次")
			}
			manager.mu.Lock()
			gw := manager.gateway
			manager.mu.Unlock()
			if gw == nil {
				return "", fmt.Errorf("lansenger gateway not running")
			}
			if err := send(gw, peer, false); err != nil {
				return "", err
			}
			return peer, nil
		}
		gw, err := a.lansengerGatewayForSend(botProfileID)
		if err != nil {
			return "", err
		}
		if err := send(gw, userID, false); err != nil {
			return "", err
		}
		return userID, nil
	default:
		return "", fmt.Errorf("unknown target kind %q", target.Kind)
	}
}

// lansengerMediaTypeForFileName picks the Lansenger upload media type. Voice and
// audio files go through the generic file type because Lansenger exposes no
// native voice upload for bots (same rule as sendLocalFiles).
func lansengerMediaTypeForFileName(name string) string {
	mediaType := mediaTypeFromFileName(name)
	if kind := normalizeIMMediaKind(mediaType); kind.IsVoice() || kind.IsAudio() {
		mediaType = imMediaFile.String()
	}
	return mediaType
}
