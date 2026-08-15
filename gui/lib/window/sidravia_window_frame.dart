import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class SidraviaWindowCommands {
  static const channel = MethodChannel('sidravia/window');

  static Future<void> minimize() => channel.invokeMethod<void>('minimize');

  static Future<void> close() => channel.invokeMethod<void>('close');
}

class SidraviaWindowFrame extends StatelessWidget {
  const SidraviaWindowFrame({super.key, required this.child, this.platform});

  static const titleBarHeight = 40.0;
  static const controlWidth = 46.0;

  final Widget child;
  final TargetPlatform? platform;

  @override
  Widget build(BuildContext context) {
    if ((platform ?? defaultTargetPlatform) != TargetPlatform.windows) {
      return child;
    }

    final theme = Theme.of(context);
    return ColoredBox(
      color: theme.scaffoldBackgroundColor,
      child: Column(
        children: [
          SizedBox(
            key: const ValueKey<String>('windows-title-bar'),
            height: titleBarHeight,
            child: Row(
              children: [
                const Expanded(
                  child: SizedBox.expand(
                    key: ValueKey<String>('windows-drag-region'),
                  ),
                ),
                _WindowControlButton(
                  key: const ValueKey<String>('window-minimize'),
                  tooltip: '最小化',
                  icon: Icons.remove,
                  onPressed: SidraviaWindowCommands.minimize,
                ),
                _WindowControlButton(
                  key: const ValueKey<String>('window-close'),
                  tooltip: '关闭',
                  icon: Icons.close,
                  danger: true,
                  onPressed: SidraviaWindowCommands.close,
                ),
              ],
            ),
          ),
          Expanded(child: child),
        ],
      ),
    );
  }
}

class _WindowControlButton extends StatelessWidget {
  const _WindowControlButton({
    super.key,
    required this.tooltip,
    required this.icon,
    required this.onPressed,
    this.danger = false,
  });

  final String tooltip;
  final IconData icon;
  final Future<void> Function() onPressed;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return SizedBox(
      width: SidraviaWindowFrame.controlWidth,
      height: SidraviaWindowFrame.titleBarHeight,
      child: IconButton(
        tooltip: tooltip,
        onPressed: onPressed,
        icon: Icon(icon),
        iconSize: 17,
        color: colorScheme.onSurfaceVariant,
        style: ButtonStyle(
          minimumSize: const WidgetStatePropertyAll(Size.zero),
          padding: const WidgetStatePropertyAll(EdgeInsets.zero),
          shape: const WidgetStatePropertyAll(RoundedRectangleBorder()),
          overlayColor: WidgetStateProperty.resolveWith((states) {
            if (danger && states.contains(WidgetState.hovered)) {
              return colorScheme.error;
            }
            if (danger && states.contains(WidgetState.pressed)) {
              return colorScheme.error.withValues(alpha: 0.88);
            }
            if (states.contains(WidgetState.hovered)) {
              return colorScheme.onSurface.withValues(alpha: 0.08);
            }
            if (states.contains(WidgetState.pressed)) {
              return colorScheme.onSurface.withValues(alpha: 0.12);
            }
            return null;
          }),
          foregroundColor: WidgetStateProperty.resolveWith((states) {
            if (danger &&
                (states.contains(WidgetState.hovered) ||
                    states.contains(WidgetState.pressed))) {
              return colorScheme.onError;
            }
            return colorScheme.onSurfaceVariant;
          }),
        ),
      ),
    );
  }
}
