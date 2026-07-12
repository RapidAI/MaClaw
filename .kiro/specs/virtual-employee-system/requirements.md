# Requirements Document

## Title: 虚拟员工系统（Virtual Employee System）

## Introduction

在现有 Hub/HubCenter 注册体系和 A2A 协议基础上，构建虚拟员工（Virtual Employee）系统。该系统允许 maclaw 用户将自己注册为虚拟员工，对外提供 AI 能力服务；其他 maclaw 用户可以在虚拟员工列表中发现并与虚拟员工进行对话或群聊。Hub 后台管理面板提供虚拟员工审批功能，HubCenter 在 Hub 注册时批准虚拟员工配额。通讯基于现有 A2A 协议实现。

群聊的核心模型是"会诊"——人类员工提出问题，多个虚拟员工围绕问题展开讨论、互相补充和质疑，人类员工随时可以补充信息或追问。本质上是虚拟员工团队为人类员工提供协作式智力服务。

## Glossary

- **Virtual_Employee**: 虚拟员工，由 maclaw 用户注册并经 Hub 管理员审批后对外提供 AI 能力服务的实体
- **Hub**: 本地部署的服务节点，管理设备注册、虚拟员工审批和 A2A 消息路由
- **HubCenter**: 中心化的 Hub 注册与发现服务，负责批准 Hub 的虚拟员工配额
- **Maclaw_Client**: maclaw 桌面客户端应用，用户通过该客户端注册虚拟员工或与虚拟员工对话
- **VE_Quota**: HubCenter 批准给某个 Hub 的虚拟员工数量上限
- **VE_Tab**: GUI 前端"最近任务"区域新增的"虚拟员工"标签页，用于展示虚拟员工列表
- **Access_Policy**: 虚拟员工的访问权限策略，包含 public、whitelist、blacklist、per_request 四种模式
- **Authorization_Request**: 当虚拟员工设置为 per_request 模式时，其他用户访问该虚拟员工时产生的授权请求
- **AI_Assistant_Panel**: GUI 中的 AI 助手面板，当前为单一对话区域，需改造为 Tab 式多对话
- **A2A_Protocol**: Agent-to-Agent 协议，已实现的跨 maclaw 实例通讯协议，包含 Session、Message、GroupDiscussion 等类型
- **Group_Chat**: 群聊功能，在虚拟员工对话 Tab 中添加多个虚拟员工形成群组讨论，参与者上限由 Hub 管理员配置（最大 10，默认 5）
- **Hub_Private_Key**: Hub 的私钥，用于加密存储敏感数据（如 VE_Quota 信息）

## Requirements

### Requirement 1: HubCenter 虚拟员工配额批准

**User Story:** As a Hub 管理员, I want HubCenter 在 Hub 注册时批准虚拟员工配额, so that 每个 Hub 的虚拟员工数量受到中心化管控。

#### Acceptance Criteria

1. WHEN a Hub registers with HubCenter via the enrollment flow, THE HubCenter SHALL include a `ve_quota` field in the enrollment response indicating the maximum number of virtual employees approved for that Hub, where `ve_quota` is an integer in the range 0 to 10,000 inclusive.
2. IF the enrollment response from HubCenter does not contain a `ve_quota` field or contains a value outside the valid range (0–10,000), THEN THE Hub SHALL treat the quota as zero and reject all virtual employee registration requests until a valid quota is received.
3. WHEN the Hub receives the enrollment response containing a valid `ve_quota`, THE Hub SHALL encrypt the quota value using the Hub_Private_Key and persist it to local storage.
4. WHEN the Hub_Private_Key is used to encrypt VE_Quota data, THE Hub SHALL use AES-256-GCM encryption with the private key derived material as the encryption key.
5. IF the encrypted VE_Quota data cannot be decrypted (key mismatch or data corruption), THEN THE Hub SHALL treat the quota as zero, reject all new virtual employee registration requests, and log an error indicating decryption failure.
6. IF the current number of virtual employees in active status equals or exceeds VE_Quota, THEN THE Hub SHALL reject new virtual employee registration requests with a "quota_exceeded" error code within 2 seconds of receiving the request.
7. WHEN the HubCenter administrator modifies the VE_Quota for a Hub, THE HubCenter SHALL push the updated quota to the Hub via the existing Hub-HubCenter communication channel.
8. WHEN the Hub receives an updated VE_Quota from HubCenter, THE Hub SHALL re-encrypt and persist the new quota value, replacing the previous value, within 5 seconds of receipt.
9. IF the Hub fails to receive or decrypt an updated VE_Quota from HubCenter (network failure or decryption error), THEN THE Hub SHALL continue enforcing the previously persisted quota value and retry receiving the update at intervals of 60 seconds for a maximum of 5 attempts.
10. IF the updated VE_Quota is lower than the current active virtual employee count, THEN THE Hub SHALL NOT forcibly deactivate existing virtual employees, but SHALL reject new registration requests until the active count drops below the new quota.

