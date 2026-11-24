# Desktop turn idempotency — body `client_message_id` alone is enough

**Verdict: proven.** A repeat POST that carries only the body field
`client_message_id` and **no** `Idempotency-Key` header binds to the run the
first POST already started and never enters a second `Execute`. Code and a
passing test both confirm it.

## Srv — one gate, one source of truth

Route (`MaClawSrv/http_routes_user.go`):

```
POST /api/v1/instances/{instanceId}/messages → s.withPrincipal(s.handleSendMessage)
```

`handleSendMessage` — `MaClawSrv/http_agent.go:189`.

Header handling, `http_agent.go:228-236`:

```go
if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
	if in.ClientMessageID != "" && strings.TrimSpace(in.ClientMessageID) != key {
		... 400 "Idempotency-Key does not match client_message_id" ...
	}
	in.ClientMessageID = key
}
```

When the header is **absent** this whole block is skipped and `in.ClientMessageID`
keeps the body value. The header only validates/overrides a present body key —
it is not the dedup source.

Dispatch:

- async (`Prefer: respond-async`) → `s.svc.SendMessageAsync` (`http_agent.go:274`)
- sync → `s.svc.SendMessage` (`http_agent.go:310`)

The real dedup lives in `corelib/agentservice`, shared by both paths:

`Service.PostMessage` — `corelib/agentservice/service_messaging.go` (the
`client_message_id` normalization + gate):

```go
clientMessageID := strings.TrimSpace(in.ClientMessageID)          // L266
if clientMessageID == "" && in.Metadata != nil {
	clientMessageID = strings.TrimSpace(in.Metadata["client_message_id"])  // L268
}
if clientMessageID != "" {
	in.Metadata["client_message_id"] = clientMessageID              // L274
	releaseIdempotency = s.lockIdempotency(idempotencyKey("message",
		tenant, user, instance, sess.ID, clientMessageID))           // L278
	...
	if run, msg, ok := s.findExistingClientMessage(sess.ID, p,
		instanceID, clientMessageID); ok {                            // L288
		in.onRunCreated(run)
		if in.asyncAdmission { releaseIdempotency(); releaseIdempotency = nil }
		return &run, msg, nil    // early return: binds the SAME run, no execute
	}
}
```

`findExistingClientMessage` — `service_messaging.go:177`. It scans the session
for the prior `Role==user` message whose `Metadata["client_message_id"]` equals
the key (L184) and resolves its run through `GetRunByUserMessageID`.

Async path: `SendMessageAsync` folds `in.ClientMessageID` into
`metadata["client_message_id"]` (L80-84) then `PostMessageAsync` sets
`asyncAdmission=true`/`onRunCreated` and calls `s.PostMessage` in the goroutine
— so the async request lands on the identical L266/L288 gate via the metadata
fallback. The comment at L255/L260-265 says it plainly: one gate, one source of
truth.

## Hub — busy queue (independent second layer)

`enqueueDesktopMessage` — `hub/internal/botmgmt/desktop_queue.go:166`. Same
`ClientMessageID` already queued returns its position without appending again
(L189-193).

`popDesktopQueue` — `desktop_queue.go:535`. `queueAdmitLocked` marks a key that
already has a runID as `running` and binds `RunID` instead of dispatching a new
turn (L558-563).

`dispatchQueuedItem` — `desktop_queue.go:671`. `desktopStarting` covers the
popped-but-run-not-started window (L675-686).

`claimDesktopTurnPref` — `desktop_admit.go:318` (the reserve window is
L367-448). A repeated key does not take a second reserve; a second concurrent
POST is left for the Srv replay.

Even if a second POST reaches Srv it is absorbed by
`PostMessage.findExistingClientMessage` — Hub and Srv are two independent
layers both keyed on the same `client_message_id`.

## Tests that prove it

| Test | Path | Header? | Proves |
| --- | --- | --- | --- |
| `TestAsyncReplayDoesNotStealTheDesktopTurn` | `MaClawSrv/desktop_async_turn_test.go` | `Idempotency-Key` set | async repeat binds same run, replay does not steal handoff. **PASS** |
| `TestAsyncReplayBindsRunWithoutIdempotencyHeader` | `MaClawSrv/desktop_async_turn_test.go` | **none** (body only) | async repeat binds same run AND `executions == 1`. **PASS** |
| `TestBothMessageEndpointsHonorTopLevelClientMessageID` | `MaClawSrv/message_idempotency_parity_test.go` | **none** | sync path, both message endpoints bind same run. **PASS** |
| `TestBothMessageEndpointsHonorMetadataClientMessageID` | `MaClawSrv/message_idempotency_parity_test.go` | **none** | metadata spelling binds same run. **PASS** |
| `TestIdempotencyHeaderMismatchRejectsTopLevelBodyKey` | `MaClawSrv/message_idempotency_parity_test.go` | set + body | header/body disagreement → 400. **PASS** |

Verification commands (run, exit 0):

```
go test -count=1 -timeout 90s ./MaClawSrv -run 'TestAsyncReplayBindsRunWithoutIdempotencyHeader' -v
go test -count=1 -timeout 90s ./MaClawSrv -run 'TestAsyncReplay|TestBothMessageEndpoints|TestIdempotencyHeaderMismatch' -v
```

Observed in `TestAsyncReplayBindsRunWithoutIdempotencyHeader`: both POSTs logged
`run=run_9685e152-…`, the second `srv.desktop_finish … replay=yes`, and the
atomic execution counter stayed at **1** — only the body's `client_message_id`
was sent, yet the replay bound the same run and never ran a second `Execute`.

## Conclusion

The body field `client_message_id` alone deduplicates a repeated POST; the
`Idempotency-Key` header is an optional cross-check, not the mechanism. T4's
"contract does not guarantee" premise is **false** — no production-code fix is
required.
