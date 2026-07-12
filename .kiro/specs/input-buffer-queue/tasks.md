# Implementation Plan: Input Buffer Queue

## Overview

Implement an input buffer queue for the MaClaw AI Assistant Panel that allows users to continue submitting instructions while the AI assistant is busy (`submitLocked = true`). Queued entries are displayed above the input area, support text + file attachments + pasted images, and are merged into a single message when the assistant becomes idle. Implementation uses TypeScript (React) for the frontend and Go for the backend Wails binding.

## Tasks

- [x] 1. Define data models and create `useBufferQueue` hook
  - [x] 1.1 Create `useBufferQueue.ts` with `BufferEntry`, `AttachmentInfo` interfaces and hook implementation
    - Define `BufferEntry` interface (`id`, `text`, `attachments`, `createdAt`)
    - Define `AttachmentInfo` interface (`filePath`, `thumbnailDataUrl?`, `isImage`, `fileName`, `extension`)
    - Define `UseBufferQueueReturn` interface
    - Implement `addEntry` — validate non-empty input (reject whitespace-only text with no attachments), generate unique ID (`buf-{timestamp}-{counter}`), append to queue
    - Implement `removeEntry` — remove by ID, preserve relative order of remaining entries
    - Implement `updateEntry` — update text and attachments by ID; if both empty, remove the entry
    - Implement `reorderEntry(fromIndex, toIndex)` — move entry from one position to another, maintain queue length and relative order of other entries
    - Implement `mergeAndFire` — concatenate texts with `\n\n---\n\n` delimiter, aggregate all file paths in queue order, return `{ mergedText, allFilePaths }` or null if queue empty
    - Implement `clearQueue`
    - Implement `restoreQueue` — restore from localStorage
    - _Requirements: 1.1, 1.3, 1.4, 2.1, 2.2, 4.2, 4.4, 5.1, 6.2, 7.2, 7.3, 7.4, 7.5_

  - [ ]* 1.2 Write property tests for `useBufferQueue` (Properties 1-12)
    - Create `gui/frontend/src/components/ai/__tests__/useBufferQueue.property.test.ts`
    - Use `fast-check` with Vitest, minimum 100 iterations per property
    - **Property 1: Entry creation preserves content** — for any non-empty text and attachments list, `addEntry` increases queue length by 1 and the new entry contains exact input text and all attachment file paths
    - **Validates: Requirements 1.1, 1.3**
    - **Property 2: Whitespace-only input rejection** — for any whitespace-only string with no attachments, `addEntry` is rejected and queue length remains unchanged
    - **Validates: Requirements 1.4**
    - **Property 3: Text preview truncation** — for any string, preview returns original if ≤80 chars, or first 80 chars + "..." if longer
    - **Validates: Requirements 2.3**
    - **Property 4: Queue ordering invariant** — for N `addEntry` calls, queue contains N entries in insertion order with non-decreasing `createdAt`
    - **Validates: Requirements 3.3, 3.4**
    - **Property 5: Reorder correctness** — for any valid (fromIndex, toIndex), entry moves correctly, queue length unchanged, other entries maintain relative order
    - **Validates: Requirements 6.2**
    - **Property 6: Edit updates entry text** — `updateEntry` with non-empty text updates text while preserving id and position
    - **Validates: Requirements 4.2**
    - **Property 7: Empty edit removes entry** — `updateEntry` with empty text and no attachments removes the entry
    - **Validates: Requirements 4.4**
    - **Property 8: Delete removes entry** — `removeEntry` reduces queue length by 1, removed id no longer in queue, other entries maintain relative order
    - **Validates: Requirements 5.1**
    - **Property 9: Merge-and-fire text concatenation** — for N entries, merged text equals texts joined by `\n\n---\n\n`; single entry has no delimiter
    - **Validates: Requirements 7.2**
    - **Property 10: Merge-and-fire file path aggregation** — all file paths from all entries aggregated in queue order, no duplicates lost, no extras added
    - **Validates: Requirements 7.3, 7.4, 12.7**
    - **Property 11: Merge-and-fire postconditions** — after merge, queue is empty
    - **Validates: Requirements 7.5, 7.6**
    - **Property 12: Persistence round-trip** — after any mutation, serialized queue (excluding `thumbnailDataUrl`) round-trips correctly through localStorage
    - **Validates: Requirements 9.1, 9.2**

  - [ ]* 1.3 Write unit tests for `useBufferQueue`
    - Create `gui/frontend/src/components/ai/__tests__/useBufferQueue.test.ts`
    - Test empty queue initial state
    - Test `addEntry` basic flow (text + attachments)
    - Test `removeEntry` last item makes queue empty
    - Test `mergeAndFire` single entry (no delimiter)
    - Test `mergeAndFire` returns null when queue empty
    - Test localStorage corrupted data recovery (start with empty queue, console.warn)
    - _Requirements: 1.1, 1.4, 5.2, 7.2, 9.4_

