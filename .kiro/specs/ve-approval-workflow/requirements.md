# Requirements Document

## Introduction

This feature implements a complete approval workflow system built on the existing VE (Virtual Employee / 数字员工) infrastructure. Users design approval workflows visually on Hub, assign VE approvers to approval nodes, and configure three-way routing rules (auto-approve, auto-reject, require-human). The system supports multiple approval node modes (single, countersign, any-N-of-M, sequential), version management with admin review for publishing, and full audit trail. Approval workflows are classified as a "Workflow" category in the Enterprise Capability Market and are isolated per user.

## Glossary

- **VE**: Virtual Employee (数字员工) — an AI digital worker running on a machine owner's computer, capable of serving as an automated approver
- **Approval_Workflow**: A directed graph of nodes defining the sequence and conditions under which approval requests are processed
- **Workflow_Designer**: The visual drag-and-drop editor on Hub (`/approval_workflow` page) for creating and editing Approval_Workflows
- **Approval_Node**: A node in the workflow graph where one or more VE approvers evaluate a request and produce a decision
- **Trigger_Node**: The entry point node that initiates a workflow instance when a triggering event occurs
- **Condition_Branch_Node**: A node that evaluates expressions against request data and routes execution to different branches
- **Action_Node**: A node that performs automated actions (e.g., update status, call API, send notification)
- **Notification_Node**: A node that sends notifications to specified recipients about workflow progress or decisions
- **Sub_Process_Node**: A node that invokes another published workflow as a nested sub-process
- **Form_Node**: A node that collects structured input data from the requester before proceeding
- **Approval_Request**: A structured payload submitted to an Approval_Workflow instance, containing title, summary, details, attachments, and hint rules
- **Three_Way_Routing**: The decision logic within an Approval_Node that routes a request to one of three outcomes: auto-approve, auto-reject, or require-human escalation
- **Approval_Rule**: A condition expression evaluated against the Approval_Request payload to determine the Three_Way_Routing outcome
- **Countersign_Mode**: An Approval_Node mode where all assigned approvers must approve for the request to pass
- **Any_N_of_M_Mode**: An Approval_Node mode where N approvals out of M total approvers are sufficient for the request to pass
- **Sequential_Mode**: An Approval_Node mode where approvers are evaluated in a defined order, each seeing the previous approver's decision
- **Workflow_Version**: A numbered revision of an Approval_Workflow; only one version can be published (active) at a time
- **Draft_Version**: An unpublished version of a workflow that is being edited or awaiting admin review
- **Published_Version**: The currently active version of a workflow that new triggers will use
- **Admin_Reviewer**: A user with administrative privileges who can approve or reject workflow version submissions for publishing
- **Capability_Market**: The enterprise marketplace where published workflows and skills are listed for discovery and use
- **Workflow_Instance**: A running execution of a specific Published_Version, created when a Trigger_Node fires
- **Audit_Trail**: A complete immutable record of all decisions, actions, and state transitions within a Workflow_Instance
- **Fallback_Approver**: An alternate VE or human designated to handle approval when the primary approver is unavailable
- **Access_Control_List**: The whitelist/blacklist configuration on a VE that determines which requesters are allowed to submit approval requests to that VE
- **Hub**: The web-based management platform accessed from the desktop app for workflow design and administration

## Requirements

### Requirement 1: Visual Workflow Designer Access

**User Story:** As a user, I want to access the visual workflow designer from the desktop app, so that I can create and edit approval workflows in a familiar environment.

#### Acceptance Criteria

