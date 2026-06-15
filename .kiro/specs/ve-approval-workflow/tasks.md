# Implementation Plan: VE Approval Workflow

## Overview

This implementation builds a complete approval workflow system on the existing VE infrastructure. The approach follows a layered strategy: Database Schema → Data Models & Stores → Workflow Engine → VE Approval Handler → Hub REST API → Admin Panel → Capability Market Integration → Frontend Workflow Designer. Each layer builds on the previous, with checkpoints to validate integration.

## Tasks

- [x] 1. Set up database schema and core data models
  - [x] 1.1 Create PostgreSQL migration for workflow tables
    - Create migration file with `workflow_definitions`, `workflow_versions`, `workflow_instances`, `node_executions`, and `audit_trail` tables
    - Include all indexes defined in the design (owner, status, published unique constraint, timestamp)
    - Add database trigger or application-level check to prevent UPDATE/DELETE on `audit_trail`
    - _Requirements: 10.3, 10.6_

  - [x] 1.2 Implement workflow graph model (`hub/internal/workflow/graph.go`)
    - Define `WorkflowGraph`, `WorkflowNode`, `WorkflowEdge`, `Position` structs
    - Define `NodeType` constants: trigger, form, approval, condition_branch, action, notification, sub_process
    - Define `ApprovalNodeConfig` with modes (single, countersign, any_n_of_m, sequential), timeout, fallback
    - Define `ConditionBranchConfig`, `BranchCondition`, `ConditionExpr` structs
    - Define `ApprovalMode` constants
    - _Requirements: 1.3, 2.2, 2.4, 2.5_

  - [x] 1.3 Implement workflow definition and version models (`hub/internal/workflow/store.go`)
    - Define `WorkflowDefinition` and `WorkflowVersion` structs
    - Define `VersionStatus` constants: draft, pending_review, published, rejected, superseded, unpublished
    - Define `WorkflowStore` interface with CRUD operations for definitions and versions
    - Include `ListPendingReviews` with pagination support
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 12.1_

  - [x] 1.4 Implement workflow instance and node execution models (`hub/internal/workflow/instance.go`)
    - Define `WorkflowInstance` struct with status, current node, instance data
    - Define `InstanceStatus` constants: running, completed, failed, blocked
    - Define `NodeExecution` struct with status tracking and result storage
    - Define `NodeStatus` constants: pending, running, completed, failed, blocked, skipped
    - Define `InstanceStore` interface
    - _Requirements: 9.1, 9.7, 9.8, 9.9_

  - [x] 1.5 Implement audit trail model and store (`hub/internal/workflow/audit.go`)
    - Define `AuditEntry` struct with millisecond-precision UTC timestamp
    - Define `AuditStore` interface with append-only `Append` method
    - Implement query methods: by instance, by approver, by time range, by decision
    - All query methods support pagination at 100 records per page
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

- [x] 2. Implement PostgreSQL store implementations
  - [x] 2.1 Implement `WorkflowStore` PostgreSQL backend
    - Implement all CRUD operations for workflow definitions
    - Implement version management with status transitions
    - Implement `GetPublishedVersion` using the unique partial index
    - Implement `ListPendingReviews` with pagination (50 items per page)
    - _Requirements: 6.1, 6.2, 6.3, 7.1, 12.2, 12.3, 12.4, 12.5_

  - [x] 2.2 Implement `InstanceStore` PostgreSQL backend
    - Implement instance creation, status updates, current node tracking
    - Implement `GetPendingApprovals` for approver queries
    - Implement node execution CRUD with status and result tracking
    - _Requirements: 9.1, 9.2, 9.7, 9.8_

  - [x] 2.3 Implement `AuditStore` PostgreSQL backend
    - Implement append-only insert (no update/delete methods)
    - Implement paginated queries by instance, approver, time range, decision
    - Ensure millisecond precision timestamps
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

