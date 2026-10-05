import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class SidraviaWindowCommands {
  static const channel = MethodChannel('sidravia/window');
  static Future<void> minimize() => channel.invokeMethod<void>('minimize');
  static Future<void> beginDrag() => channel.invokeMethod<void>('beginDrag');
  static Future<void> toggleMaximize() =>
      channel.invokeMethod<void>('toggleMaximize');
  static Future<void> close() => channel.invokeMethod<void>('close');
  static Future<Map<Object?, Object?>> diagnostics() async =>
      await channel.invokeMapMethod<Object?, Object?>('diagnostics') ?? {};
}

/// Root Navigator covers the whole window, including the frame, for dialogs.
/// Native hit testing must respect the same modal barrier as Flutter.
class SidraviaWindowObserver extends NavigatorObserver {
  void _sync(Route<dynamic>? route) {
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.windows) {
      unawaited(
        SidraviaWindowCommands.channel
            .invokeMethod<void>('setModalBlocked', route is PopupRoute)
            .catchError((Object _) {}),
      );
    }
  }

  @override
  void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) =>
      _sync(route);
  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) =>
      _sync(previousRoute);
  @override
  void didReplace({Route<dynamic>? newRoute, Route<dynamic>? oldRoute}) =>
      _sync(newRoute);
}

class SidraviaWindowFrame extends StatefulWidget {
  const SidraviaWindowFrame({super.key, required this.child, this.platform});
  final Widget child;
  final TargetPlatform? platform;
  @override
  State<SidraviaWindowFrame> createState() => _SidraviaWindowFrameState();
}

class _SidraviaWindowFrameState extends State<SidraviaWindowFrame> {
  bool _maximized = false, _hovered = false, _pressed = false;
  String? _error;
  final _maximizeTooltip = GlobalKey<TooltipState>();
  Brightness? _brightness;
  bool get _windows =>
      !kIsWeb &&
      (widget.platform ?? defaultTargetPlatform) == TargetPlatform.windows;
  @override
  void initState() {
    super.initState();
    if (_windows) {
      SidraviaWindowCommands.channel.setMethodCallHandler((call) async {
        if (call.method == 'stateChanged') _update(call.arguments);
      });
      unawaited(_configure());
    }
  }

