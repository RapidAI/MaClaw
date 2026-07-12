import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/settings/app_preferences.dart';
import 'app_locale.dart';

/// Lightweight zh/en string table. Chinese UI for zh; English for all other UI locales.
class AppStrings {
  final bool isZh;

  const AppStrings._(this.isZh);

  factory AppStrings.forLanguage(String language) {
    return AppStrings._(isChineseUiLanguage(language));
  }

  factory AppStrings.forLocale(Locale locale) {
    return AppStrings._(locale.languageCode.toLowerCase() == 'zh');
  }

  String get appTitle => isZh ? 'MaClaw Mobile' : 'MaClaw Mobile';

  // —— Tabs ——
  String get tabAssistant => isZh ? 'AI助手' : 'Assistant';
  String get tabTwin => isZh ? '数字分身' : 'My Twin';
  String get tabDocuments => isZh ? '文档' : 'Docs';
  String get tabTasks => isZh ? '后台' : 'Tasks';
  String get tabEmployees => isZh ? '数字员工' : 'Employees';
  String get tabAccount => isZh ? '我的' : 'Me';

  // —— Common ——
  String get ok => isZh ? '知道了' : 'OK';
  String get cancel => isZh ? '取消' : 'Cancel';
  String get retry => isZh ? '重试' : 'Retry';
  String get refresh => isZh ? '刷新' : 'Refresh';
  String get loading => isZh ? '加载中…' : 'Loading…';
  String get save => isZh ? '保存' : 'Save';
  String get share => isZh ? '分享' : 'Share';
  String get settings => isZh ? '设置' : 'Settings';
  String get themeAndLanguage => isZh ? '主题与语言' : 'Theme & language';
  String get themeSystem => isZh ? '系统' : 'System';
  String get themeLight => isZh ? '浅色' : 'Light';
  String get themeDark => isZh ? '深色' : 'Dark';
  String get uiLanguage => isZh ? '界面语言' : 'Language';
  String get languageSystem => isZh ? '跟随系统' : 'System';
  String get languageChinese => '简体中文';
  String get languageEnglish => 'English';
  String get languageHint => isZh
      ? '中文界面显示中文；其它语言显示英文。语音输入语言与此一致。'
      : 'Chinese UI for Chinese; English UI for all other languages. Speech follows this.';

  // —— Login ——
  String get loginTitle => isZh ? '手机号注册/登录' : 'Phone sign-in';
  String get loginAccountVerify => isZh ? '账户验证' : 'Account verification';
  String get loginAccountHint => isZh
      ? '先验证手机号，再输入短信验证码进入工作台。若已收到短信但发送按钮报错，仍可在下方输入验证码。'
      : 'Verify your phone, then enter the SMS code. If you already received the code, you can enter it below even if send status is unclear.';
  String get phoneNumber => isZh ? '手机号' : 'Phone number';
  String get sendCode => isZh ? '发送验证码' : 'Send code';
  String get resendCode => isZh ? '重新发送验证码' : 'Resend code';
  String resendCodeIn(int seconds) =>
      isZh ? '重新发送验证码（$seconds秒）' : 'Resend in ${seconds}s';
  String get verificationCode => isZh ? '验证码' : 'Verification code';
  String nDigitCode(int n) => isZh ? '$n 位验证码' : '$n-digit code';
  String get verifyAndLogin => isZh ? '验证并登录' : 'Verify & sign in';
  String get loginFooter => isZh
      ? '登录后默认进入 AI 助手，可随时切换文档、后台、员工与账户设置。'
      : 'After sign-in you land on the assistant; switch Docs, Tasks, Employees, and Me anytime.';
  String get invalidPhone => isZh
      ? '请输入有效手机号，只支持数字和常见手机号分隔符。'
      : 'Enter a valid phone number (digits and common separators).';
  String get connectingOfficial =>
      isZh ? '正在连接 MaClaw 官方服务并发送验证码…' : 'Connecting and sending code…';
  String get codeSent => isZh ? '验证码已发送，请输入短信验证码。' : 'Code sent. Enter the SMS code.';
  String get codeMayBeSent => isZh
      ? '短信可能已发出，若已收到请直接输入验证码；未收到请稍后重试。'
      : 'SMS may have been sent. Enter the code if you received it, or retry later.';
  String get loginSuccess =>
      isZh ? '登录成功，已接入手机号账户的官方服务 credits。' : 'Signed in successfully.';
  String get verifyingLogin =>
      isZh ? '正在验证手机号并进入 MaClaw Mobile...' : 'Verifying and signing in…';

