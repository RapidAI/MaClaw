import 'dart:collection';

/// In-process record of one step on the bot send path.
///
/// The phone has no adb log the person can open. These lines stay in memory
/// so a failed bubble can copy a diagnosis without a debugger or a file.
class SendTraceEvent {
  final DateTime at;
  final String stage;
  final String messageId;
  final String botId;
  final String detail;
  final bool reachedServer;

  const SendTraceEvent({
    required this.at,
    required this.stage,
    this.messageId = '',
    this.botId = '',
    this.detail = '',
    this.reachedServer = false,
  });

  String get line {
    final stamp = at.toUtc().toIso8601String();
    final id = messageId.isEmpty ? '-' : messageId;
    final bot = botId.isEmpty ? '-' : botId;
    final extra = detail.isEmpty ? '' : ' $detail';
    final wire = reachedServer ? ' reached' : '';
    return '$stamp $stage id=$id bot=$bot$wire$extra';
  }
}

/// Ring buffer of send-path steps. Process-local: it does not touch disk,
/// logcat, or adb. [text] is the copyable view.
class SendTrace {
  SendTrace._();

  static const int maxEvents = 80;
  static final SendTrace instance = SendTrace._();

  final Queue<SendTraceEvent> _events = Queue<SendTraceEvent>();

  void add(SendTraceEvent event) {
    _events.addLast(event);
    while (_events.length > maxEvents) {
      _events.removeFirst();
    }
  }

  void note({
    required String stage,
    String messageId = '',
    String botId = '',
    String detail = '',
    bool reachedServer = false,
    DateTime? at,
  }) {
    add(
      SendTraceEvent(
        at: at ?? DateTime.now(),
        stage: stage,
        messageId: messageId,
        botId: botId,
        detail: detail,
        reachedServer: reachedServer,
      ),
    );
  }

  List<SendTraceEvent> get events => List.unmodifiable(_events);

  /// Copyable diagnosis. Empty when nothing has been recorded yet.
  /// [messageId] keeps only that send, newest last.
  String text({String messageId = ''}) {
    final wanted = messageId.trim();
    final lines = <String>[];
    for (final event in _events) {
      if (wanted.isNotEmpty && event.messageId != wanted) continue;
      lines.add(event.line);
    }
    return lines.join('\n');
  }

  void clear() => _events.clear();
}
