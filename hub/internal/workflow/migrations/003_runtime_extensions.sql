-- Migration 003: Runtime Extensions - Confirmations Table
-- This migration creates the confirmations table for tracking post-completion
-- confirmation/acknowledgment lifecycle (executor confirmations and notifier acknowledgments).

-- Confirmation tracking table
CREATE TABLE IF NOT EXISTS confirmations (
    id                      TEXT PRIMARY KEY,
    instance_id             TEXT NOT NULL REFERENCES workflow_instances(id),
    recipient_id            TEXT NOT NULL,
    type                    TEXT NOT NULL,          -- 'executor' or 'notifier'
    status                  TEXT NOT NULL DEFAULT 'pending', -- pending/confirmed/auto_closed
    notes                   TEXT DEFAULT '',
    timeout_hours           INTEGER NOT NULL DEFAULT 48,
    max_reminders           INTEGER NOT NULL DEFAULT 3,
    reminders_sent          INTEGER NOT NULL DEFAULT 0,
    reminder_interval_hours INTEGER NOT NULL DEFAULT 24,
    last_reminder_at        TEXT,
    confirmed_at            TEXT,
    auto_closed_at          TEXT,
    auto_close_reason       TEXT DEFAULT '',
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);

CREATE INDEX IF NOT EXISTS idx_confirm_instance ON confirmations(instance_id);
CREATE INDEX IF NOT EXISTS idx_confirm_recipient ON confirmations(recipient_id);
CREATE INDEX IF NOT EXISTS idx_confirm_status ON confirmations(status);
CREATE INDEX IF NOT EXISTS idx_confirm_pending ON confirmations(status, recipient_id)
    WHERE status = 'pending';
