# Requirements Document

## Introduction

本功能为 MaClaw AI 助手面板新增输入缓冲队列（Input Buffer Queue）。当 AI 助手正在处理上一条指令（`submitLocked = true`）时，用户可以继续编辑并提交多条待发送指令，这些指令以有序队列形式缓存在输入框上方。每条缓存条目支持文字、文件路径附件和粘贴图片。待上一条指令处理完成后，所有缓存条目按顺序合并为一条消息，一次性发射到智能体。

视觉体验参考 Codex 的输入队列设计——缓冲队列面板位于输入框正上方，紧凑、可交互。

## Glossary

- **Buffer_Queue**：输入缓冲队列，存储用户在 AI 助手忙碌期间提交的待发送指令条目的有序列表
- **Buffer_Entry**：缓冲队列中的单条条目，包含文字内容、可选的文件路径附件列表和可选的粘贴图片列表
- **AI_Assistant_Panel**：MaClaw 桌面应用的 AI 助手面板组件（`AIAssistantPanel.tsx`）
- **Submit_Lock**：提交锁定状态，当 `sending` 或 `cancelPending` 为 true 时激活，阻止消息直接发送到智能体
- **Merge_And_Fire**：合并发射操作，将 Buffer_Queue 中所有 Buffer_Entry 按顺序合并为一条消息并发送到智能体
- **Attachment**：附件，指 Buffer_Entry 中关联的文件路径（包括用户浏览选择的文件和粘贴图片保存的临时文件）
- **Drag_Handle**：拖拽手柄，用于通过拖拽操作调整 Buffer_Entry 在 Buffer_Queue 中的顺序

## Requirements

### Requirement 1: 缓冲条目创建

**User Story:** As a user, I want to submit instructions while the AI assistant is busy, so that I can continue my thought process without waiting.

#### Acceptance Criteria

1. WHILE Submit_Lock is active, WHEN the user presses Enter or clicks the send button, THE AI_Assistant_Panel SHALL create a new Buffer_Entry containing the current input text and append it to the end of the Buffer_Queue.
2. WHILE Submit_Lock is active, WHEN a Buffer_Entry is created, THE AI_Assistant_Panel SHALL clear the input textarea and reset its height to the default single-line state.
3. WHILE Submit_Lock is active, WHEN a Buffer_Entry is created and a file path attachment is selected, THE AI_Assistant_Panel SHALL include the selected file path in the Buffer_Entry and clear the `selectedFilePath` state.
4. IF the user presses Enter or clicks the send button with an empty input text and no file attachment while Submit_Lock is active, THEN THE AI_Assistant_Panel SHALL not create a Buffer_Entry.
5. WHILE Submit_Lock is not active and the Buffer_Queue is empty, WHEN the user presses Enter or clicks the send button, THE AI_Assistant_Panel SHALL send the message directly to the agent using the existing `sendMessage` flow.

### Requirement 2: 缓冲条目内容类型

**User Story:** As a user, I want each buffered instruction to support text, file attachments, and pasted images, so that I can provide rich context in my queued instructions.

#### Acceptance Criteria

1. THE Buffer_Entry SHALL support a text content field of type string.
2. THE Buffer_Entry SHALL support an optional list of file path attachments (strings), including both user-browsed files and paste-saved image temp files.
3. WHEN a Buffer_Entry is displayed in the Buffer_Queue, THE AI_Assistant_Panel SHALL show a text preview (truncated to 80 characters with ellipsis if longer) and compact attachment indicators: pasted images as small thumbnails (24×24px), other files as a file-type icon based on extension (e.g., Word icon for .docx, PDF icon for .pdf, code icon for .py/.js); unrecognized extensions SHALL use a unified generic file icon ().
4. THE Buffer_Entry SHALL store image attachments as file paths (not base64), with an associated thumbnail data URL for display purposes only.
5. WHEN the user enters edit mode for a Buffer_Entry, THE AI_Assistant_Panel SHALL display each attachment indicator (thumbnail or file-type icon) with a delete button (✕) to remove individual attachments, keeping the layout compact.
6. WHEN the user hovers the mouse over any file attachment indicator (file-type icon or image thumbnail), THE AI_Assistant_Panel SHALL display a tooltip showing the full absolute file path and file name of that attachment.

### Requirement 12: 输入框粘贴图片

**User Story:** As a user, I want to paste images from the clipboard into the input area, so that I can quickly share screenshots or copied images with the AI assistant.

#### Acceptance Criteria

