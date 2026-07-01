import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';

void main() {
  test('labels document templates in Chinese', () {
    expect(documentTemplateLabel(DocumentTemplate.notice), '通知');
    expect(documentTemplateLabel(DocumentTemplate.meetingMinutes), '会议纪要');
  });

  test('parses document draft and export job wire values', () {
    final draft = DocumentDraft.fromJson({
      'id': 'd1',
      'title': '应急通知',
      'template': 'notice',
      'markdown': '# 应急通知',
      'updated_at': '2026-07-01T00:00:00Z',
    });
    expect(draft.template, DocumentTemplate.notice);
    expect(documentTemplateWireValue(draft.template), 'notice');

    final job = DocumentExportJob.fromJson({
      'job_id': 'j1',
      'draft_id': 'd1',
      'format': 'word',
      'status': 'queued',
      'created_at': '2026-07-01T00:00:00Z',
    });
    expect(job.format, DocumentExportFormat.word);
    expect(documentExportFormatWireValue(job.format), 'word');
  });

  test('copyWith preserves template and id', () {
    final draft = DocumentDraft(
      id: 'doc-1',
      title: '旧标题',
      template: DocumentTemplate.report,
      markdown: '# 旧内容',
      updatedAt: DateTime(2026),
    );

    final next = draft.copyWith(title: '新标题');
    expect(next.id, 'doc-1');
    expect(next.template, DocumentTemplate.report);
    expect(next.title, '新标题');
  });

  test('serializes document draft for local cache', () {
    final draft = DocumentDraft(
      id: 'doc-cache',
      title: 'Cache Draft',
      template: DocumentTemplate.email,
      markdown: '# Cache Draft',
      updatedAt: DateTime.utc(2026, 7, 1),
    );

    final json = draft.toJson();
    expect(json['id'], 'doc-cache');
    expect(json['template'], 'email');
    expect(json['updated_at'], '2026-07-01T00:00:00.000Z');
  });
}
