# Spawn coding agent worker requirements

Complex programming work should be delegable via spawn_coding_agent without letting multiple workers write the primary checkout at once.
Read-only explorer/reviewer fan-out already exists. Worker writes require an isolated write workspace, a frozen write-set, and a controlled cherry-pick merge.

This document is the user-visible contract. Mechanism reuse: existing worktree, write-set, and cherry-pick gates.

## Goal

The full-environment coding workbench root may spawn an implementation child that:

1. Completes a focused implement/fix task in a clean context.
2. Writes only an isolated Git worktree, never the primary checkout.
3. Merges back through the frozen write-set and controlled cherry-pick.
4. Returns to the same parent turn. The parent waits inside the tool call and does not use the read-only ledger waiting_child handoff.

## Non-goals

- Nested spawn from a worker (depth stays 1).
- Fallback writes into the primary tree when Git worktree / isolate setup fails.
- Treating task titles or model claims of independence as a concurrency license.

## Roles

- explorer: read-only search/read; up to 3 may run in parallel when the whole batch is inspection-only.
- reviewer: read-only review including git_diff; same parallel rule as explorer.
- worker: standard coding tools, no further spawn; isolated worktree then merge.

Any batch that contains a worker stays out of the inspection ledger parallelizer.

## Worker admission (fail-closed)

All of the following are required:

1. Parent is a local or remote full-environment root (fullEnvironment, nestDepth 0).
2. Each worker declares an exact files write-set: non-empty, no wildcards, no tilde or shell expansion, not project root.
3. The project is a Git repo with at least one commit. Worktree/isolate create failure fails the spawn and must not write the primary tree.
4. The primary checkout must be clean before merge.
5. Actual changed paths must stay inside the frozen files. Undeclared paths or cherry-pick conflicts refuse the merge and keep the review branch / isolate.

## Lifecycle

    parent spawn_coding_agent(role=worker, files=[...])
      -> validate write-set
      -> create worktree / remote git worktree isolate
      -> child ExecuteTask in isolate, depth=1
      -> failure: do not merge; keep worktree/isolate if it has changes
      -> success: commit + write-set check + cherry-pick onto primary
      -> remap child paths onto the primary project and record parent audit
      -> return an engineer summary that includes the merge result

The parent stays blocked in the tool call so it keeps its write lease and does not call AdmitReadOnlyChildren. Inspection children with a Runtime Attempt still use the existing ledger admission path.

## Prompt contract

- Root: use worker for isolated implementation; files is required; do not parallelize overlapping writers.
- Nested worker: do not spawn; edit only declared paths.
- Keep audit headings out of the user-visible Summary.

## Slices

- P0 (done): local sequential worker + files + worktree + controlled merge.
- P1 (done): at most two isolated local workers in one spawn when write-sets do not overlap; merge stays sequential.
- P2 (done): remote isolated-directory workers, sequential only, git worktree isolate (allowFullCopy=false). Full-copy isolates cannot auto-merge.

## Acceptance

- role=worker without files fails parse.
- Non-Git / no commit / worktree create failure fails spawn and leaves the primary tree unchanged.
- Undeclared changed paths fail merge and leave the primary tree unchanged.
- Dirty primary checkout refuses merge.
- Local: two workers with disjoint files may run in parallel; overlapping or mixed explorer+worker stay sequential; more than two workers stay sequential.
- Remote: worker without SSH session/project fails closed; worker with isolate create failure does not write primary; no remote parallel writers; remote write-set must also pass isolate path rules at admission.
- Explorer/reviewer behavior and parallel rules stay unchanged.
- Nested workers cannot call spawn_coding_agent.
