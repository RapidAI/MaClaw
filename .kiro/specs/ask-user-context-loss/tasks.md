# Implementation Plan

- [x] 1. Fix ask_user return path to save conversation history
  - In `gui/im_message_handler.go`, located the `ParseAskUserResult(result)` block in `runAgentLoop`
  - Before `return resp`, appended the tool result to `conversation` and `history`
  - Called `h.saveConversationHistoryTimed(userID, history, nil)` to persist the full history
  - Verified that the assistant message (with tool_calls) was already appended to history earlier in the iteration (line ~4296)
  - _Requirements: 2.1_

- [x] 2. Add pending ask_user state tracking
  - In `gui/im_message_handler.go`, added `pendingAskUser sync.Map` field to `IMMessageHandler`
  - Defined `pendingAskUserState` struct with Question, Options, InputType, Timestamp fields
  - In the ask_user return path (after saving history), stored the pending state: `h.pendingAskUser.Store(userID, &pendingAskUserState{...})`
  - _Requirements: 2.2_

- [x] 3. Consume pending state and inject context on next message
  - In `handleIMMessageWithLoop`, before the topic detection block, added check for pending ask_user state: `h.pendingAskUser.LoadAndDelete(msg.UserID)`
  - If found and not expired (< 30 minutes), built `askUserContext` string with the question and user's answer
  - Added `askUserContext == ""` condition to topic detection to skip when responding to ask_user
  - After system prompt construction (after workflow phase prompt injection), appended `askUserContext` to the system prompt
  - _Requirements: 2.3, 2.4, 2.5_

- [x] 4. Clean up pending state on conversation reset
  - In `handleExitCommand`, added `h.pendingAskUser.Delete(userID)`
  - In `cancelWorkflowForUser`, added `h.pendingAskUser.Delete(userID)`
  - In `StartNewTask` decision path, added `h.pendingAskUser.Delete(msg.UserID)`
  - In `shouldAutoClearIncompleteTaskContext` path, added `h.pendingAskUser.Delete(msg.UserID)`
  - In topic detection's auto-clear path, added `h.pendingAskUser.Delete(msg.UserID)`
  - _Requirements: 2.5, 3.3_

- [x] 5. Verify no regressions
  - `go vet ./gui/...` passes cleanly
  - Existing GUI tests pass (TestLLMStream, TestAutoUpload, TestTagGenerator, etc.)
  - Workflow engine tests pass (`go test ./corelib/workflow/`)
  - Pre-existing TestSpecWorkflowProperty1 failure is unrelated (GBK encoding issue in test environment)
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_
