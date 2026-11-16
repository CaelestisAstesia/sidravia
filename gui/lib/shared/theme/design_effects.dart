import 'package:flutter/material.dart';

/// Shared low-elevation edge shadow for groups and outlined controls.
abstract final class DesignEffects {
  static List<BoxShadow> subtleShadow(ColorScheme colors) => [
    BoxShadow(
      color: colors.shadow.withValues(alpha: .06),
      blurRadius: 4,
      offset: const Offset(0, 1),
    ),
  ];
}