1. WHEN the user pastes clipboard content containing image data (via Ctrl+V / Cmd+V) into the input textarea, THE AI_Assistant_Panel SHALL intercept the paste event and extract the image blob.
2. WHEN an image blob is extracted from a paste event, THE AI_Assistant_Panel SHALL call a backend Wails binding (`SavePastedImage`) to save the image as a temporary file and receive the saved file path in return.
3. WHEN the backend returns the saved temp file path, THE AI_Assistant_Panel SHALL add the file path to the current attachment list (either the pending input's attachment or the current Buffer_Entry being edited).
4. WHEN an image is pasted and saved, THE AI_Assistant_Panel SHALL generate a thumbnail preview (from the original blob data URL) and display it inline in the input area below the textarea.
5. THE AI_Assistant_Panel SHALL support pasting multiple images sequentially; each paste SHALL add a new attachment to the current attachment list.
6. WHEN a pasted image thumbnail is displayed, THE AI_Assistant_Panel SHALL provide a remove button (✕) to delete the attachment from the list and remove the thumbnail from the display.
7. WHEN Merge_And_Fire or direct send executes, THE AI_Assistant_Panel SHALL include all pasted image file paths in the outgoing message using the existing `buildOutgoingMessage` file path format, identical to user-browsed file attachments.
8. THE backend `SavePastedImage` binding SHALL save the image to a temporary directory (e.g., `os.TempDir()/maclaw-paste/`), using a timestamped filename with the appropriate extension (png/jpg), and return the absolute file path.
9. IF the paste event does not contain image data, THEN THE AI_Assistant_Panel SHALL allow the default paste behavior (text paste) to proceed unmodified.

### Requirement 3: 缓冲队列显示

**User Story:** As a user, I want to see my queued instructions above the input area, so that I know what will be sent when the AI finishes.

#### Acceptance Criteria

1. WHILE the Buffer_Queue contains one or more Buffer_Entry items, THE AI_Assistant_Panel SHALL display the Buffer_Queue panel directly above the input textarea.
2. WHILE the Buffer_Queue is empty, THE AI_Assistant_Panel SHALL not render the Buffer_Queue panel.
3. THE AI_Assistant_Panel SHALL display each Buffer_Entry in the Buffer_Queue in submission order (first submitted at the top).
4. THE AI_Assistant_Panel SHALL display a header in the Buffer_Queue panel showing the count of queued entries, localized as "N 条待发送" (zh-Hans) / "N 條待發送" (zh-Hant) / "N queued" (en).
5. THE AI_Assistant_Panel SHALL apply the current theme (light or dark) to the Buffer_Queue panel, consistent with the existing panel styling.

### Requirement 4: 缓冲条目编辑

**User Story:** As a user, I want to edit my queued instructions before they are sent, so that I can correct or refine my thoughts.

#### Acceptance Criteria

1. WHEN the user clicks the edit button on a Buffer_Entry, THE AI_Assistant_Panel SHALL enter inline edit mode for that entry, displaying an editable textarea pre-filled with the entry's text content.
2. WHILE in inline edit mode for a Buffer_Entry, WHEN the user presses Enter (without Shift) or clicks the confirm button, THE AI_Assistant_Panel SHALL save the updated text to the Buffer_Entry and exit inline edit mode.
3. WHILE in inline edit mode for a Buffer_Entry, WHEN the user presses Escape or clicks the cancel button, THE AI_Assistant_Panel SHALL discard changes and exit inline edit mode.
4. IF the user saves a Buffer_Entry with empty text and no attachments, THEN THE AI_Assistant_Panel SHALL remove that Buffer_Entry from the Buffer_Queue.

### Requirement 5: 缓冲条目删除

**User Story:** As a user, I want to remove queued instructions I no longer need, so that only relevant instructions are sent.

#### Acceptance Criteria

1. WHEN the user clicks the delete button on a Buffer_Entry, THE AI_Assistant_Panel SHALL remove that Buffer_Entry from the Buffer_Queue.
2. WHEN the last Buffer_Entry is removed from the Buffer_Queue, THE AI_Assistant_Panel SHALL hide the Buffer_Queue panel.

### Requirement 6: 缓冲条目拖拽排序

**User Story:** As a user, I want to reorder my queued instructions by dragging, so that I can control the sequence in which they are sent.

#### Acceptance Criteria

1. THE AI_Assistant_Panel SHALL display a Drag_Handle on each Buffer_Entry in the Buffer_Queue.
2. WHEN the user drags a Buffer_Entry via its Drag_Handle and drops it at a new position, THE AI_Assistant_Panel SHALL reorder the Buffer_Queue to reflect the new position.
3. WHILE a Buffer_Entry is being dragged, THE AI_Assistant_Panel SHALL display a visual indicator (drop zone highlight or insertion line) showing where the entry will be placed.
4. THE AI_Assistant_Panel SHALL implement drag-and-drop using pointer events (not HTML5 drag API) to ensure consistent behavior across platforms in the Wails WebView.

### Requirement 7: 合并发射

**User Story:** As a user, I want all my queued instructions to be sent as a single combined message when the AI finishes, so that the AI receives my complete context at once.

#### Acceptance Criteria

1. WHEN Submit_Lock transitions from active to inactive and the Buffer_Queue contains one or more Buffer_Entry items, THE AI_Assistant_Panel SHALL execute the Merge_And_Fire operation.
2. WHEN Merge_And_Fire executes, THE AI_Assistant_Panel SHALL concatenate all Buffer_Entry text contents in queue order, separated by a newline delimiter (`\n\n---\n\n`).
1. WHEN Merge_And_Fire executes and Buffer_Entry items contain file path attachments, THE AI_Assistant_Panel SHALL aggregate all file paths (including pasted image temp file paths) and append them to the merged message using the existing `buildOutgoingMessage` format.
4. WHEN Merge_And_Fire executes and Buffer_Entry items contain pasted image attachments, THE AI_Assistant_Panel SHALL treat them identically to file path attachments (since pasted images are already saved as temp files with file paths).
5. WHEN Merge_And_Fire completes, THE AI_Assistant_Panel SHALL clear the Buffer_Queue and hide the Buffer_Queue panel.
6. WHEN Merge_And_Fire executes, THE AI_Assistant_Panel SHALL record each Buffer_Entry's text in the submitted prompts history via `recordSubmittedPrompt`.

### Requirement 8: 输入框交互兼容

**User Story:** As a user, I want the input area to behave naturally whether or not the buffer queue is active, so that my typing experience is not disrupted.

#### Acceptance Criteria

1. WHILE Submit_Lock is active, THE AI_Assistant_Panel SHALL keep the input textarea editable and focusable (existing behavior preserved).
2. WHILE Submit_Lock is active, THE AI_Assistant_Panel SHALL update the placeholder text to indicate buffering mode, localized as "输入后按回车缓存..." (zh-Hans) / "輸入後按 Enter 緩存..." (zh-Hant) / "Press Enter to queue..." (en).
3. WHEN a Buffer_Entry is created, THE AI_Assistant_Panel SHALL return focus to the input textarea.
4. THE AI_Assistant_Panel SHALL preserve the existing history recall (up/down arrow) behavior when the Buffer_Queue is empty or when the input textarea is focused.
5. WHILE Submit_Lock is active, THE AI_Assistant_Panel SHALL continue to support the existing file browse action (`browseFile`) for attaching files to the next Buffer_Entry.

### Requirement 9: 缓冲队列持久化

**User Story:** As a user, I want my queued instructions to survive accidental panel close or app restart, so that I do not lose my prepared instructions.

#### Acceptance Criteria

1. WHEN a Buffer_Entry is added, edited, removed, or reordered, THE AI_Assistant_Panel SHALL persist the current Buffer_Queue state to `localStorage` under a dedicated key.
2. WHEN the AI_Assistant_Panel mounts and Submit_Lock is active, THE AI_Assistant_Panel SHALL restore the Buffer_Queue from `localStorage`.
3. WHEN the AI_Assistant_Panel mounts and Submit_Lock is not active and a persisted Buffer_Queue exists, THE AI_Assistant_Panel SHALL execute Merge_And_Fire for the restored entries (the agent finished while the panel was closed).
4. IF `localStorage` is unavailable or the persisted data is corrupted, THEN THE AI_Assistant_Panel SHALL start with an empty Buffer_Queue and log a warning to the console.

### Requirement 10: 本地化支持

**User Story:** As a user, I want all buffer queue UI text to be displayed in my preferred language, so that the experience is consistent with the rest of the application.

#### Acceptance Criteria

1. THE AI_Assistant_Panel SHALL localize all Buffer_Queue UI strings (header, tooltips, button labels, placeholder text) using the existing `localizeText(lang, en, zhHans, zhHant)` helper.
2. THE AI_Assistant_Panel SHALL support zh-Hans, zh-Hant, and en locales for all Buffer_Queue related strings.

### Requirement 11: 主题与布局兼容

**User Story:** As a user, I want the buffer queue to look and feel consistent with the existing AI assistant panel, so that the UI is cohesive.

#### Acceptance Criteria

1. THE AI_Assistant_Panel SHALL render the Buffer_Queue panel using the same color tokens and spacing conventions as the existing message area and input area.
2. THE AI_Assistant_Panel SHALL render the Buffer_Queue panel correctly in both inline mode and overlay mode.
3. WHILE the workflow split-pane (WorkflowDocPreview) is active, THE AI_Assistant_Panel SHALL render the Buffer_Queue panel within the chat pane without overlapping the document preview pane.
4. THE AI_Assistant_Panel SHALL ensure the Buffer_Queue panel does not exceed 40% of the visible chat area height; WHEN the queue exceeds this limit, THE AI_Assistant_Panel SHALL make the Buffer_Queue panel scrollable.