  // —— Documents ——
  String get documentsTitle => isZh ? '文档' : 'Documents';
  String get documentsSubtitle => isZh
      ? '与电脑端 MaClaw GUI 共享同一 Hub 文稿库。手机侧重查看、导入、AI 处理与分享，正文请用电脑 GUI 或 AI 助手改写。'
      : 'Hub library shared with GUI. On phone: browse, import, AI process, and share — edit body on desktop or via AI assistant.';
  String get documentsLibraryHint => isZh
      ? '本列表来自当前账号的 Hub 文稿（GUI 与 Mobile 同源）。点开只读预览；改写请用 AI 助手或电脑 GUI。可分享到微信等；长任务在「后台」。'
      : 'Hub library shared with GUI. Tap for read-only preview; rewrite via AI assistant or desktop GUI. Share sheet available; long jobs under Tasks.';
  String get hubLibrary =>
      isZh ? 'Hub 文稿库（与 GUI 共享）' : 'Hub library (shared with GUI)';
  String get hubLibraryEmpty => isZh
      ? '暂无文稿。可从电脑端 MaClaw 创建后刷新，或在下方导入/接收系统分享的文件。'
      : 'No documents yet. Refresh after creating on desktop, or import a shared file below.';
  String get hubLibraryUnavailable =>
      isZh ? '文稿库暂不可用' : 'Library unavailable';
  String get hubLibraryLoading =>
      isZh ? '加载 Hub 文稿库…' : 'Loading Hub library…';
  String get continueProcessing => isZh ? '继续处理' : 'Continue';
  String continueProcessingFor(String title) =>
      isZh ? '继续处理：$title' : 'Continue: $title';
  String get summarize => isZh ? '摘要整理' : 'Summarize';
  String get translate => isZh ? '翻译' : 'Translate';
  String get rewrite => isZh ? '改写' : 'Rewrite';
  String get expand => isZh ? '扩写' : 'Expand';
  String get polish => isZh ? '润色' : 'Polish';
  String get formatDoc => isZh ? '整理' : 'Format';
  String get processSummarizeShort => isZh ? '摘要' : 'Summary';
  String get export => isZh ? '导出' : 'Export';
  String get delete => isZh ? '删除' : 'Delete';
  String get close => isZh ? '关闭' : 'Close';
  String get shareToWechat => isZh ? '分享到微信等' : 'Share…';
  String get shareOriginal => isZh ? '分享原件' : 'Share original';
  String get shareText => isZh ? '分享文本' : 'Share text';
  String get copyText => isZh ? '复制文本' : 'Copy text';
  String get importDocument => isZh ? '导入文档' : 'Import';
  String get mobileImport => isZh ? '移动导入' : 'Mobile import';
  String get mobileImportHint => isZh
      ? '从文件、拍照或相册快速提交给官方服务解析，适合截图、纸质通知、现场照片和临时材料。'
      : 'Import from files, camera, or gallery for official parsing — screenshots, notices, and field photos.';
  String get importFromFile => isZh ? '文件导入' : 'File';
  String get importFromCamera => isZh ? '拍照导入' : 'Camera';
  String get importFromGallery => isZh ? '相册导入' : 'Gallery';
  String get refreshLibrary => isZh ? '刷新文稿库' : 'Refresh library';
  String get unnamedDocument => isZh ? '未命名文档' : 'Untitled';
  String get blankDraft => isZh ? '空白草稿' : 'Empty draft';
  String get shareOpened =>
      isZh ? '已打开系统分享，可发送到微信等应用' : 'System share sheet opened';
  String get documentPreview => isZh ? '文档预览（只读）' : 'Document preview (read-only)';
  String get documentPreviewHint => isZh
      ? '手机不适合大段改稿。需要改写请到 AI 助手说明意图，或在电脑端 GUI 编辑。'
      : 'Phone is for review. Rewrite via AI assistant, or edit in desktop GUI.';
  String get documentBodyEmpty => isZh ? '（正文为空）' : '(empty body)';
  String get documentOriginalOnlyBody => isZh
      ? '原件已保存，可分享到微信等。正文提取可能尚未完成。'
      : 'Original file saved. You can share it; text extract may still be pending.';
  String documentOriginalLabel(String name, String sizePart) => isZh
      ? '原件：$name$sizePart'
      : 'Original: $name$sizePart';
  String get operationIncomplete => isZh ? '操作未完成' : 'Action failed';
  String get documentServiceError => isZh
      ? '文档服务请求失败，请刷新后重试。若刚导入请确认 Hub 已保存该文稿。'
      : 'Document service failed. Refresh and retry. If you just imported, confirm Hub saved the draft.';
  String get retryRefresh => isZh ? '重试 / 刷新' : 'Retry / refresh';
  String get processingDocument => isZh ? '正在处理文档…' : 'Processing document…';
  String get aiProcess => isZh ? 'AI 处理' : 'AI process';
  String get aiProcessHint => isZh
      ? '对当前选中文档执行摘要、翻译、改写、扩写、润色或格式整理（由 Hub AI 完成，结果写回文稿库）。'
      : 'Summarize, translate, rewrite, expand, polish, or format the selected document (Hub AI; result writes back to the library).';
  String get deleteDocumentTitle => isZh ? '删除文稿' : 'Delete document';
  String deleteDocumentConfirm(String title) => isZh
      ? '从 Hub 共享库删除「$title」？电脑 GUI 与其它设备也将同步看不到该文稿。'
      : 'Delete “$title” from the Hub library? Desktop GUI will sync the same removal.';
  String deletedDocument(String title) =>
      isZh ? '已删除「$title」' : 'Deleted “$title”';
  String deleteFailed(String msg) =>
      isZh ? '删除失败：$msg' : 'Delete failed: $msg';
  String shareFailed(Object e) => isZh ? '分享失败：$e' : 'Share failed: $e';
  String shareOriginalFailed(Object e) =>
      isZh ? '分享原件失败：$e' : 'Share original failed: $e';
  String get noOriginalToShare =>
      isZh ? '该文稿没有可分享的原件。' : 'No original file to share.';
  String get preparingExport =>
      isZh ? '正在准备导出文件...' : 'Preparing export…';
  String get exportShareSuccess =>
      isZh ? '导出文件已交给系统分享。' : 'Export handed to system share.';
  String get exportShareDismissed => isZh
      ? '已取消分享，文件仍保存在临时导出目录。'
      : 'Share cancelled; file remains in the temp export folder.';
  String exportShareUnavailable(String path) => isZh
      ? '系统分享不可用，文件已保存到 $path'
      : 'System share unavailable; saved to $path';
  String exportShareFailed(Object error) =>
      isZh ? '分享导出文件失败：$error' : 'Failed to share export: $error';
  String get documentTextCopied =>
      isZh ? '文档文本已复制' : 'Document text copied';
  String get documentTextShared =>
      isZh ? '文档文本已发送到系统分享' : 'Document text sent to system share';
  String exportJobStatus(String jobId, String status) =>
      isZh ? '导出任务 $jobId：$status' : 'Export job $jobId: $status';
  String get exportReadyHint => isZh
      ? '文件已生成，可直接调起系统分享；分享前会先下载到本机临时目录。'
      : 'File is ready. Share will download to a temp folder first.';
  String get refreshStatus => isZh ? '刷新状态' : 'Refresh status';
  String get retryExport => isZh ? '重试导出' : 'Retry export';
  String get preparingShare => isZh ? '准备分享' : 'Preparing share';
  String get shareFile => isZh ? '分享文件' : 'Share file';
  String uploadTaskLine(String taskId, String status) =>
      isZh ? '导入任务 $taskId：$status' : 'Import task $taskId: $status';
  String get uploadFailedHint => isZh
      ? '可重试导入，或改用文本、PDF、Word、图片截图等移动端更稳定的格式。'
      : 'Retry import, or use text, PDF, Word, or screenshots for better mobile support.';
  String get uploadInProgressHint => isZh
      ? '这是官方服务长任务，可以先离开页面；完成后会通过通知或回到文档页继续处理。'
      : 'This is a long official job — you can leave; return via notification or Docs when done.';
  String get uploadDoneHint => isZh
      ? '导入已完成。点「关闭」可收起此卡片。'
      : 'Import finished. Tap Close to dismiss this card.';
  String get retryImport => isZh ? '重试导入' : 'Retry import';
  String uploadStatusLabel(String status) {
    return switch (status) {
      'queued' => isZh ? '等待远程解析' : 'Queued for remote parse',
      'in_progress' => isZh ? '远程解析中' : 'Parsing remotely',
      'needs_ocr' => isZh ? '等待 OCR/视觉识别' : 'Waiting for OCR',
      'ready' => isZh ? '已生成草稿' : 'Draft ready',
      'failed' => isZh ? '解析失败' : 'Parse failed',
      _ => status,
    };
  }