1. WHEN the user navigates to Settings → Digital Employees tab and clicks the "Approval Workflow Design" button, THE application SHALL open the Hub page at `/approval_workflow` in the embedded browser or system browser
2. THE Workflow_Designer SHALL provide a canvas-based drag-and-drop interface for placing and connecting workflow nodes
3. THE Workflow_Designer SHALL support the following node types: Trigger_Node, Form_Node, Approval_Node, Condition_Branch_Node, Action_Node, Notification_Node, Sub_Process_Node
4. WHEN the user drags a node onto the canvas, THE Workflow_Designer SHALL display a configuration panel for that node's properties within 500 milliseconds
5. WHEN the user connects two nodes with an edge, THE Workflow_Designer SHALL validate that the connection is semantically valid according to the following rules: a Trigger_Node can only be a start node with no incoming edges, each node type defines allowed incoming and outgoing edge types, and no node may have more outgoing edges than its type permits
6. IF the user attempts to create a connection that violates the semantic validation rules, THEN THE Workflow_Designer SHALL reject the connection, prevent the edge from being drawn, and display an inline error message adjacent to the source node stating the reason for rejection
7. IF the user attempts to save a workflow with disconnected nodes or missing required configurations, THEN THE Workflow_Designer SHALL display a validation error list where each error identifies the affected node by its label and position on the canvas, and states the specific validation failure
8. IF the user attempts to save a workflow that contains no Trigger_Node or contains more than one Trigger_Node, THEN THE Workflow_Designer SHALL display a validation error indicating that exactly one Trigger_Node is required as the workflow entry point

### Requirement 2: Approval Node Configuration

**User Story:** As a user, I want to configure approval nodes with approvers, rules, and modes, so that I can define how approval decisions are made.

#### Acceptance Criteria

1. WHEN the user selects an Approval_Node for configuration, THE Workflow_Designer SHALL display fields for: approver assignment, approval mode, timeout duration, and fallback approver
2. THE Approval_Node SHALL support four approval modes: single approver, Countersign_Mode, Any_N_of_M_Mode, and Sequential_Mode
3. WHEN the user selects Countersign_Mode, THE Approval_Node SHALL require all assigned approvers to approve before the request passes, and SHALL reject the request immediately when any single approver rejects
4. WHEN the user selects Any_N_of_M_Mode, THE Approval_Node SHALL allow the user to specify the minimum number N of approvals required out of M total assigned approvers, where N must be at least 1 and at most M, and M must be at least 2
5. WHEN the user selects Sequential_Mode, THE Approval_Node SHALL allow the user to define the ordered sequence in which approvers are consulted, and SHALL stop the sequence and reject the request if any approver in the sequence rejects
6. WHEN the user assigns a VE as an approver and that VE does not have approval capability enabled, THE Workflow_Designer SHALL prevent the assignment and display an error message indicating that the selected VE lacks approval capability
7. THE Approval_Node SHALL allow configuration of a timeout duration between 1 and 720 hours, after which the node automatically escalates the request to the configured Fallback_Approver
8. THE Approval_Node SHALL allow configuration of a Fallback_Approver (another VE or human) to handle the request when the primary approver has not responded within the configured timeout duration or has explicitly declined the assignment

### Requirement 3: VE Approval Settings

**User Story:** As a VE owner, I want to configure my VE's approval capabilities, so that I can control how and when my VE participates in approval workflows.

#### Acceptance Criteria

1. THE VE settings panel SHALL include an "Approval Capability" section with an enable/disable toggle, defaulting to disabled on VE creation
2. WHEN the VE owner disables the approval capability while requests are pending in the queue, THE VE SHALL reject all new incoming approval requests with a "capability disabled" response and continue processing already-accepted requests until completion or timeout
3. WHILE the VE's approval capability is disabled, THE VE SHALL reject all incoming approval requests with a "capability disabled" response
4. WHEN the VE's approval capability is enabled, THE VE settings SHALL display configuration for: Access_Control_List, Approval_Rules, and operational limits
5. THE Access_Control_List SHALL support whitelist mode (only listed entities can submit requests) and blacklist mode (all except listed entities can submit requests), with a maximum of 500 entries per list
6. THE Access_Control_List SHALL allow filtering by: department list, role list, and skill list, each supporting a maximum of 100 entries
7. THE operational limits section SHALL allow configuration of: maximum pending approval requests (queue size, range 1 to 1000), request timeout in hours before auto-escalation (range 1 to 720), and daily approval quota (range 1 to 10000)
8. IF the VE's pending approval queue reaches the configured maximum and a Fallback_Approver is configured, THEN THE VE SHALL reject new incoming approval requests with a "queue full" response and route the rejected request to the Fallback_Approver
9. IF the VE's pending approval queue reaches the configured maximum and no Fallback_Approver is configured, THEN THE VE SHALL reject new incoming approval requests with a "queue full" response and notify the requesting entity that no fallback is available