- [x] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Implement approval request payload and A2A message extensions
  - [x] 4.1 Implement approval request payload (`hub/internal/workflow/payload.go`)
    - Define `ApprovalRequest` struct with title (max 200 chars), summary (max 2000 chars), details, attachments, hint_rules
    - Define `AttachmentRef` struct with URL, filename, mime type, size
    - Define `ApprovalResponse` struct with decision, rationale (max 2000 chars), matched rule
    - Implement payload size validation (max 100 KB, truncate details if exceeded)
    - Implement attachment validation (max 10 attachments, max 50 MB total)
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 4.2 Extend A2A GroupEnvelope for approval messages
    - Add `EnvelopeTypeApprovalRequest = "approval_request"` constant
    - Add `EnvelopeTypeApprovalResponse = "approval_response"` constant
    - Ensure existing GroupEnvelope.Payload field carries serialized ApprovalRequest/ApprovalResponse
    - _Requirements: 5.2, 9.6_

- [x] 5. Implement VE approval rule engine (`gui/ve_approval_rules.go`)
  - [x] 5.1 Implement rule condition evaluation
    - Implement `RuleCondition` struct with field (dot-notation, max depth 3), operator, value
    - Implement all comparison operators: equals, not_equals, greater_than, less_than, contains, in_list, not_in_list, is_empty, is_not_empty
    - Handle missing/null fields by treating condition as not matched
    - _Requirements: 4.6, 4.7, 4.8_

  - [x] 5.2 Implement three-way routing rule engine
    - Implement `ApprovalRuleEngine.Evaluate()` with priority order: auto-reject → auto-approve → require-human
    - Implement rule ordering within categories by position index
    - Return routing decision and matched rule
    - Ensure evaluation completes within 5 seconds
    - When multiple rules match, apply first matching rule by position
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.10_

  - [ ]* 5.3 Write unit tests for rule engine
    - Test all operator types with various data types
    - Test priority order (reject before approve before human)
    - Test missing field handling
    - Test rule ordering within categories
    - Test timeout behavior
    - _Requirements: 4.2, 4.6, 4.7, 4.8, 4.10_

- [x] 6. Implement VE approval handler (`gui/ve_approval_handler.go`)
  - [x] 6.1 Implement VE approval configuration model
    - Define `VEApprovalConfig` struct with enabled toggle, ACL, rules, queue limits, timeout, daily quota, fallback
    - Define `AccessControlList` with whitelist/blacklist mode, departments, roles, skills, entities
    - Implement ACL validation (max 500 entries per list, max 100 per filter category)
    - _Requirements: 3.1, 3.4, 3.5, 3.6, 3.7_

  - [x] 6.2 Implement approval queue management
    - Implement `ApprovalQueue` with configurable max size (1-1000)
    - Implement queue full detection and rejection with "queue full" response
    - Implement daily quota tracking (1-10000)
    - Route rejected requests to fallback approver when configured
    - _Requirements: 3.7, 3.8, 3.9_

  - [x] 6.3 Implement `HandleApprovalRequest` main handler
    - Check approval capability enabled status; reject with "capability disabled" if disabled
    - Evaluate ACL (whitelist/blacklist mode) against requester
    - Check queue capacity and daily quota
    - Invoke rule engine for three-way routing
    - Return structured `ApprovalDecision` with decision, rationale, matched rule
    - Auto-approve/reject within 2 seconds of evaluation completion
    - _Requirements: 3.2, 3.3, 4.3, 4.4, 4.5_

  - [x] 6.4 Implement capability disable behavior
    - When disabled while requests pending: reject new requests, continue processing accepted ones
    - While disabled: reject all incoming requests with "capability disabled" response
    - _Requirements: 3.2, 3.3_

  - [ ]* 6.5 Write unit tests for VE approval handler
    - Test ACL whitelist/blacklist filtering
    - Test queue full behavior with/without fallback
    - Test capability disable/enable transitions
    - Test daily quota enforcement
    - _Requirements: 3.2, 3.3, 3.7, 3.8, 3.9_

