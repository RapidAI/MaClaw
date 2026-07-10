import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';

void main() {
  test('parses mobile AI assistant answer with citation snippets', () {
    final answer = SearchAnswer.fromJson({
      'answer': 'Check the latest incident notes.',
      'llm_mode': 'official',
      'llm_request_id': 'llm-mobile-1',
      'llm_usage_record_id': 'llm-mobile-1',
      'citations': [
        {
          'title': 'Incident notes',
          'url': 'https://example.test/incident',
          'snippet': 'Service recovered after restart.',
        },
      ],
    });

    expect(answer.answer, 'Check the latest incident notes.');
    expect(answer.llmMode, 'official');
    expect(answer.llmRequestId, 'llm-mobile-1');
    expect(answer.llmUsageRecordId, 'llm-mobile-1');
    expect(answer.hasLlmTrace, isTrue);
    expect(answer.citations, hasLength(1));
    expect(answer.citations.first.title, 'Incident notes');
    expect(answer.citations.first.url, 'https://example.test/incident');
    expect(answer.citations.first.snippet, 'Service recovered after restart.');
  });

  test('parses mobile document upload task', () {
    final task = MobileDocumentUploadTask.fromJson({
      'task_id': 'mobparse_1',
      'filename': 'report.pdf',
      'status': 'queued',
      'draft_id': 'mobdoc_1',
      'message': 'waiting',
      'draft': {
        'id': 'mobdoc_1',
        'title': 'report',
        'template': 'report',
        'markdown': '# report',
        'updated_at': '2026-07-01T00:00:00Z',
      },
    });

    expect(task.taskId, 'mobparse_1');
    expect(task.filename, 'report.pdf');
    expect(task.status, 'queued');
    expect(task.draftId, 'mobdoc_1');
    expect(task.message, 'waiting');
    expect(task.draft?.id, 'mobdoc_1');
  });

  test('parses mobile ssh analysis', () {
    final analysis = MobileSSHAnalysis.fromJson({
      'summary': 'nginx failed',
      'recommendation': 'check config',
      'command_draft': 'nginx -t',
    });

    expect(analysis.summary, 'nginx failed');
    expect(analysis.recommendation, 'check config');
    expect(analysis.commandDraft, 'nginx -t');
  });

  test('parses mobile digital employee task', () {
    final task = MobileDigitalEmployeeTask.fromJson({
      'task_id': 'mobve_1',
      'employee_id': 'ops',
      'prompt': 'check disk',
      'task_type': 'server_maintenance',
      'context': {'source': 'maclaw_mobile'},
      'status': 'queued',
      'result': 'waiting',
      'message': 'remote worker accepted',
      'claimed_by': 'machine_1',
    });

    expect(task.taskId, 'mobve_1');
    expect(task.employeeId, 'ops');
    expect(task.prompt, 'check disk');
    expect(task.taskType, 'server_maintenance');
    expect(task.context['source'], 'maclaw_mobile');
    expect(task.status, 'queued');
    expect(task.result, 'waiting');
    expect(task.message, 'remote worker accepted');
    expect(task.claimedBy, 'machine_1');
  });
}