### Requirement 4: Three-Way Routing Rules

**User Story:** As a VE owner, I want to define rules for automatic approval decisions, so that my VE can handle routine requests without human intervention.

#### Acceptance Criteria

1. THE VE approval settings SHALL allow configuration of three rule categories: auto-approve conditions, auto-reject conditions, and require-human conditions, with a maximum of 50 rules per category
2. WHEN an Approval_Request arrives at a VE, THE VE SHALL evaluate the request payload against the configured Approval_Rules in the following priority order: auto-reject first, then auto-approve, then require-human, and SHALL complete evaluation within 5 seconds
3. WHEN the request matches an auto-approve condition, THE VE SHALL approve the request within 2 seconds of evaluation completion and record the decision in the Audit_Trail
4. WHEN the request matches an auto-reject condition, THE VE SHALL reject the request within 2 seconds of evaluation completion, include a reason (maximum 500 characters) indicating which rule triggered the rejection, and record the decision in the Audit_Trail
5. WHEN the request matches a require-human condition or no rule covers the case, THE VE SHALL escalate the request to the configured human approver and record the escalation in the Audit_Trail
6. THE Approval_Rules SHALL support condition expressions referencing fields in the Approval_Request payload (e.g., `request.amount`, `request.department`, `request.requester_id`) up to a nesting depth of 3 levels
7. THE Approval_Rules SHALL support comparison operators: equals, not-equals, greater-than, less-than, contains, in-list, not-in-list, is-empty, is-not-empty
8. IF a condition expression references a field that does not exist or is null in the Approval_Request payload, THEN THE VE SHALL treat the condition as not matched and continue evaluating the next rule in priority order
9. IF the configured human approver is unavailable when escalation is triggered, THEN THE VE SHALL retain the request in a pending-escalation queue, record the unavailability in the Audit_Trail, and retry escalation at intervals of 60 seconds for a maximum of 5 attempts before marking the request as escalation-failed
10. WHEN multiple rules within the same category match a request, THE VE SHALL apply the first matching rule based on the configured rule ordering (position index) within that category

### Requirement 5: Approval Request Payload

**User Story:** As a workflow designer, I want to define the structured content sent to approvers, so that VE approvers have sufficient context to make decisions.

#### Acceptance Criteria

1. THE Approval_Request payload SHALL include the following fields: title (string, maximum 200 characters), summary (string, maximum 2000 characters), details (structured key-value pairs), attachments (list of file references), and hint_rules (list of rule hints for the approver)
2. WHEN an Approval_Request is delivered to a VE approver, THE system SHALL include the complete structured payload in the VE's input context
3. THE hint_rules field SHALL contain human-readable descriptions of the applicable Approval_Rules, enabling the VE to explain its decision rationale
4. IF the Approval_Request payload exceeds 100 KB in serialized size, THEN THE system SHALL truncate the details field while preserving title, summary, and hint_rules intact
5. THE attachments field SHALL support references to files stored on Hub (URLs), with a maximum of 10 attachments per request and a maximum of 50 MB total attachment size

### Requirement 6: Version Management

**User Story:** As a user, I want to manage versions of my workflows, so that I can iterate on designs without disrupting running instances.

#### Acceptance Criteria

