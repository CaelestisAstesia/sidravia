import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

String displayedSnapshotText(WidgetTester tester) {
  final text = tester.widget<SelectableText>(
    find.byKey(const ValueKey('runtime-snapshot-text')),
  );
  return text.data ?? text.textSpan!.toPlainText();
}

Map<String, dynamic> displayedSnapshot(WidgetTester tester) =>
    jsonDecode(displayedSnapshotText(tester)) as Map<String, dynamic>;
