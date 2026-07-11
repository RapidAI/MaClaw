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
  /// Original uploaded file (source of truth for share / AI).
  final bool hasOriginal;
  final String sourceFilename;
  final String sourceContentType;
  final int sourceSize;
  final String sourceDownloadUrl;

  const DocumentDraft({
    required this.id,
    required this.title,
    required this.template,
    required this.markdown,
    required this.updatedAt,
    this.hasOriginal = false,
    this.sourceFilename = '',
    this.sourceContentType = '',
    this.sourceSize = 0,
    this.sourceDownloadUrl = '',
  });

  factory DocumentDraft.fromJson(Map<String, dynamic> json) {
    final body = (json['markdown'] as String? ?? '').trim();
    final preview = (json['preview'] as String? ?? '').trim();
    final hasOriginal = json['has_original'] == true;
    final sourceSizeRaw = json['source_size'];
    final sourceSize = sourceSizeRaw is int
        ? sourceSizeRaw
        : sourceSizeRaw is num
            ? sourceSizeRaw.toInt()
            : int.tryParse('$sourceSizeRaw') ?? 0;
    return DocumentDraft(
      id: json['id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      template: documentTemplateFromWire(json['template'] as String?),
      // List API may only return preview; full body comes from GET by id.
      markdown: body.isNotEmpty ? body : preview,
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
      hasOriginal: hasOriginal,
      sourceFilename: json['source_filename'] as String? ?? '',
      sourceContentType: json['source_content_type'] as String? ?? '',
      sourceSize: sourceSize,
      sourceDownloadUrl: json['source_download_url'] as String? ?? '',
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'title': title,
      'template': documentTemplateWireValue(template),
      'markdown': markdown,
      'updated_at': updatedAt.toUtc().toIso8601String(),
      'has_original': hasOriginal,
      'source_filename': sourceFilename,
      'source_content_type': sourceContentType,
      'source_size': sourceSize,
      'source_download_url': sourceDownloadUrl,
    };
  }

  DocumentDraft copyWith({
    String? title,
    String? markdown,
    DateTime? updatedAt,
    bool? hasOriginal,
    String? sourceFilename,
    String? sourceContentType,
    int? sourceSize,
    String? sourceDownloadUrl,
  }) {
    return DocumentDraft(
      id: id,
      title: title ?? this.title,
      template: template,
      markdown: markdown ?? this.markdown,
      updatedAt: updatedAt ?? this.updatedAt,
      hasOriginal: hasOriginal ?? this.hasOriginal,
      sourceFilename: sourceFilename ?? this.sourceFilename,
      sourceContentType: sourceContentType ?? this.sourceContentType,
      sourceSize: sourceSize ?? this.sourceSize,
      sourceDownloadUrl: sourceDownloadUrl ?? this.sourceDownloadUrl,
    );
  }

  /// Whether the attached original is a raster image we can thumbnail.
  bool get isImageOriginal {
    if (!hasOriginal && sourceDownloadUrl.trim().isEmpty) return false;
    final mime = sourceContentType.trim().toLowerCase();
    if (mime.startsWith('image/')) return true;
    final name = sourceFilename.trim().toLowerCase();
    return name.endsWith('.png') ||
        name.endsWith('.jpg') ||
        name.endsWith('.jpeg') ||
        name.endsWith('.webp') ||
        name.endsWith('.gif') ||
        name.endsWith('.bmp');
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
