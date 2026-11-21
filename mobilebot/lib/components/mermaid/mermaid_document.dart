/// Offline HTML shell that renders one Mermaid source inside a WebView.
///
/// The page never fetches a network. The widget serves this shell and the
/// bundled `assets/mermaid/mermaid.min.js` from a loopback HTTP server.
/// Android WebView blocks a `file://` page from loading a sibling script.
library;

import 'mermaid_source.dart';

/// Flutter asset key of the page the WebView loads.
const String mermaidShellAsset = 'assets/mermaid/mermaid_shell.html';

/// Flutter asset key of the bundled Mermaid 11.14.0 browser build.
const String mermaidScriptAsset = 'assets/mermaid/mermaid.min.js';

/// Channel the page uses to report render success, failure, and height.
const String mermaidJavaScriptChannel = 'MermaidHost';

/// Builds the one-shot render call. [source] must already be sanitised.
///
/// The source is passed as a JSON string so quotes, newlines, and `</script>`
/// cannot break out of the script element. `securityLevel: "strict"` asks
/// Mermaid to sanitise labels and disables click handlers — chat text is
/// untrusted model output.
String mermaidRenderScript({
  required String source,
  required bool dark,
  String fontFamily = 'Inter, sans-serif',
}) {
  final payload = _jsString(source);
  final theme = dark ? 'dark' : 'default';
  final font = _jsString(fontFamily);
  return '''
mermaid.initialize({
  startOnLoad: false,
  securityLevel: "strict",
  theme: "$theme",
  fontFamily: $font,
  suppressErrorRendering: true
});
mermaid.render("mmd", $payload).then(function (result) {
  var host = document.getElementById("stage");
  host.innerHTML = result.svg;
  var svg = host.querySelector("svg");
  var height = svg ? Math.ceil(svg.getBoundingClientRect().height) : 0;
  MermaidHost.postMessage(JSON.stringify({ok: true, height: height}));
}).catch(function (error) {
  var message = (error && error.message) ? String(error.message) : String(error);
  MermaidHost.postMessage(JSON.stringify({ok: false, error: message}));
});
''';
}

/// Page posted back by [mermaidJavaScriptChannel].
class MermaidHostEvent {
  const MermaidHostEvent.ok(this.height) : error = null;

  const MermaidHostEvent.failed(this.error) : height = 0;

  final int height;
  final String? error;

  bool get ok => error == null;

  static MermaidHostEvent? tryParse(String raw) {
    final ok = RegExp(r'"ok"\s*:\s*(true|false)').firstMatch(raw);
    if (ok == null) return null;
    if (ok.group(1) == 'true') {
      final height = RegExp(r'"height"\s*:\s*(\d+)').firstMatch(raw);
      return MermaidHostEvent.ok(int.tryParse(height?.group(1) ?? '') ?? 0);
    }
    final error = RegExp(r'"error"\s*:\s*"((?:\\.|[^"\\])*)"').firstMatch(raw);
    final text = error == null ? 'render failed' : _unescapeJson(error.group(1)!);
    return MermaidHostEvent.failed(text);
  }
}

String _unescapeJson(String value) {
  return value
      .replaceAll(r'\"', '"')
      .replaceAll(r'\\', r'\')
      .replaceAll(r'\n', '\n')
      .replaceAll(r'\r', '\r')
      .replaceAll(r'\t', '\t');
}

String _jsString(String value) {
  final buffer = StringBuffer('"');
  for (final unit in value.codeUnits) {
    switch (unit) {
      case 0x22:
        buffer.write(r'\"');
      case 0x5C:
        buffer.write(r'\\');
      case 0x0A:
        buffer.write(r'\n');
      case 0x0D:
        buffer.write(r'\r');
      case 0x09:
        buffer.write(r'\t');
      case 0x3C:
        buffer.write(r'\u003C');
      case 0x3E:
        buffer.write(r'\u003E');
      case 0x26:
        buffer.write(r'\u0026');
      default:
        if (unit < 0x20) {
          buffer.write('\\u${unit.toRadixString(16).padLeft(4, '0')}');
        } else {
          buffer.writeCharCode(unit);
        }
    }
  }
  buffer.write('"');
  return buffer.toString();
}

/// Marker in [buildMermaidShellHtml] replaced with the bundled engine.
const String mermaidScriptMarker = '<script src="mermaid.min.js"></script>';

/// Inlines [engineSource] so the page has no network and no file-URL fetch.
///
/// `</` inside the minified bundle is escaped so a string in the engine cannot
/// close the surrounding script element.
String composeMermaidPage({
  required String shellHtml,
  required String engineSource,
}) {
  if (!shellHtml.contains(mermaidScriptMarker)) {
    throw StateError('mermaid shell is missing the bundled script marker');
  }
  final safeEngine = engineSource.replaceAll('</', r'<\/');
  return shellHtml.replaceFirst(
    mermaidScriptMarker,
    '<script>\n$safeEngine\n</script>',
  );
}

/// HTML shell. The script marker is replaced by [composeMermaidPage].
String buildMermaidShellHtml() {
  return '''
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  html, body { margin: 0; padding: 0; background: transparent; }
  #stage { overflow: auto; }
  #stage svg { max-width: 100%; height: auto; }
</style>
<script src="mermaid.min.js"></script>
</head>
<body>
<div id="stage"></div>
</body>
</html>
''';
}

/// Convenience used by tests: a source that would be sent to the shell.
String? mermaidRenderScriptForSource(String raw, {bool dark = false}) {
  final decision = prepareMermaidSource(raw);
  if (!decision.accepted) return null;
  return mermaidRenderScript(source: decision.source, dark: dark);
}
