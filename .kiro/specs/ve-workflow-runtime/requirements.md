# Requirements Document

## Introduction

This feature implements the runtime lifecycle of approval workflows after they are designed and published. While the existing ve-approval-workflow spec covers workflow DESIGN (graph editor, version management, admin review, publishing), this spec covers RUNTIME: how workflows are initiated, how data flows through execution nodes, how results are delivered to executors and notifiers, and how participants confirm outcomes. The system builds on the existing Hub infrastructure (PostgreSQL with workflow_instances, node_executions, audit_trail tables), WorkflowExecutor (StartInstance/ResumeInstance/HandleTimeout), A2A protocol for VE-to-VE communication, and IM channels (飞书/微信/QQ) connected via Hub.

## Glossary

- **Hub**: The web-based management platform with PostgreSQL database, serving as the central data store and execution engine for workflow instances
- **Workflow_Instance**: A running execution of a specific Published_Version, created when a workflow is initiated through any channel
- **Initiator**: The user or system that triggers a new Workflow_Instance by submitting form data
- **Result_Executor**: A real person designated to take action based on the workflow outcome (e.g., HR processes a leave request after approval)
- **Notifier**: A real person designated to be informed of the workflow outcome without required action (e.g., team lead notified of approved leave)
- **Confirmation**: An acknowledgment action required from Result_Executors ("已操作") or Notifiers ("已知会") after receiving workflow results
- **Instance_Timeline**: A chronological event log of all actions, decisions, and state transitions within a Workflow_Instance, visible to participants
- **IM_Channel**: Instant messaging platforms (飞书/微信/QQ) connected to Hub for workflow initiation and notification delivery
- **VE**: Virtual Employee (数字员工) — an AI digital worker capable of extracting structured data from natural language for IM-based workflow initiation
- **Form_Data**: The structured input data submitted when initiating a workflow, containing all fields required by the workflow's Form_Node configuration
- **Terminal_Node**: A node in the workflow graph that represents workflow completion, configured with Result_Executor and Notifier assignments
- **Notification_Node**: A node in the workflow graph configured to send notifications to specified recipients at specific points in the workflow
- **Workflow_Directory**: The Hub page and desktop app panel providing categorized views of workflow instances relevant to the current user
- **Data_Retention_Policy**: The configured duration for which completed Workflow_Instance data is preserved before archival or deletion
- **Escalation**: The automatic action taken when a confirmation is not received within the configured timeout period

## Requirements

### Requirement 1: Hub Page Form Initiation

**User Story:** As a user, I want to initiate a workflow by filling out a form on the Hub page, so that I can submit structured approval requests with all required fields.

#### Acceptance Criteria

1. WHEN the user navigates to a published workflow's initiation page on Hub, THE Hub SHALL display a form containing all fields defined in the workflow's Form_Node configuration
2. WHEN the user submits a completed form, THE Hub SHALL validate all required fields are present and conform to their defined data types before creating a Workflow_Instance
3. IF the user submits a form with missing required fields or invalid data types, THEN THE Hub SHALL display inline validation errors adjacent to each invalid field without clearing the form
4. WHEN form validation succeeds, THE Hub SHALL create a new Workflow_Instance with status "running", persist the complete Form_Data to the workflow_instances table, and record an "instance_created" event in the audit_trail
5. WHEN a Workflow_Instance is successfully created, THE Hub SHALL redirect the user to the instance detail page showing the Instance_Timeline within 2 seconds of submission
6. THE Form_Data persisted to the database SHALL include: all field values, the Initiator's user ID, submission timestamp (UTC, millisecond precision), and the workflow version ID

### Requirement 2: IM Quick Initiation

**User Story:** As a user, I want to initiate simple approvals via IM by sending a natural language message to my VE, so that I can start workflows without navigating to the Hub page.

#### Acceptance Criteria

1. WHEN a user sends a message to their VE in an IM_Channel containing an approval initiation intent (e.g., "@VE 帮我发起请假审批，明天一天"), THE VE SHALL extract structured Form_Data from the natural language message using the VE's natural language understanding capability
2. WHEN the VE successfully extracts Form_Data, THE VE SHALL present the extracted data back to the user in the IM_Channel for confirmation before creating the Workflow_Instance
3. WHEN the user confirms the extracted data in the IM_Channel, THE VE SHALL submit the Form_Data to Hub via API to create a new Workflow_Instance
4. IF the VE cannot extract all required fields from the natural language message, THEN THE VE SHALL ask the user for the missing fields in the IM_Channel, specifying which fields are needed
5. IF the user's message does not match any published workflow's Form_Node schema, THEN THE VE SHALL inform the user that no matching workflow was found and suggest available workflows
6. WHEN a Workflow_Instance is created via IM initiation, THE Hub SHALL persist the same complete Form_Data as Hub page initiation, with the initiation channel recorded as the IM platform identifier

