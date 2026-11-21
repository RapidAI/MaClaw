import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:webview_flutter/webview_flutter.dart';

import 'mermaid_document.dart';
import 'mermaid_source.dart';

/// What the renderer is about to draw. Tests inspect this instead of a WebView.
class MermaidRenderRequest {
  const MermaidRenderRequest({
    required this.source,
    required this.script,
    required this.dark,
  });

  /// Sanitised diagram text.
  final String source;

  /// JavaScript passed to the bundled engine after the shell loads.
  final String script;

  final bool dark;
}

/// Stand-in for the platform view. Production leaves this null.
typedef MermaidViewBuilder = Widget Function(
  BuildContext context,
  MermaidRenderRequest request,
);

/// Renders one Mermaid source as a diagram.
///
/// The engine is the bundled `assets/mermaid/mermaid.min.js`, served with the
/// shell from a loopback HTTP server. A chat bubble does not need a network.
/// Sources that fail the budget check never reach the WebView. A render error
/// falls back to the original fence text.
///
/// Pass [viewBuilder] in tests to pump the widget without a platform view.
class MermaidDiagram extends StatefulWidget {
  const MermaidDiagram({
    super.key,
    required this.source,
    this.dark,
    this.languageCode,
    this.viewBuilder,
  });

  /// Raw fence body. Sanitised inside the widget.
  final String source;

  /// Overrides [ThemeData.brightness] when the host already knows the mode.
  final bool? dark;

  /// `zh` selects Chinese refusal copy. Defaults to the ambient locale.
  final String? languageCode;

  final MermaidViewBuilder? viewBuilder;

  @override
  State<MermaidDiagram> createState() => _MermaidDiagramState();
}

class _MermaidDiagramState extends State<MermaidDiagram> {
  String? _renderError;
  int _height = 160;

  bool get _dark {
    if (widget.dark != null) return widget.dark!;
    return Theme.of(context).brightness == Brightness.dark;
  }

  String? get _languageCode {
    return widget.languageCode ?? Localizations.localeOf(context).languageCode;
  }

  @override
  void didUpdateWidget(MermaidDiagram oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.source != widget.source || oldWidget.dark != widget.dark) {
      _renderError = null;
    }
  }

  void _onHostEvent(MermaidHostEvent event) {
    if (!mounted) return;
    setState(() {
      if (event.ok) {
        _renderError = null;
        if (event.height > 0) _height = event.height.clamp(48, 1600);
      } else {
        _renderError = event.error;
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final decision = prepareMermaidSource(widget.source);
    if (!decision.accepted) {
      return _Fallback(
        message: mermaidRejectMessage(
          decision.reason!,
          languageCode: _languageCode,
        ),
        source: widget.source,
      );
    }
    final error = _renderError;
    if (error != null) {
      return _Fallback(message: error, source: widget.source);
    }
    final request = MermaidRenderRequest(
      source: decision.source,
      script: mermaidRenderScript(source: decision.source, dark: _dark),
      dark: _dark,
    );
    final custom = widget.viewBuilder;
    if (custom != null) return custom(context, request);
    return SizedBox(
      height: _height.toDouble(),
      width: double.infinity,
      child: _MermaidWebView(
        key: ValueKey('${request.dark}:${request.source}'),
        request: request,
        onEvent: _onHostEvent,
        onScriptError: (message) {
          if (!mounted) return;
          setState(() => _renderError = message);
        },
      ),
    );
  }
}

class _MermaidWebView extends StatefulWidget {
  const _MermaidWebView({
    super.key,
    required this.request,
    required this.onEvent,
    required this.onScriptError,
  });

  final MermaidRenderRequest request;
  final ValueChanged<MermaidHostEvent> onEvent;
  final ValueChanged<String> onScriptError;

  @override
  State<_MermaidWebView> createState() => _MermaidWebViewState();
}

class _MermaidWebViewState extends State<_MermaidWebView> {
  late final WebViewController _controller;

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(const Color(0x00000000))
      ..addJavaScriptChannel(
        mermaidJavaScriptChannel,
        onMessageReceived: (message) {
          final event = MermaidHostEvent.tryParse(message.message);
          if (event != null) widget.onEvent(event);
        },
      )
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageFinished: (_) => unawaited(_render()),
          onWebResourceError: (error) {
            if (error.isForMainFrame != false) {
              widget.onScriptError(error.description);
            }
          },
        ),
      );
    unawaited(_loadOfflinePage());
  }

  /// Serves the bundled shell and engine on loopback, then opens that page.
  ///
  /// Android WebView refuses a relative script from `file://` and from
  /// `file:///android_asset/`. `loadHtmlString` also cannot carry the ~3MB
  /// engine. Loopback HTTP keeps both files local and lets the relative
  /// `<script src="mermaid.min.js">` resolve.
  Future<void> _loadOfflinePage() async {
    try {
      final origin = await _MermaidAssetServer.instance.origin();
      if (!mounted) return;
      await _controller.loadRequest(Uri.parse('$origin/'));
    } catch (error) {
      widget.onScriptError('$error');
    }
  }

  Future<void> _render() async {
    try {
      await _controller.runJavaScript(widget.request.script);
    } catch (error) {
      widget.onScriptError('$error');
    }
  }

  @override
  Widget build(BuildContext context) {
    return WebViewWidget(controller: _controller);
  }
}