  String exportStatusLabel(String status) {
    return switch (status) {
      'queued' => isZh ? '等待生成' : 'Queued',
      'in_progress' => isZh ? '生成中' : 'Generating',
      'ready' => isZh ? '已可分享' : 'Ready to share',
      'failed' => isZh ? '导出失败' : 'Export failed',
      _ => status,
    };
  }

  // —— Tasks ——
  String get tasksTitle => isZh ? '后台' : 'Background tasks';
  String get tasksSubtitle => isZh
      ? '长任务统一查看：文档解析/导出、员工任务等。短操作请回 AI 助手或数字员工页。'
      : 'Long-running jobs: document import/export, employee tasks, and more.';
  String get loadingHubJobs => isZh ? '加载 Hub 任务…' : 'Loading Hub jobs…';
  String get hubJobsLoadFailed =>
      isZh ? 'Hub 任务加载失败' : 'Failed to load Hub jobs';
  String get loadingDocumentTasks =>
      isZh ? '加载文档任务…' : 'Loading document tasks…';
  String get documentTasksLoadFailed =>
      isZh ? '文档任务加载失败' : 'Failed to load document tasks';
  String get loadingEmployeeTasks =>
      isZh ? '加载员工任务…' : 'Loading employee tasks…';
  String get employeeTasksLoadFailed =>
      isZh ? '员工任务加载失败' : 'Failed to load employee tasks';
  String get openDocumentsEditor => isZh ? '打开文档编辑' : 'Open documents';
  String get openDocumentsEditorHint => isZh
      ? '查看草稿、轻编辑、导入导出（二级页面）'
      : 'Preview drafts, import/export (secondary page)';
  String get enter => isZh ? '进入' : 'Open';
  String get openLabel => isZh ? '打开' : 'Open';
  String get details => isZh ? '详情' : 'Details';
  String get employeesShortcutHint =>
      isZh ? '与分身交谈、查看派单' : 'Chat with twins and review assignments';
  String get officialAgentBackend =>
      isZh ? '官方 Agent 后台' : 'Official agent backend';
  String get refreshAgentStatus =>
      isZh ? '刷新 Agent 状态' : 'Refresh agent status';
  String get knowledgeNotReady => isZh ? '知识库：未就绪' : 'Knowledge: not ready';
  String knowledgeSummary(int sources, int cards, String mode) => isZh
      ? '知识库：来源 $sources · 卡片 $cards（$mode）'
      : 'Knowledge: $sources sources · $cards cards ($mode)';
  String get knowledgeLoading => isZh ? '知识库：加载中…' : 'Knowledge: loading…';
  String get knowledgeUnavailable =>
      isZh ? '知识库：不可用' : 'Knowledge: unavailable';
  String get mcpNotProbed =>
      isZh ? 'MCP：未探测（点刷新探测）' : 'MCP: not probed (tap refresh)';
  String mcpSummary(int healthy, int servers, int tools) => isZh
      ? 'MCP：健康 $healthy/$servers · 工具 $tools'
      : 'MCP: healthy $healthy/$servers · tools $tools';
  String get mcpProbing => isZh ? 'MCP：探测中…' : 'MCP: probing…';
  String get mcpProbeFailed => isZh ? 'MCP：探测失败' : 'MCP: probe failed';
  String get skillsUnknown => isZh ? '技能：未知' : 'Skills: unknown';
  String skillsCount(int n) => isZh ? '技能：$n 个' : 'Skills: $n';
  String get skillsLoading => isZh ? '技能：加载中…' : 'Skills: loading…';
  String get skillsUnavailable => isZh ? '技能：不可用' : 'Skills: unavailable';
  String get documentStorage => isZh ? '文档空间' : 'Document storage';
  String get defaultFreeQuota => isZh ? '（默认免费额度）' : ' (default free quota)';
  String get hubJobsTitle => isZh ? 'Hub 任务列表' : 'Hub jobs';
  String get hubJobsUnavailableHint => isZh
      ? '未登录或 Hub 暂不可用。下方仍显示本机缓存的文档/员工任务。'
      : 'Not signed in or Hub unavailable. Local document/employee tasks still show below.';
  String get unifiedJobs => isZh ? '统一任务' : 'Unified jobs';
  String get noHubLongJobs =>
      isZh ? '暂无 Hub 侧长任务' : 'No Hub long-running jobs';
  String hubJobsCountLine(int total, int active, {int? filtered}) {
    final base = isZh
        ? '共 $total 条 · 进行中 $active'
        : '$total total · $active active';
    if (filtered == null) return base;
    return isZh ? '$base · 筛选 $filtered' : '$base · filtered $filtered';
  }

