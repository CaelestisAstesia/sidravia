import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class SidraviaWindowCommands {
  static const channel = MethodChannel('sidravia/window');
  static Future<void> minimize() => channel.invokeMethod<void>('minimize');
  static Future<void> close() => channel.invokeMethod<void>('close');
  static Future<Map<Object?, Object?>> diagnostics() async =>
      await channel.invokeMapMethod<Object?, Object?>('diagnostics') ?? {};
}

// The OS owns non-client input even while Flutter presents a modal route.
class SidraviaWindowObserver extends NavigatorObserver {}

class SidraviaWindowFrame extends StatefulWidget {
  const SidraviaWindowFrame({super.key, required this.child, this.platform});
  final Widget child;
  final TargetPlatform? platform;
  @override
  State<SidraviaWindowFrame> createState() => _SidraviaWindowFrameState();
}

class _SidraviaWindowFrameState extends State<SidraviaWindowFrame> {
  Brightness? _brightness;
  String? _error;
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final brightness = Theme.of(context).brightness;
    if (!kIsWeb &&
        (widget.platform ?? defaultTargetPlatform) == TargetPlatform.windows &&
        _brightness != brightness) {
      _brightness = brightness;
      unawaited(_updateTheme(brightness));
    }
  }

  Future<void> _updateTheme(Brightness brightness) async {
    try {
      await SidraviaWindowCommands.channel.invokeMethod<void>(
        'setDarkMode',
        brightness == Brightness.dark,
      );
      if (mounted && _error != null) setState(() => _error = null);
    } on MissingPluginException {
      // Widget platform simulations have no native HWND.
    } on PlatformException catch (error) {
      if (mounted) {
        setState(() => _error = '窗口主题更新失败：${error.message ?? error.code}');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    // Native caption, resize, snap and accessibility stay owned by Windows.
    if (_error == null) return widget.child;
    return Column(
      children: [
        Text(_error!),
        Expanded(child: widget.child),
      ],
    );
  }
}
