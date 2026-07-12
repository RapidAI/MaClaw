# Maclaw Hub VE Observability Ops

This note documents Hub-side observability for digital employee discovery and
communication.

## Related: adaptive prompt cost metrics

Online GUI machines report compact adaptive system-prompt counters on
`machine.heartbeat` (`adaptive_prompt`). Tenant admins can query:

```text
GET /api/admin/adaptive-prompt/metrics
```

Response fields: `online_machines`, `machines_with_stats`, `totals`
(`light_turns`, `full_turns`, `est_tokens_saved`, `light_tool_denies`,
`light_upgrades`, …). Per-machine detail is on
`GET /api/admin/debug/machines`. See also
`docs/adaptive-prompt-and-shared-loop-ops.md`.

## Endpoint

Tenant admins can query:

```text
GET /api/admin/ve/metrics
```

The response is JSON and has these top-level sections:

- `discoverable`
- `initiate`
- `auth_response`
- `discussion_delivery`
- `runtime_delivery`
- `control_delivery`

The endpoint intentionally avoids tenant, user, machine, and employee labels.
Counters are global process counters to avoid high-cardinality metrics and to
keep the endpoint cheap under load.

## Discoverable

Use `discoverable` to diagnose employee list polling.

Key fields:

- `requests_total`: total `/api/ve/discoverable` requests.
- `not_modified_total`: requests served as HTTP `304`.
- `cache_hit_total` / `cache_miss_total`: server-side short-cache behavior.
- `coalesced_total`: concurrent cache misses coalesced by singleflight.
- `overloaded_total`: requests rejected because build concurrency was full and no stale cache was usable.
- `stale_served_total`: requests served from stale cache during overload.
- `cache_entries`: current in-memory discoverable cache size.
- `build_in_flight`: current discoverable builds.
- `build_in_flight_max`: highest observed concurrent discoverable builds since process start.
- `build_concurrency_limit`: configured build concurrency cap.
- `cache_ttl_seconds` / `stale_ttl_seconds`: fresh and stale cache windows.

Response headers:

- `ETag`: stable hash of the current discoverable payload.
- `Cache-Control: private, max-age=5, must-revalidate`: short client revalidation window.
- `X-VE-Cache: miss`: Hub built a fresh discoverable payload.
- `X-VE-Cache: hit`: Hub served the short server cache.
- `X-VE-Cache: coalesced`: Hub merged this request with another concurrent cache miss.
- `X-VE-Cache: stale`: Hub served stale cache during discovery build pressure.

Tuning:

- `HUB_VE_DISCOVERABLE_CACHE_TTL_SECONDS` controls fresh server cache TTL. Default: `2`.
- `HUB_VE_DISCOVERABLE_STALE_TTL_SECONDS` controls stale cache fallback window. Default: `30`.
- `HUB_VE_DISCOVERABLE_CACHE_MAX_KEYS` controls discoverable cache size. Default: `2048`.
- `HUB_VE_DISCOVERABLE_BUILD_CONCURRENCY` controls concurrent discoverable payload builds. Default: `128`.

Common reads:

- High `requests_total`, high `not_modified_total`: clients are polling often, but ETag is reducing body load.
- High `cache_miss_total`, low `cache_hit_total`: clients may be using many different machine/user keys or polling interval exceeds cache TTL.
- Increasing `coalesced_total`: cache expiry bursts are being merged correctly.
- Increasing `overloaded_total`: Hub discovery build pressure is too high; reduce client polling, increase Hub capacity, or investigate slow presence/runtime lookups.
- Increasing `stale_served_total`: users are still getting a list during pressure, but Hub is relying on stale snapshots.

## Initiate

Use `initiate` for direct conversation creation.

Important fields:

- `created_session_total`: new direct sessions created.
- `reused_session_total`: existing open sessions reused.
- `pending_confirmation_total`: per-request authorization flow started.
- `runtime_offline_total`: runtime-backed employee was not online.
- `access_denied_total`: access policy blocked the requester.
- `not_found_total` / `not_active_total`: bad or inactive target.

Common reads:

- High `pending_confirmation_total`: users are waiting on employee owner approval.
- High `runtime_offline_total`: MacLawSrv runtime presence/reporting is unstable.
- High `access_denied_total`: access policy or group visibility may be too restrictive.

## Auth Response

Use `auth_response` to observe owner approval decisions.

Important fields:

- `allowed_total`, `denied_total`, `blocked_total`: approval outcomes.
- `not_found_total`: expired/missing request ID.
- `already_handled_total`: duplicate or late responses.
- `save_failed_total`: registry/request persistence failure.

## Discussion Delivery

Use `discussion_delivery` for A2A/group message fanout.

Important fields:

- `messages_total`: messages passed to fanout.
- `targets_total`: delivery targets attempted.
- `async_queued_total`: async runtime deliveries accepted.
- `async_queue_failed_total`: async delivery could not be queued.
- `websocket_sent_total`: fallback WebSocket sends succeeded.
- `websocket_failed_total`: fallback WebSocket sends failed.
- `websocket_offline_total`: target machine offline.
- `websocket_buffer_full_total`: target connection existed but its send buffer was full.
- `reply_persist_failed_total`: runtime/local reply could not be persisted.
- `reply_notify_failed_total`: persisted reply could not be pushed to peers.