  String get activeOnly => isZh ? '仅进行中' : 'Active only';
  String get includeFinished => isZh ? '含已结束' : 'Include finished';
  String get filterAll => isZh ? '全部' : 'All';
  String get filterAssistant => isZh ? '助手' : 'Assistant';
  String get filterDocument => isZh ? '文档' : 'Docs';
  String get filterEmployee => isZh ? '员工' : 'Employees';
  String get noRecentHubJobs =>
      isZh ? '没有进行中或最近的 Hub 任务' : 'No active or recent Hub jobs';
  String get noRecentHubJobsHint => isZh
      ? '导入/导出、员工派单、SSH 长命令会出现在这里。'
      : 'Import/export, employee tasks, and long SSH commands appear here.';
  String get filterNoResults =>
      isZh ? '当前筛选无结果' : 'No results for this filter';
  String filterNoActive(int n) => isZh
      ? '没有进行中的任务（筛选内进行中 $n）'
      : 'No active jobs (active in filter: $n)';
  String get filterTryOther =>
      isZh ? '试试「全部」或其他类型' : 'Try All or another type';
  String get documentTasks => isZh ? '文档任务' : 'Document tasks';
  String get noActiveDraft => isZh ? '暂无活动草稿' : 'No active draft';
  String currentDraftLine(String title) =>
      isZh ? '当前草稿：$title' : 'Current draft: $title';
  String importTaskLine(String name) => isZh ? '导入 · $name' : 'Import · $name';
  String exportTaskLine(String id) => isZh ? '导出 · $id' : 'Export · $id';
  String get noActiveImportExport =>
      isZh ? '没有进行中的导入/导出' : 'No active import/export';
  String get noActiveImportExportHint => isZh
      ? '从文档页导入文件或导出后，进度会出现在这里。'
      : 'Import or export from Docs — progress shows here.';
  String get digitalEmployeeTasks =>
      isZh ? '数字员工任务' : 'Employee tasks';
  String get noRecentEmployeeTask =>
      isZh ? '暂无最近任务' : 'No recent tasks';
  String get employeesPage => isZh ? '员工页' : 'Employees';
  String recentHistoryCount(int n) =>
      isZh ? '最近 $n 条' : 'Recent $n';

  String jobKindLabel(String kind) {
    return switch (kind.trim().toLowerCase()) {
      'document_upload' => isZh ? '文档导入' : 'Doc import',
      'document_export' => isZh ? '文档导出' : 'Doc export',
      'document_process' => isZh ? '文档处理' : 'Doc process',
      'digital_employee' => isZh ? '数字员工' : 'Employee',
      'ssh_command' => isZh ? 'SSH 命令' : 'SSH command',
      'ssh_file' => isZh ? 'SSH 文件' : 'SSH file',
      'ssh_session' => isZh ? 'SSH 会话' : 'SSH session',
      'assistant' => isZh ? 'AI 助手' : 'Assistant',
      _ => kind.isEmpty ? (isZh ? '任务' : 'Job') : kind,
    };
  }

