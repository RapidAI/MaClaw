# MaClaw Remote Control System Overview

## 1. Goal
MaClaw remote control supports this core scenario:

- The user runs Claude Code on their computer through MaClaw Desktop.
- The user temporarily leaves the computer or local network.
- The user continues to inspect progress and send commands from a phone.
- The real coding session always stays on the user's own machine.

The first fully supported tool is **Claude Code**.

## 2. Design Principles

### 2.1 Desktop is the session host
MaClaw Desktop is responsible for:

- launching and hosting Claude Code
- managing session lifecycle
- collecting output
- generating compressed session state
- connecting to MaClaw Hub

### 2.2 Hub is the work control plane
MaClaw Hub is responsible for:

- issuing user identity inside the hub
- managing machines and sessions
- storing compressed session state
- serving the PWA
- routing remote commands

Hub does not execute Claude Code itself.

### 2.3 Hub Center is only the entry center
MaClaw Hub Center is responsible for:

- hub registration
- hub discovery
- email-to-entry resolution
- platform-level governance

Hub Center is not an auth server and does not relay live sessions.

### 2.4 Pocket-first
The mobile experience is not a full terminal mirror. The phone should mainly show:

- current status
- current task
- last result
- important events
- suggested next action

### 2.5 Compressed-state first
Hub stores compressed session state rather than raw transcript:

- summary
- important events
- preview output

## 3. System Components

### 3.1 MaClaw Desktop
Desktop session host:

- hosts Claude Code
- manages PTY/ConPTY lifecycle
- produces compressed state
- connects to Hub

### 3.2 MaClaw Hub
Work Hub:

- self-issues SN
- manages users, machines, and sessions
- hosts the PWA
- provides admin backend

### 3.3 MaClaw Hub Center
Official and self-hosted hub directory:

- default official address: `https://hubs.mypapers.top`
- resolves entry by email
- tracks registered hubs
- provides platform governance

### 3.4 MaClaw PWA
Mobile remote-control UI:

- login
- machine list
- session list
- pocket-style session detail

### 3.5 MaClaw Pocket
Lightweight Android/iOS entry app:

- input email
- resolve entry through Hub Center
- open the correct PWA

## 4. Topology

```text
Claude Code
   ^
   | PTY / ConPTY
   v
MaClaw Desktop
   |
   | WSS
   v
MaClaw Hub
   ^
   | HTTPS / WSS
   |
MaClaw PWA

MaClaw Pocket
   |
   | HTTPS
   v
MaClaw Hub Center
```

## 5. Identity Model

### 5.1 No standalone auth server
There is no separate centralized auth service.

### 5.2 Each Hub is its own identity authority
Each Hub manages:

- email
- SN
- machine token
- viewer token

### 5.3 Desktop identity
Desktop uses hub enrollment to get:

- SN
- machine_id
- machine_token

### 5.4 PWA identity
PWA users do not manually type SN.
They:

- enter email
- receive a confirmation email from the Hub
- click the sign-in link
- receive a viewer token

### 5.5 Hub modes
Each Hub supports:

- `open`
- `approval`
- `manual`

## 6. Storage and Deployment

### 6.1 Storage
Hub and Hub Center use SQLite by default with:

- WAL
- read/write split
- repository abstraction

### 6.2 Deployment
Desktop, Hub, and Hub Center are independent deliverables.
Inside this repository, Hub and Hub Center live under:

- [hub](/D:/workprj/aicoder/hub)
- [hubcenter](/D:/workprj/aicoder/hubcenter)

## 7. First Milestone
The first milestone is a working Claude Code remote-session loop:

- Desktop hosts Claude
- Desktop produces summary/event/preview
- Hub receives and stores compressed state
- Hub debug API can inspect current session