- [x] 7. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Implement workflow executor (`hub/internal/workflow/executor.go`)
  - [x] 8.1 Implement `StartInstance` for workflow instance creation
    - Create new `WorkflowInstance` bound to current `PublishedVersion`
    - Set instance status to "running"
    - Record `instance_created` event in audit trail
    - Begin execution from the Trigger_Node
    - _Requirements: 9.1, 9.2_

  - [x] 8.2 Implement node execution dispatcher
    - Execute nodes according to directed graph edges
    - Handle each node type: Trigger, Form, Approval, ConditionBranch, Action, Notification, SubProcess
    - Record `node_completed` event for each node
    - Mark instance as "completed" when all nodes finish
    - _Requirements: 9.2, 9.3, 9.6, 9.7_

  - [x] 8.3 Implement condition branch evaluation
    - Evaluate branch conditions in priority order against instance data
    - Route to first matching branch
    - Route to default branch if no condition matches and default is configured
    - Mark node as "failed" if no match and no default branch
    - _Requirements: 9.3, 9.4, 9.5_

  - [x] 8.4 Implement approval node dispatch
    - Dispatch `ApprovalRequest` to assigned VE approver(s) via A2A
    - Handle approval modes: single, countersign (all must approve, reject on first reject), any_n_of_m, sequential (stop on reject)
    - _Requirements: 9.6, 2.3, 2.4, 2.5_

  - [x] 8.5 Implement `ResumeInstance` for approval response handling
    - Process approval responses and advance workflow
    - For countersign: track all approver decisions, reject immediately on any reject
    - For any_n_of_m: track approval count, pass when N reached
    - For sequential: advance to next approver or complete
    - Record decision events in audit trail
    - _Requirements: 2.3, 2.4, 2.5, 10.1_

  - [x] 8.6 Implement timeout and fallback handling
    - Implement `HandleTimeout` for pending approval nodes
    - Route to fallback approver within 30 seconds of detecting unavailability
    - Route to fallback when queue is full (default: 50 requests)
    - Route to fallback when timeout exceeded (default: 24 hours)
    - Mark node as "blocked" if no fallback configured
    - Notify workflow initiator when blocked (within 60 seconds)
    - Handle cascading fallback failure (fallback also unavailable)
    - Record all fallback events in audit trail
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

  - [ ]* 8.7 Write unit tests for workflow executor
    - Test instance creation and binding to published version
    - Test condition branch evaluation with various conditions
    - Test approval mode behaviors (countersign, any_n_of_m, sequential)
    - Test timeout and fallback routing
    - Test concurrent instance execution
    - _Requirements: 9.1, 9.3, 9.4, 9.5, 9.6, 9.9, 11.1, 11.3_

- [x] 9. Implement escalation retry mechanism
  - [x] 9.1 Implement human escalation with retry
    - When configured human approver is unavailable, retain request in pending-escalation queue
    - Record unavailability in audit trail
    - Retry escalation at 60-second intervals, maximum 5 attempts
    - Mark request as "escalation-failed" after all retries exhausted
    - _Requirements: 4.9_

- [x] 10. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. Implement version management and admin review
  - [x] 11.1 Implement version lifecycle management
    - Auto-increment version number (major.minor.patch) on save
    - Implement status transitions: draft → pending_review → published/rejected
    - Implement "Submit for Review" with workflow structure validation
    - Create new draft version (increment minor) when modifying published workflow
    - Bind new instances to new published version; existing instances continue on bound version
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [x] 11.2 Implement admin review endpoints
    - List pending submissions (sorted by date, paginated at 50 per page)
    - Display complete workflow graph and configurations for review
    - Approve submission: transition to "published", register in Capability Market
    - Reject submission: require reason (10-2000 chars), transition to "rejected"
    - Unpublish: prevent new instances, don't terminate running ones
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 6.7_

  - [x] 11.3 Implement review notifications
    - Send Hub notification to author within 60 seconds of status change
    - Include rejection reason in notification when rejected
    - Send reminder to admin every 7 days for unactioned submissions
    - _Requirements: 7.5, 7.6_

  - [x] 11.4 Implement workflow withdrawal
    - Allow user to withdraw pending_review submission back to draft
    - _Requirements: 12.3_