  String jobStatusLabel(String status) {
    final s = status.trim().toLowerCase();
    return switch (s) {
      'queued' || 'pending' => isZh ? '排队中' : 'Queued',
      'running' || 'processing' => isZh ? '进行中' : 'Running',
      'ready' ||
      'done' ||
      'completed' ||
      'success' =>
        isZh ? '已完成' : 'Done',
      'failed' || 'error' => isZh ? '失败' : 'Failed',
      'cancelled' || 'canceled' => isZh ? '已取消' : 'Cancelled',
      'agent_claimed' => isZh ? '已接管' : 'Claimed',
      'kill_requested' => isZh ? '终止中' : 'Stopping',
      'wait_requested' => isZh ? '等待中' : 'Waiting',
      _ => status.isEmpty ? (isZh ? '未知' : 'Unknown') : status,
    };
  }

  // —— Employees ——
  String get employeesTitle => isZh ? '数字员工' : 'Digital employees';
  String get employeesSubtitle => isZh
      ? '接入远程服务器/电脑上的能力，让手机发起任务、查看结果和请求授权。'
      : 'Run tasks on remote PCs, review results, and request authorization.';
  String get accessPolicyTitle =>
      isZh ? '数字员工访问策略' : 'Employee access policy';
  String get accessPolicyBody => isZh
      ? '手机端只向 MaClaw 官方服务提交任务。远程服务器或电脑上的数字员工会按机器端策略领取任务；私有、按次授权或需要确认的能力仍由远程端控制，手机不会绕过审批或自动执行高风险操作。'
      : 'Mobile only submits tasks to the official Hub. Remote employees claim work under machine policy; private, per-request, or confirm-required capabilities stay under remote control. The phone never bypasses approval or auto-runs high-risk ops.';
  String get accessPolicyActionTitle => isZh ? '权限说明' : 'Permissions';
  String get accessPolicyActionSubtitle => isZh
      ? '私有或按次授权的数字员工会先向拥有者发起确认，手机不会绕过远程电脑策略。'
      : 'Private or per-request employees ask the owner first; mobile never bypasses remote policy.';
  String get viewPolicy => isZh ? '查看策略' : 'View policy';
  String get handoffNoEmployee => isZh
      ? '已收到 AI 助手交接，但当前没有可派单的在线数字员工。可下拉刷新后再试。'
      : 'Assistant handoff received, but no online employee can take tasks. Pull to refresh.';
  String handoffDraftTo(String name) =>
      isZh ? '已从 AI 助手带入任务草稿 → $name' : 'Task draft from assistant → $name';
  String get handoffInProgress => isZh
      ? '正在处理来自 AI 助手的任务交接…'
      : 'Processing assistant handoff…';
  String get employeesOnlineSharedHint => isZh
      ? '只列出在线数字员工（共享池可用）。离线不展示；仍受远程访问策略约束。'
      : 'Online employees only (shared pool available). Offline hidden; remote policy still applies.';
  String get employeesOnlineOwnHint => isZh
      ? '只列出在线数字员工（仅自己的分身）。离线不展示；升级服务卡可查看共享池。'
      : 'Online employees only (your twins). Offline hidden; upgrade plan for shared pool.';
  String get recentEmployeeTasks =>
      isZh ? '最近数字员工任务' : 'Recent employee tasks';
  String get recentTasks => isZh ? '最近任务' : 'Recent tasks';
  String recentTasksLoadFailed(Object e) =>
      isZh ? '最近任务加载失败：$e' : 'Failed to load recent tasks: $e';
  String get taskStatusLoadFailed =>
      isZh ? '任务状态加载失败' : 'Failed to load task status';
  String taskStatusLoadFailedDetail(Object e) =>
      isZh ? '任务状态加载失败：$e' : 'Failed to load task status: $e';
  String statusLine(String status) => isZh ? '状态：$status' : 'Status: $status';
  String taskLine(String prompt) => isZh ? '任务：$prompt' : 'Task: $prompt';
  String claimedByLine(String who) =>
      isZh ? '领取者：$who' : 'Claimed by: $who';
  String noteLine(String msg) => isZh ? '说明：$msg' : 'Note: $msg';
  // copyResult already defined under Assistant (same wording).
  String get shareResultTooltip => isZh ? '分享结果' : 'Share result';
  String get makeDraftFromResult => isZh ? '整理为草稿' : 'Save as draft';
  String get taskResultCopied =>
      isZh ? '任务结果已复制' : 'Task result copied';
  String get taskResultShared =>
      isZh ? '任务结果已发送到系统分享' : 'Task result sent to system share';
  String get employeeTaskResultTitle =>
      isZh ? '数字员工任务结果' : 'Employee task result';
  String draftFromResultFailed(Object e) =>
      isZh ? '整理为草稿失败：$e' : 'Failed to save draft: $e';
  String get draftFromResultOk =>
      isZh ? '已整理为文档草稿' : 'Saved as document draft';
  String get loadingEmployees =>
      isZh ? '正在加载数字员工…' : 'Loading employees…';
  String get employeesLoadFailed =>
      isZh ? '数字员工加载失败' : 'Failed to load employees';
  String get noOnlineEmployees =>
      isZh ? '暂无在线数字员工' : 'No online employees';
  String get noOnlineEmployeesSharedHint => isZh
      ? '仅展示在线员工。请确认电脑端分身已上线，或租户共享池中有在线员工后下拉刷新。'
      : 'Online only. Confirm a desktop twin is online, or shared-pool employees are online, then refresh.';
  String get noOnlineEmployeesOwnHint => isZh
      ? '仅展示在线员工。请在电脑上登录同一账号并启用数字员工，待显示在线后刷新；升级服务卡可查看租户共享池。'
      : 'Online only. Sign in on desktop with the same account, enable employees, then refresh; upgrade for shared pool.';
  String get submitTask => isZh ? '发起任务' : 'Start task';
  String get submittingTask => isZh ? '提交中' : 'Submitting';
  String get analyzeLogOutput => isZh ? '分析日志/输出' : 'Analyze logs';
  String get analyzeLogPrompt => isZh
      ? '请读取并分析远程服务器/电脑最近的后台会话输出和关键日志，重点说明异常、影响范围、排查依据和建议命令。高风险命令只给草案，不要自动执行。'
      : 'Read and analyze recent remote session output and key logs. Cover anomalies, impact, evidence, and suggested commands. High-risk commands as drafts only — do not auto-run.';
  String sendToEmployee(String name) => isZh ? '发给 $name' : 'To $name';
  String get taskTypeLabel => isZh ? '任务类型' : 'Task type';
  String get taskTypeServer => isZh ? '服务器' : 'Server';
  String get taskTypeDesktop => isZh ? '电脑' : 'Desktop';
  String get taskTypeDocument => isZh ? '文档' : 'Docs';
  String get taskTypeCheck => isZh ? '核查' : 'Check';
  String get taskDescription => isZh ? '任务说明' : 'Task details';
  String get highRiskDraftOnly =>
      isZh ? '高风险命令只给草案' : 'High-risk commands as drafts only';
  String get highRiskDraftOnlyHint => isZh
      ? '远程端不要自动执行删除、重启、改权限等高风险操作。'
      : 'Remote must not auto-run delete, reboot, or permission changes.';
  String get taskTemplates => isZh ? '任务模板' : 'Templates';
  String get submitTaskButton => isZh ? '提交任务' : 'Submit task';
  String get online => isZh ? '在线' : 'Online';
  String get offline => isZh ? '离线' : 'Offline';
  String get remoteOwner => isZh ? '远程端拥有者' : 'Remote owner';
  String awaitingAuthorization(String owner) => isZh
      ? '正在等待 $owner 在远程服务器/电脑上确认授权。手机端不会绕过远程策略；确认后可刷新查看结果。'
      : 'Waiting for $owner to approve on the remote machine. Mobile never bypasses policy; refresh after approval.';
  String get chatUnavailable => isZh
      ? '该数字员工当前不可提交任务，请确认远程端在线且运行时可用。'
      : 'This employee cannot take tasks. Confirm remote is online and runtime is available.';
  String get refreshEmployeeStatus =>
      isZh ? '刷新员工状态' : 'Refresh employee status';
  String get addAttachment => isZh ? '添加附件' : 'Add attachment';
  String get voiceInput => isZh ? '语音输入' : 'Voice input';
  String get stopVoiceInput => isZh ? '停止语音输入' : 'Stop voice';
  String get send => isZh ? '发送' : 'Send';
  String get chatHint => isZh ? '描述要处理的事情…' : 'Describe what to do…';
  String get takePhotoUpload => isZh ? '拍照并上传' : 'Take photo';
  String get pickFromGallery => isZh ? '从相册选择图片' : 'Choose from gallery';
  String get pickDocument => isZh ? '选择文档' : 'Choose document';
  String get attachmentPickerFailed => isZh
      ? '无法打开附件选择器，请检查权限后重试。'
      : 'Cannot open attachment picker. Check permissions and retry.';
  String get attachmentSubmitting => isZh
      ? '已选择附件，正在提交 Hub 文档解析。'
      : 'Attachment selected; submitting for Hub document parse.';
  String get attachmentSubmitFailed => isZh
      ? '附件提交失败，请到文档页查看错误并重试。'
      : 'Attachment submit failed. Check Docs and retry.';
  String get attachmentSubmitted => isZh
      ? '附件已提交到 Hub 文档解析。'
      : 'Attachment submitted to Hub document parse.';
  String get attachmentSubmittedContinue => isZh
      ? '附件已提交到 Hub 文档解析，解析完成后可以继续告诉我如何处理。'
      : 'Attachment submitted to Hub document parse. When done, tell me what to do next.';
  String get voiceUnavailable => isZh
      ? '语音输入不可用，请检查麦克风权限。'
      : 'Voice input unavailable. Check microphone permission.';
  String get taskSubmitFailed => isZh
      ? '任务提交失败，请检查登录状态或网络连接。'
      : 'Task submit failed. Check sign-in or network.';
  String get taskRunningAck => isZh
      ? '已收到，我正在处理这项任务。完成后会把结果发回这里。'
      : 'Got it — working on it. Results will appear here.';
  String get taskFailedDefault => isZh ? '任务执行失败。' : 'Task failed.';
  String get onlineViaHub =>
      isZh ? '在线 · 通过所属 Hub 接入' : 'Online · via Hub';
  String get offlineCannotSubmit =>
      isZh ? '离线 · 暂不可提交任务' : 'Offline · cannot submit';
  String employeeGreeting(String name) => isZh
      ? '你好，我是$name。告诉我需要处理的服务器、电脑或资料任务，我会通过所属 Hub 执行并把结果带回来。'
      : 'Hi, I am $name. Tell me the server, desktop, or document work — I will run it via Hub and bring results back.';
  String get remoteStillUnclaimed =>
      isZh ? '远程仍未领取' : 'Not claimed yet';
  String get taskProcessing => isZh ? '任务处理中' : 'Task in progress';
  String get queuedStuckHint => isZh
      ? '请确认：① 桌面 MaClaw GUI 已登录同一 Hub 账号并在线；② 数字员工已在桌面启用。任务由桌面领取后才会有结果（与 GUI 内聊天通道不同）。'
      : 'Confirm: (1) Desktop MaClaw GUI is signed into the same Hub and online; (2) the digital employee is enabled on desktop. Results appear only after the desktop claims the task (different from in-GUI chat).';
  String get taskSubmittedWaitingClaim => isZh
      ? '任务已提交，等待桌面端数字员工领取。'
      : 'Task submitted; waiting for the desktop employee to claim it.';
  String get taskStillProcessingRemote =>
      isZh ? '任务仍在远程处理中' : 'Task still processing remotely';
  String get waitingRemoteClaimShort =>
      isZh ? '等待远程' : 'waiting remote';