### Requirement 2: Hub 后台虚拟员工审批管理

**User Story:** As a Hub 管理员, I want 在 Hub 后台管理面板中审批虚拟员工注册申请, so that 我可以控制哪些 maclaw 用户能成为虚拟员工。

#### Acceptance Criteria

1. THE Hub admin panel SHALL provide a "虚拟员工" Tab in the backend management interface, displaying all pending and approved virtual employee registration requests.
2. WHEN a maclaw user submits a virtual employee registration request, THE Hub SHALL store the request with status "pending" and display it in the admin panel's virtual employee Tab.
3. WHEN the Hub administrator approves a pending registration request, THE Hub SHALL update the request status to "active" and notify the requesting maclaw client via the existing WebSocket connection.
4. WHEN the Hub administrator rejects a pending registration request, THE Hub SHALL update the request status to "rejected" and notify the requesting maclaw client with the rejection reason.
5. THE Hub admin panel SHALL display for each virtual employee: name, skill description, access policy, registration time, and current online status.
6. WHEN the Hub administrator disables an active virtual employee, THE Hub SHALL update the status to "disabled", remove the virtual employee from the discoverable list, and notify the owning maclaw client.

### Requirement 3: Maclaw 客户端虚拟员工注册与设置

**User Story:** As a maclaw 用户, I want 在客户端设置中配置我的虚拟员工信息, so that 我可以将自己注册为虚拟员工并控制访问权限。

#### Acceptance Criteria

