import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/shared/theme/design_metrics.dart';

import 'design_groups.dart' show shortUiText;

class DesignSwitch extends StatelessWidget {
  const DesignSwitch({
    super.key,
    required this.value,
    required this.onChanged,
    required this.label,
  });
  final bool value;
  final ValueChanged<bool>? onChanged;
  final String label;
  @override
  Widget build(BuildContext context) {
    final platform = Theme.of(context).platform;
    final touch = DesignMetrics.isTouchPlatform(platform);
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      label: label,
      toggled: value,
      enabled: onChanged != null,
      child: InkWell(
        onTap: onChanged == null ? null : () => onChanged!(!value),
        borderRadius: BorderRadius.circular(22),
        child: SizedBox(
          width: touch ? 48 : 38,
          height: touch ? 48 : 22,
          child: Center(
            child: Opacity(
              opacity: onChanged == null ? .6 : 1,
              child: Container(
                width: 38,
                height: 22,
                decoration: BoxDecoration(
                  color: value ? scheme.primary : scheme.outline,
                  borderRadius: BorderRadius.circular(22),
                ),
                child: Align(
                  alignment: value
                      ? Alignment.centerRight
                      : Alignment.centerLeft,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 3),
                    child: Container(
                      width: 16,
                      height: 16,
                      decoration: const BoxDecoration(
                        color: Colors.white,
                        shape: BoxShape.circle,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class DesignInputRow extends StatelessWidget {
  const DesignInputRow({
    super.key,
    required this.label,
    required this.controller,
    required this.enabled,
    this.obscureText = false,
    this.hint,
    this.error,
    this.helper,
    this.trailing,
  });
  final String label;
  final TextEditingController controller;
  final bool enabled, obscureText;
  final String? hint, error, helper;
  final Widget? trailing;

  bool _rejectMultiline(BuildContext context, String text) {
    if (!text.contains('\n') && !text.contains('\r')) return false;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('输入内容包含换行，已拒绝本次输入；输入框内容未更改。')));
    return true;
  }

  Future<void> _pasteCredential(BuildContext context) async {
    ClipboardData? clipboard;
    try {
      clipboard = await Clipboard.getData(Clipboard.kTextPlain);
    } on PlatformException {
      if (context.mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('无法读取剪贴板，输入框内容未更改。')));
      }
      return;
    }
    if (!context.mounted || !enabled || clipboard?.text == null) return;
    final pasted = clipboard!.text!;
    // Flutter's single-line formatter would silently join lines. Reject the
    // whole paste before it reaches that formatter, for keyboard and toolbar.
    if (_rejectMultiline(context, pasted)) return;
    final value = controller.value;
    final selection = value.selection;
    final start = selection.isValid ? selection.start : value.text.length;
    final end = selection.isValid ? selection.end : value.text.length;
    controller.value = TextEditingValue(
      text: value.text.replaceRange(start, end, pasted),
      selection: TextSelection.collapsed(offset: start + pasted.length),
    );
  }

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: DesignMetrics.rowPadding),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ConstrainedBox(
          constraints: const BoxConstraints(
            minHeight: DesignMetrics.rowMinHeight,
          ),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      label,
                      style: const TextStyle(
                        fontSize: DesignMetrics.bodyFont,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    if (helper != null && error == null) ...[
                      const SizedBox(height: DesignMetrics.subtitleGap),
                      Text(
                        shortUiText(helper!),
                        style: TextStyle(
                          fontSize: DesignMetrics.captionFont,
                          height: 1.35,
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 16),
              Expanded(
                child: Row(
                  children: [
                    Expanded(
                      child: MergeSemantics(
                        child: Semantics(
                          onPaste: enabled
                              ? () => _pasteCredential(context)
                              : null,
                          onSetText: enabled
                              ? (text) {
                                  if (_rejectMultiline(context, text)) return;
                                  controller.value = TextEditingValue(
                                    text: text,
                                    selection: TextSelection.collapsed(
                                      offset: text.length,
                                    ),
                                  );
                                }
                              : null,
                          child: Actions(
                            actions: {
                              PasteTextIntent: CallbackAction<PasteTextIntent>(
                                onInvoke: (_) {
                                  _pasteCredential(context);
                                  return null;
                                },
                              ),
                            },
                            child: TextField(
                              controller: controller,
                              enabled: enabled,
                              obscureText: obscureText,
                              autocorrect: false,
                              enableSuggestions: false,
                              smartDashesType: SmartDashesType.disabled,
                              smartQuotesType: SmartQuotesType.disabled,
                              contextMenuBuilder: (context, state) =>
                                  AdaptiveTextSelectionToolbar.buttonItems(
                                    anchors: state.contextMenuAnchors,
                                    buttonItems: [
                                      for (final item
                                          in state.contextMenuButtonItems)
                                        if (item.type ==
                                            ContextMenuButtonType.paste)
                                          ContextMenuButtonItem(
                                            type: ContextMenuButtonType.paste,
                                            onPressed: () {
                                              ContextMenuController.removeAny();
                                              _pasteCredential(context);
                                            },
                                          )
                                        else
                                          item,
                                    ],
                                  ),
                              textAlign: TextAlign.right,
                              style: const TextStyle(
                                fontSize: DesignMetrics.valueFont,
                                fontFamily: 'HarmonyOS Sans',
                              ),
                              textAlignVertical: TextAlignVertical.center,
                              decoration: InputDecoration(
                                hintText: hint,
                                hintStyle: TextStyle(
                                  fontSize: DesignMetrics.valueFont,
                                  color: Theme.of(context)
                                      .colorScheme
                                      .onSurfaceVariant,
                                ),
                                isDense: true,
                                filled: false,
                                border: InputBorder.none,
                                enabledBorder: InputBorder.none,
                                focusedBorder: InputBorder.none,
                                disabledBorder: InputBorder.none,
                                contentPadding: const EdgeInsets.symmetric(
                                  vertical: 12,
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
                    ?trailing,
                  ],
                ),
              ),
            ],
          ),
        ),
        if (error != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Text(
              shortUiText(error!),
              style: TextStyle(
                color: Theme.of(context).colorScheme.error,
                fontSize: DesignMetrics.valueFont,
              ),
            ),
          ),
      ],
    ),
  );
}
