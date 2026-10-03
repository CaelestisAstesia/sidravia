import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class SidraviaWindowCommands {
  static const channel = MethodChannel('sidravia/window');

  static Future<void> minimize() => channel.invokeMethod<void>('minimize');

  static Future<void> beginDrag() => channel.invokeMethod<void>('beginDrag');

  static Future<void> toggleMaximize() =>
      channel.invokeMethod<void>('toggleMaximize');

  static Future<void> close() => channel.invokeMethod<void>('close');
}

class SidraviaWindowFrame extends StatelessWidget {
  const SidraviaWindowFrame({super.key, required this.child, this.platform});

  final Widget child;
  final TargetPlatform? platform;

  @override
  Widget build(BuildContext context) {
    // Keep the caption and non-client frame native on Windows so the system
    // owns moving, resizing, snapping, and accessibility behavior.
    return child;
  }
}