  String get employeeTemplateStatus => isZh
      ? '请检查远程电脑/服务器当前运行状态，列出异常、风险和建议操作。'
      : 'Check remote PC/server status; list anomalies, risks, and suggested actions.';
  String get employeeTemplateLogs => isZh
      ? '请查看最近的服务错误日志，整理可能原因和下一步排查命令。'
      : 'Review recent service error logs; summarize causes and next diagnostic commands.';
  String get employeeTemplateResources => isZh
      ? '请检查磁盘、内存、CPU、网络连接状态，并给出应急处理建议。'
      : 'Check disk, memory, CPU, and network; give emergency recommendations.';
  String get employeeTemplateFiles => isZh
      ? '请帮我在远程电脑上整理指定目录/文件的关键信息，并返回摘要。'
      : 'Summarize key info for a specified remote directory/files.';

  String accessPolicyLabel(String policy) {
    return switch (policy.toLowerCase()) {
      'public' => isZh ? '公开可用' : 'Public',
      'private' => isZh ? '私有授权' : 'Private',
      'per_request' => isZh ? '按次授权' : 'Per request',
      'owner_confirm' => isZh ? '需拥有者确认' : 'Owner confirm',
      _ => isZh ? '策略：$policy' : 'Policy: $policy',
    };
  }

