import 'package:flutter/material.dart';

import '../../shared/theme.dart';
import 'bot.dart';

/// Create or rename a bot.
///
/// Name and description are the only fields Hub stores, so this sheet is
/// deliberately just those two: the limit is enforced here rather than by a
/// server error the person has to decode.
class BotCreateSheet extends StatefulWidget {
  final Bot? existing;

  const BotCreateSheet({super.key, this.existing});

  static const int maxNameLength = 40;
  static const int maxDescriptionLength = 200;

  /// Validate and trim a bot name. Returns the error text, or null when valid.
  static String? validateName(String raw) {
    final name = raw.trim();
    if (name.isEmpty) return '请填写 Bot 名称';
    if (name.length > maxNameLength) return '名称不能超过 $maxNameLength 个字符';
    return null;
  }

  static String? validateDescription(String raw) {
    if (raw.trim().length > maxDescriptionLength) {
      return '描述不能超过 $maxDescriptionLength 个字符';
    }
    return null;
  }

  @override
  State<BotCreateSheet> createState() => _BotCreateSheetState();
}

class _BotCreateSheetState extends State<BotCreateSheet> {
  late final TextEditingController _name =
      TextEditingController(text: widget.existing?.name ?? '');
  late final TextEditingController _description =
      TextEditingController(text: widget.existing?.description ?? '');
  String? _error;
  bool _busy = false;

  bool get _isRename => widget.existing != null;

  @override
  void dispose() {
    _name.dispose();
    _description.dispose();
    super.dispose();
  }

  /// Close the sheet with the entered values.
  ///
  /// The sheet's only job is validation and input, so the caller performs the
  /// save once this returns. Nothing is awaited here: `Navigator.pop` returns
  /// void and the caller decides what a failed save means.
  void _submit() {
    final nameError = BotCreateSheet.validateName(_name.text);
    final descriptionError =
        BotCreateSheet.validateDescription(_description.text);
    if (nameError != null || descriptionError != null) {
      setState(() => _error = nameError ?? descriptionError);
      return;
    }
    setState(() {
      _error = null;
      _busy = true;
    });
    Navigator.of(context).pop((_name.text.trim(), _description.text.trim()));
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        left: MaClawColors.spaceXl,
        right: MaClawColors.spaceXl,
        top: MaClawColors.spaceLg,
        bottom: MediaQuery.of(context).viewInsets.bottom + MaClawColors.spaceLg,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            _isRename ? '编辑 Bot' : '新建 Bot',
            style: Theme.of(context)
                .textTheme
                .titleLarge
                ?.copyWith(fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: MaClawColors.spaceLg),
          TextField(
            controller: _name,
            autofocus: true,
            enabled: !_busy,
            maxLength: BotCreateSheet.maxNameLength,
            decoration: const InputDecoration(
              labelText: '名称',
              hintText: '例如：日报助手',
              border: OutlineInputBorder(),
            ),
            onSubmitted: (_) => _submit(),
          ),
          const SizedBox(height: MaClawColors.spaceMd),
          TextField(
            controller: _description,
            enabled: !_busy,
            minLines: 2,
            maxLines: 4,
            maxLength: BotCreateSheet.maxDescriptionLength,
            decoration: const InputDecoration(
              labelText: '描述（可选）',
              hintText: '这个 Bot 负责什么',
              border: OutlineInputBorder(),
            ),
          ),
          if (_error != null) ...[
            const SizedBox(height: MaClawColors.spaceSm),
            Text(
              _error!,
              style: Theme.of(context)
                  .textTheme
                  .bodySmall
                  ?.copyWith(color: Theme.of(context).colorScheme.error),
            ),
          ],
          const SizedBox(height: MaClawColors.spaceLg),
          FilledButton(
            onPressed: _busy ? null : _submit,
            child: Text(_isRename ? '保存' : '创建'),
          ),
        ],
      ),
    );
  }
}