### Requirement 3: API Trigger Initiation

**User Story:** As a system integrator, I want to trigger workflows via API, so that external systems can programmatically initiate approval processes.

#### Acceptance Criteria

1. THE Hub SHALL expose a REST API endpoint for creating Workflow_Instances that accepts: workflow ID, Form_Data payload (JSON), and API authentication credentials
2. WHEN the API receives a valid request with correct authentication, THE Hub SHALL validate the Form_Data against the workflow's Form_Node schema and create a Workflow_Instance
3. IF the API request contains invalid authentication credentials, THEN THE Hub SHALL return HTTP 401 with an error message and not create a Workflow_Instance
4. IF the API request contains Form_Data that does not conform to the workflow's Form_Node schema, THEN THE Hub SHALL return HTTP 400 with a list of validation errors specifying each invalid field
5. WHEN a Workflow_Instance is successfully created via API, THE Hub SHALL return HTTP 201 with the instance ID, creation timestamp, and initial status
6. THE API endpoint SHALL enforce rate limiting of 100 requests per minute per authenticated client

### Requirement 4: Runtime Data Persistence

**User Story:** As a compliance officer, I want complete instance lifecycle data persisted on Hub, so that I can audit the full history of any workflow execution.

#### Acceptance Criteria

1. THE Hub SHALL persist the following data for each Workflow_Instance: Form_Data submitted at initiation, each node's input data and output data, each approval decision with rationale text, all timestamps in UTC with millisecond precision, and references to any attachments
2. WHEN a node in the Workflow_Instance completes execution, THE Hub SHALL record a node_execution entry containing: instance ID, node ID, node type, input data, output data, start timestamp, completion timestamp, and execution status
3. THE Hub SHALL maintain an Instance_Timeline as an ordered sequence of events for each Workflow_Instance, where each event contains: event type, actor ID, timestamp, and event-specific details
4. WHEN any participant queries the Instance_Timeline, THE Hub SHALL return the complete chronological sequence of events, paginated at 50 events per page
5. THE Hub SHALL enforce a Data_Retention_Policy where completed Workflow_Instance data is retained for a minimum of 3 years from the instance completion date
6. WHEN a Workflow_Instance's retention period expires, THE Hub SHALL archive the instance data to cold storage rather than deleting it, preserving the ability to restore if needed

### Requirement 5: Result Notification to Executors

**User Story:** As a workflow designer, I want to configure Result_Executors on terminal nodes, so that the right person is notified to take action when a workflow completes.

#### Acceptance Criteria

1. WHEN a Workflow_Instance reaches a Terminal_Node with configured Result_Executors, THE Hub SHALL send a notification to each Result_Executor within 60 seconds of workflow completion
2. THE notification to Result_Executors SHALL include: workflow name, approval result (approved/rejected), a summary of the Form_Data, the Initiator's identity, and a link to the instance detail page on Hub
3. THE Hub SHALL deliver Result_Executor notifications through two channels simultaneously: Hub in-app notification and IM push notification to the executor's connected IM_Channel (飞书/微信)
4. WHEN a Result_Executor receives the notification, THE instance detail page SHALL display the complete Form_Data, all approval decisions with rationale, and a "确认已操作" (confirm action taken) button
5. THE workflow graph designer SHALL allow configuring one or more Result_Executors per Terminal_Node, where each executor is identified by their Hub user ID
6. IF a configured Result_Executor's IM_Channel is not connected, THEN THE Hub SHALL deliver the notification only through the Hub in-app channel and record the IM delivery failure in the Instance_Timeline

### Requirement 6: Result Notification to Notifiers

**User Story:** As a workflow designer, I want to configure Notifiers on terminal nodes, so that relevant stakeholders are informed of workflow outcomes.

#### Acceptance Criteria