- [x] 2. Implement backend `SavePastedImage` Wails binding
  - [x] 2.1 Add `SavePastedImage` method to `gui/app_wails_bindings.go`
    - Validate extension against whitelist (png, jpg, jpeg, gif, webp, bmp)
    - Validate base64 data size ≤ 50MB
    - Create temp directory `os.TempDir()/maclaw-paste/` with permissions 0755
    - Generate timestamped filename: `paste_YYYYMMDD_HHMMSS_<random4>.<ext>`
    - Decode base64 and write to file with permissions 0644
    - Return absolute file path
    - _Requirements: 12.2, 12.8_

  - [ ]* 2.2 Write unit tests for `SavePastedImage`
    - Create tests in `gui/app_wails_bindings_test.go`
    - Test normal save PNG/JPG
    - Test rejection of non-image extensions
    - Test rejection of oversized base64 data
    - Test rejection of invalid base64
    - Test temp directory auto-creation
    - Test filename format (timestamp + random suffix)
    - _Requirements: 12.2, 12.8_

- [x] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Create `BufferQueuePanel` component with display, edit, and delete
  - [x] 4.1 Create `BufferQueuePanel.tsx` with `BufferQueuePanelProps` interface and rendering logic
    - Define `BufferQueuePanelProps` interface (queue, lang, theme, editingEntryId, onEdit, onCancelEdit, onSaveEdit, onDelete, onReorder)
    - Render header with queue count localized: "N 条待发送" (zh-Hans) / "N 條待發送" (zh-Hant) / "N queued" (en)
    - Render each `BufferEntryRow`: drag handle (`⠿`), text preview (truncated 80 chars + `...`), attachment indicators (image thumbnails 24×24px / file-type icons), edit button (✏️), delete button ()
    - Implement inline edit mode: textarea pre-filled with entry text, attachment management area with delete buttons (✕) per attachment, confirm/cancel buttons
    - Enter (without Shift) or confirm button saves edit; Escape or cancel button discards
    - Saving with empty text and no attachments removes the entry
    - Apply max-height 40% of chat area with `overflow-y: auto`
    - Apply current theme (light/dark) using same color tokens as existing panel
    - Show tooltip with full file path on hover over attachment indicators
    - Do not render panel when queue is empty
    - _Requirements: 2.3, 2.5, 2.6, 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4, 5.1, 5.2, 11.1, 11.4_

  - [x] 4.2 Implement file-type icon mapping utility
    - Create `FILE_TYPE_ICONS` record and `getFileTypeIcon(extension)` function
    - Map document extensions (.pdf, .doc, .docx, .xls, .xlsx, .ppt, .pptx, .txt, .md, .csv)
    - Map code extensions (.js, .ts, .jsx, .tsx, .py, .go, .rs, .java, .c, .cpp, .h, .cs, .html, .css, .json, .xml, .yaml, .yml, .toml)
    - Map image extensions (.png, .jpg, .jpeg, .gif, .svg, .webp, .bmp)
    - Map archive extensions (.zip, .tar, .gz, .rar)
    - Map script extensions (.sh, .bat, .ps1)
    - Default icon `` for unrecognized extensions
    - _Requirements: 2.3_

  - [ ]* 4.3 Write component tests for `BufferQueuePanel`
    - Create `gui/frontend/src/components/ai/__tests__/BufferQueuePanel.test.tsx`
    - Test panel renders when queue non-empty
    - Test panel does not render when queue empty
    - Test header count display (zh-Hans, zh-Hant, en)
    - Test drag handle presence on each entry
    - Test edit mode UI toggle (textarea + confirm/cancel)
    - Test delete button click callback
    - Test attachment indicators (image thumbnail / file-type icon)
    - Test theme switching (light/dark)
    - Test max-height 40% scrollable overflow
    - Test file-type icon mapping (known + unknown extensions)
    - _Requirements: 2.3, 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 5.1, 11.1, 11.4_

