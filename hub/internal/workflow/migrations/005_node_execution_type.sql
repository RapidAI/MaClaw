-- Migration 005: Add node_type column to workflow_node_executions
-- This migration adds the node_type field to track what type of node was executed,
-- enabling richer audit trail and directory queries.

ALTER TABLE workflow_node_executions ADD COLUMN node_type TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_node_exec_node_type ON workflow_node_executions(node_type);