- [x] 12. Implement Capability Market integration
  - [x] 12.1 Register published workflows in Capability Market
    - Register with `capability_type: "approval_workflow"` in existing capability service
    - Include metadata: category ("审批类"), node_count, approval_modes, thumbnail_url
    - _Requirements: 8.1, 8.2_

  - [x] 12.2 Implement market listing and discovery
    - Categorize into Workflows (审批类, 自动化类, 协作类) and Normal Skills
    - Display published workflows with name, description, author, version, usage count, flow preview
    - Support filtering by category, sub-category, author, keyword search
    - _Requirements: 8.1, 8.3, 8.5, 8.6_

  - [x] 12.3 Implement workflow triggering from market
    - Create new `WorkflowInstance` when user triggers a published workflow
    - Ensure user workflow isolation (not visible/editable by others unless published)
    - _Requirements: 8.4, 8.5_

- [x] 13. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 14. Implement Hub REST API endpoints
  - [x] 14.1 Implement workflow CRUD API
    - POST/GET/PUT/DELETE `/api/v1/workflows` for definitions
    - POST/GET `/api/v1/workflows/:id/versions` for version management
    - POST `/api/v1/workflows/:id/versions/:vid/submit` for review submission
    - Ensure owner isolation (users can only access their own workflows)
    - _Requirements: 1.1, 6.1, 6.2, 8.5_

  - [x] 14.2 Implement workflow validation API
    - Validate graph structure on save: exactly one Trigger_Node, no disconnected nodes
    - Validate semantic edge connections (Trigger_Node only as start, edge type rules)
    - Return validation error list with affected node labels and positions
    - _Requirements: 1.5, 1.6, 1.7, 1.8_

  - [x] 14.3 Implement admin review API
    - GET `/api/v1/admin/reviews` for pending submissions queue
    - POST `/api/v1/admin/reviews/:id/approve` and `/reject`
    - POST `/api/v1/admin/reviews/:id/unpublish`
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [x] 14.4 Implement audit trail query API
    - GET `/api/v1/audit` with filters: instance_id, approver_id, requester_id, time_range, decision
    - Paginate at 100 records per page
    - _Requirements: 10.4, 10.5_

  - [x] 14.5 Implement workflow instance API
    - POST `/api/v1/workflows/:id/trigger` to start new instance
    - GET `/api/v1/instances/:id` for instance status
    - GET `/api/v1/instances/:id/audit` for instance audit trail
    - _Requirements: 9.1, 10.4_

- [x] 15. Implement VE approval settings in desktop app
  - [x] 15.1 Add approval capability configuration to VE settings
    - Add "Approval Capability" section with enable/disable toggle (default: disabled)
    - Display ACL configuration (whitelist/blacklist mode, departments, roles, skills)
    - Display operational limits (max queue size, timeout hours, daily quota)
    - Display fallback approver configuration
    - _Requirements: 3.1, 3.4, 3.5, 3.6, 3.7_

  - [x] 15.2 Add three-way routing rules configuration UI
    - Display three rule categories: auto-approve, auto-reject, require-human (max 50 per category)
    - Allow rule creation with field path, operator selection, value input
    - Support rule ordering (drag-and-drop or position index)
    - _Requirements: 4.1, 4.6, 4.7_

  - [x] 15.3 Implement VE approval capability validation
    - When assigning VE as approver in workflow designer, validate approval capability is enabled
    - Display error if VE lacks approval capability
    - _Requirements: 2.6_