- [x] 5. Implement drag-and-drop reordering with Pointer Events
  - [x] 5.1 Add drag-and-drop state and handlers to `BufferQueuePanel`
    - Implement `dragState` with `draggingId`, `startY`, `currentY`, `startIndex`
    - `onPointerDown` on drag handle: record start position, `setPointerCapture`
    - `onPointerMove`: calculate offset, determine target insertion position, show insertion line indicator (2px blue line using `headingColor`)
    - `onPointerUp`: call `onReorder(fromIndex, toIndex)`, clear drag state, `releasePointerCapture`
    - Handle `onPointerCancel`: reset drag state without reordering
    - Visual feedback: dragged entry `opacity: 0.5` + `transform: translateY(deltaY)`
    - Use Pointer Events API only (no HTML5 Drag API) for Wails WebView compatibility
    - _Requirements: 6.1, 6.2, 6.3, 6.4_

- [x] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Integrate paste image handling in `AIAssistantPanel`
  - [x] 7.1 Add paste event handler to textarea in `AIAssistantPanel.tsx`
    - Intercept `onPaste` event, check for image data items (`item.type.startsWith('image/')`)
    - Extract image blob, determine extension (png/jpg)
    - Convert blob to base64 via `FileReader`
    - Call `SavePastedImage(base64, ext)` Wails binding
    - On success: create `AttachmentInfo` with `filePath`, `thumbnailDataUrl` (from `URL.createObjectURL(blob)`), `isImage: true`
    - Add to `pendingAttachments` state array
    - Display inline thumbnail previews below textarea with remove button (✕)
    - Support sequential pastes (each adds one attachment)
    - If paste has no image data, allow default text paste behavior
    - Handle errors: toast on `SavePastedImage` failure, console.error on base64 encoding failure
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5, 12.6, 12.9_

  - [ ]* 7.2 Write property test for sequential paste accumulation (Property 13)
    - **Property 13: Sequential paste accumulation** — for N paste events, pending attachment list has length N, no previous attachments lost or duplicated
    - **Validates: Requirements 12.5**

- [x] 8. Modify `handleSend` for queue vs direct send branching
  - [x] 8.1 Update `handleSend` logic in `AIAssistantPanel.tsx`
    - When `submitLocked` is true: validate non-empty input (text or attachments or selectedFilePath), call `addEntry` with collected attachments, clear textarea + reset height, clear `pendingAttachments`, clear `selectedFilePath`, refocus textarea
    - When `submitLocked` is false and queue is non-empty: safety fallback, execute `mergeAndFire`
    - When `submitLocked` is false and queue is empty: existing `sendMessage` flow unchanged
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 8.1, 8.3, 8.5_

  - [x] 8.2 Update placeholder text based on `submitLocked` state
    - When `submitLocked` is true: show localized "Press Enter to queue..." / "输入后按回车缓存..." / "輸入後按 Enter 緩存..."
    - Preserve existing placeholder logic for other states (init, thinking, processing, idle)
    - _Requirements: 8.2_

- [x] 9. Implement merge-and-fire logic with `useEffect` on `submitLocked` transition
  - [x] 9.1 Add `useEffect` to detect `submitLocked` true→false transition
    - Use `useRef` to track previous `submitLocked` value
    - When `prevSubmitLockedRef.current === true && submitLocked === false && queue.length > 0`: execute merge-and-fire
    - Call `mergeAndFire()` to get `{ mergedText, allFilePaths }`
    - Call `sendMessage` with `buildOutgoingMessageMulti(mergedText, allFilePaths)`
    - Record each entry's text via `recordSubmittedPrompt`
    - Clear queue and hide panel
    - On `sendMessage` failure: do not clear queue, preserve entries for retry
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_

