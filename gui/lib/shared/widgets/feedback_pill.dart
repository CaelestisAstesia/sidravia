import 'package:flutter/material.dart';

/// Dismisses only this presentation; session failures retain their own state.
class FeedbackPill extends StatefulWidget {
  const FeedbackPill({super.key, required this.message});
  final String message;
  @override
  State<FeedbackPill> createState() => _FeedbackPillState();
}

class _FeedbackPillState extends State<FeedbackPill> {
  bool _dismissed = false;
  @override
  void didUpdateWidget(FeedbackPill oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.message != widget.message) _dismissed = false;
  }

  @override
  Widget build(BuildContext context) {
    if (_dismissed) return const SizedBox.shrink();
    final colors = Theme.of(context).colorScheme;
    final message = widget.message
        .replaceAll('。', '')
        .replaceAll(RegExp(r'\.+$'), '');
    return Align(
      alignment: Alignment.center,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: colors.surfaceContainerHighest,
          border: Border.all(color: colors.outlineVariant),
          borderRadius: BorderRadius.circular(18),
        ),
        child: Padding(
          padding: const EdgeInsets.only(left: 12),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Flexible(
                child: Text(
                  message,
                  style: TextStyle(
                    fontSize: 11,
                    height: 1.5,
                    color: colors.onSurfaceVariant,
                  ),
                ),
              ),
              IconButton(
                tooltip: '关闭提示',
                onPressed: () => setState(() => _dismissed = true),
                icon: const Icon(Icons.close, size: 14),
                visualDensity: VisualDensity.compact,
                constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
