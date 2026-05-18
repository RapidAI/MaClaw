-- Migration 004: Notifications Table
-- Creates the notifications log table for tracking notification delivery.
-- Supports Requirements 5.3, 6.3, 6.6

CREATE TABLE notifications (
    id               TEXT PRIMARY KEY,
    instance_id      TEXT NOT NULL,
    type             TEXT NOT NULL, -- result_executor/notifier/withdrawal/reminder/escalation
    recipient_id     TEXT NOT NULL,
    channel          TEXT NOT NULL, -- hub_inapp/im_feishu/im_wechat/im_qq
    payload_json     JSONB NOT NULL DEFAULT '{}',
    delivered        BOOLEAN NOT NULL DEFAULT FALSE,
    delivered_at     TIMESTAMP,
    failure_reason   TEXT DEFAULT '',
    created_at       TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notif_instance ON notifications(instance_id);
CREATE INDEX idx_notif_recipient ON notifications(recipient_id);
CREATE INDEX idx_notif_delivered ON notifications(delivered);
