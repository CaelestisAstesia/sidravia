import 'package:flutter/material.dart';

/// Frontend-only appearance owner. No credentials or backend preferences.
class Appearance extends ValueNotifier<ThemeMode> {
  Appearance() : super(ThemeMode.system);
}

class AppearanceScope extends InheritedNotifier<Appearance> {
  const AppearanceScope({
    super.key,
    required Appearance appearance,
    required super.child,
  }) : super(notifier: appearance);
  static Appearance? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppearanceScope>()?.notifier;
}