  String residencyLabel(bool resident) => resident
      ? (isZh ? '常驻远程端' : 'Always-on remote')
      : (isZh ? '按需唤起' : 'On demand');

  String runtimeLabel({required bool online, required bool runtimeMissing}) {
    if (runtimeMissing) {
      return isZh ? '远程运行时缺失' : 'Remote runtime missing';
    }
    return online
        ? (isZh ? '远程端在线' : 'Remote online')
        : (isZh ? '远程端离线' : 'Remote offline');
  }

  String employeeTaskStatusLabel(String status) {
    return switch (status.trim().toLowerCase()) {
      'queued' => isZh ? '等待远程领取' : 'Waiting for remote claim',
      'claimed' || 'running' || 'in_progress' =>
        isZh ? '远程处理中' : 'Processing remotely',
      'approval_required' ||
      'pending_approval' ||
      'awaiting_approval' ||
      'authorization_required' ||
      'waiting_authorization' =>
        isZh ? '等待远程授权' : 'Awaiting remote approval',
      'approval_denied' ||
      'authorization_denied' ||
      'rejected' =>
        isZh ? '远程授权被拒绝' : 'Remote approval denied',
      'done' || 'completed' => isZh ? '已完成' : 'Done',
      'failed' => isZh ? '失败' : 'Failed',
      _ => status,
    };
  }

  String employeeTaskTypeLabel(String value) {
    return switch (value.trim()) {
      'server_maintenance' => isZh ? '服务器维护' : 'Server maintenance',
      'desktop_assist' => isZh ? '远程电脑' : 'Desktop assist',
      'document_work' => isZh ? '文档处理' : 'Document work',
      'information_check' => isZh ? '信息核查' : 'Information check',
      _ => isZh ? '通用任务' : 'General task',
    };
  }

  String employeeTaskTypeEnumLabel(String wire) => employeeTaskTypeLabel(wire);

  // —— Account ——
  String get accountTitle => isZh ? '我的' : 'Account';
  String get accountSubtitle => isZh
      ? '官方服务绑定、额度、模型/助手联网状态、凭据和本地隐私数据。'
      : 'Official service, quotas, model status, credentials, and privacy.';
  String get requestNotificationPermission =>
      isZh ? '通知权限' : 'Notifications';
  String get privacy => isZh ? '凭据与隐私' : 'Privacy';
  String get speechLanguage => isZh ? '界面与语音语言' : 'UI & speech language';
  String get preferencesLoadFailed =>
      isZh ? '偏好设置加载失败' : 'Failed to load preferences';