1. WHEN a Workflow_Instance reaches a Terminal_Node with configured Notifiers, THE Hub SHALL send a notification to each Notifier within 60 seconds of workflow completion
2. THE notification to Notifiers SHALL include: workflow name, approval result (approved/rejected), a summary of the Form_Data, and a link to the instance detail page on Hub
3. THE Hub SHALL deliver Notifier notifications through two channels simultaneously: Hub in-app notification and IM push notification to the notifier's connected IM_Channel
4. WHEN a Notifier receives the notification, THE instance detail page SHALL display the approval result summary and a "确认已知会" (confirm acknowledged) button
5. THE workflow graph designer SHALL allow configuring zero or more Notifiers per Terminal_Node, where each notifier is identified by their Hub user ID
6. THE Hub SHALL distinguish between Result_Executor notifications and Notifier notifications in the Instance_Timeline event log, recording the notification type, recipient, delivery channel, and delivery timestamp

### Requirement 7: Result Confirmation by Executors

**User Story:** As a Result_Executor, I want to confirm that I have taken action on a workflow result, so that the system tracks completion of post-approval tasks.

#### Acceptance Criteria

1. WHEN a Result_Executor clicks "确认已操作" on the instance detail page, THE Hub SHALL record the confirmation with: executor user ID, confirmation timestamp, and optional notes (maximum 2000 characters)
2. THE Hub SHALL allow Result_Executors to add notes when confirming, describing what action was taken
3. WHEN a Result_Executor has not confirmed within the configured timeout period (default: 48 hours), THE Hub SHALL send a reminder notification through both Hub in-app and IM_Channel
4. THE Hub SHALL send reminder notifications at intervals of 24 hours, up to a maximum of 3 reminders
5. IF a Result_Executor has not confirmed after all reminders are exhausted, THEN THE Hub SHALL escalate by notifying the Initiator that the executor has not confirmed, and record an "escalation_triggered" event in the Instance_Timeline
6. THE confirmation status of each Result_Executor SHALL be visible on the instance detail page, showing: pending/confirmed status, confirmation timestamp (if confirmed), and notes (if provided)

### Requirement 8: Result Confirmation by Notifiers

**User Story:** As a Notifier, I want to acknowledge that I have been informed of a workflow result, so that the system tracks information dissemination.

#### Acceptance Criteria

1. WHEN a Notifier clicks "确认已知会" on the instance detail page, THE Hub SHALL record the acknowledgment with: notifier user ID and acknowledgment timestamp
2. WHEN a Notifier has not acknowledged within the configured timeout period (default: 72 hours), THE Hub SHALL send a reminder notification through both Hub in-app and IM_Channel
3. THE Hub SHALL send reminder notifications at intervals of 24 hours, up to a maximum of 2 reminders
4. IF a Notifier has not acknowledged after all reminders are exhausted, THEN THE Hub SHALL auto-close the acknowledgment request and record an "auto_closed" event in the Instance_Timeline with reason "notifier_timeout"
5. THE acknowledgment status of each Notifier SHALL be visible on the instance detail page, showing: pending/acknowledged/auto-closed status and timestamp

### Requirement 9: Workflow Directory - My Initiated

**User Story:** As a user, I want to see all workflows I have initiated, so that I can track the progress of my requests.

#### Acceptance Criteria

1. THE Workflow_Directory SHALL provide a "我发起的" (My Initiated) view listing all Workflow_Instances where the current user is the Initiator
2. THE "我发起的" view SHALL display for each instance: workflow name, initiation date, current status (running/completed/cancelled/withdrawn), and current node label
3. THE "我发起的" view SHALL support filtering by: status (running/completed/cancelled/withdrawn), date range (start date and end date), and workflow type (workflow definition name)
4. THE "我发起的" view SHALL be sorted by initiation date (newest first) by default, with pagination at 20 items per page
5. THE Workflow_Directory SHALL be accessible from both the Hub web page and the desktop app panel

### Requirement 10: Workflow Directory - Pending My Action

**User Story:** As an approver, I want to see all workflows waiting for my approval decision, so that I can prioritize and process pending requests.

#### Acceptance Criteria

1. THE Workflow_Directory SHALL provide a "待我处理的" (Pending My Action) view listing all Workflow_Instances where the current user (or their VE) has a pending approval node
2. THE "待我处理的" view SHALL display for each instance: workflow name, Initiator name, submission date, time elapsed since the approval was assigned to the user, and urgency indicator (normal/approaching timeout/overdue)
3. THE "待我处理的" view SHALL be sorted by urgency (overdue first, then approaching timeout, then normal) and within each urgency level by submission date (oldest first)
4. WHEN the user clicks an item in the "待我处理的" view, THE Hub SHALL navigate to the approval detail page where the user can review the request and make a decision

