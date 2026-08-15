import 'package:flutter/material.dart';

abstract final class SidraviaLayout {
  static const compactContentBreakpoint = 600.0;

  static bool isCompactWidth(double width) => width < compactContentBreakpoint;

  static EdgeInsets pagePadding({required bool compact}) => compact
      ? const EdgeInsets.fromLTRB(16, 18, 16, 24)
      : const EdgeInsets.fromLTRB(24, 48, 24, 48);

  static double cardPadding({required bool compact}) => compact ? 16 : 24;
}
