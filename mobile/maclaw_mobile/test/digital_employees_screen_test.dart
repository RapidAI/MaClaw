import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_chat_screen.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_prompt.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_screen.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';

class _TestDigitalEmployeesController extends DigitalEmployeesController {
  @override
  Future<List<DigitalEmployee>> build() async => const [
        DigitalEmployee(
          id: 'employee-1',
          machineId: 'srv-1',
          name: '服务器助手',
          skillDescription: '远程服务器巡检、日志分析和应急处理。',
          onlineStatus: 'online',
          accessPolicy: 'per_request',
          resident: true,
          runtimeMissing: false,
        ),
        DigitalEmployee(
          id: 'employee-2',
          machineId: 'desktop-1',
          name: '办公电脑助手',
          skillDescription: '远程电脑文件整理和桌面任务协助。',
          onlineStatus: 'online',
          accessPolicy: 'private',
          resident: false,
          runtimeMissing: true,
        ),
      ];
}

class _ApprovalDigitalEmployeeTaskController
    extends DigitalEmployeeTaskController {
  @override
  Future<MobileDigitalEmployeeTask?> build() async =>
      const MobileDigitalEmployeeTask(
        taskId: 'task-approval',
        employeeId: 'employee-1',
        prompt: '读取远程电脑上的业务报表',
        status: 'approval_required',
        result: '',
        message: '需要远程电脑拥有者确认访问文件。',
        claimedBy: 'desktop-owner',
      );
}

class _SecretDigitalEmployeeTaskController
    extends DigitalEmployeeTaskController {
  @override
  Future<MobileDigitalEmployeeTask?> build() async =>
      const MobileDigitalEmployeeTask(
        taskId: 'task-secret',
        employeeId: 'employee-1',
        prompt: '检查远程服务器状态 token: prompt-secret',
        status: 'done',
        result: 'service ok\nAuthorization: Bearer remote-secret-token\n'
            'password=prod-password',
        message: '远程巡检已完成 token: message-secret',
        claimedBy: 'srv-1 token=claimed-secret',
      );
}

class _HistoryDigitalEmployeeTaskController
    extends DigitalEmployeeTaskController {
  static final selected = <String>[];

  @override
  Future<MobileDigitalEmployeeTask?> build() async =>
      const MobileDigitalEmployeeTask(
        taskId: 'task-current',
        employeeId: 'employee-1',
        prompt: 'current task',
        taskType: 'server_maintenance',
        status: 'done',
        result: 'current result',
        message: 'current done',
        claimedBy: 'srv-1',
      );

  @override
  Future<void> selectTask(MobileDigitalEmployeeTask task) async {
    selected.add(task.taskId);
    state = AsyncData(task);
  }
}

class _HistoryDigitalEmployeeTaskHistoryController
    extends DigitalEmployeeTaskHistoryController {
  @override
  Future<List<MobileDigitalEmployeeTask>> build() async => const [
        MobileDigitalEmployeeTask(
          taskId: 'task-current',
          employeeId: 'employee-1',
          prompt: 'current task',
          taskType: 'server_maintenance',
          status: 'done',
          result: 'current result',
          message: 'current done',
          claimedBy: 'srv-1',
        ),
        MobileDigitalEmployeeTask(
          taskId: 'task-history',
          employeeId: 'employee-1',
          prompt: 'historical desktop task',
          taskType: 'desktop_assist',
          status: 'failed',
          result: 'desktop offline',
          message: 'cannot reach desktop',
          claimedBy: 'desktop-1',
        ),
      ];
}

class _EmptyDigitalEmployeePromptHistoryController
    extends DigitalEmployeePromptHistoryController {
  @override
  Future<List<DigitalEmployeePromptEntry>> build() async => const [];
}

class _EmptyDigitalEmployeeTaskHistoryController
    extends DigitalEmployeeTaskHistoryController {
  @override
  Future<List<MobileDigitalEmployeeTask>> build() async => const [];
}

