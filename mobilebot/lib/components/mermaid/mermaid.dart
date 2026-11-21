/// Shared Mermaid renderer for mobilebot chat surfaces.
///
/// Import this library from message bodies. The WebView is an implementation
/// detail; source decisions and the offline document live beside it so tests
/// can run without a platform view.
library;

export 'mermaid_diagram.dart';
export 'mermaid_document.dart';
export 'mermaid_markdown.dart';
export 'mermaid_source.dart';
