import 'package:flutter/material.dart';

abstract final class SidraviaLayout {
  static const compactContentBreakpoint = 600.0;
  static const wideNavigationBreakpoint = 820.0;
  static const compactChromeMaxTextScale = 1.3;
  static const compactContentMaxTextScale = 1.6;
  static const maxContentWidth = 760.0;

  static bool isCompactWidth(double width) => width < compactContentBreakpoint;

  static bool usesWideNavigation(double width) =>
      width >= wideNavigationBreakpoint;

  static EdgeInsets pagePadding({required bool compact}) => compact
      ? const EdgeInsets.fromLTRB(18, 20, 18, 112)
      : const EdgeInsets.fromLTRB(32, 34, 32, 48);

  static double cardPadding({required bool compact}) => compact ? 18 : 26;

  static Widget limitTextScale({
    required bool compact,
    required double maxScaleFactor,
    required Widget child,
  }) => compact
      ? MediaQuery.withClampedTextScaling(
          maxScaleFactor: maxScaleFactor,
          child: child,
        )
      : child;
}
