import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import 'tray_popover_contract.dart';

export 'tray_popover_contract.dart';

const double trayPopoverWidth = 210;

class TrayPopover extends StatelessWidget {
  const TrayPopover({
    super.key,
    required this.data,
    required this.onAction,
    this.onDismiss,
    this.now,
  });

  final TrayPopoverData data;
  final ValueChanged<TrayIntent> onAction;
  final VoidCallback? onDismiss;
  final DateTime? now;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final tone = switch (data.tone) {
      HomeTone.success =>
        Theme.of(context).brightness == Brightness.dark
            ? AppColors.successDark
            : AppColors.success,
      HomeTone.warning => scheme.tertiary,
      HomeTone.error => scheme.error,
      HomeTone.idle => scheme.onSurfaceVariant,
    };
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.escape): () =>
            onDismiss?.call(),
      },
      child: Focus(
        autofocus: true,
        child: Material(
          color: scheme.surface,
          child: Container(
            width: trayPopoverWidth,
            padding: const EdgeInsets.all(6),
            decoration: BoxDecoration(
              color: Color.lerp(scheme.surface, Colors.transparent, .03),
              border: Border.all(color: scheme.outline),
              borderRadius: BorderRadius.circular(11),
              boxShadow: const [
                BoxShadow(
                  color: Color(0x2E101C28),
                  blurRadius: 28,
                  offset: Offset(0, 12),
                ),
              ],
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                _StatusCard(data: data, tone: tone, now: now ?? DateTime.now()),
                const _Separator(),
                _MenuButton(
                  label: data.primaryLabel,
                  primary: true,
                  enabled: data.primaryEnabled,
                  tooltip: data.primaryEnabled ? null : data.disabledReason,
                  onPressed: () => onAction(data.primaryAction),
                ),
                if (data.secondaryLabel != null && data.secondaryAction != null)
                  _MenuButton(
                    label: data.secondaryLabel!,
                    enabled: data.secondaryEnabled,
                    tooltip: data.secondaryEnabled ? null : data.disabledReason,
                    onPressed: () => onAction(data.secondaryAction!),
                  ),
                const _Separator(),
                _MenuButton(
                  label: '显示主界面',
                  trailing: '双击图标',
                  onPressed: () =>
                      onAction(const TrayIntent(TrayIntentKind.restoreMain)),
                ),
                _MenuButton(
                  label: '退出 Sidravia',
                  muted: true,
                  onPressed: () =>
                      onAction(const TrayIntent(TrayIntentKind.exit)),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _StatusCard extends StatelessWidget {
  const _StatusCard({
    required this.data,
    required this.tone,
    required this.now,
  });
  final TrayPopoverData data;
  final DateTime now;
  final Color tone;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(vertical: 7, horizontal: 8),
    decoration: BoxDecoration(
      color: Color.lerp(Theme.of(context).colorScheme.surface, tone, .06),
      border: Border.all(
        color: Color.lerp(
          Theme.of(context).colorScheme.outlineVariant,
          tone,
          .18,
        )!,
      ),
      borderRadius: BorderRadius.circular(8),
    ),
    child: Row(
      children: [
        Container(
          width: 34,
          height: 34,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: Color.lerp(Theme.of(context).colorScheme.surface, tone, .11),
            border: Border.all(
              color: Color.lerp(
                Theme.of(context).colorScheme.outline,
                tone,
                .25,
              )!,
            ),
            borderRadius: BorderRadius.circular(9),
          ),
          child: Text(
            data.glyph,
            style: TextStyle(
              color: tone,
              fontSize: 16,
              fontWeight: FontWeight.w700,
            ),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _EllipsisText(
                data.statusTitle,
                style: const TextStyle(
                  fontSize: 11.5,
                  fontWeight: FontWeight.w700,
                  height: 1.15,
                ),
              ),
              const SizedBox(height: 3),
              _EllipsisText(
                '${data.institution} · ${data.username}',
                style: TextStyle(
                  fontSize: 9.5,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                  height: 1.15,
                ),
              ),
              if (data.statusDetail.isNotEmpty) ...[
                const SizedBox(height: 3),
                Text(
                  data.detailAt(now),
                  maxLines: data.establishedAt == null ? null : 1,
                  softWrap: data.establishedAt == null,
                  overflow: data.establishedAt == null
                      ? TextOverflow.clip
                      : TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 9,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                    height: 1.15,
                  ),
                ),
              ],
              if (data.statusContext.isNotEmpty &&
                  data.statusContext != data.statusDetail) ...[
                const SizedBox(height: 2),
                _EllipsisText(
                  data.statusContext,
                  style: TextStyle(
                    fontSize: 9,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                    height: 1.15,
                  ),
                ),
              ],
            ],
          ),
        ),
      ],
    ),
  );
}

class _EllipsisText extends StatelessWidget {
  const _EllipsisText(this.text, {required this.style});
  final String text;
  final TextStyle style;
  @override
  Widget build(BuildContext context) => Tooltip(
    message: text,
    waitDuration: const Duration(milliseconds: 450),
    child: Text(
      text,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: style,
    ),
  );
}

class _Separator extends StatelessWidget {
  const _Separator();
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 5),
    child: Divider(
      height: 1,
      thickness: 1,
      color: Theme.of(context).colorScheme.outlineVariant,
    ),
  );
}

class _MenuButton extends StatelessWidget {
  const _MenuButton({
    required this.label,
    required this.onPressed,
    this.enabled = true,
    this.primary = false,
    this.muted = false,
    this.trailing,
    this.tooltip,
  });
  final String label;
  final VoidCallback onPressed;
  final bool enabled, primary, muted;
  final String? trailing, tooltip;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final button = SizedBox(
      height: 30,
      width: double.infinity,
      child: TextButton(
        onPressed: enabled ? onPressed : null,
        style: TextButton.styleFrom(
          alignment: Alignment.centerLeft,
          padding: const EdgeInsets.symmetric(horizontal: 8),
          minimumSize: const Size(0, 30),
          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(6)),
          foregroundColor: primary
              ? scheme.primary
              : muted
              ? scheme.onSurfaceVariant
              : scheme.onSurface,
          textStyle: TextStyle(
            fontSize: 11,
            fontWeight: primary ? FontWeight.w600 : FontWeight.w400,
          ),
        ),
        child: Row(
          children: [
            Expanded(
              child: Text(label, maxLines: 1, overflow: TextOverflow.ellipsis),
            ),
            if (trailing != null)
              Text(
                trailing!,
                style: TextStyle(fontSize: 9, color: scheme.onSurfaceVariant),
              ),
          ],
        ),
      ),
    );
    return tooltip == null ? button : Tooltip(message: tooltip!, child: button);
  }
}