- [x] 16. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 17. Implement workflow designer frontend (Hub)
  - [x] 17.1 Implement canvas-based workflow editor
    - Create `/approval_workflow` page accessible from desktop app Settings → Digital Employees tab
    - Implement drag-and-drop canvas for node placement
    - Support all node types: Trigger, Form, Approval, ConditionBranch, Action, Notification, SubProcess
    - Display configuration panel within 500ms of node placement
    - _Requirements: 1.1, 1.2, 1.3, 1.4_

  - [x] 17.2 Implement edge connection and validation
    - Implement edge drawing between nodes
    - Validate semantic connection rules (Trigger_Node as start only, edge type constraints)
    - Reject invalid connections with inline error message adjacent to source node
    - _Requirements: 1.5, 1.6_

  - [x] 17.3 Implement approval node configuration panel
    - Display fields: approver assignment, approval mode, timeout (1-720 hours), fallback approver
    - Implement mode-specific UI: single, countersign, any_n_of_m (N/M inputs), sequential (ordered list)
    - Validate N ≥ 1, N ≤ M, M ≥ 2 for any_n_of_m mode
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.7, 2.8_

  - [x] 17.4 Implement workflow save and validation
    - Validate on save: no disconnected nodes, no missing required configurations
    - Validate exactly one Trigger_Node
    - Display validation error list with node labels and positions
    - _Requirements: 1.7, 1.8_

  - [x] 17.5 Implement version state display
    - Display current version state in editor header with distinct visual indicators (color and icon)
    - Support states: draft, pending_review, published, rejected, superseded, unpublished
    - _Requirements: 12.7_

  - [x] 17.6 Implement "Submit for Review" and withdrawal UI
    - Submit button with pre-submission validation
    - Withdrawal button for pending_review versions
    - Display rejection reason for rejected versions
    - _Requirements: 6.2, 12.3, 12.4_

- [x] 18. Implement desktop app navigation to workflow designer
  - [x] 18.1 Add "Approval Workflow Design" button to VE settings
    - Add button in Settings → Digital Employees tab
    - Open Hub page at `/approval_workflow` in embedded browser or system browser
    - _Requirements: 1.1_

- [x] 19. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- The design does not include a Correctness Properties section, so property-based tests are not included
- Unit tests validate specific examples and edge cases
- The implementation follows a layered approach: DB → Models → Engine → Handler → API → Frontend
- All approval decisions are recorded in the immutable audit trail
- The existing A2A protocol (GroupEnvelope) is reused for approval request/response delivery
- The existing Capability Market infrastructure is reused for workflow publishing
- VE approval rules execute locally on the VE's machine (privacy by design)

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["1.3", "1.4", "1.5"] },
    { "id": 2, "tasks": ["2.1", "2.2", "2.3"] },
    { "id": 3, "tasks": ["4.1", "4.2", "5.1"] },
    { "id": 4, "tasks": ["5.2", "5.3", "6.1"] },
    { "id": 5, "tasks": ["6.2", "6.3", "6.4"] },
    { "id": 6, "tasks": ["6.5", "8.1"] },
    { "id": 7, "tasks": ["8.2", "8.3", "8.4"] },
    { "id": 8, "tasks": ["8.5", "8.6", "9.1"] },
    { "id": 9, "tasks": ["8.7", "11.1"] },
    { "id": 10, "tasks": ["11.2", "11.3", "11.4"] },
    { "id": 11, "tasks": ["12.1", "12.2", "12.3"] },
    { "id": 12, "tasks": ["14.1", "14.2", "14.3"] },
    { "id": 13, "tasks": ["14.4", "14.5", "15.1"] },
    { "id": 14, "tasks": ["15.2", "15.3"] },
    { "id": 15, "tasks": ["17.1", "18.1"] },
    { "id": 16, "tasks": ["17.2", "17.3"] },
    { "id": 17, "tasks": ["17.4", "17.5", "17.6"] }
  ]
}
```