  Future<void> _configure() async {
    try {
      _update(
        await SidraviaWindowCommands.channel.invokeMethod<Object?>(
          'configureFrame',
          true,
        ),
      );
    } on MissingPluginException {
      // Widget-platform simulation has no native HWND; never claim native success.
    } on PlatformException catch (e) {
      if (mounted) setState(() => _error = '窗口初始化失败：${e.message ?? e.code}');
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final brightness = Theme.of(context).brightness;
    if (_windows && _brightness != brightness) {
      _brightness = brightness;
      unawaited(
        SidraviaWindowCommands.channel
            .invokeMethod<void>('setDarkMode', brightness == Brightness.dark)
            .catchError((Object error) {
              if (error is PlatformException && mounted) {
                setState(
                  () => _error = '窗口主题更新失败：${error.message ?? error.code}',
                );
              }
            }),
      );
    }
  }

  void _update(Object? value) {
    if (!mounted || value is! Map) return;
    final wasHovered = _hovered;
    setState(() {
      _maximized = value['maximized'] == true;
      _hovered = value['maximizeHovered'] == true;
      _pressed = value['maximizePressed'] == true;
    });
    if (_hovered && !wasHovered) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _hovered) {
          _maximizeTooltip.currentState?.ensureTooltipVisible();
        }
      });
    } else if (!_hovered && wasHovered) {
      Tooltip.dismissAllToolTips();
    }
  }

  Future<void> _command(Future<void> Function() action) async {
    try {
      await action();
      if (mounted) setState(() => _error = null);
    } on PlatformException catch (e) {
      if (mounted) setState(() => _error = '窗口操作失败：${e.message ?? e.code}');
    } on MissingPluginException {
      if (mounted) setState(() => _error = '当前环境没有 Windows 窗口控制');
    }
  }

  @override
  void dispose() {
    if (_windows) SidraviaWindowCommands.channel.setMethodCallHandler(null);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_windows) return widget.child;
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: Theme.of(context).scaffoldBackgroundColor,
      child: Column(
        children: [
          SizedBox(
            key: const ValueKey('window-top'),
            height: 46,
            child: Stack(
              children: [
                // Native HTCAPTION owns this blank area, including double-click.
                Positioned(
                  top: 10,
                  right: 10,
                  child: Row(
                    children: [
                      _control(
                        '最小化',
                        Icons.remove,
                        SidraviaWindowCommands.minimize,
                      ),
                      const SizedBox(width: 2),
                      _control(
                        _maximized ? '还原' : '最大化',
                        _maximized ? Icons.filter_none : Icons.crop_square,
                        SidraviaWindowCommands.toggleMaximize,
                        tooltipKey: _maximizeTooltip,
                        hovered: _hovered,
                        pressed: _pressed,
                      ),
                      const SizedBox(width: 2),
                      _control(
                        '关闭',
                        Icons.close,
                        SidraviaWindowCommands.close,
                        close: true,
                      ),
                    ],
                  ),
                ),
                if (_error != null)
                  Positioned(
                    left: 8,
                    top: 4,
                    right: 128,
                    child: Text(
                      _error!,
                      maxLines: 2,
                      style: TextStyle(fontSize: 10, color: scheme.error),
                    ),
                  ),
              ],
            ),
          ),
          Expanded(child: widget.child),
        ],
      ),
    );
  }

  Widget _control(
    String label,
    IconData icon,
    Future<void> Function() command, {
    GlobalKey<TooltipState>? tooltipKey,
    bool close = false,
    bool hovered = false,
    bool pressed = false,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return TextButton(
      onPressed: () => _command(command),
      style: ButtonStyle(
        visualDensity: VisualDensity.standard,
        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
        padding: const WidgetStatePropertyAll(EdgeInsets.zero),
        minimumSize: const WidgetStatePropertyAll(Size(36, 32)),
        maximumSize: const WidgetStatePropertyAll(Size(36, 32)),
        shape: WidgetStatePropertyAll(
          RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
        ),
        side: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.focused)
              ? BorderSide(color: scheme.primary)
              : BorderSide.none,
        ),
        foregroundColor: WidgetStateProperty.resolveWith(
          (states) =>
              close &&
                  (states.contains(WidgetState.hovered) ||
                      states.contains(WidgetState.pressed))
              ? scheme.error
              : (hovered ||
                        pressed ||
                        states.contains(WidgetState.hovered) ||
                        states.contains(WidgetState.focused)
                    ? scheme.onSurface
                    : scheme.onSurfaceVariant),
        ),
        backgroundColor: WidgetStateProperty.resolveWith((states) {
          final active =
              hovered ||
              pressed ||
              states.contains(WidgetState.hovered) ||
              states.contains(WidgetState.pressed) ||
              states.contains(WidgetState.focused);
          if (!active) return Colors.transparent;
          return close
              ? scheme.error.withValues(alpha: .16)
              : (pressed || states.contains(WidgetState.pressed)
                    ? Color.alphaBlend(
                        scheme.onSurface.withValues(alpha: .08),
                        scheme.surfaceContainerHighest,
                      )
                    : scheme.surfaceContainerHighest);
        }),
      ),
      child: Tooltip(
        key: tooltipKey,
        message: label,
        child: Semantics(
          label: label,
          child: Builder(
            builder: (buttonContext) => CustomPaint(
              size: const Size(17, 17),
              painter: _WindowGlyph(
                icon,
                IconTheme.of(buttonContext).color ?? scheme.onSurfaceVariant,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

// Original HTML SVG paths, using its 24-unit viewBox and 17px control icon.
class _WindowGlyph extends CustomPainter {
  const _WindowGlyph(this.icon, this.color);
  final IconData icon;
  final Color color;
  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / 24, size.height / 24);
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.75
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round;
    if (icon == Icons.remove) {
      canvas.drawLine(const Offset(6, 12.5), const Offset(18, 12.5), paint);
    } else if (icon == Icons.close) {
      canvas.drawLine(const Offset(7.5, 7.5), const Offset(16.5, 16.5), paint);
      canvas.drawLine(const Offset(16.5, 7.5), const Offset(7.5, 16.5), paint);
    } else if (icon == Icons.filter_none) {
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          const Rect.fromLTWH(8, 5, 11, 11),
          const Radius.circular(.6),
        ),
        paint,
      );
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          const Rect.fromLTWH(5, 8, 11, 11),
          const Radius.circular(.6),
        ),
        paint,
      );
    } else {
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          const Rect.fromLTWH(6.5, 6.5, 11, 11),
          const Radius.circular(.6),
        ),
        paint,
      );
    }
  }

  @override
  bool shouldRepaint(_WindowGlyph oldDelegate) =>
      oldDelegate.icon != icon || oldDelegate.color != color;
}
