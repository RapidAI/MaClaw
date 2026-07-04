const mobileDocumentImportExtensions = [
  'docx',
  'doc',
  'pdf',
  'xlsx',
  'xls',
  'txt',
  'md',
  'markdown',
  'log',
  'csv',
  'json',
  'png',
  'jpg',
  'jpeg',
];

bool canImportMobileDocumentPath(String path) {
  return validateMobileDocumentImportPath(path) == null;
}

String? validateMobileDocumentImportPath(String path) {
  final extension = mobileDocumentExtension(path);
  if (extension == null ||
      !mobileDocumentImportExtensions.contains(extension.toLowerCase())) {
    return '暂不支持该文件类型。请导入 Word、PDF、Excel、图片、Markdown、'
        'CSV、JSON、TXT 或日志文件。';
  }
  return null;
}

String? mobileDocumentExtension(String path) {
  final withoutQuery = path.split('?').first.split('#').first.trim();
  final parts = withoutQuery.split(RegExp(r'[\\/]'));
  final filename = parts.where((part) => part.isNotEmpty).lastOrNull;
  if (filename == null) return null;
  final dot = filename.lastIndexOf('.');
  if (dot <= 0 || dot == filename.length - 1) return null;
  return filename.substring(dot + 1);
}
