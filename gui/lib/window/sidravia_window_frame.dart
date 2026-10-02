import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class SidraviaWindowCommands {
  static const channel = MethodChannel('sidravia/window');

  static Future<void> minimize() => channel.invokeMethod<void>('minimize');

  static Future<void> close() => channel.invokeMethod<void>('close');
}

class SidraviaWindowFrame extends StatelessWidget {
  const SidraviaWindowFrame({super.key, required this.child, this.platform});

  final Widget child;
  final TargetPlatform? platform;

  @override
  Widget build(BuildContext context) {
    // Windows uses the native caption and non-client frame. This keeps drag,
    // resize, snap, and accessibility behavior owned by the operating system.
    return child;
  }
}
