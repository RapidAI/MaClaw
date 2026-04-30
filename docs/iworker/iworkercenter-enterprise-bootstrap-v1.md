# iWorkerCenter Enterprise Bootstrap v1

## Why this exists

When a new company buys the iWorker system, the company should not start by manually assigning tasks one by one. The first product job is to turn an empty iWorkerCenter into a runnable AI-native organization.

Enterprise Bootstrap is the organization-level launch workflow that creates the first operating model, memory base, digital employee crew, goals, recurring tasks, and GoalWatcher run loop.

## Product boundary

- iWorkerCenter owns the bootstrap process because it is the customer's organization runtime.
- iWorker desktop is only a local body/cache and should join after Center creates the worker identity and memory authority.
- iWorkerCloud may provide compute, skill market, license, and multi-tenant management, but it does not participate in the customer's operations.

## Required interfaces

### Admin UI

The admin console needs an Enterprise Bootstrap page with these phases:

1. Company profile intake
2. Virtual organization map
3. Initial iWorker crew
4. Memory seeding
5. Goal and task discovery
6. Autonomous run loop

The administrator and executive sponsor should confirm boundaries, priorities, risk rules, and external communication rules. They should not become the daily control center.

### Backend APIs

Minimum API set:

```http
GET /admin/bootstrap/status
POST /admin/bootstrap/draft-plan
POST /admin/bootstrap/validate-plan
POST /admin/bootstrap/apply-plan
POST /admin/bootstrap/start-first-wave
GET /admin/bootstrap/runs/{run_id}
GET /admin/goalwatch/policy
PUT /admin/goalwatch/policy
```

Expected DTO shape:

```json
{
  "company_name": "Acme Manufacturing",
  "business_summary": "Manufactures and delivers industrial parts.",
  "priority": "Stabilize delivery and customer exceptions.",
  "virtual_departments": ["Sales", "Operations", "Customer Success", "Finance", "Quality", "Office"],
  "initial_iworkers": ["Office iWorker", "Ops iWorker", "Data iWorker", "Quality iWorker"],
  "memory_scopes": ["company", "department", "personal"],
  "recurring_tasks": ["Daily operating brief", "Customer exception scan"],
  "watcher_policy": {
    "enabled": true,
    "single_flight": true,
    "max_run_seconds": 120,
    "scale_by_worker_count": true
  }
}
```

### Import tools

Bootstrap needs importers that classify source material into durable Center memory:

- Company memory: company rules, strategy, product definitions, customer commitments, compliance constraints.
- Department memory: SOPs, handoff rules, recurring workflows, department-specific playbooks.
- Personal memory: worker preferences and personal execution context, owned by a worker identity.

Local files are evidence during import. Durable memory must be written to iWorkerCenter.

### Goal tools

Goal discovery should generate:

- Goal tree
- First-wave tasks
- Recurring task templates
- Exception monitors
- Executive escalation rules
- GoalWatcher cluster policy

GoalWatcher should push work to iWorkers automatically and should not rely on the boss/board for normal execution.

## First-wave task startup

A newly bootstrapped company should start with a small task wave:

- Daily operating brief
- Customer exception scan
- Policy memory check
- Current open issue classification
- Weekly decision summary draft

Each task must include owner iWorker, expected output, memory scope, escalation threshold, and whether peer iWorker discussion is required. After `apply-plan` creates the reusable workflow templates, `start-first-wave` should start real workflow instances and collaboration tasks, not merely show a preview.

## Implementation order

1. Add admin UI page for Enterprise Bootstrap.
2. Add in-memory/mock status API so the UI can call real endpoints.
3. Persist bootstrap plan in iWorkerCenter database.
4. Generate virtual departments and initial iWorkers.
5. Seed memories through corelib multi-tenant memory.
6. Generate first-wave tasks and recurring task templates.
7. Connect GoalWatcher cluster startup and push queue.

## Current code status

The admin UI now has an Enterprise Bootstrap page as the product entry point. The `/admin/bootstrap/*` APIs now support status, draft plan, validation, apply plan, persisted bootstrap state, and first-wave task startup. `apply-plan` is now connected to role, colleague, and workflow-template provisioning, so virtual departments become roles, initial iWorkers become active colleague records, and the first operating workflows are created and published idempotently. `start-first-wave` starts the published workflow instances and records them on the bootstrap run. Bootstrap memory seeding now writes initial company, department, and personal memories through the iWorkerCenter worker-memory module backed by corelib multi-tenant memory. Bootstrap now persists applied assets in its status state, so administrators can refresh the console and still see created roles, iWorkers, workflow templates, memory seeds, workflow instances, and GoalWatcher policy records. GoalWatcher policy is stored per tenant in system_settings and can be viewed or updated through the admin policy API. The next backend step is to make the running GoalWatcher monitor consume tenant-specific policy values during shard scheduling instead of only persisting and reporting them.



