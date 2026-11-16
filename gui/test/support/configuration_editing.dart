import 'package:flutter_test/flutter_test.dart';

/// Existing credential contract tests enter the explicit new-password editor.
Future<void> openPasswordEditor(WidgetTester tester) async {
  final change = find.text('更改');
  if (change.evaluate().isNotEmpty) {
    await tester.ensureVisible(change);
    await tester.tap(change);
    await tester.pumpAndSettle();
  }
}
