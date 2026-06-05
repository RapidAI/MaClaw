# AI Assistant LLM Concurrency Scheduling

## Problem

AI assistant tabs are independent agent instances, but they share one outbound LLM provider. If every agent, memory task, and background classifier can call the provider directly, independent tabs still interfere at the LLM exit. Symptoms include:

- Main tab and recent-task tabs appear to affect each other.
- A tab stays on thinking while another tab is producing tokens or running post-turn work.
- Background memory/simple LLM work continues after visible answers and can trigger 429/503 bursts.
- Provider pressure is invisible to the UI and logs until users see long waits.

The core rule: open tab count is not LLM concurrency. Tabs are user workspaces; LLM concurrency is a bounded shared resource.

## Resource Model

```text
open tabs            UI/session capacity, mostly idle
active agent loops   foreground work capacity
foreground LLM       provider request capacity for user-visible work
background LLM       low-priority maintenance capacity
skill/tool runs      local/external execution capacity
```

Default policy:

```text
max_open_tabs          soft UI limit only
max_active_agents      independent per-tab loops, bounded separately when needed
max_foreground_llm     2 when healthy, 1 when degraded
max_background_llm     1 when healthy, 0 when foreground/degraded
per_tab_queue          same tab serial, different tabs concurrent
```

## Scheduling Rules

1. Same tab remains serial so conversation state cannot reorder.
2. Different tabs can run concurrently, but all LLM calls pass through one scheduler.
3. Foreground LLM calls are user-visible and have priority.
4. Background LLM calls are cancellable and wait while foreground work is active or provider is degraded.
5. A 429/503/overloaded provider response trips degraded mode.
6. Degraded mode reduces foreground concurrency to 1 and background concurrency to 0 for a cooldown window.
7. Background priority must be explicit from request trace. An unowned `simple_llm` is treated as foreground so a foreground request cannot self-block during pre-loop classification.
8. Foreground pre-loop classifiers (`task-intent-classifier`, `gate-intent`, confirmation/task understanding) must carry the tab/session owner so main-agent and recent-task-agent timelines remain attributable before the loop ID exists.
9. IM channels that enter `IMMessageHandler` are foreground agent loops when they are handling user-visible work. Their owner is the channel/session user ID, so Weixin/QQ/Telegram/Lansenger/TUI routing remains attributable.
10. Scheduled/system IM work is not foreground. It uses `caller=background_agent_loop`, keeps foreground work counters unchanged, and waits behind foreground LLM demand.
11. Digital employee local AI is also a foreground agent loop. It must use `owner=digital-employee:<session_id>`, `request_id=<message_id>`, and `loop=ve-agent:<session_id>` before calling `agent.RunLoop`.
12. Group discussion helper LLM calls are not anonymous `simple_llm`; they must carry `owner=group-discussion:<discussion_id>` or `<session_id>` so queue waits and pressure are explainable.
13. Logs must show queue wait, active counts, mode, caller, owner, request, loop, and iteration.

## Failure Handling

Provider pressure markers:

```text
HTTP 429
HTTP 503
rate_limit / too many requests
overloaded / service unavailable
gateway timeout / bad gateway
```

On pressure:

```text
healthy -> degraded
foreground_llm: 2 -> 1
background_llm: 1 -> 0
cooldown: 60s by default
agent retry: existing adaptive retry keeps exponential backoff
UI progress: existing retry progress reports wait instead of silent thinking
```

Recovery:

```text
after cooldown expires, scheduler returns to healthy limits
new pressure extends cooldown
background work resumes only when no foreground agent is active
```

## Implementation Plan

1. Add GUI-level LLM scheduler used by both streaming and simple LLM helpers.
2. Classify request priority from `llm.RequestTrace`.
3. Wrap `doLLMRequestStream` and `doSimpleLLMRequest` with acquire/release.
4. Observe errors and trigger degraded mode for provider pressure.
5. Keep memory/post-conversation QoS so background work waits for foreground idle.
6. Route digital employee `agent.RunLoop` through the same scheduler via `LLMRequestContext` and foreground QoS.
7. Add owner traces for group discussion contribution/summary LLM calls.
8. Make background agent loops skip foreground QoS and trace as `background_agent_loop`.
9. Add focused tests for foreground limits, degraded limits, background blocking, and owner trace propagation.

## Expected Logs

```text
[llm-scheduler] enqueue priority=foreground caller="agent_loop" owner="desktop-user" active_fg=2 limit_fg=2 mode=healthy
[llm-scheduler] acquired priority=foreground caller="agent_loop" owner="desktop-user" waited=812ms active_fg=2 limit_fg=2 mode=healthy
[llm-scheduler] pressure caller="agent_loop" status=degraded cooldown=1m err="HTTP 503 ..."
[llm-scheduler] released priority=foreground caller="agent_loop" active_fg=1 active_bg=0 mode=degraded
```
