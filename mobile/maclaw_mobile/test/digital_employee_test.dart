import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
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
}
