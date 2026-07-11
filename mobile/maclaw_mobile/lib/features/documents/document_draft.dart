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

  factory DocumentDraft.fromJson(Map<String, dynamic> json) {
    final body = (json['markdown'] as String? ?? '').trim();
    final preview = (json['preview'] as String? ?? '').trim();
    return DocumentDraft(
      id: json['id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      template: documentTemplateFromWire(json['template'] as String?),
      // List API may only return preview; full body comes from GET by id.
      markdown: body.isNotEmpty ? body : preview,
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'title': title,
      'template': documentTemplateWireValue(template),
      'markdown': markdown,
      'updated_at': updatedAt.toUtc().toIso8601String(),
    };
  }

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

DocumentTemplate documentTemplateFromWire(String? value) {
  return switch ((value ?? '').trim()) {
    'notice' => DocumentTemplate.notice,
    'email' => DocumentTemplate.email,
    'proposal' => DocumentTemplate.proposal,
    'meeting_minutes' => DocumentTemplate.meetingMinutes,
    'statement' => DocumentTemplate.statement,
    _ => DocumentTemplate.report,
  };
}

String documentTemplateWireValue(DocumentTemplate template) {
  return switch (template) {
    DocumentTemplate.notice => 'notice',
    DocumentTemplate.report => 'report',
    DocumentTemplate.email => 'email',
    DocumentTemplate.proposal => 'proposal',
    DocumentTemplate.meetingMinutes => 'meeting_minutes',
    DocumentTemplate.statement => 'statement',
  };
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

class DocumentExportJob {
  final String jobId;
  final String draftId;
  final DocumentExportFormat format;
  final String status;
  final String downloadUrl;
  final String message;
  final DateTime createdAt;

  const DocumentExportJob({
    required this.jobId,
    required this.draftId,
    required this.format,
    required this.status,
    required this.downloadUrl,
    this.message = '',
    required this.createdAt,
  });

  factory DocumentExportJob.fromJson(Map<String, dynamic> json) {
    return DocumentExportJob(
      jobId: json['job_id'] as String? ?? '',
      draftId: json['draft_id'] as String? ?? '',
      format: documentExportFormatFromWire(json['format'] as String?),
      status: json['status'] as String? ?? 'unknown',
      downloadUrl: json['download_url'] as String? ?? '',
      message: json['message'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
    );
  }
}

DocumentExportFormat documentExportFormatFromWire(String? value) {
  return switch ((value ?? '').trim().toLowerCase()) {
    'word' => DocumentExportFormat.word,
    'markdown' => DocumentExportFormat.markdown,
    _ => DocumentExportFormat.pdf,
  };
}

String documentExportFormatWireValue(DocumentExportFormat format) {
  return switch (format) {
    DocumentExportFormat.pdf => 'pdf',
    DocumentExportFormat.word => 'word',
    DocumentExportFormat.markdown => 'markdown',
  };
}