1. WHEN the user saves a workflow in the Workflow_Designer, THE system SHALL create or update a Draft_Version with an auto-incremented version number (major.minor.patch format)
2. WHEN the user clicks "Submit for Review", THE system SHALL validate the workflow structure and submit the Draft_Version to the Admin_Reviewer queue, transitioning the version status to "pending_review"
3. WHEN an Admin_Reviewer approves a submitted version, THE system SHALL publish that version as the new Published_Version, mark the previous Published_Version as "superseded", and list the new version in the Capability_Market
4. WHEN an Admin_Reviewer rejects a submitted version, THE system SHALL return the version to "rejected" status with the rejection reason visible to the author
5. WHEN a user modifies an already-published workflow, THE system SHALL create a new Draft_Version (incrementing the minor version number) without affecting the current Published_Version
6. WHEN a new version is published, THE system SHALL bind all new Workflow_Instances to the new Published_Version while existing running instances continue on their bound version until completion
7. THE Admin_Reviewer SHALL have the authority to unpublish (take down) a Published_Version, which prevents new instances from being created but does not terminate running instances

### Requirement 7: Admin Review Process

**User Story:** As an admin, I want to review and approve workflow submissions, so that only validated workflows are published to the enterprise market.

#### Acceptance Criteria

1. THE Hub admin panel SHALL display a queue of pending workflow submissions sorted by submission date (oldest first), paginated at 50 items per page, showing: workflow name, author, submission date, and version number
2. WHEN the admin selects a submission for review, THE system SHALL display the complete workflow graph, node configurations, and approval rules for inspection
3. WHEN the admin approves a submission, THE system SHALL transition the version status from "pending_review" to "published" and make it available in the Capability_Market
4. WHEN the admin rejects a submission, THE system SHALL require a rejection reason (minimum 10 characters, maximum 2000 characters) and transition the version status from "pending_review" to "rejected"
5. WHEN a submission status transitions to "published" or "rejected", THE system SHALL send a Hub notification to the workflow author within 60 seconds, including the new status and, if rejected, the rejection reason provided by the admin
6. IF the admin takes no action on a submission within 7 days, THEN THE system SHALL send a reminder notification to the admin, repeating every 7 days until the submission is approved or rejected

### Requirement 8: Enterprise Capability Market Integration

**User Story:** As a user, I want to discover and use published approval workflows from the enterprise market, so that I can leverage workflows designed by others.

#### Acceptance Criteria

1. THE Capability_Market SHALL categorize items into two top-level categories: Workflows (with sub-categories: 审批类, 自动化类, 协作类) and Normal Skills
2. THE Capability_Market SHALL automatically classify approval workflows (kind=approval_workflow) under the "审批类" (Approval) sub-category of Workflows
3. WHEN a user browses the Capability_Market, THE system SHALL display published approval workflows with: name, description, author, version, usage count, and a visual flow preview thumbnail
4. WHEN a user triggers a published approval workflow, THE system SHALL create a new Workflow_Instance bound to the current Published_Version
5. THE system SHALL ensure each user's designed workflows are independent and isolated — one user's workflow definitions are not visible to or editable by other users (unless published to the market)
6. WHEN a user searches the Capability_Market, THE system SHALL support filtering by: category (Workflow/Normal Skill), sub-category, author, and keyword search across name and description

### Requirement 9: Workflow Instance Execution

**User Story:** As a system, I want to execute workflow instances reliably, so that approval requests are processed according to the defined workflow logic.

#### Acceptance Criteria

1. WHEN a Trigger_Node fires, THE system SHALL create a new Workflow_Instance bound to the current Published_Version of the workflow and set the instance status to "running"
2. THE system SHALL execute nodes in the Workflow_Instance according to the directed graph edges, respecting the defined execution order
3. WHEN execution reaches a Condition_Branch_Node, THE system SHALL evaluate the branch conditions in their defined priority order against the current instance data and route to the first matching branch
4. IF no branch condition matches at a Condition_Branch_Node and a default branch is configured, THEN THE system SHALL route execution to the default branch
5. IF no branch condition matches at a Condition_Branch_Node and no default branch is configured, THEN THE system SHALL mark the node as "failed", record the failure reason in the Audit_Trail, and trigger the configured fallback behavior
6. WHEN execution reaches an Approval_Node, THE system SHALL deliver the Approval_Request to the assigned VE approver(s) according to the configured approval mode
7. WHEN all nodes in a Workflow_Instance have completed execution, THE system SHALL mark the instance status as "completed"
8. IF a node execution fails (e.g., VE unreachable, timeout exceeded after 72 hours of inactivity, system error), THEN THE system SHALL mark the node as "failed", trigger the configured fallback behavior, and record the failure reason in the Audit_Trail
9. THE system SHALL support concurrent execution of multiple Workflow_Instances of the same workflow definition

