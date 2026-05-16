# Bugfix Requirements Document

## Introduction

移动端 onboarding 注册流程存在两个关联缺陷：(1) 邀请码输入使用浏览器 `prompt()` 弹窗，在移动端体验差且可被跳过，导致需要邀请码时用户可以绕过直接注册；(2) 移动端手机号未正确传递或格式不规范时，飞书自动注册（`AddToFeishuOrg`）在后台静默失败，用户虽然注册成功但未被加入飞书组织，仅收到飞书注册邮件而非组织邀请。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN 移动端 bootstrap 页面探测到 hub 需要邀请码（`invitation_code_required` 为 true）THEN 系统使用浏览器原生 `prompt()` 弹窗提示输入邀请码，该弹窗在移动端体验差，且用户点击取消后 `prompt()` 返回空字符串，导致注册请求发送时邀请码为空

1.2 WHEN 移动端用户取消 `prompt()` 弹窗或输入为空 THEN 系统抛出 "Invitation code is required" 错误，但错误仅显示为文本状态信息，用户无法重新输入邀请码，需要刷新页面重新操作

1.3 WHEN 移动端用户输入手机号但未包含国际区号前缀（如输入 "13800138000" 而非 "+8613800138000"）THEN 系统将原始手机号直接传递给 `enroll/start` API，飞书 `AddToFeishuOrg` 中的 `normalizeChinaMobile` 虽然会补充 +86 前缀，但移动端 bootstrap 页面没有国际区号选择器，用户可能输入非中国号码而缺少正确前缀

1.4 WHEN 飞书自动注册（`AddToFeishuOrg`）因手机号为空或格式无效而失败 THEN 系统仅在后台日志记录错误（`log.Printf`），不向用户返回任何飞书注册失败的提示，用户认为注册成功但实际未被加入飞书组织

1.5 WHEN 移动端用户未填写手机号直接注册 THEN 系统允许注册成功，但飞书中国区 API 要求 mobile 字段必填（错误码 41010），导致飞书自动注册静默失败

### Expected Behavior (Correct)

2.1 WHEN 移动端 bootstrap 页面探测到 hub 需要邀请码 THEN 系统 SHALL 在注册表单中显示一个专用的邀请码输入框（类似桌面端 OnboardingWizard 的实现），而非使用浏览器 `prompt()` 弹窗

2.2 WHEN 移动端用户提交注册时邀请码为空或无效 THEN 系统 SHALL 在邀请码输入框下方显示明确的错误提示，并允许用户直接修改邀请码后重新提交，无需刷新页面

2.3 WHEN 移动端用户输入手机号 THEN 系统 SHALL 提供国际区号选择器（至少包含 +86），并在提交注册时将区号与手机号拼接为完整格式（如 "+8613800138000"）传递给 API

2.4 WHEN 飞书自动注册失败 THEN 系统 SHALL 将飞书注册结果（成功或失败）包含在注册响应中返回给客户端，移动端页面 SHALL 显示飞书邀请状态提示（如 "飞书组织邀请失败，请联系管理员"）

2.5 WHEN 移动端用户未填写手机号且 hub 启用了飞书自动注册 THEN 系统 SHALL 在注册表单中提示手机号为飞书邀请所必需，或在注册响应中明确告知飞书邀请因缺少手机号而跳过

### Unchanged Behavior (Regression Prevention)

3.1 WHEN 桌面端用户通过 OnboardingWizard 注册 THEN 系统 SHALL CONTINUE TO 使用现有的邀请码输入框和手机号区号选择器正常工作

3.2 WHEN 用户通过 Hub Center 发现多个 hub 并选择连接 THEN 系统 SHALL CONTINUE TO 正常跳转到对应 hub 的 PWA 页面

3.3 WHEN 用户注册时 hub 不需要邀请码（`invitation_code_required` 为 false）THEN 系统 SHALL CONTINUE TO 允许用户直接注册而不显示邀请码输入框

3.4 WHEN 用户已存在于飞书组织中 THEN 系统 SHALL CONTINUE TO 跳过创建用户步骤，仅绑定 open_id

3.5 WHEN 移动端用户已有保存的 hub 连接信息 THEN 系统 SHALL CONTINUE TO 自动重连而不显示注册表单