1. WHEN a maclaw user is eligible to become a virtual employee (Hub has approved quota and user's machine is enrolled), THE Maclaw_Client settings SHALL display a "虚拟员工设置" section.
2. THE virtual employee settings SHALL include: name (default: current maclaw role name), skill description (free text), and access policy selection.
3. WHEN the user configures virtual employee settings and submits registration, THE Maclaw_Client SHALL send a registration request to the Hub containing name, skill description, and access policy.
4. THE Access_Policy selection SHALL provide four options: public (所有人可访问), whitelist (仅白名单中的 maclaw 用户可访问), blacklist (黑名单中的 maclaw 用户不可访问), per_request (每次访问需授权).
5. WHEN the access policy is set to "whitelist", THE settings SHALL provide an interface to add or remove maclaw user identifiers to the whitelist.
6. WHEN the access policy is set to "blacklist", THE settings SHALL provide an interface to add or remove maclaw user identifiers to the blacklist.
7. WHEN the virtual employee registration is approved by the Hub administrator, THE Maclaw_Client SHALL display a confirmation notification and update the settings to show "已激活" status.

### Requirement 4: 虚拟员工列表展示

**User Story:** As a maclaw 用户, I want 在"最近任务"区域看到可用的虚拟员工列表, so that 我可以发现并选择虚拟员工进行对话。

#### Acceptance Criteria

1. THE GUI frontend SHALL add a new "虚拟员工" Tab in the "最近任务" area, appended after the existing tabs.
2. WHEN the VE_Tab is selected, THE System SHALL display a loading indicator within 100 milliseconds, query the Hub for the list of discoverable virtual employees, and render the results in the tab content area within 5 seconds of tab selection.
3. THE virtual employee list SHALL display for each entry: name (truncated to 20 characters with ellipsis if exceeded), skill description (truncated to 50 characters with ellipsis if exceeded), online status indicator (green dot for online, gray dot for offline), and access policy icon.
4. WHEN a virtual employee's access policy is "public", THE list entry SHALL be visible to all maclaw users on the same Hub.
5. WHEN a virtual employee's access policy is "whitelist", THE list entry SHALL only be visible to maclaw users whose identifiers are in the whitelist.
6. WHEN a virtual employee's access policy is "blacklist", THE list entry SHALL be visible to all maclaw users except those whose identifiers are in the blacklist.
7. WHEN a virtual employee's access policy is "per_request", THE list entry SHALL be visible to all maclaw users with a "需授权" badge displayed on the entry.
8. WHEN the Hub pushes updates via WebSocket (new virtual employee registered, status changed, or virtual employee goes offline), THE virtual employee list SHALL refresh automatically within 1 second of receiving the push event, throttled to at most one refresh per 500 milliseconds.
9. IF the Hub WebSocket connection is not established or the query fails or no response is received within 5 seconds, THEN THE System SHALL display an empty state message indicating that the Hub is unavailable, and SHALL retry the query automatically when the WebSocket connection is re-established.
10. WHEN the Hub query returns zero virtual employees matching the user's visibility rules, THE System SHALL display an empty state message indicating no virtual employees are currently available.

### Requirement 5: 每次授权模式的授权请求处理

**User Story:** As a 虚拟员工所有者, I want 在有人请求访问我的虚拟员工时收到通知并决定是否授权, so that 我可以逐次控制谁能使用我的虚拟员工。

#### Acceptance Criteria

1. WHEN a maclaw user attempts to initiate a conversation with a per_request virtual employee, THE Hub SHALL send an Authorization_Request to the virtual employee owner's maclaw client.
2. WHEN an Authorization_Request is received, THE Maclaw_Client SHALL display a flashing indicator icon at the top-right corner of the VE_Tab.
3. WHEN the user clicks the flashing indicator, THE Maclaw_Client SHALL display an authorization dialog showing: requester name, requester machine identifier, and the target virtual employee name.
4. THE authorization dialog SHALL provide "允许" (allow) and "拒绝" (deny) buttons.
5. WHEN the owner clicks "允许", THE Maclaw_Client SHALL send an approval response to the Hub, and THE Hub SHALL establish the A2A session between the requester and the virtual employee.
6. WHEN the owner clicks "拒绝", THE Maclaw_Client SHALL send a denial response to the Hub, and THE Hub SHALL notify the requester that access was denied.
7. IF the virtual employee owner does not respond to the Authorization_Request within 60 seconds, THEN THE Hub SHALL notify the requester that the request timed out.

### Requirement 6: AI 助手面板 Tab 化改造

**User Story:** As a maclaw 用户, I want AI 助手面板支持多个对话 Tab, so that 我可以同时与本地 AI 和多个虚拟员工进行独立对话。

#### Acceptance Criteria

1. THE AI_Assistant_Panel SHALL be restructured from a single conversation view to a tabbed interface with tabs displayed horizontally at the top of the panel.
2. THE first tab SHALL be the local AI assistant (current functionality), with a fixed label "AI 助手", and it SHALL always remain in the leftmost position.
3. WHEN a user initiates a conversation with a virtual employee (double-click or right-click menu "对话" on the VE list), THE System SHALL create a new tab in the AI_Assistant_Panel with the tab title set to the virtual employee's name.
4. IF a user initiates a conversation with a virtual employee that already has an open tab, THEN THE System SHALL activate the existing tab for that virtual employee instead of creating a duplicate.
5. EACH virtual employee conversation tab SHALL display a message input field, scrollable conversation history, and streaming response display, matching the layout of the local AI assistant tab.
6. WHILE a virtual employee conversation tab is active, WHEN the user submits a message, THE System SHALL route the message through the A2A_Protocol to the corresponding virtual employee's maclaw instance.
7. IF the A2A_Protocol connection to a virtual employee fails or the virtual employee becomes unreachable, THEN THE System SHALL display an error indication within the affected tab and retain the conversation history and any unsent message text in the input field.
8. THE user SHALL be able to close virtual employee conversation tabs by clicking a close button (×) displayed on the tab; WHEN a tab is closed, THE System SHALL end the A2A session for that virtual employee.
9. THE local AI assistant tab SHALL NOT display a close button and SHALL NOT be closable.
10. WHEN switching between tabs, THE System SHALL preserve each tab's conversation state independently, including message history, scroll position, and any text in the input field.
11. THE AI_Assistant_Panel SHALL support a maximum of 8 simultaneously open virtual employee conversation tabs (9 total including the local AI tab); IF the user attempts to open a 9th virtual employee tab, THEN THE System SHALL display an error message indicating the maximum tab limit has been reached.

### Requirement 7: 虚拟员工对话通讯（A2A 协议）

**User Story:** As a maclaw 用户, I want 与虚拟员工的对话通过 A2A 协议进行, so that 通讯安全可靠且复用现有基础设施。

#### Acceptance Criteria

1. WHEN a user initiates a conversation with a virtual employee, THE Maclaw_Client SHALL create an A2A Session with the virtual employee's agent_id as a participant, and the session creation request SHALL complete within 5 seconds; IF session creation does not complete within 5 seconds, THEN THE Maclaw_Client SHALL display an error message in the conversation tab indicating connection timeout.
2. THE A2A Session SHALL use the existing Hub as the message relay, routing messages between the requester's maclaw instance and the virtual employee's maclaw instance.
3. WHEN the user sends a message in a virtual employee tab, THE Maclaw_Client SHALL construct an A2A Message with kind="statement" and content not exceeding 32,000 Unicode code points, and send it to the Hub for relay; IF the Hub does not acknowledge receipt within 10 seconds, THEN THE Maclaw_Client SHALL display a delivery failure indicator on the message.
4. WHEN the virtual employee's maclaw instance receives an A2A Message, THE virtual employee's local AI agent SHALL begin processing the message within 2 seconds of receipt and generate a response; IF processing does not produce a first response chunk within 60 seconds, THEN THE virtual employee's maclaw instance SHALL send a timeout error message back to the requester.
5. WHEN the virtual employee's AI agent generates a response, THE virtual employee's maclaw instance SHALL send the response as an A2A Message back through the Hub to the requester, with streaming chunks delivered as they become available (each chunk sent within 500 milliseconds of generation).
6. WHEN the requester's maclaw client receives the first streaming chunk of the virtual employee's response, THE client SHALL display the chunk in the corresponding conversation tab within 200 milliseconds of receipt, and SHALL render subsequent chunks incrementally as they arrive.
7. IF the A2A message relay fails due to Hub connection lost, THEN THE System SHALL display an error message in the conversation tab indicating "Hub 连接中断" and SHALL attempt automatic reconnection with exponential backoff (initial delay 2 seconds, maximum delay 30 seconds, maximum 5 attempts).
8. IF the A2A message relay fails due to the virtual employee being offline, THEN THE System SHALL display an error message indicating the virtual employee is unavailable and SHALL not retry message delivery until the virtual employee's online status is restored.
9. WHEN the Hub connection is restored after a temporary disconnection, THE Maclaw_Client SHALL re-establish the A2A Session without requiring user intervention, and SHALL deliver any messages queued during the disconnection period in their original chronological order.

### Requirement 8: 群聊功能（会诊模式——真人提问 + 虚拟员工团队讨论）

**User Story:** As a maclaw 用户, I want 召集多个虚拟员工进行"会诊"式群聊, so that 虚拟员工团队能围绕我的问题展开多方讨论、互相补充质疑，为我提供协作式智力服务。

#### Acceptance Criteria

1. EACH virtual employee conversation tab SHALL display a "+" button for adding additional participants to the conversation.
2. WHEN the user clicks the "+" button, THE System SHALL display a picker showing: (a) "本地 AI 助手" as the first option (the local maclaw instance), and (b) available virtual employees (filtered by access policy) that can be added to the group.
3. WHEN the user selects "本地 AI 助手", THE System SHALL add the local maclaw AI agent as a participant in the group discussion. The local maclaw participates with full tool capabilities (bash, write_file, ssh, etc.) and can execute actions based on the discussion context.
4. WHEN a virtual employee is added to the group, THE System SHALL send a GroupInvitation via the A2A_Protocol to the added virtual employee's maclaw instance.
4. WHEN the added virtual employee's maclaw instance accepts the invitation (based on its auto-accept policy), THE System SHALL add the virtual employee as a participant in the group discussion.
5. THE group chat tab title SHALL update to show the names of all participants (truncated with "..." if exceeding display width).
6. THE group chat tab header SHALL display a participants button (icon with participant count badge). WHEN the user clicks the participants button, THE System SHALL display a participants panel showing all current participants with their name, online status, and role (本地AI/远程虚拟员工). The panel SHALL provide a "移除" (remove) option for each participant (except the user themselves) to remove a participant from the group.
6. WHEN the user sends a message in a group chat, THE Hub SHALL relay the message to all virtual employee participants using GroupDiscussionMessage with scope="current_hub".
7. WHEN a virtual employee participant receives a message (from the user or from another virtual employee), THE virtual employee MAY generate a response. The response SHALL be broadcast to the user and all other participants via the Hub, enabling multi-party discussion where virtual employees can respond to each other's messages, ask follow-up questions, agree, disagree, or build upon each other's answers.
8. EACH message in the group chat SHALL be displayed with the sender's name as a prefix label (user messages labeled with user name, VE messages labeled with VE name).
9. THE System SHALL implement a turn-taking mechanism to prevent message flooding: after a virtual employee sends a response, it SHALL wait at least 3 seconds before sending another message, and the Hub SHALL queue messages if more than 2 virtual employees attempt to respond simultaneously (FIFO delivery).
10. THE System SHALL limit the maximum number of virtual employees in a single group chat to a configurable value (set in the Hub admin panel's virtual employee Tab), with a maximum upper limit of 10 participants and a default value of 5 participants (excluding the initiating user), to manage context length and message volume.
11. WHEN the Hub administrator changes the group chat participant limit in the virtual employee Tab, THE Hub SHALL push the updated limit to all connected maclaw clients.
12. IF a virtual employee in the group goes offline, THEN THE System SHALL display a notification in the group chat and mark the participant as offline; remaining participants SHALL continue the discussion.
13. THE user SHALL be able to send a message at any time during the discussion (no turn-taking restriction for the human user), and user messages SHALL interrupt the current discussion flow and be prioritized for delivery to all participants.

### Requirement 9: 虚拟员工在线状态管理

**User Story:** As a maclaw 用户, I want 看到虚拟员工的实时在线状态, so that 我知道哪些虚拟员工当前可以响应对话。

#### Acceptance Criteria

1. WHEN a maclaw instance with an active virtual employee connects to the Hub via WebSocket, THE Hub SHALL mark the virtual employee as "online" in the discoverable list.
2. WHEN a maclaw instance with an active virtual employee disconnects from the Hub (WebSocket closed or heartbeat timeout), THE Hub SHALL mark the virtual employee as "offline" within 30 seconds. The heartbeat interval SHALL be 15 seconds, and a virtual employee SHALL be considered disconnected after 2 consecutive missed heartbeats.
3. THE VE_Tab list SHALL display a green dot indicator for online virtual employees and a gray dot indicator for offline virtual employees.
4. WHEN a user attempts to initiate a conversation with an offline virtual employee, THE System SHALL display a message "该虚拟员工当前不在线，无法发起对话".
5. WHEN a virtual employee transitions from offline to online or from online to offline, THE Hub SHALL push a status update event to all connected maclaw clients within 5 seconds of the transition, and THE VE_Tab SHALL update the status indicator without requiring manual refresh.
6. IF the same virtual employee is active on multiple maclaw instances, THEN THE Hub SHALL mark the virtual employee as "online" as long as at least one instance maintains an active WebSocket connection, and SHALL mark it as "offline" only after all instances have disconnected.

### Requirement 10: Hub 配额加密存储

**User Story:** As a Hub 管理员, I want 虚拟员工配额信息被安全加密存储, so that 配额数据不会被篡改。

#### Acceptance Criteria

1. WHEN the Hub receives VE_Quota from HubCenter, THE Hub SHALL encrypt the quota data using the Hub_Private_Key and persist the encrypted data to disk within 5 seconds of receipt.
2. THE encrypted quota data SHALL include: quota value, Hub ID, a timestamp (UTC, millisecond precision) of when the quota was granted, and a message authentication code (MAC) computed over the quota value, Hub ID, and timestamp, to enable tamper detection and replay prevention.
3. WHEN the Hub needs to read the VE_Quota, THE Hub SHALL decrypt the stored data using the Hub_Private_Key, verify the MAC is valid, verify the Hub ID matches the current Hub's identity, and verify the timestamp is not older than 24 hours.
4. IF the decrypted Hub ID does not match the current Hub's identity, OR the MAC verification fails, OR the timestamp is older than 24 hours, THEN THE Hub SHALL treat the quota as invalid (zero) and log a security warning indicating the specific failure reason (identity mismatch, integrity failure, or expired timestamp).
5. THE Hub SHALL NOT store the VE_Quota value in plaintext in any configuration file, log file, or API response.
6. IF the encrypted quota file is missing, corrupted, or cannot be decrypted, THEN THE Hub SHALL treat the quota as zero and log a security warning.

### Requirement 11: 对话附件支持（文本/图片/文件）

**User Story:** As a maclaw 用户, I want 在与虚拟员工的对话中发送附件（文本文件、图片等）, so that 虚拟员工能基于我提供的文件内容进行分析和处理。

#### Acceptance Criteria

1. THE VE conversation input area SHALL provide an attachment button () allowing the user to select local files to attach to a message.
2. THE System SHALL support the following attachment types: text files (.txt, .md, .csv, .json, .xml, .yaml, .log, .go, .py, .js, .ts, .html, .css), images (.png, .jpg, .jpeg, .gif, .webp, .bmp), and documents (.pdf, .docx).
3. WHEN the user attaches a text file (≤ 500KB), THE Maclaw_Client SHALL read the file content and include it inline in the A2A Message as a `text_attachment` field (base64 encoded content + filename + mime_type).
4. WHEN the user attaches an image file (≤ 10MB), THE Maclaw_Client SHALL upload the image to the Hub's file relay endpoint and include the resulting file_url in the A2A Message as an `image_attachment` field (file_url + filename + mime_type + dimensions).
5. WHEN the user attaches a document file (≤ 20MB), THE Maclaw_Client SHALL upload the file to the Hub's file relay endpoint and include the resulting file_url in the A2A Message as a `file_attachment` field (file_url + filename + mime_type + size_bytes).
6. WHEN the virtual employee's maclaw instance receives an A2A Message with attachments, THE System SHALL download the attachment content (for url-based attachments) and pass both the message text and attachment content to the local AI Agent for processing.
7. THE VE conversation view SHALL display attached images inline (thumbnail preview with click-to-expand) and text/document attachments as clickable file chips showing filename and size.
8. WHEN the virtual employee's AI Agent generates a response that includes file output (e.g., generated images, documents), THE virtual employee's maclaw instance SHALL upload the file to the Hub's file relay endpoint and include the file_url in the response A2A Message as an attachment.
9. THE Hub file relay endpoint SHALL store uploaded files temporarily (24-hour TTL) and serve them to authorized session participants only (verified by session_id + participant_id).
10. IF an attachment upload fails (network error, size exceeded, unsupported type), THEN THE System SHALL display an error message indicating the specific failure reason and retain the message text in the input field for retry.
11. IN a group chat, WHEN a message with attachments is broadcast to all participants, THE Hub SHALL ensure all participants can access the attached files through the file relay endpoint for the duration of the session.

