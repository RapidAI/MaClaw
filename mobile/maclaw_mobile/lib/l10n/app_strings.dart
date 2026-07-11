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
  String get hubLibrary =>
      isZh ? 'Hub 文稿库（与 GUI 共享）' : 'Hub library (shared with GUI)';
  String get hubLibraryEmpty => isZh
      ? '暂无文稿。可从电脑端 MaClaw 创建后刷新，或在下方导入/接收系统分享的文件。'
      : 'No documents yet. Refresh after creating on desktop, or import a shared file below.';
  String get hubLibraryUnavailable =>
      isZh ? '文稿库暂不可用' : 'Library unavailable';
  String get continueProcessing => isZh ? '继续处理' : 'Continue';
  String get summarize => isZh ? '摘要整理' : 'Summarize';
  String get polish => isZh ? '润色' : 'Polish';
  String get export => isZh ? '导出' : 'Export';
  String get shareToWechat => isZh ? '分享到微信等' : 'Share…';
  String get importDocument => isZh ? '导入文档' : 'Import';
  String get refreshLibrary => isZh ? '刷新文稿库' : 'Refresh library';
  String get unnamedDocument => isZh ? '未命名文档' : 'Untitled';
  String get shareOpened =>
      isZh ? '已打开系统分享，可发送到微信等应用' : 'System share sheet opened';
  String get documentPreview => isZh ? '文档预览（只读）' : 'Document preview (read-only)';
  String get documentPreviewHint => isZh
      ? '手机不适合大段改稿。需要改写请到 AI 助手说明意图，或在电脑端 GUI 编辑。'
      : 'Phone is for review. Rewrite via AI assistant, or edit in desktop GUI.';

  // —— Tasks ——
  String get tasksTitle => isZh ? '后台' : 'Background tasks';
  String get tasksSubtitle => isZh
      ? '长任务统一查看：文档解析/导出、员工任务等。短操作请回 AI 助手或数字员工页。'
      : 'Long-running jobs: document import/export, employee tasks, and more.';

  // —— Employees ——
  String get employeesTitle => isZh ? '数字员工' : 'Digital employees';
  String get employeesSubtitle => isZh
      ? '接入远程服务器/电脑上的能力，让手机发起任务、查看结果和请求授权。'
      : 'Run tasks on remote PCs, review results, and request authorization.';

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