class _RecordingDigitalEmployeeTaskController
    extends DigitalEmployeeTaskController {
  static final created = <({
    String employeeId,
    String prompt,
    String taskType,
    Map<String, String> context,
  })>[];

  @override
  Future<MobileDigitalEmployeeTask?> build() async => null;

  @override
  Future<void> createTask({
    required String employeeId,
    required String prompt,
    String taskType = 'general',
    Map<String, String> context = const {},
  }) async {
    created.add(
      (
        employeeId: employeeId,
        prompt: prompt,
        taskType: taskType,
        context: context,
      ),
    );
    state = AsyncData(
      MobileDigitalEmployeeTask(
        taskId: 'task-${created.length}',
        employeeId: employeeId,
        prompt: prompt,
        taskType: taskType,
        context: context,
        status: 'queued',
        result: '',
        message: '等待远程端领取',
        claimedBy: '',
      ),
    );
  }
}

class _RecordingDocumentsController extends DocumentsController {
  static final created =
      <({String title, DocumentTemplate template, String content})>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> createDraft({
    required String title,
    required DocumentTemplate template,
    String content = '',
  }) async {
    created.add((title: title, template: template, content: content));
    state = AsyncData(
      DocumentsState(
        draft: DocumentDraft(
          id: 'draft-${created.length}',
          title: title,
          template: template,
          markdown: content,
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      ),
    );
  }
}

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: _signedInBootstrap.connection.hubUrl,
        bootstrap: _signedInBootstrap,
      );
}

final _signedInBootstrap = MobileBootstrap.fromJson({
  'user': {
    'user_id': 'u-mobile',
    'phone_number': '19900001111',
    'tenant_id': 'tenant-a',
  },
  'connection': {
    'hubcenter_candidates': [
      'https://hubs.mypapers.top',
      'https://hubs.maclaw.top',
      'https://hubs2.maclaw.top',
    ],
    'selected_hubcenter_url': 'https://hubs.maclaw.top',
    'hub': {
      'id': 'hub-a',
      'base_url': 'https://tenant-a.maclaw.top',
    },
    'tenant_id': 'tenant-a',
  },
  'llm_access': {
    'mode': 'maclaw_official',
    'status': 'available',
    'credits_account': 'phone:19900001111',
  },
});