### Requirement 10: Audit Trail

**User Story:** As a compliance officer, I want a complete audit trail of all approval decisions, so that I can review who approved what, when, and based on what rules.

#### Acceptance Criteria

1. THE Audit_Trail SHALL record the following for each approval decision: instance ID, node ID, approver VE ID, decision (approve/reject/escalate), timestamp (UTC, millisecond precision), matched rule (if auto-decision), and decision rationale (maximum 2000 characters)
2. THE Audit_Trail SHALL record workflow instance lifecycle events: creation, node transitions, completion, and failure, each with a UTC timestamp at millisecond precision
3. THE Audit_Trail SHALL be immutable — once a record is written, it cannot be modified or deleted by any user including administrators
4. WHEN a user queries the Audit_Trail for a specific Workflow_Instance, THE system SHALL return the complete chronological sequence of events for that instance
5. THE Audit_Trail SHALL support querying by: instance ID, approver VE ID, requester ID, time range, and decision outcome, with results paginated at 100 records per page
6. THE Audit_Trail records SHALL be retained for a minimum of 3 years from the record creation date

### Requirement 11: Delegation and Fallback

**User Story:** As a workflow designer, I want to configure fallback behavior for unavailable approvers, so that approval requests are not stuck indefinitely.

#### Acceptance Criteria

1. WHEN a VE approver is unavailable (approval capability disabled), THE system SHALL route the request to the configured Fallback_Approver within 30 seconds of detecting the unavailability
2. WHEN a VE approver's pending queue has reached the maximum configured limit (default: 50 requests), THE system SHALL route the request to the configured Fallback_Approver
3. WHEN a VE approver does not respond within the configured timeout period (default: 24 hours), THE system SHALL route the request to the configured Fallback_Approver and record the timeout in the Audit_Trail
4. IF no Fallback_Approver is configured and the primary approver is unavailable, THEN THE system SHALL mark the approval node as "blocked", notify the workflow instance initiator via system notification within 60 seconds, and include the reason for the block and the identity of the unavailable approver in the notification
5. THE Fallback_Approver SHALL receive the same Approval_Request payload and hint_rules as the primary approver
6. THE Audit_Trail SHALL record all fallback events including: timestamp, reason for fallback (disabled/queue_full/timeout), original approver, fallback approver, and the Approval_Request identifier
7. IF the configured Fallback_Approver is also unavailable (disabled or queue full), THEN THE system SHALL mark the approval node as "blocked", notify the workflow instance initiator via system notification within 60 seconds, and record the cascading failure in the Audit_Trail

### Requirement 12: Workflow Lifecycle States

**User Story:** As a user, I want clear visibility into my workflow's lifecycle state, so that I know whether it's in draft, under review, published, or taken down.

#### Acceptance Criteria

1. THE system SHALL support the following workflow version states: draft, pending_review, published, rejected, superseded, unpublished
2. WHEN a version is in "draft" state, THE user SHALL be able to edit and save changes freely
3. WHEN a version is in "pending_review" state, THE user SHALL NOT be able to edit the version but SHALL be able to withdraw the submission, returning it to "draft" state
4. WHEN a version is in "rejected" state, THE user SHALL be able to view the rejection reason and create a new draft version incorporating feedback
5. WHEN a version is in "published" state, THE user SHALL NOT be able to edit it directly but SHALL be able to create a new version based on it
6. WHEN a version is in "unpublished" state (admin take-down), THE user SHALL be notified with the take-down reason and be able to create a new version addressing the issues
7. THE Workflow_Designer SHALL display the current version state prominently in the workflow editor header, using distinct visual indicators (color and icon) for each state
