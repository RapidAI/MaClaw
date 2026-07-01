import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';

void main() {
  test('labels document templates in Chinese', () {
    expect(documentTemplateLabel(DocumentTemplate.notice), '通知');
    expect(documentTemplateLabel(DocumentTemplate.meetingMinutes), '会议纪要');
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
}