void main() {
  final copiedResults = <String>[];
  final sharedResults = <String>[];

  test('digital employee mobile task types build safe handoff prompts', () {
    expect(
      digitalEmployeeMobileTaskTypeWireValue(
        DigitalEmployeeMobileTaskType.serverMaintenance,
      ),
      'server_maintenance',
    );
    expect(
      digitalEmployeeMobileTaskTypeWireValue(
        DigitalEmployeeMobileTaskType.desktopAssist,
      ),
      'desktop_assist',
    );
    expect(
      digitalEmployeeMobileTaskTypeWireValue(
        DigitalEmployeeMobileTaskType.documentWork,
      ),
      'document_work',
    );
    expect(
      digitalEmployeeMobileTaskTypeWireValue(
        DigitalEmployeeMobileTaskType.informationCheck,
      ),
      'information_check',
    );

    final prompt = buildDigitalEmployeeMobilePrompt(
      type: DigitalEmployeeMobileTaskType.serverMaintenance,
      prompt: '检查远程服务器磁盘和应用日志。',
    );

    expect(prompt, contains('任务类型：服务器维护'));
    expect(prompt, contains('输出适合手机快速阅读'));
    expect(prompt, contains('避免要求长时间盯屏'));
    expect(prompt, contains('目标、风险、验证方式和回滚方式'));
    expect(prompt, contains('高风险命令只生成命令草案'));
  });

  test('digital employee mobile prompt redacts common secrets', () {
    final prompt = buildDigitalEmployeeMobilePrompt(
      type: DigitalEmployeeMobileTaskType.serverMaintenance,
      prompt: 'check outage token=raw-task-token password=raw-task-password\n'
          'Authorization: Bearer raw-task-bearer\n'
          '-----BEGIN PRIVATE KEY-----\nraw-task-key\n'
          '-----END PRIVATE KEY-----',
    );

    expect(prompt, contains('token=[REDACTED_SECRET]'));
    expect(prompt, contains('password=[REDACTED_SECRET]'));
    expect(prompt, contains('Authorization: Bearer [REDACTED_TOKEN]'));
    expect(prompt, contains('[REDACTED_PRIVATE_KEY]'));
    expect(prompt, isNot(contains('raw-task-token')));
    expect(prompt, isNot(contains('raw-task-password')));
    expect(prompt, isNot(contains('raw-task-bearer')));
    expect(prompt, isNot(contains('raw-task-key')));
  });

  test('digital employee mobile task context carries remote policy metadata',
      () {
    const employee = DigitalEmployee(
      id: 'employee-remote',
      machineId: 'desktop-42',
      name: '远程电脑助手',
      skillDescription: '处理电脑上的文件和桌面任务。',
      onlineStatus: 'online',
      accessPolicy: 'owner_confirm',
      resident: false,
      runtimeMissing: true,
    );
    const draft = DigitalEmployeeMobileTaskDraft(
      prompt: '帮我找一下桌面上的报价单。',
      type: DigitalEmployeeMobileTaskType.desktopAssist,
      requireManualConfirmation: true,
    );

    expect(draft.taskTypeWireValue, 'desktop_assist');
    expect(draft.contextFor(employee), {
      'source': 'maclaw_mobile',
      'handoff': 'mobile_emergency',
      'employee_id': 'employee-remote',
      'employee_name': '远程电脑助手',
      'machine_id': 'desktop-42',
      'machine_online_status': 'online',
      'access_policy': 'owner_confirm',
      'access_policy_label': '需拥有者确认',
      'resident': 'false',
      'runtime_missing': 'true',
      'task_type_label': '远程电脑',
      'manual_confirmation_required': 'true',
      'execution_boundary': 'draft_only_until_mobile_user_confirms',
      'manual_confirmation_scope':
          'destructive_or_high_risk_server_desktop_operations',
    });

    expect(
      draft.contextFor(
        employee,
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: _signedInBootstrap,
      ),
      containsPair('tenant_id', 'tenant-a'),
    );
    final sessionContext = draft.contextFor(
      employee,
      hubUrl: 'https://tenant-a.maclaw.top',
      bootstrap: _signedInBootstrap,
    );
    expect(sessionContext['hub_url'], 'https://tenant-a.maclaw.top');
    expect(sessionContext['discovered_hub_url'], 'https://tenant-a.maclaw.top');
    expect(sessionContext['selected_hubcenter_url'], 'https://hubs.maclaw.top');
    expect(sessionContext['llm_access_mode'], 'maclaw_official');
    expect(sessionContext['llm_access_status'], 'available');
    expect(sessionContext['credits_account'], 'phone:19900001111');

    final malformedLlmCreditsContext = draft.contextFor(
      employee,
      bootstrap: MobileBootstrap.fromJson({
        'user': {
          'user_id': 'u-mobile',
          'phone_number': '19900001111',
          'tenant_id': 'tenant-a',
        },
        'llm_access': {
          'mode': 'maclaw_official',
          'status': 'available',
          'credits_account': 'phone:user19900001111',
        },
      }),
    );
    expect(malformedLlmCreditsContext['credits_account'], 'phone:19900001111');

    final untrustedCreditsContext = draft.contextFor(
      employee,
      bootstrap: MobileBootstrap.fromJson({
        'user': {
          'user_id': 'u-mobile',
          'credits_account': 'phone:user19900001111',
          'tenant_id': 'tenant-a',
        },
        'llm_access': {
          'mode': 'maclaw_official',
          'status': 'available',
          'credits_account': 'phone:user19900001111',
        },
      }),
    );
    expect(untrustedCreditsContext, isNot(contains('credits_account')));
  });

  testWidgets('digital employees screen exposes remote task controls',
      (tester) async {
    _RecordingDocumentsController.created.clear();
    copiedResults.clear();
    sharedResults.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          digitalEmployeesProvider.overrideWith(
            _TestDigitalEmployeesController.new,
          ),
          digitalEmployeeTaskProvider.overrideWith(
            _SecretDigitalEmployeeTaskController.new,
          ),
          digitalEmployeePromptHistoryProvider.overrideWith(
            _EmptyDigitalEmployeePromptHistoryController.new,
          ),
          digitalEmployeeTaskHistoryProvider.overrideWith(
            _EmptyDigitalEmployeeTaskHistoryController.new,
          ),
          sessionControllerProvider.overrideWith(
            _SignedInSessionController.new,
          ),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          digitalEmployeeResultClipboardWriterProvider.overrideWithValue(
            (text) async => copiedResults.add(text),
          ),
          digitalEmployeeResultShareProvider.overrideWithValue(
            (text) async => sharedResults.add(text),
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 1),
              ),
            ),
          ),
        ],
        child:
            const MaterialApp(home: Scaffold(body: DigitalEmployeesScreen())),
      ),
    );
    await tester.pump();

    expect(find.text('数字员工'), findsOneWidget);
    expect(find.text('服务器助手'), findsOneWidget);
    expect(find.text('办公电脑助手'), findsOneWidget);
    expect(find.text('按次授权'), findsOneWidget);
    expect(find.text('私有授权'), findsOneWidget);
    expect(find.text('常驻远程端'), findsOneWidget);
    expect(find.text('按需唤起'), findsOneWidget);
    expect(find.text('远程运行时缺失'), findsOneWidget);
    expect(find.text('发起任务'), findsNWidgets(2));
    expect(find.byTooltip('分析日志/输出'), findsNWidgets(2));

    await tester.tap(find.byType(Card).first);
    await tester.pumpAndSettle();
    expect(find.byType(DigitalEmployeeChatScreen), findsOneWidget);
    expect(find.byType(TextField), findsOneWidget);
    expect(find.textContaining('Hub'), findsWidgets);
    expect(find.byTooltip('添加附件'), findsOneWidget);
    Navigator.of(tester.element(find.byType(DigitalEmployeeChatScreen))).pop();
    await tester.pumpAndSettle();

    await tester
        .tap(find.byTooltip('\u5206\u6790\u65e5\u5fd7/\u8f93\u51fa').first);
    await tester.pumpAndSettle();
    expect(
      find.textContaining(
        '\u540e\u53f0\u4f1a\u8bdd\u8f93\u51fa\u548c\u5173\u952e\u65e5\u5fd7',
      ),
      findsOneWidget,
    );
    expect(
      find.textContaining(
        '\u5173\u952e\u65e5\u5fd7\u548c\u7ec8\u7aef\u8f93\u51fa',
      ),
      findsNothing,
    );
    Navigator.of(tester.element(find.byType(DigitalEmployeesScreen))).pop();
    await tester.pumpAndSettle();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('最近任务'), findsOneWidget);
    expect(find.text('状态：已完成'), findsOneWidget);
    expect(find.text('说明：远程巡检已完成 token: message-secret'), findsOneWidget);
    expect(find.text('权限说明'), findsOneWidget);
    expect(find.text('整理为草稿'), findsOneWidget);

    await tester.tap(find.byTooltip('复制结果'));
    await tester.pumpAndSettle();
    expect(copiedResults.single, contains('service ok'));
    expect(
      copiedResults.single,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(copiedResults.single, contains('password=[REDACTED_SECRET]'));
    expect(copiedResults.single, isNot(contains('remote-secret-token')));
    expect(copiedResults.single, isNot(contains('prod-password')));
    expect(find.text('任务结果已复制'), findsOneWidget);

    await tester.tap(find.byTooltip('分享结果'));
    await tester.pump(const Duration(milliseconds: 250));
    expect(sharedResults.single, contains('service ok'));
    expect(
      sharedResults.single,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(sharedResults.single, contains('password=[REDACTED_SECRET]'));
    expect(sharedResults.single, isNot(contains('remote-secret-token')));
    expect(sharedResults.single, isNot(contains('prod-password')));

    await tester.tap(find.text('整理为草稿'));
    await tester.pumpAndSettle();

    expect(_RecordingDocumentsController.created.single.title, '数字员工任务结果');
    expect(
      _RecordingDocumentsController.created.single.template,
      DocumentTemplate.report,
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      contains('检查远程服务器状态'),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      contains('token=[REDACTED_SECRET]'),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      contains('srv-1 token=[REDACTED_SECRET]'),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      isNot(contains('remote-secret-token')),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      isNot(contains('prod-password')),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      isNot(contains('message-secret')),
    );
    expect(
      _RecordingDocumentsController.created.single.content,
      isNot(contains('claimed-secret')),
    );
  });

  testWidgets('digital employees screen explains pending remote authorization',
      (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          digitalEmployeesProvider.overrideWith(
            _TestDigitalEmployeesController.new,
          ),
          digitalEmployeeTaskProvider.overrideWith(
            _ApprovalDigitalEmployeeTaskController.new,
          ),
          digitalEmployeePromptHistoryProvider.overrideWith(
            _EmptyDigitalEmployeePromptHistoryController.new,
          ),
          digitalEmployeeTaskHistoryProvider.overrideWith(
            _EmptyDigitalEmployeeTaskHistoryController.new,
          ),
          sessionControllerProvider.overrideWith(
            _SignedInSessionController.new,
          ),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 1),
              ),
            ),
          ),
        ],
        child:
            const MaterialApp(home: Scaffold(body: DigitalEmployeesScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('状态：等待远程授权'), findsOneWidget);
    expect(find.text('说明：需要远程电脑拥有者确认访问文件。'), findsOneWidget);
    expect(
      find.textContaining('正在等待 desktop-owner 在远程服务器/电脑上确认授权'),
      findsOneWidget,
    );
    expect(find.textContaining('手机端不会绕过远程策略'), findsOneWidget);
  });

  testWidgets('digital employees screen can restore recent task history',
      (tester) async {
    _HistoryDigitalEmployeeTaskController.selected.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          digitalEmployeesProvider.overrideWith(
            _TestDigitalEmployeesController.new,
          ),
          digitalEmployeeTaskProvider.overrideWith(
            _HistoryDigitalEmployeeTaskController.new,
          ),
          digitalEmployeePromptHistoryProvider.overrideWith(
            _EmptyDigitalEmployeePromptHistoryController.new,
          ),
          digitalEmployeeTaskHistoryProvider.overrideWith(
            _HistoryDigitalEmployeeTaskHistoryController.new,
          ),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 1),
              ),
            ),
          ),
        ],
        child:
            const MaterialApp(home: Scaffold(body: DigitalEmployeesScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1200));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('最近数字员工任务'), findsOneWidget);
    expect(find.text('historical desktop task'), findsOneWidget);
    expect(find.text('远程电脑 · 失败'), findsOneWidget);

    await tester.tap(find.text('historical desktop task'));
    await tester.pump();

    expect(_HistoryDigitalEmployeeTaskController.selected, ['task-history']);
  });

  testWidgets('typed digital employee task uses mobile emergency prompt',
      (tester) async {
    _RecordingDigitalEmployeeTaskController.created.clear();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          digitalEmployeesProvider.overrideWith(
            _TestDigitalEmployeesController.new,
          ),
          digitalEmployeeTaskProvider.overrideWith(
            _RecordingDigitalEmployeeTaskController.new,
          ),
          digitalEmployeePromptHistoryProvider.overrideWith(
            _EmptyDigitalEmployeePromptHistoryController.new,
          ),
          digitalEmployeeTaskHistoryProvider.overrideWith(
            _EmptyDigitalEmployeeTaskHistoryController.new,
          ),
          sessionControllerProvider.overrideWith(
            _SignedInSessionController.new,
          ),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 1),
              ),
            ),
          ),
        ],
        child:
            const MaterialApp(home: Scaffold(body: DigitalEmployeesScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('发起任务').first);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('发给 服务器助手'), findsOneWidget);
    await tester.tap(find.text('核查'));
    await tester.pump();
    await tester.enterText(
      find.byType(TextField),
      '请核查这份线上故障说明是否遗漏影响范围。',
    );
    await tester.scrollUntilVisible(
      find.text('提交任务'),
      200,
      scrollable: find.byType(Scrollable).last,
    );
    await tester.pump();
    await tester.tap(find.text('提交任务'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    final created = _RecordingDigitalEmployeeTaskController.created.single;
    expect(created.employeeId, 'employee-1');
    expect(created.taskType, 'information_check');
    expect(created.context['source'], 'maclaw_mobile');
    expect(created.context['machine_id'], 'srv-1');
    expect(created.context['hub_url'], 'https://tenant-a.maclaw.top');
    expect(
      created.context['discovered_hub_url'],
      'https://tenant-a.maclaw.top',
    );
    expect(
      created.context['selected_hubcenter_url'],
      'https://hubs.maclaw.top',
    );
    expect(created.context['tenant_id'], 'tenant-a');
    expect(created.context['llm_access_mode'], 'maclaw_official');
    expect(created.context['credits_account'], 'phone:19900001111');
    expect(
      created.context['execution_boundary'],
      'draft_only_until_mobile_user_confirms',
    );
    expect(
      created.context['manual_confirmation_scope'],
      'destructive_or_high_risk_server_desktop_operations',
    );
    expect(created.prompt, contains('【MaClaw Mobile 应急任务】'));
    expect(created.prompt, contains('任务类型：信息核查'));
    expect(created.prompt, contains('输出适合手机快速阅读'));
    expect(created.prompt, contains('高风险命令只生成命令草案'));
    expect(created.prompt, contains('请核查这份线上故障说明是否遗漏影响范围。'));
  });
}