  // —— Assistant ——
  String get assistantTitle => isZh ? 'AI助手' : 'AI assistant';
  String get assistantSubtitle => isZh
      ? '像桌面端一样，随时聊聊、一起处理事情'
      : 'Chat and get work done, like on desktop';
  String get assistantReplying => isZh ? '助手正在回答…' : 'Assistant is typing…';
  String get assistantAnswer => isZh ? '助手回答' : 'Answer';
  String get shareResult => isZh ? '分享结果' : 'Share';
  String get copyResult => isZh ? '复制结果' : 'Copy';
  String get canContinue => isZh ? '可以继续' : 'Next steps';
  String get canContinueDesc => isZh
      ? '把回答落到草稿、文档，或交给数字员工跟进。'
      : 'Save as a draft, open Docs, or hand off to an employee.';
  String get makeDraft => isZh ? '整理为草稿' : 'Save as draft';
  String get assignEmployee => isZh ? '派给员工' : 'Assign employee';
  String get openDocuments => isZh ? '打开文档' : 'Open Docs';
  String get saySomething => isZh ? '说点什么…' : 'Say something…';
  String get mainChat => isZh ? '主对话' : 'Main chat';
  String get openedDocuments => isZh ? '已打开文档' : 'Opened Docs';
  String get handedToEmployee => isZh ? '已交接给数字员工' : 'Handed off to employee';
  String get recallInputHistory => isZh ? '召回历史输入' : 'Recall past input';
  String get recallInputTitle => isZh ? '历史输入' : 'Past inputs';
  String get recallInputEmpty => isZh
      ? '还没有可召回的输入。发送过的问题会出现在这里，点一下即可填入输入框（不会自动发送）。'
      : 'No past inputs yet. After you send messages, tap one here to fill the box (won’t auto-send).';
  String get recallInputHint => isZh ? '搜索历史输入…' : 'Search past inputs…';
  String get recallInputFill => isZh ? '填入' : 'Use';
  String get recallOlder => isZh ? '更早一条' : 'Older';
  String get recallNewer => isZh ? '更新一条' : 'Newer';
  String get recallExit => isZh ? '退出召回' : 'Exit recall';
  String recallPosition(int position, int total) => isZh
      ? '历史 $position / $total'
      : 'History $position / $total';

  // —— Shared intents ——
  String get sharedFileReceived => isZh ? '已接收分享文件' : 'Shared file received';
  String get sharedContentReceived =>
      isZh ? '已接收分享内容' : 'Shared content received';

  // —— Login extras ——
  String get missingHubUrl =>
      isZh ? '缺少 Hub 地址，请重新发送验证码后再试。' : 'Missing Hub URL. Resend the code.';
  String get codeNotConfirmed =>
      isZh ? '验证码尚未确认，请重试。' : 'Code not confirmed. Try again.';
  String verifyFailed(String detail) =>
      isZh ? '验证码验证失败：$detail' : 'Verification failed: $detail';
  String sendCodeFailed(String detail) =>
      isZh ? '验证码发送失败：$detail' : 'Failed to send code: $detail';
  String sendUnconfirmed(String detail) => isZh
      ? '发送未确认（$detail）。若手机已收到验证码，请直接输入；否则请稍后重试。'
      : 'Send unconfirmed ($detail). Enter the code if received, or retry later.';
  String get codeEntryHelper => isZh
      ? '发送回执未确认：收到短信即可在此输入'
      : 'Delivery unconfirmed: enter the SMS code if you received it';
  String codeSentWithTtl(String ttl) => isZh
      ? '验证码已发送，请在$ttl输入短信验证码。'
      : 'Code sent. Enter the SMS code$ttl.';
  String get networkTimeoutMaybeSent =>
      isZh ? '网络超时（短信可能已发出）' : 'Network timeout (SMS may have been sent)';
  String get cannotConnectOfficial =>
      isZh ? '无法连接官方服务' : 'Cannot reach official service';
  String get unknownError => isZh ? '未知错误' : 'Unknown error';
}

final appStringsProvider = Provider<AppStrings>((ref) {
  final prefs =
      ref.watch(appPreferencesProvider).valueOrNull ?? const AppPreferences();
  final ui = resolveAppUiLanguage(preferenceLanguage: prefs.language);
  return AppStrings.forLanguage(ui);
});

extension AppStringsContext on BuildContext {
  /// Prefer [appStringsProvider] in Consumer widgets; this falls back to Chinese.
  AppStrings get s {
    // InheritedWidget-style access without requiring Riverpod at call site.
    final inherited = AppStringsScope.maybeOf(this);
    return inherited ?? const AppStrings._(true);
  }
}

/// Injects [AppStrings] below [MaterialApp] for BuildContext access.
class AppStringsScope extends InheritedWidget {
  final AppStrings strings;

  const AppStringsScope({
    super.key,
    required this.strings,
    required super.child,
  });

  static AppStrings? maybeOf(BuildContext context) {
    return context
        .dependOnInheritedWidgetOfExactType<AppStringsScope>()
        ?.strings;
  }

  static AppStrings of(BuildContext context) {
    return maybeOf(context) ?? const AppStrings._(true);
  }

  @override
  bool updateShouldNotify(AppStringsScope oldWidget) {
    return oldWidget.strings.isZh != strings.isZh;
  }
}
