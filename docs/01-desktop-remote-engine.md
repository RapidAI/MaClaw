# MaClaw Desktop Remote Engine

## 1. Goal
MaClaw Desktop Remote Engine turns Desktop from a launcher into a session host.

It is responsible for:

- hosting Claude Code locally
- managing remote session lifecycle
- collecting PTY output
- generating compressed session state
- syncing that state to MaClaw Hub

## 2. Core Modules

The first implementation is centered around these files:

- [remote_types.go](/D:/workprj/aicoder/remote_types.go)
- [remote_tool_adapter.go](/D:/workprj/aicoder/remote_tool_adapter.go)
- [remote_tool_claude.go](/D:/workprj/aicoder/remote_tool_claude.go)
- [remote_pty_windows.go](/D:/workprj/aicoder/remote_pty_windows.go)
- [remote_session_manager.go](/D:/workprj/aicoder/remote_session_manager.go)
- [remote_preview_buffer.go](/D:/workprj/aicoder/remote_preview_buffer.go)
- [remote_event_extractor.go](/D:/workprj/aicoder/remote_event_extractor.go)
- [remote_summary_reducer.go](/D:/workprj/aicoder/remote_summary_reducer.go)
- [remote_output_pipeline.go](/D:/workprj/aicoder/remote_output_pipeline.go)
- [remote_hub_client.go](/D:/workprj/aicoder/remote_hub_client.go)
- [remote_activation.go](/D:/workprj/aicoder/remote_activation.go)

## 3. Launch Flow

Remote Claude launch flow:

```text
LaunchTool(...)
 -> buildClaudeLaunchSpec(...)
 -> RemoteSessionManager.Create(...)
 -> ClaudeAdapter.BuildCommand(...)
 -> WindowsPTYSession.Start(...)
 -> OutputPipeline
 -> RemoteHubClient
```

The existing tool configuration logic remains in [app.go](/D:/workprj/aicoder/app.go). Remote mode only replaces the execution layer.

## 4. Claude Adapter

Claude remote mode should:

- resolve the Claude executable from the private tool directory
- prefer `claude.exe` on Windows when possible
- reuse existing environment construction
- add minimal runtime PATH support
- support `interrupt` and `kill`

## 5. Windows PTY

The Windows PTY layer is the highest technical risk.

The first implementation only needs to support:

- start process
- read output
- write input
- interrupt
- kill
- observe exit

It does not need to become a full terminal framework in phase one.

## 6. Output Pipeline

Desktop converts raw Claude output into:

- `summary`
- `important_event`
- `preview_delta`

The first rules are intentionally simple and stable:

- file read
- file modified
- command started
- input required
- error detected

## 7. Hub Sync

Desktop sends these messages to Hub:

- `auth.machine`
- `machine.hello`
- `machine.heartbeat`
- `session.created`
- `session.summary`
- `session.important_event`
- `session.preview_delta`
- `session.closed`

## 8. Current Phase Boundary

Phase one includes:

- Claude-only support
- Windows-only PTY
- one-way Desktop-to-Hub sync

Phase one excludes:

- full mobile control loop
- multi-tool support
- advanced approval semantics
- transcript archival
