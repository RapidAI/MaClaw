import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_screen.dart';

void main() {
  test('parses digital employee status', () {
    final employee = DigitalEmployee.fromJson({
      'id': 've-1',
      'machine_id': 'srv-1',
      'name': 'Server Assistant',
      'skill_description': 'ops',
      'online_status': 'online',
      'access_policy': 'per_request',
      'resident': true,
    });

    expect(employee.id, 've-1');
    expect(employee.online, isTrue);
    expect(employee.canSubmitTask, isTrue);
    expect(employee.accessPolicy, 'per_request');
    expect(employee.accessPolicyLabel, '按次授权');
    expect(employee.residencyLabel, '常驻远程端');
    expect(employee.runtimeLabel, '远程端在线');
  });

  test('defaults missing name to readable Chinese label', () {
    final employee = DigitalEmployee.fromJson({});

    expect(employee.name, '数字员工');
    expect(employee.online, isFalse);
  });

  test('filterOnlineDigitalEmployees keeps only online entries', () {
    const online = DigitalEmployee(
      id: 'on',
      machineId: 'm1',
      name: '在线',
      skillDescription: '',
      onlineStatus: 'online',
      accessPolicy: 'public',
      resident: true,
      runtimeMissing: false,
    );
    const offline = DigitalEmployee(
      id: 'off',
      machineId: 'm2',
      name: '离线',
      skillDescription: '',
      onlineStatus: 'offline',
      accessPolicy: 'public',
      resident: false,
      runtimeMissing: false,
    );
    final filtered = filterOnlineDigitalEmployees([online, offline]);
    expect(filtered.map((e) => e.id), ['on']);
  });

  test('blocks mobile task submission when remote runtime is missing', () {
    final employee = DigitalEmployee.fromJson({
      'online_status': 'online',
      'access_policy': 'private',
      'runtime_missing': true,
    });

    expect(employee.online, isTrue);
    expect(employee.canSubmitTask, isFalse);
    expect(employee.accessPolicyLabel, '私有授权');
    expect(employee.runtimeLabel, '远程运行时缺失');
  });

  test('formats digital employee task result as emergency document markdown',
      () {
    const task = MobileDigitalEmployeeTask(
      taskId: 'task-1',
      employeeId: 'employee-1',
      prompt: '检查远程服务器状态',
      status: 'done',
      result: '服务正常',
      message: '远程巡检已完成',
      claimedBy: 'srv-1',
    );

    expect(
      digitalEmployeeTaskDocumentMarkdown(task),
      contains('# 数字员工任务结果'),
    );
    expect(digitalEmployeeTaskDocumentMarkdown(task), contains('## 任务'));
    expect(digitalEmployeeTaskDocumentMarkdown(task), contains('检查远程服务器状态'));
    expect(digitalEmployeeTaskDocumentMarkdown(task), contains('## 领取者'));
    expect(digitalEmployeeTaskDocumentMarkdown(task), contains('srv-1'));
    expect(digitalEmployeeTaskDocumentMarkdown(task), contains('服务正常'));
  });

  test('labels digital employee authorization states for mobile users', () {
    expect(digitalEmployeeTaskStatusLabel('approval_required'), '等待远程授权');
    expect(digitalEmployeeTaskStatusLabel('pending_approval'), '等待远程授权');
    expect(digitalEmployeeTaskStatusLabel('authorization_denied'), '远程授权被拒绝');
    expect(
      digitalEmployeeTaskAwaitingAuthorization('awaiting_approval'),
      isTrue,
    );
    expect(digitalEmployeeTaskAwaitingAuthorization('done'), isFalse);
  });

  test('prefers live result text for digital employee progress preview', () {
    expect(
      digitalEmployeeTaskProgressPreview(
        result: '远程数字员工正在处理手机任务。',
        message: '远程数字员工已领取任务，正在处理。',
      ),
      isEmpty,
    );
    expect(
      digitalEmployeeTaskProgressPreview(
        result: '磁盘使用率 42%，建议清理 /var/log',
        message: '生成中',
      ),
      contains('磁盘使用率'),
    );
    expect(
      digitalEmployeeTaskProgressPreview(
        result: '',
        message: '正在汇总巡检结论…',
      ),
      contains('巡检结论'),
    );
    expect(digitalEmployeeTaskIsRunning('in_progress'), isTrue);
    expect(digitalEmployeeTaskIsRunning('queued'), isTrue);
    expect(digitalEmployeeTaskIsRunning('done'), isFalse);
  });
  test('builds structured mobile emergency prompts for digital employees', () {
    final prompt = buildDigitalEmployeeMobilePrompt(
      type: DigitalEmployeeMobileTaskType.serverMaintenance,
      prompt: '检查 nginx 502，给我可复制的排查命令。',
    );

    expect(prompt, contains('MaClaw Mobile 应急任务'));
    expect(prompt, contains('任务类型：服务器维护'));
    expect(prompt, contains('适合手机快速阅读'));
    expect(prompt, contains('风险、验证方式和回滚方式'));
    expect(prompt, contains('高风险命令只生成命令草案'));
    expect(prompt, contains('检查 nginx 502'));
  });

  test('digital employee mobile task context carries Hub and safety boundary',
      () {
    const employee = DigitalEmployee(
      id: 'employee-1',
      machineId: 'srv-1',
      name: '服务器助手',
      skillDescription: '远程服务器巡检、日志分析和应急处理。',
      onlineStatus: 'online',
      accessPolicy: 'per_request',
      resident: true,
      runtimeMissing: false,
    );
    const bootstrap = MobileBootstrap(
      user: MobileUser(
        userId: 'u-phone',
        email: 'phone:19900001111',
        phoneNumber: '19900001111',
        creditsAccount: 'phone:19900001111',
        tenantId: 'tenant-user',
      ),
      services: MobileServices(
        hubStatus: 'online',
        llmStatus: 'available',
        searchStatus: 'available',
        documentsStatus: 'available',
        digitalEmployeesStatus: 'available',
        llmStatusPath: '/api/llm/status',
        modelsPath: '/api/llm/models',
        searchPath: '/api/mobile/search',
        documentsPath: '/api/mobile/documents',
        digitalEmployeesPath: '/api/mobile/digital-employees',
        realtimePath: '/api/mobile/realtime',
      ),
      connection: MobileConnection(
        hubCenterCandidates: [
          'https://hubs.mypapers.top',
          'https://hubs.maclaw.top',
          'https://hubs2.maclaw.top',
        ],
        selectedHubCenterUrl: 'https://hubs.maclaw.top',
        hubUrl: 'https://tenant-a.maclaw.top',
        hubId: 'hub-a',
        tenantId: 'tenant-a',
      ),
      llmAccess: MobileLlmAccess(
        mode: 'maclaw_official',
        status: 'available',
        authorizationId: '',
        authorizedBy: '',
        creditsAccount: 'phone:19900001111',
        authorizedAt: null,
      ),
      features: MobileFeatures(
        search: true,
        documents: true,
        backendSshSessions: true,
        digitalEmployees: true,
        pushNotifications: false,
      ),
      limits: MobileLimits(maxUploadBytes: 1024, maxExportJobs: 2),
    );
    const draft = DigitalEmployeeMobileTaskDraft(
      prompt: '检查 nginx 502，给我可复制的排查命令。',
      type: DigitalEmployeeMobileTaskType.serverMaintenance,
      requireManualConfirmation: true,
    );

    final context = draft.contextFor(
      employee,
      hubUrl: 'https://tenant-a.maclaw.top',
      bootstrap: bootstrap,
    );

    expect(draft.taskTypeWireValue, 'server_maintenance');
    expect(context['source'], 'maclaw_mobile');
    expect(context['handoff'], 'mobile_emergency');
    expect(context['task_type_label'], '服务器维护');
    expect(context['hub_url'], 'https://tenant-a.maclaw.top');
    expect(context['discovered_hub_url'], 'https://tenant-a.maclaw.top');
    expect(context['selected_hubcenter_url'], 'https://hubs.maclaw.top');
    expect(context['tenant_id'], 'tenant-a');
    expect(context['credits_account'], 'phone:19900001111');
    expect(context['manual_confirmation_required'], 'true');
    expect(
      context['execution_boundary'],
      'draft_only_until_mobile_user_confirms',
    );
    expect(
      context['manual_confirmation_scope'],
      'destructive_or_high_risk_server_desktop_operations',
    );
  });
}
