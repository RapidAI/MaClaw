enum DocumentTemplate {
  notice,
  report,
  email,
  proposal,
  meetingMinutes,
  statement,
}

enum DocumentExportFormat { pdf, word, markdown }

class DocumentDraft {
  final String id;
  final String title;
  final DocumentTemplate template;
  final String markdown;
  final DateTime updatedAt;

  const DocumentDraft({
    required this.id,
    required this.title,
    required this.template,
    required this.markdown,
    required this.updatedAt,
  });

  DocumentDraft copyWith({
    String? title,
    String? markdown,
    DateTime? updatedAt,
  }) {
    return DocumentDraft(
      id: id,
      title: title ?? this.title,
      template: template,
      markdown: markdown ?? this.markdown,
      updatedAt: updatedAt ?? this.updatedAt,
    );
  }
}

String documentTemplateLabel(DocumentTemplate template) {
  return switch (template) {
    DocumentTemplate.notice => '通知',
    DocumentTemplate.report => '报告',
    DocumentTemplate.email => '邮件',
    DocumentTemplate.proposal => '方案',
    DocumentTemplate.meetingMinutes => '会议纪要',
    DocumentTemplate.statement => '说明书',
  };
}