Common reads:

- High `websocket_buffer_full_total`: clients are connected but not draining fast enough; reduce push rate, inspect client event loop, or scale Hub.
- High `websocket_offline_total`: machines are disconnected or presence is stale.
- High `reply_persist_failed_total`: storage path is unhealthy.

## Runtime Delivery

Use `runtime_delivery` for MacLawSrv runtime calls.

Important fields:

- `accepted_total`: runtime calls admitted by the concurrency limiter.
- `rejected_total`: runtime calls rejected because the limiter was full.
- `in_flight`: current runtime calls.
- `in_flight_max`: highest observed concurrent runtime calls since process start.
- `concurrency_limit`: configured runtime delivery cap.
- `delivery_timeout_sec`: configured runtime HTTP delivery timeout.
- `failed_total`: admitted runtime calls that failed.
- `timeout_total`: runtime calls that timed out.
- `http_status_failed_total`: runtime calls that returned non-2xx HTTP status.
- `empty_reply_total`: runtime calls that completed but did not return assistant content.
- `transport_failed_total`: runtime calls that failed before a usable HTTP response.
- `other_failed_total`: runtime calls that failed for another reason.
- `circuit_open_total`: per-runtime circuit opened after repeated failures.
- `circuit_rejected_total`: calls skipped because circuit was open.
- `circuit_entries`: current circuit table size.
- `circuit_failure_limit`: failures before opening circuit.
- `circuit_failure_window_seconds`: failure accumulation window for opening circuit.
- `circuit_open_seconds`: circuit open duration.

HTTP behavior:

- Runtime limiter full returns `503 VE_RUNTIME_BUSY` with `Retry-After: 1`.
- Runtime circuit open returns `503 VE_RUNTIME_CIRCUIT_OPEN` with `Retry-After` equal to the circuit open window.

Tuning:

- `HUB_VE_RUNTIME_DELIVERY_CONCURRENCY` controls concurrent MacLawSrv runtime calls. Default: `64`.
- `HUB_VE_RUNTIME_DELIVERY_TIMEOUT_SECONDS` controls MacLawSrv runtime HTTP timeout. Default: `corelib.DefaultAgentTimeoutSec`.
- `HUB_VE_RUNTIME_CIRCUIT_FAILURE_LIMIT` controls failures before opening a circuit. Default: `3`.
- `HUB_VE_RUNTIME_CIRCUIT_FAILURE_WINDOW_SECONDS` controls how close failures must be to count toward opening a circuit. Default: `30`.
- `HUB_VE_RUNTIME_CIRCUIT_OPEN_SECONDS` controls how long the circuit stays open. Default: `5`.
- `HUB_VE_RUNTIME_CIRCUIT_MAX_KEYS` controls circuit table size. Default: `4096`.

Common reads:

- `in_flight` near `concurrency_limit`: runtime throughput is saturated.
- `in_flight_max` near `concurrency_limit`: runtime had saturation spikes even if current `in_flight` is low.
- Increasing `rejected_total`: Hub is protecting itself; scale runtime/Hub or reduce fanout.
- Increasing `timeout_total`: runtime is too slow or timeout is too low.
- Increasing `http_status_failed_total`: runtime returned explicit failures; inspect MacLawSrv logs.
- Increasing `empty_reply_total`: runtime completed but returned malformed/empty assistant content.
- Increasing `transport_failed_total`: network, DNS, TLS, or runtime process availability is unstable.
- Increasing `circuit_open_total`: one or more runtimes are repeatedly failing.
- Increasing `circuit_rejected_total`: Hub is avoiding calls to unhealthy runtimes.

## Control Delivery

Use `control_delivery` for VE control events:

- `ve:auth_request`
- `ve:auth_result`
- `ve:list_update`
- admin action/status/list update events

Important fields:

- `sent_total`: control events pushed successfully.
- `failed_total`: control events failed.
- `offline_total`: target machine offline.
- `buffer_full_total`: target connection send buffer full.
- `other_failed_total`: other send failures.

Common reads:

- High `buffer_full_total`: users may not see approval dialogs or list updates promptly even though connected.
- High `offline_total`: control events are being sent to disconnected clients.

## First Checks During "Cannot Connect"

1. Check `discoverable.requests_total`, `cache_hit_total`, and `overloaded_total`.
2. If discovery is healthy, check `initiate.runtime_offline_total` and `access_denied_total`.
3. If initiation is healthy, check `runtime_delivery.rejected_total`, `circuit_open_total`, and `circuit_rejected_total`.
4. If runtime is healthy, check `discussion_delivery.websocket_buffer_full_total` and `control_delivery.buffer_full_total`.
5. If buffer full counters rise, inspect client-side event draining, Hub push rate, and Hub capacity.
