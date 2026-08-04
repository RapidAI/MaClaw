bool canImportMobileDocumentPath(String path) {
  return path.trim().isNotEmpty;
}

String? validateMobileDocumentImportPath(String path) {
  return path.trim().isEmpty ? '文件路径不能为空。' : null;
}