### Requirement 11: Workflow Directory - Pending My Confirmation

**User Story:** As a Result_Executor or Notifier, I want to see all workflow results waiting for my confirmation, so that I can track my pending acknowledgments.

#### Acceptance Criteria

1. THE Workflow_Directory SHALL provide a "待我确认的" (Pending My Confirmation) view listing all completed Workflow_Instances where the current user has a pending executor confirmation or notifier acknowledgment
2. THE "待我确认的" view SHALL display for each instance: workflow name, result (approved/rejected), completion date, confirmation type (executor/notifier), and time remaining before timeout
3. THE "待我确认的" view SHALL be sorted by time remaining (least time remaining first)
4. WHEN the user clicks an item in the "待我确认的" view, THE Hub SHALL navigate to the instance detail page with the confirmation button prominently displayed

### Requirement 12: Workflow Directory - Completed

**User Story:** As a user, I want to see all completed workflows I participated in, so that I can review historical decisions and outcomes.

#### Acceptance Criteria

1. THE Workflow_Directory SHALL provide a "已完成的" (Completed) view listing all Workflow_Instances where the current user participated as Initiator, approver, Result_Executor, or Notifier, and the instance has reached a terminal state
2. THE "已完成的" view SHALL display for each instance: workflow name, role of the current user in that instance, completion date, and final result (approved/rejected/cancelled/withdrawn)
3. THE "已完成的" view SHALL support filtering by: date range, workflow type, final result, and user's role in the instance
4. THE "已完成的" view SHALL be sorted by completion date (newest first) by default, with pagination at 20 items per page

### Requirement 13: Workflow Withdrawal and Cancellation

**User Story:** As an Initiator, I want to withdraw or cancel a workflow I started before it completes, so that I can stop unnecessary approval processes.

#### Acceptance Criteria

1. WHILE a Workflow_Instance is in "running" status and has not yet reached a Terminal_Node, THE Hub SHALL allow the Initiator to withdraw the instance from the Hub platform
2. WHEN the Initiator withdraws a Workflow_Instance, THE Hub SHALL immediately cancel all pending approval nodes, set the instance status to "withdrawn", and record a "withdrawal" event in the Instance_Timeline with the Initiator's user ID and timestamp
3. WHEN a Workflow_Instance is withdrawn, THE Hub SHALL notify all participants who had pending actions (approvers with pending decisions, Result_Executors, Notifiers) within 60 seconds, informing them that the workflow has been withdrawn by the Initiator
4. IF the Initiator attempts to withdraw a Workflow_Instance that has already reached a Terminal_Node and delivered results, THEN THE Hub SHALL reject the withdrawal request and display a message indicating that withdrawal is not possible after results have been delivered
5. THE withdrawal notification to participants SHALL include: workflow name, Initiator identity, withdrawal timestamp, and a note that no further action is required
6. THE Instance_Timeline SHALL record the withdrawal event and all subsequent cancellation notifications as distinct events with timestamps

### Requirement 14: Terminal Node Executor and Notifier Configuration

**User Story:** As a workflow designer, I want to configure Result_Executors and Notifiers on terminal nodes in the graph editor, so that the right people are notified when workflows complete.

#### Acceptance Criteria

1. WHEN the workflow designer selects a Terminal_Node for configuration, THE Workflow_Designer SHALL display fields for: Result_Executor assignment (one or more Hub user IDs) and Notifier assignment (zero or more Hub user IDs)
2. THE Workflow_Designer SHALL allow searching and selecting users from the Hub user directory when assigning Result_Executors and Notifiers
3. IF the workflow designer attempts to save a Terminal_Node without at least one Result_Executor configured, THEN THE Workflow_Designer SHALL display a validation warning (not error) indicating that no executor is assigned, allowing the designer to proceed or add one
4. THE Workflow_Designer SHALL allow configuring timeout durations for executor confirmation (range: 1 to 720 hours, default: 48 hours) and notifier acknowledgment (range: 1 to 720 hours, default: 72 hours) per Terminal_Node
5. THE Workflow_Designer SHALL allow configuring the maximum number of reminders for executor confirmation (range: 1 to 10, default: 3) and notifier acknowledgment (range: 1 to 10, default: 2) per Terminal_Node
