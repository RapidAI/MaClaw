# Design Document: Hermes Skill Self-Improvement

## Overview

This feature ports six key design patterns from the Hermes Agent project into MacLaw's Go codebase, enhancing the skill system with knowledge-type skills, self-improvement loops, execution outcome tracking, enhanced security scanning, memory frozen snapshots, atomic file writes, and post-use skill nudges.

The changes span 7 subsystems across `corelib/`, `gui/`, and `tui/` packages, maintaining GUI/TUI parity and full backward compatibility with existing skills.

### Design Decisions

1. **Knowledge skills as a first-class type**: Rather than bolting knowledge onto the existing executable skill model, we add a `Type` field to `NLSkillEntry` with explicit `executable`/`knowledge` discrimination. This keeps the scanner, runner, and prompt builder cleanly separated by type.

2. **Patch over full rewrite**: The `manage_skill(action=patch)` action uses targeted find-and-replace rather than full file rewrite. This minimizes the risk of the LLM accidentally destroying unrelated parts of a skill definition and produces a clean audit trail.

3. **Frozen snapshot with live recall**: The memory snapshot is frozen at session start for KV cache stability, but proactive recall still queries live storage. This gives us the best of both worlds — stable prompt prefix and fresh entity recall.

4. **Atomic writes via temp+rename**: The standard POSIX/NTFS pattern of writing to a temp file in the same directory then renaming. Cross-device fallback handles edge cases on Windows multi-volume setups.

5. **Nudge system as system messages**: Nudges are injected as invisible system messages rather than user-visible text, keeping the conversation clean while still influencing LLM behavior.

6. **Trust level hierarchy in risk assessment**: Rather than a binary safe/unsafe model, we use a 4-tier trust hierarchy (`builtin` > `trusted` > `agent-created` > `community`) that caps or escalates risk based on provenance.

7. **Shared nudge logic via `corelib/nudge` package**: To maintain GUI/TUI parity, the nudge state machine and cooldown logic live in `corelib/`, with both `gui/` and `tui/` calling the same functions.

## Architecture

```mermaid
graph TB
    subgraph corelib["corelib (shared)"]
        types["types.go<br/>NLSkillEntry + Type field"]
        scanner["skill/scanner.go<br/>KNOWLEDGE.md detection<br/>type: knowledge parsing"]
        risk["security/risk_assessor.go<br/>12 threat categories<br/>Unicode detection<br/>structural checks<br/>trust level hierarchy"]
        fileutil["fileutil/atomic.go<br/>AtomicWriteFile"]
        nudge["nudge/nudge.go<br/>NudgeTracker<br/>cooldown + dedup"]
    end

    subgraph gui["gui (Wails desktop)"]
        prompt["im_system_prompt.go<br/>Memory frozen snapshot<br/>Knowledge skill injection"]
        tools["im_tools_misc.go<br/>manage_skill(action=patch)<br/>manage_skill(action=history)"]
        handler["im_message_handler.go<br/>Outcome tracking<br/>Nudge injection"]
        tooldefs["im_tool_definitions.go<br/>patch/history actions"]
    end

    subgraph tui["tui (terminal)"]
        tuihandler["agent_handler.go<br/>Outcome tracking<br/>Nudge injection"]
        tuitools["agent_tools.go<br/>manage_skill(action=patch)<br/>manage_skill(action=history)"]
    end

    scanner --> types
    risk --> types
    prompt --> scanner
    prompt --> fileutil
    tools --> scanner
    tools --> fileutil
    tools --> nudge
    handler --> nudge
    tuitools --> scanner
    tuitools --> fileutil
    tuitools --> nudge
    tuihandler --> nudge
