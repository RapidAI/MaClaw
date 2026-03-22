package chat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MessageService handles sending and retrieving messages.
type MessageService struct {
	store    *Store
	notifier *Notifier
}

// NewMessageService creates a MessageService.
func NewMessageService(store *Store, notifier *Notifier) *MessageService {
	return &MessageService{store: store, notifier: notifier}
}

// SendMessage stores a message, assigns a seq, and notifies channel members.
func (s *MessageService) SendMessage(senderID, channelID, content, clientMsgID string, msgType MessageType, attachments []Attachment) (*Message, error) {
	msg := &Message{
		ID:          uuid.NewString(),
		ChannelID:   channelID,
		SenderID:    senderID,
		Content:     content,
		MsgType:     msgType,
		Attachments: attachments,
		CreatedAt:   time.Now(),
		ClientMsgID: clientMsgID,
	}

	seq, err := s.store.InsertMessage(msg)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	msg.Seq = seq

	// Notify all channel members (async, best-effort).
	go s.notifier.NotifyNewMessage(channelID, senderID, msg.ID, seq)

	return msg, nil
}

// GetMessagesAfter returns messages after the given seq (for incremental sync).
func (s *MessageService) GetMessagesAfter(channelID string, afterSeq int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.store.GetMessagesAfter(channelID, afterSeq, limit)
}

// GetMessagesBefore returns messages before the given seq (for history scroll).
func (s *MessageService) GetMessagesBefore(channelID string, beforeSeq int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.store.GetMessagesBefore(channelID, beforeSeq, limit)
}

// RecallMessage marks a message as recalled.
func (s *MessageService) RecallMessage(channelID, msgID string, seq int64) error {
	_, err := s.store.db.Exec(
		`UPDATE chat_messages SET recalled = 1 WHERE id = ? AND channel_id = ?`,
		msgID, channelID,
	)
	if err != nil {
		return err
	}
	go s.notifier.NotifyRecall(channelID, msgID, seq)
	return nil
}
