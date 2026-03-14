# MaClaw PWA Pocket UI

## 1. Goal
MaClaw PWA is the mobile-facing remote-control UI. It is designed for phone usage, not for full desktop IDE parity.

## 2. Pocket-First Principle

The primary mobile view should focus on:

- current status
- current task
- recent result
- important events
- suggested next action

The mobile UI should not default to showing:

- full transcript
- large file contents
- raw terminal noise

## 3. Login Model

PWA login is:

- enter email
- receive sign-in email from Hub
- confirm via email link
- obtain viewer token

SN should not be manually typed on phones.

## 4. Main Pages

### Login
- email input
- send sign-in link

### Machine List
- machine name
- platform
- online state
- active sessions

### Session List
- tool
- title
- status
- current task
- last result
- waiting-for-user signal

### Session Detail
- summary panel
- important events
- preview output
- later control actions

## 5. Session Detail Data

Primary fields:

- `Status`
- `Severity`
- `WaitingForUser`
- `CurrentTask`
- `ProgressSummary`
- `LastResult`
- `SuggestedAction`
- `ImportantFiles`

## 6. Event Display

Phase-one important event types:

- `file.read`
- `file.modified`
- `command.started`
- `session.error`
- `input.required`

## 7. Preview Display

Preview is secondary. It should:

- remain small
- be collapsible
- show only recent lines

## 8. Later Controls

After the read-only phase, the session detail page will add:

- input
- send
- interrupt
- kill

## 9. Reconnect Strategy

PWA should:

- load snapshot first
- then subscribe over WebSocket
- reconnect automatically
- refresh snapshot after reconnect