class _Fallback extends StatelessWidget {
  const _Fallback({required this.message, required this.source});

  final String message;
  final String source;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            message,
            key: const Key('mermaid-fallback-message'),
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.error,
            ),
          ),
          const SizedBox(height: 4),
          DecoratedBox(
            decoration: BoxDecoration(
              color: theme.colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(6),
            ),
            child: Padding(
              padding: const EdgeInsets.all(8),
              child: SelectableText(
                source.trim(),
                key: const Key('mermaid-fallback-source'),
                style: theme.textTheme.bodySmall?.copyWith(
                  fontFamily: 'monospace',
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}


/// Loopback server for the bundled shell and mermaid.min.js.
///
/// Bound to 127.0.0.1 only. One process-wide instance so chat bubbles do not
/// each copy the 3MB engine or open another port.
class _MermaidAssetServer {
  _MermaidAssetServer._();

  static final _MermaidAssetServer instance = _MermaidAssetServer._();

  HttpServer? _server;
  Uint8List? _engine;
  List<int>? _shell;
  Future<String>? _starting;

  Future<String> origin() {
    final pending = _starting;
    if (pending != null) return pending;
    final next = _start();
    _starting = next;
    return next;
  }

  Future<String> _start() async {
    final existing = _server;
    if (existing != null) {
      return 'http://${existing.address.host}:${existing.port}';
    }
    final engine = await rootBundle.load(mermaidScriptAsset);
    _engine = engine.buffer.asUint8List(
      engine.offsetInBytes,
      engine.lengthInBytes,
    );
    _shell = utf8.encode(buildMermaidShellHtml());
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server = server;
    server.listen(_handle, onError: (_) {});
    return 'http://${server.address.host}:${server.port}';
  }

  void _handle(HttpRequest request) {
    final path = request.uri.path;
    if (path == '/' || path == '/index.html') {
      _send(request, _shell ?? const <int>[], 'text/html; charset=utf-8');
      return;
    }
    if (path == '/mermaid.min.js') {
      final bytes = _engine;
      if (bytes == null) {
        request.response.statusCode = HttpStatus.notFound;
        unawaited(request.response.close());
        return;
      }
      _send(request, bytes, 'text/javascript; charset=utf-8');
      return;
    }
    request.response.statusCode = HttpStatus.notFound;
    unawaited(request.response.close());
  }

  void _send(HttpRequest request, List<int> body, String contentType) {
    final response = request.response;
    response.statusCode = HttpStatus.ok;
    response.headers.contentType = ContentType.parse(contentType);
    response.headers.set(HttpHeaders.cacheControlHeader, 'no-store');
    response.contentLength = body.length;
    response.add(body);
    unawaited(response.close());
  }
}
