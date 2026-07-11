import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';

void main() {
  test('MobileAgentMcpConfig parses public server list', () {
    final config = MobileAgentMcpConfig.fromJson({
      'mcp_servers': [
        {
          'id': 'alpha',
          'name': 'Alpha',
          'endpoint_url': 'https://mcp.example/v1',
          'auth_type': 'bearer',
          'has_auth_secret': true,
        },
      ],
      'local_mcp_allowed': false,
    });
    expect(config.localMcpAllowed, isFalse);
    expect(config.servers, hasLength(1));
    expect(config.servers.first.id, 'alpha');
    expect(config.servers.first.hasAuthSecret, isTrue);
    expect(config.servers.first.toJson()['auth_secret'], isNull);
  });

  test('MobileMcpServer toJson omits empty secret', () {
    const server = MobileMcpServer(
      id: 'a',
      name: 'A',
      endpointUrl: 'https://x',
      authType: 'none',
    );
    expect(server.toJson().containsKey('auth_secret'), isFalse);
    final withSecret = server.copyWith(authSecret: 'tok');
    expect(withSecret.toJson()['auth_secret'], 'tok');
  });

  test('MobileAgentKnowledgeStatus parses stats', () {
    final status = MobileAgentKnowledgeStatus.fromJson({
      'available': true,
      'sources': 3,
      'cards': 10,
      'facts': 2,
      'mode': 'fts',
      'message': 'ok',
    });
    expect(status.available, isTrue);
    expect(status.sources, 3);
    expect(status.mode, 'fts');
  });

  test('MobileAgentMcpHealth parses probe payload', () {
    final health = MobileAgentMcpHealth.fromJson({
      'server_count': 1,
      'healthy_count': 1,
      'available_tools': 4,
      'probed_at': '2026-07-11T00:00:00Z',
      'servers': [
        {
          'id': 'alpha',
          'name': 'Alpha',
          'health_status': 'healthy',
          'tool_count': 4,
          'running': true,
          'endpoint_url': 'https://mcp.example',
        },
      ],
    });
    expect(health.healthyCount, 1);
    expect(health.servers.single.isHealthy, isTrue);
    expect(health.servers.single.toolCount, 4);
  });

  test('MobileAgentSkillsList parses skills', () {
    final list = MobileAgentSkillsList.fromJson({
      'count': 1,
      'skills': [
        {
          'name': '代码审查',
          'description': 'review',
          'type': 'executable',
          'status': 'active',
          'version': '1.0.0',
          'step_count': 2,
        },
      ],
    });
    expect(list.count, 1);
    expect(list.skills.single.name, '代码审查');
    expect(list.skills.single.stepCount, 2);
  });

  test('MobileAgentKnowledgeIngestResult parses response', () {
    final result = MobileAgentKnowledgeIngestResult.fromJson({
      'ok': true,
      'source_id': 'src_1',
      'title': '备忘',
      'rune_count': 12,
      'mode': 'fts',
    });
    expect(result.ok, isTrue);
    expect(result.sourceId, 'src_1');
    expect(result.runeCount, 12);
  });

  test('MobileJobsList parses unified jobs', () {
    final list = MobileJobsList.fromJson({
      'count': 2,
      'active_count': 1,
      'generated_at': '2026-07-11T00:00:00Z',
      'jobs': [
        {
          'job_id': 'up_1',
          'kind': 'document_upload',
          'title': '导入 · a.pdf',
          'status': 'processing',
          'progress': 0.5,
          'deep_link': '/documents',
        },
        {
          'job_id': 'exp_1',
          'kind': 'document_export',
          'title': '导出 · pdf',
          'status': 'ready',
          'deep_link': '/documents',
        },
      ],
    });
    expect(list.count, 2);
    expect(list.activeCount, 1);
    expect(list.jobs.first.isActive, isTrue);
    expect(list.jobs.last.isActive, isFalse);
    expect(list.jobs.first.kind, 'document_upload');
  });

  test('MobileAgentJob parses async job payload', () {
    final job = MobileAgentJob.fromJson({
      'job_id': 'mobagent_1',
      'kind': 'assistant',
      'query': '长任务',
      'status': 'queued',
      'message': 'queued',
      'deep_link': '/tasks',
    });
    expect(job.jobId, 'mobagent_1');
    expect(job.isActive, isTrue);
    expect(job.isReady, isFalse);

    final nested = MobileAgentJob.fromJson({
      'async': true,
      'job_id': 'mobagent_2',
      'job': {
        'job_id': 'mobagent_2',
        'status': 'ready',
        'answer': '完成了',
      },
    });
    // Prefer top-level job_id/status when present.
    expect(nested.jobId, 'mobagent_2');
  });
}