- [x] 10. Create `buildOutgoingMessageMulti` function
  - [x] 10.1 Implement `buildOutgoingMessageMulti(text, filePaths)` in `useAIAssistant.ts`
    - Trim text and filter valid file paths
    - If no valid paths: return trimmed text
    - Detect image file paths, compose appropriate instruction text
    - Build file block with `FILE_PATH_PROMPT_PREFIX`, all paths, and instruction text
    - Return combined text + file block
    - _Requirements: 7.3, 7.4, 12.7_

  - [ ]* 10.2 Write unit tests for `buildOutgoingMessageMulti`
    - Test with text only (no file paths)
    - Test with text + multiple file paths
    - Test with image file paths (instruction text includes image-specific guidance)
    - Test with empty text + file paths only
    - Test with empty/whitespace paths filtered out
    - _Requirements: 7.3, 7.4, 12.7_

- [x] 11. Implement localStorage persistence in `useBufferQueue`
  - [x] 11.1 Add persistence logic to `useBufferQueue` hook
    - Persist queue to localStorage key `"ai_assistant_buffer_queue"` after every mutation (add, remove, update, reorder)
    - Exclude `thumbnailDataUrl` fields during serialization to avoid localStorage bloat
    - Implement `restoreQueue`: parse from localStorage on mount, handle corrupted data with `console.warn` and empty queue fallback
    - On mount with `submitLocked` active: restore queue from localStorage
    - On mount with `submitLocked` inactive and persisted queue exists: execute merge-and-fire for restored entries
    - Handle `localStorage` write failures gracefully (console.warn, queue still works in memory)
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

- [x] 12. Wire `BufferQueuePanel` into `AIAssistantPanel` layout
  - [x] 12.1 Integrate `BufferQueuePanel` into `AIAssistantPanel.tsx` render tree
    - Position `BufferQueuePanel` directly above `ai-input-bar` div, below workflow docs bar
    - Pass queue, lang, theme, editing state, and callback props
    - Ensure correct rendering in both inline mode and overlay mode
    - Ensure no overlap with `WorkflowDocPreview` split-pane
    - _Requirements: 3.1, 3.2, 11.2, 11.3_

- [x] 13. Add localization strings for all Buffer Queue UI text
  - [x] 13.1 Add all localized strings using `localizeText(lang, en, zhHans, zhHant)` pattern
    - Queue header: "N queued" / "N 条待发送" / "N 條待發送"
    - Placeholder: "Press Enter to queue..." / "输入后按回车缓存..." / "輸入後按 Enter 緩存..."
    - Edit button tooltip, delete button tooltip, confirm/cancel button labels
    - Drag handle aria-label
    - Image save failure toast message
    - _Requirements: 10.1, 10.2_

- [x] 14. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ]* 15. Write integration tests for `AIAssistantPanel` buffer queue behavior
  - [ ]* 15.1 Write integration tests
    - Create `gui/frontend/src/components/ai/__tests__/AIAssistantPanel.buffer.test.tsx`
    - Test `submitLocked=true` + Enter key creates BufferEntry
    - Test `submitLocked=false` + Enter key sends message normally
    - Test `submitLocked` true→false transition triggers mergeAndFire
    - Test paste image → `SavePastedImage` call → thumbnail display
    - Test non-image paste → default text paste behavior
    - Test placeholder text changes with `submitLocked` state
    - Test panel mount restores queue from localStorage
    - _Requirements: 1.1, 1.5, 7.1, 8.2, 9.2, 12.1, 12.9_

- [x] 16. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- The design uses TypeScript throughout — no language selection needed
- Property-based tests use `fast-check` with Vitest (minimum 100 iterations per property)
- Drag-and-drop uses Pointer Events API (not HTML5 Drag API) for Wails WebView compatibility
- Pasted images are saved as temp files via Go backend; only file paths are stored (not base64)
- `thumbnailDataUrl` is excluded from localStorage serialization to prevent bloat
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
